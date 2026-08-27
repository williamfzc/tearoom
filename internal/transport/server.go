// Server side of the transport: it publishes a core.Store as HTTP endpoints and,
// optionally, serves the console assets beside them.
//
// It owns protocol concerns only — routing, decoding, status codes. State assembly
// belongs to resolve, judgement to view, persistence to store; a handler that grew
// any of those would be taking work from a layer that already owns it.
package transport

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/williamfzc/tearoom/internal/core"
	"github.com/williamfzc/tearoom/internal/resolve"
)

// compactEvents is how many trailing events a compact context carries. Five is
// enough to see how a thread got here without paying for its whole history.
const compactEvents = 5

// Server exposes a core.Store over HTTP. It holds a Store rather than a concrete
// file store, so the same handler set works for a local single-tenant daemon and
// for a hosted backend later.
type Server struct {
	Store core.Store
	// Assets serves the web console when non-nil.
	Assets http.Handler
	// Now is injectable so tests get deterministic staleness.
	Now func() time.Time
}

func NewServer(s core.Store) *Server {
	return &Server{Store: s, Now: time.Now}
}

func (s *Server) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/workspace", s.wrap(func(r *http.Request) (any, error) {
		return s.Store.Workspace(r.Context())
	}))

	// /api/board is the whole reason the server exists rather than just the CLI:
	// it runs the shared view package server-side and ships one resolved board, so
	// the console never reimplements "who is stuck" in another language.
	mux.HandleFunc("/api/board", s.wrap(func(r *http.Request) (any, error) {
		return resolve.Board(r.Context(), s.Store, s.now)
	}))

	mux.HandleFunc("/api/agents", s.wrap(func(r *http.Request) (any, error) {
		if r.Method == http.MethodPost {
			var in struct{ ID, Role string }
			if err := decode(r, &in); err != nil {
				return nil, err
			}
			return s.Store.UpsertAgent(r.Context(), in.ID, in.Role)
		}
		return s.Store.Agents(r.Context())
	}))

	mux.HandleFunc("/api/threads", s.wrap(func(r *http.Request) (any, error) {
		if r.Method == http.MethodPost {
			var in struct{ Agent, Title, Body string }
			if err := decode(r, &in); err != nil {
				return nil, err
			}
			return s.Store.CreateThread(r.Context(), in.Agent, in.Title, in.Body)
		}
		return s.Store.Threads(r.Context())
	}))

	mux.HandleFunc("/api/threads/", s.wrap(func(r *http.Request) (any, error) {
		rest := strings.TrimPrefix(r.URL.Path, "/api/threads/")
		if id, ok := cutSuffix(rest, "/events"); ok {
			var in struct {
				Agent, Body string
				Status      core.ThreadStatus
				Evidence    []string
			}
			if err := decode(r, &in); err != nil {
				return nil, err
			}
			return s.Store.AppendEvent(r.Context(), id, in.Agent, in.Body, in.Status, in.Evidence)
		}
		if id, ok := cutSuffix(rest, "/context"); ok {
			return resolve.Compact(r.Context(), s.Store, id, compactEvents, s.now)
		}
		return s.Store.Thread(r.Context(), rest)
	}))

	mux.HandleFunc("/api/leases", s.wrap(func(r *http.Request) (any, error) {
		if r.Method == http.MethodPost {
			var in struct {
				Agent, Resource, Thread, TTL string
			}
			if err := decode(r, &in); err != nil {
				return nil, err
			}
			ttl := core.DefaultLeaseTTL
			if in.TTL != "" {
				if d, err := time.ParseDuration(in.TTL); err == nil {
					ttl = d
				}
			}
			return s.Store.Claim(r.Context(), in.Agent, in.Resource, in.Thread, ttl)
		}
		return s.Store.Leases(r.Context())
	}))

	mux.HandleFunc("/api/leases/release", s.wrap(func(r *http.Request) (any, error) {
		var in struct{ Agent, Resource string }
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		return nil, s.Store.Release(r.Context(), in.Agent, in.Resource)
	}))

	mux.HandleFunc("/api/gates", s.wrap(func(r *http.Request) (any, error) {
		if r.Method == http.MethodPost {
			var in struct{ Agent, Reason, Thread string }
			if err := decode(r, &in); err != nil {
				return nil, err
			}
			return s.Store.CreateGate(r.Context(), in.Agent, in.Reason, in.Thread)
		}
		return s.Store.Gates(r.Context())
	}))

	mux.HandleFunc("/api/gates/", s.wrap(func(r *http.Request) (any, error) {
		rest := strings.TrimPrefix(r.URL.Path, "/api/gates/")
		if id, ok := cutSuffix(rest, "/decide"); ok {
			var in struct {
				State core.GateState
				By    string
				Note  string
			}
			if err := decode(r, &in); err != nil {
				return nil, err
			}
			return s.Store.DecideGate(r.Context(), id, in.State, in.By, in.Note)
		}
		return s.Store.Gate(r.Context(), rest)
	}))

	mux.HandleFunc("/api/ledger", s.wrap(func(r *http.Request) (any, error) {
		if r.Method == http.MethodPost {
			var e core.LedgerEntry
			if err := decode(r, &e); err != nil {
				return nil, err
			}
			return nil, s.Store.Append(r.Context(), e)
		}
		tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
		return s.Store.Ledger(r.Context(), tail)
	}))

	if s.Assets != nil {
		mux.Handle("/", s.Assets)
	}
	return mux
}

func (s *Server) wrap(fn func(*http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := fn(r)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			code, we := EncodeError(err)
			w.WriteHeader(code)
			json.NewEncoder(w).Encode(we)
			return
		}
		if out == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		json.NewEncoder(w).Encode(out)
	}
}

func decode(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func cutSuffix(s, suffix string) (string, bool) {
	if strings.HasSuffix(s, suffix) {
		return strings.TrimSuffix(s, suffix), true
	}
	return s, false
}
