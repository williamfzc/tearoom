// Package transport carries the core.Store contract over HTTP.
// The server owns routing and error mapping, never room policy.
package transport

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/williamfzc/tearoom/internal/core"
)

// LongPollMax caps how long one wait request may hold the server. Clients
// slice longer waits into repeated requests, so this bound keeps one stalled
// client from pinning a connection arbitrarily.
const LongPollMax = 60 * time.Second

// LongPollInterval is the idle fallback cadence: how often a waiter checks the
// store for changes that did not arrive through this server's own appends.
const LongPollInterval = time.Second

type Server struct {
	Store core.Store
	// Wake is closed and replaced on every successful append so long-poll
	// waiters stop burning polls while a room is quiet.
	Wake *Broadcaster
}

func NewServer(store core.Store) *Server { return &Server{Store: store, Wake: NewBroadcaster()} }

// Broadcaster fans one wakeup out to any number of waiters. A waiter holds the
// current channel; notify() closes it and installs a fresh one, so a wakeup is
// delivered to every waiter and never delivered twice.
type Broadcaster struct {
	mu sync.Mutex
	ch chan struct{}
}

func NewBroadcaster() *Broadcaster { return &Broadcaster{ch: make(chan struct{})} }

func (b *Broadcaster) Notify() {
	b.mu.Lock()
	defer b.mu.Unlock()
	close(b.ch)
	b.ch = make(chan struct{})
}

func (b *Broadcaster) Wait() <-chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ch
}

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
			event, err := s.Store.AppendEvent(r.Context(), input)
			if err == nil {
				s.Wake.Notify()
			}
			return event, err
		}
		return s.readEvents(r)
	}))
	return mux
}

// readEvents serves GET /api/events. The wait parameter, in seconds, makes the
// request hold until a visible event appears or the wait elapses; an empty
// feed then means "nothing new, ask again".
func (s *Server) readEvents(r *http.Request) (any, error) {
	actor := r.URL.Query().Get("actor")
	after, err := parseUint(r.URL.Query().Get("after"))
	if err != nil {
		return nil, err
	}
	limit, err := strconv.Atoi(defaultValue(r.URL.Query().Get("limit"), "0"))
	if err != nil || limit < 0 {
		return nil, errorsText("limit must be a non-negative integer")
	}
	wait, err := parseUint(r.URL.Query().Get("wait"))
	if err != nil {
		return nil, errorsText("wait must be a non-negative integer of seconds")
	}
	if wait == 0 {
		return s.Store.Read(r.Context(), actor, after, limit)
	}
	waiting := time.Duration(wait) * time.Second
	if waiting > LongPollMax {
		waiting = LongPollMax
	}
	deadline := time.Now().Add(waiting)
	dirty := true
	for {
		// Read only when something may have changed; a quiet room otherwise
		// waits on the broadcaster or a stat-based probe, never a full read.
		if dirty {
			feed, err := s.Store.Read(r.Context(), actor, after, limit)
			if err != nil {
				return nil, err
			}
			if feed.Cursor > after {
				return feed, nil
			}
			dirty = false
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			feed, err := s.Store.Read(r.Context(), actor, after, limit)
			if err != nil {
				return nil, err
			}
			return feed, nil
		}
		step := remaining
		if step > LongPollInterval {
			step = LongPollInterval
		}
		select {
		case <-s.Wake.Wait():
			// An append arrived through this server.
			dirty = true
		case <-time.After(step):
			// Idle fallback: pick up writes that bypassed the server. A store
			// with core.Probe answers by stat, which does not take the lock.
			probe, ok := s.Store.(core.Probe)
			if !ok {
				dirty = true
				continue
			}
			changed, err := probe.Changed(r.Context())
			if err != nil {
				return nil, err
			}
			dirty = changed
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	}
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
