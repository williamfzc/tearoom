// Package transport carries the core.Store contract over HTTP.
// The server owns routing and error mapping, never room policy.
package transport

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/williamfzc/tearoom/internal/core"
)

type Server struct{ Store core.Store }

func NewServer(store core.Store) *Server { return &Server{Store: store} }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/room", s.wrap(func(r *http.Request) (any, error) {
		return s.Store.Room(r.Context())
	}))
	mux.HandleFunc("/api/actors", s.wrap(func(r *http.Request) (any, error) {
		if r.Method == http.MethodPost {
			var input struct{ ID, Role string }
			if err := decode(r, &input); err != nil {
				return nil, err
			}
			return s.Store.UpsertActor(r.Context(), input.ID, input.Role)
		}
		return s.Store.Actors(r.Context())
	}))
	mux.HandleFunc("/api/events", s.wrap(func(r *http.Request) (any, error) {
		if r.Method == http.MethodPost {
			var input core.AppendInput
			if err := decode(r, &input); err != nil {
				return nil, err
			}
			return s.Store.AppendEvent(r.Context(), input)
		}
		after, err := parseUint(r.URL.Query().Get("after"))
		if err != nil {
			return nil, err
		}
		limit, err := strconv.Atoi(defaultValue(r.URL.Query().Get("limit"), "0"))
		if err != nil || limit < 0 {
			return nil, errorsText("limit must be a non-negative integer")
		}
		return s.Store.Read(r.Context(), r.URL.Query().Get("actor"), after, limit)
	}))
	return mux
}

func (s *Server) wrap(fn func(*http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		out, err := fn(r)
		if err != nil {
			status, wire := EncodeError(err)
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(wire)
			return
		}
		_ = json.NewEncoder(w).Encode(out)
	}
}

func decode(r *http.Request, value any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(value)
}

func parseUint(value string) (uint64, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, errorsText("cursor must be a non-negative integer")
	}
	return parsed, nil
}

func defaultValue(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

type errorsText string

func (e errorsText) Error() string { return string(e) }
