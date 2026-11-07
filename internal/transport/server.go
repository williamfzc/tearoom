// Package transport carries the core.Store contract over HTTP.
// The server owns routing, strict JSON decoding, and error mapping, never
// room policy. Every route is one Store call: reads are pure, so callers
// that need to wait for new records poll from their own boundary. There are
// no authentication paths: the server is the loopback face of one trusted
// workspace.
package transport

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/williamfzc/tearoom/internal/core"
)

// authorHeader declares the append signature on the events route.
const authorHeader = "X-Tearoom-Author"

// idempotencyHeader is the optional append request header.
const idempotencyHeader = "Idempotency-Key"

// errMethodNotAllowed distinguishes a route with the wrong method so the
// client sees 405 instead of a generic bad request.
var errMethodNotAllowed = errors.New("method not allowed")

// Server serves one core.Store over HTTP. It holds no state of its own and
// no authentication paths.
type Server struct {
	Store core.Store
}

func NewServer(store core.Store) *Server { return &Server{Store: store} }

// Handler routes the contract surface:
//
//	POST /api/rooms                       create a room in the tree
//	GET  /api/rooms                       list the rooms
//	GET  /api/events?room=X&after=42      read one room's records
//	POST /api/events                      append one record
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/rooms", s.wrap(func(r *http.Request) (any, error) {
		switch r.Method {
		case http.MethodGet:
			return s.Store.Rooms(r.Context())
		case http.MethodPost:
			return s.createRoom(r)
		default:
			return nil, methodError(r.Method)
		}
	}))
	mux.HandleFunc("/api/events", s.wrap(func(r *http.Request) (any, error) {
		switch r.Method {
		case http.MethodGet:
			return s.readRecords(r)
		case http.MethodPost:
			return s.appendRecord(r)
		default:
			return nil, methodError(r.Method)
		}
	}))
	return mux
}

// createRoom creates one room in the tree. The body carries name and the
// optional parent; an unknown parent is a not-found refusal.
func (s *Server) createRoom(r *http.Request) (any, error) {
	var input struct {
		Name   string `json:"name"`
		Parent string `json:"parent"`
	}
	if err := DecodeStrict(r, &input); err != nil {
		return nil, fmt.Errorf("invalid create-room body: %w", err)
	}
	return s.Store.CreateRoom(r.Context(), input.Name, input.Parent)
}

// appendRecord appends one record. The body carries only room and content;
// any other field is rejected as invalid. The signature rides
// X-Tearoom-Author and the optional idempotency key in Idempotency-Key.
func (s *Server) appendRecord(r *http.Request) (any, error) {
	var input struct {
		Room    string          `json:"room"`
		Content json.RawMessage `json:"content"`
	}
	if err := DecodeStrict(r, &input); err != nil {
		return nil, fmt.Errorf("invalid append body: %w", err)
	}
	return s.Store.Append(r.Context(), input.Room, strings.TrimSpace(r.Header.Get(authorHeader)), core.AppendRequest{
		Content:        input.Content,
		IdempotencyKey: strings.TrimSpace(r.Header.Get(idempotencyHeader)),
	})
}

// readRecords serves GET /api/events with one record array. The read is
// pure: callers truncate or poll from their own boundary.
func (s *Server) readRecords(r *http.Request) (any, error) {
	room := strings.TrimSpace(r.URL.Query().Get("room"))
	if room == "" {
		return nil, errors.New("room is a required query parameter")
	}
	after, err := parseUint(r.URL.Query().Get("after"), "after")
	if err != nil {
		return nil, err
	}
	return s.Store.Read(r.Context(), room, after)
}

// DecodeStrict decodes exactly one JSON object and rejects unknown fields, so
// removed fields of old wire formats fail loudly instead of being ignored.
func DecodeStrict(r *http.Request, out any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("request body must contain exactly one JSON value")
		}
		return err
	}
	return nil
}

func parseUint(value, name string) (uint64, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, errors.New(name + " must be a non-negative integer")
	}
	return parsed, nil
}

func methodError(method string) error {
	return fmt.Errorf("%w: %s", errMethodNotAllowed, method)
}

// errorEnvelope is the unified error body: {"error":{"code":...,"message":...}}.
type errorEnvelope struct {
	Error wireError `json:"error"`
}

// wireError is one entry of the unified error envelope. The code travels over
// the wire so the client can map it back to the store error it describes.
type wireError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// EncodeError maps a store error to the HTTP status and wire code. 409 is
// reserved for idempotency-key conflict only.
func EncodeError(err error) (int, wireError) {
	switch {
	case errors.Is(err, errMethodNotAllowed):
		return http.StatusMethodNotAllowed, wireError{Code: "method_not_allowed", Message: err.Error()}
	case errors.Is(err, core.ErrNotFound):
		return http.StatusNotFound, wireError{Code: "not_found", Message: err.Error()}
	case errors.Is(err, core.ErrNoRoom):
		return http.StatusNotFound, wireError{Code: "no_room", Message: err.Error()}
	case errors.Is(err, core.ErrConflict):
		return http.StatusConflict, wireError{Code: "conflict", Message: err.Error()}
	default:
		return http.StatusBadRequest, wireError{Code: "invalid", Message: err.Error()}
	}
}

func (s *Server) wrap(fn func(*http.Request) (any, error)) http.HandlerFunc {
	return Wrap(fn)
}

// Wrap applies the one response policy: JSON content type, the unified error
// envelope on failure, and one JSON body on success.
func Wrap(fn func(*http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		out, err := fn(r)
		if err != nil {
			status, wire := EncodeError(err)
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(errorEnvelope{Error: wire})
			return
		}
		_ = json.NewEncoder(w).Encode(out)
	}
}
