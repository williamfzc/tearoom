// Package transport carries the core.Store contract over HTTP.
// The server owns routing, strict JSON decoding, and error mapping, never
// room policy. Wait lives here, invocation-scoped: bounded polling against
// the Store, with no cursor, no subscription, and no durable state.
package transport

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/williamfzc/tearoom/internal/core"
)

// actorHeader declares the read/send Actor on the events routes.
const actorHeader = "X-Tearoom-Actor"

// idempotencyHeader is the optional append request header.
const idempotencyHeader = "Idempotency-Key"

// waitCap caps one wait request server-side; callers slice longer waits into
// repeated requests.
const waitCap = 60 * time.Second

// waitInterval is the bounded-polling cadence of a wait request.
const waitInterval = 500 * time.Millisecond

// errMethodNotAllowed distinguishes a route with the wrong method so the
// client sees 405 instead of a generic bad request.
var errMethodNotAllowed = errors.New("method not allowed")

// Server serves one core.Store over HTTP. It holds no state of its own: wait
// is answered by polling the Store within one request.
type Server struct {
	Store core.Store
}

func NewServer(store core.Store) *Server { return &Server{Store: store} }

// Handler routes the contract surface:
//
//	GET  /api/room
//	GET  /api/actors
//	POST /api/actors
//	GET  /api/events?after=42&limit=100&wait=30
//	POST /api/events
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/room", s.wrap(func(r *http.Request) (any, error) {
		if r.Method != http.MethodGet {
			return nil, methodError(r.Method)
		}
		return s.Store.Room(r.Context())
	}))
	mux.HandleFunc("/api/actors", s.wrap(func(r *http.Request) (any, error) {
		switch r.Method {
		case http.MethodGet:
			return s.Store.Actors(r.Context())
		case http.MethodPost:
			return s.joinActor(r)
		default:
			return nil, methodError(r.Method)
		}
	}))
	mux.HandleFunc("/api/events", s.wrap(func(r *http.Request) (any, error) {
		switch r.Method {
		case http.MethodGet:
			return s.readEvents(r)
		case http.MethodPost:
			return s.appendEvent(r)
		default:
			return nil, methodError(r.Method)
		}
	}))
	return mux
}

// joinActor admits the actor named in the JSON body under loopback/OS trust.
func (s *Server) joinActor(r *http.Request) (any, error) {
	var input struct {
		ID string `json:"id"`
	}
	if err := decodeStrict(r, &input); err != nil {
		return nil, fmt.Errorf("invalid join body: %w", err)
	}
	return s.Store.AdmitActor(r.Context(), input.ID)
}

// appendEvent appends one Event. The body carries only to and content; any
// other field — in particular the removed fields of the old wire format — is
// rejected as invalid. The Actor comes from X-Tearoom-Actor and the optional
// idempotency key from Idempotency-Key.
func (s *Server) appendEvent(r *http.Request) (any, error) {
	actor := strings.TrimSpace(r.Header.Get(actorHeader))
	if actor == "" {
		return nil, errors.New(actorHeader + " header is required")
	}
	var input struct {
		To      []string        `json:"to"`
		Content json.RawMessage `json:"content"`
	}
	if err := decodeStrict(r, &input); err != nil {
		return nil, fmt.Errorf("invalid append body: %w", err)
	}
	return s.Store.Append(r.Context(), actor, core.AppendRequest{
		To:             input.To,
		Content:        input.Content,
		IdempotencyKey: strings.TrimSpace(r.Header.Get(idempotencyHeader)),
	})
}

// readEvents serves GET /api/events with an Event array. The wait parameter,
// in seconds, makes the request poll the Store until an event past after
// appears or the wait elapses; an empty array then means "nothing new".
func (s *Server) readEvents(r *http.Request) (any, error) {
	actor := strings.TrimSpace(r.Header.Get(actorHeader))
	if actor == "" {
		return nil, errors.New(actorHeader + " header is required")
	}
	after, err := parseUint(r.URL.Query().Get("after"), "after")
	if err != nil {
		return nil, err
	}
	limit, err := parseNonNegativeInt(r.URL.Query().Get("limit"), "limit")
	if err != nil {
		return nil, err
	}
	wait, err := parseUint(r.URL.Query().Get("wait"), "wait")
	if err != nil {
		return nil, err
	}
	if wait == 0 {
		return s.Store.Read(r.Context(), actor, after, limit)
	}
	budget := time.Duration(wait) * time.Second
	if budget > waitCap {
		budget = waitCap
	}
	deadline := time.Now().Add(budget)
	for {
		events, err := s.Store.Read(r.Context(), actor, after, limit)
		if err != nil {
			return nil, err
		}
		if len(events) > 0 {
			return events, nil
		}
		if remaining := time.Until(deadline); remaining <= 0 {
			return events, nil
		}
		step := waitInterval
		if remaining := time.Until(deadline); remaining < step {
			step = remaining
		}
		select {
		case <-time.After(step):
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	}
}

// decodeStrict decodes exactly one JSON object and rejects unknown fields, so
// removed fields of the old wire format fail loudly instead of being ignored.
func decodeStrict(r *http.Request, out any) error {
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

func parseNonNegativeInt(value, name string) (int, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
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
	case errors.Is(err, core.ErrUnknownActor):
		return http.StatusUnauthorized, wireError{Code: "unknown_actor", Message: err.Error()}
	case errors.Is(err, core.ErrConflict):
		return http.StatusConflict, wireError{Code: "conflict", Message: err.Error()}
	case errors.Is(err, core.ErrNoRoom):
		return http.StatusNotFound, wireError{Code: "no_room", Message: err.Error()}
	case errors.Is(err, core.ErrNotFound):
		return http.StatusNotFound, wireError{Code: "not_found", Message: err.Error()}
	default:
		return http.StatusBadRequest, wireError{Code: "invalid", Message: err.Error()}
	}
}

func (s *Server) wrap(fn func(*http.Request) (any, error)) http.HandlerFunc {
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
