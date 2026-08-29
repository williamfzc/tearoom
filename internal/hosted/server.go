// Package hosted is the multi-tenant server: management routes under
// /api/hosted/* and one authenticated event API under /r/<room-id>/api/*.
// It composes the control plane and the coordination transport: a Bearer
// credential is resolved to (room, actor, capabilities) and checked before
// the request is delegated to a per-room transport.Server over a shared
// database. Room policy stays in the kernel; hosted only gates access.
package hosted

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/williamfzc/tearoom/internal/control"
	"github.com/williamfzc/tearoom/internal/relational"
	"github.com/williamfzc/tearoom/internal/transport"
	"gorm.io/gorm"
)

// Server hosts many Rooms over one shared database. roomServers holds one
// transport.Server per Room — including its long-poll Broadcaster, so
// appends and waiters of different Rooms never share a wakeup.
type Server struct {
	db    *gorm.DB
	admin string

	// corsOrigin, when set, is the exact Origin the management UI is served
	// from (a Cloudflare Pages domain, say). Cross-origin browsers then get
	// the CORS headers and preflight answers; other origins stay closed.
	corsOrigin string

	mu          sync.Mutex
	roomServers map[string]*transport.Server
}

// New opens the hosted server over a shared database. admin is the bearer
// token management routes accept; the CLI passes control.DevAdmin(), which
// is empty unless dev mode is on.
func New(db *gorm.DB, admin string) *Server {
	return &Server{
		db:          db,
		admin:       admin,
		roomServers: make(map[string]*transport.Server),
	}
}

// SetCORS allows cross-origin browser requests (the management UI) from
// exactly origin. Empty disables CORS entirely.
func (s *Server) SetCORS(origin string) {
	s.corsOrigin = origin
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/api/hosted/login", s.handleLogin)
	mux.HandleFunc("/api/hosted/rooms", s.requireAdmin(s.handleRooms))
	mux.HandleFunc("/api/hosted/rooms/", s.requireAdmin(s.handleRoom))
	mux.HandleFunc("/r/", s.handleRoomAPI)
	return s.cors(mux)
}

// cors answers preflights and annotates responses when the request Origin
// matches the configured management-UI origin. Authorization and
// Content-Type are the headers the UI's fetch calls carry.
func (s *Server) cors(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.corsOrigin != "" {
			origin := r.Header.Get("Origin")
			if origin == s.corsOrigin {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		h.ServeHTTP(w, r)
	})
}

// handleHealthz answers liveness for the hosting platform: no authentication,
// no state, so Render's health check can hit it without credentials.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Management routes -------------------------------------------------------

// handleLogin is the Phase 1 session stub: with dev mode on it answers the
// admin account for the admin bearer. The GitHub OAuth exchange (Phase 2)
// replaces it; the endpoint stays as the shape of the future session API.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.admin == "" || bearer(r) != s.admin {
		writeError(w, http.StatusUnauthorized, "hosted dev admin is not configured")
		return
	}
	account, err := control.DevLogin(r.Context(), s.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":    account.ID,
		"login": account.Login,
		"name":  account.Name,
	})
}

func (s *Server) handleRooms(w http.ResponseWriter, r *http.Request) {
	account, err := control.DevLogin(r.Context(), s.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	switch r.Method {
	case http.MethodPost:
		var input struct {
			Name string `json:"name"`
		}
		if err := decode(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid room request")
			return
		}
		roomID, err := control.CreateRoom(r.Context(), s.db, account.ID, input.Name)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"id": roomID, "name": input.Name})
	case http.MethodGet:
		rooms, err := control.RoomsOf(r.Context(), s.db, account.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"rooms": rooms})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleRoom(w http.ResponseWriter, r *http.Request) {
	// /api/hosted/rooms/<roomID>/credentials[/<credID>]
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/hosted/rooms/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}
	roomID := parts[0]
	account, err := control.DevLogin(r.Context(), s.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := control.MembershipOf(r.Context(), s.db, account.ID, roomID); err != nil {
		writeError(w, http.StatusForbidden, "no membership in this room")
		return
	}
	if len(parts) == 1 {
		writeError(w, http.StatusNotFound, "credential route not found")
		return
	}
	if parts[1] != "credentials" {
		writeError(w, http.StatusNotFound, "credential route not found")
		return
	}
	switch {
	case r.Method == http.MethodPost && len(parts) == 2:
		var input struct {
			Actor        string `json:"actor"`
			Capabilities string `json:"capabilities"`
			TTLSeconds   int64  `json:"ttl_seconds"`
		}
		if err := decode(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid credential request")
			return
		}
		token, cred, err := control.Issue(r.Context(), s.db, roomID, input.Actor, input.Capabilities,
			time.Duration(input.TTLSeconds)*time.Second)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"id":           cred.ID,
			"token":        token,
			"room":         roomID,
			"actor":        cred.ActorID,
			"capabilities": cred.Capabilities,
			"expires_at":   cred.ExpiresAt,
		})
	case r.Method == http.MethodDelete && len(parts) == 3:
		id, err := strconv.ParseUint(parts[2], 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "credential id must be an integer")
			return
		}
		if err := control.Revoke(r.Context(), s.db, roomID, uint(id)); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"revoked": id})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// Event API ---------------------------------------------------------------

// handleRoomAPI serves /r/<roomID>/api/*: authenticate the Bearer against
// the Room, enforce actor binding and capabilities, then delegate the rest
// of the path to that Room's transport server.
func (s *Server) handleRoomAPI(w http.ResponseWriter, r *http.Request) {
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/r/"), "/", 3)
	if len(parts) != 3 || parts[1] != "api" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	roomID := parts[0]

	cred, err := s.authenticate(r, roomID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if err := s.authorize(r, cred); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}

	// The Room's transport server needs the original request with only the
	// /r/<roomID> prefix stripped, so its own /api/* routes match.
	inner := r.Clone(r.Context())
	inner.URL.Path = "/" + parts[1] + "/" + parts[2]
	inner.URL.RawPath = ""
	s.roomServer(roomID).Handler().ServeHTTP(w, inner)
}

// authenticate resolves the Bearer to a live credential of this exact Room.
// A wrong-room credential answers 401, not 403, so a token never proves a
// Room exists.
func (s *Server) authenticate(r *http.Request, roomID string) (*control.Credential, error) {
	token := bearer(r)
	if token == "" {
		return nil, errors.New("a bearer credential is required")
	}
	cred, err := control.Verify(r.Context(), s.db, token)
	if err != nil {
		return nil, err
	}
	if cred.RoomID != roomID {
		return nil, errors.New("credential does not belong to this room")
	}
	return cred, nil
}

// authorize enforces capability and actor binding. The request must declare
// the credential's own Actor: reads name it in the query, writes in the
// body — a credential is one agent's identity, not a room-level key.
// Registering one's own actor (POST /api/actors) needs any capability: it
// establishes identity, not content, so a read-only agent can still join.
func (s *Server) authorize(r *http.Request, cred *control.Credential) error {
	if strings.HasSuffix(r.URL.Path, "/api/actors") && r.Method == http.MethodPost {
		if !control.HasCaps(cred.Capabilities, control.CapRead) && !control.HasCaps(cred.Capabilities, control.CapAppend) {
			return errors.New("credential cannot join this room")
		}
		actor, err := declaredActor(r)
		if err != nil {
			return err
		}
		if actor != cred.ActorID {
			return errors.New("credential is bound to another actor")
		}
		return nil
	}
	if r.Method == http.MethodGet {
		if !control.HasCaps(cred.Capabilities, control.CapRead) {
			return errors.New("credential cannot read")
		}
		actor := r.URL.Query().Get("actor")
		if actor != "" && actor != cred.ActorID {
			return errors.New("credential is bound to another actor")
		}
		return nil
	}
	if !control.HasCaps(cred.Capabilities, control.CapAppend) {
		return errors.New("credential cannot append")
	}
	actor, err := declaredActor(r)
	if err != nil {
		return err
	}
	if actor != cred.ActorID {
		return errors.New("credential is bound to another actor")
	}
	return nil
}

// declaredActor peeks the actor declared in a write body without consuming
// it: events carry "actor", actor registration carries "id".
func declaredActor(r *http.Request) (string, error) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return "", errors.New("cannot read request body")
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	var declared struct {
		Actor string `json:"actor"`
		ID    string `json:"id"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &declared); err != nil {
			return "", errors.New("body must be valid JSON")
		}
	}
	if declared.Actor != "" {
		return declared.Actor, nil
	}
	return declared.ID, nil
}

// roomServer returns (creating on first use) the transport server for one
// Room: a relational.Store over the shared database keyed on the Room id,
// with its own Broadcaster for long-poll wakeups.
func (s *Server) roomServer(roomID string) *transport.Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	if inst, ok := s.roomServers[roomID]; ok {
		return inst
	}
	inst := transport.NewServer(relational.New(s.db, roomID))
	s.roomServers[roomID] = inst
	return inst
}

// Helpers -----------------------------------------------------------------

func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.admin == "" {
			writeError(w, http.StatusUnauthorized, "hosted dev admin is not configured")
			return
		}
		if bearer(r) != s.admin {
			writeError(w, http.StatusUnauthorized, "admin credential required")
			return
		}
		next(w, r)
	}
}

func bearer(r *http.Request) string {
	return strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
}

// writeError emits the same wire shape the transport client decodes, so CLI
// callers see a real message instead of "server returned 401".
func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"kind": "error", "message": message})
}

func writeJSON(w http.ResponseWriter, status int, out any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(out)
}

func decode(r *http.Request, value any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(value)
}
