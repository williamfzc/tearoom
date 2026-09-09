// hosted serves many rooms of one shared database behind one process: the
// owner creates rooms through POST /api/rooms, and /r/<id>/api/* dispatches
// to that room's transport server with the shared credential control. Reads
// stay open; writes require a room credential; ownership gates the rest.
package hosted

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/williamfzc/tearoom/internal/control"
	"github.com/williamfzc/tearoom/internal/relational"
	"github.com/williamfzc/tearoom/internal/transport"
)

// Server hosts the rooms of one deployment database.
type Server struct {
	db             *gorm.DB
	control        *control.Control
	ownerTokenHash string
	now            func() time.Time
}

// New migrates the shared database and builds the hosted server. The owner
// token is the deployment bootstrap secret; without it there is no hosted
// mode, because an exposed service without credentials is refused.
func New(db *gorm.DB, ownerToken string, now func() time.Time) (*Server, error) {
	if strings.TrimSpace(ownerToken) == "" {
		return nil, errors.New("hosted mode requires an owner token: set TEAROOM_OWNER_TOKEN")
	}
	if err := relational.Migrate(db); err != nil {
		return nil, fmt.Errorf("migrate deployment database: %w", err)
	}
	if now == nil {
		now = time.Now
	}
	return &Server{
		db:             db,
		control:        control.New(db, now),
		ownerTokenHash: control.HashToken(ownerToken),
		now:            now,
	}, nil
}

// Handler routes the hosted surface:
//
//	POST /api/rooms        owner-only: create a Room, learn its room URL
//	/r/<id>/api/...        the Room contract of that Room
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/rooms", transport.Wrap(s.createRoom))
	mux.HandleFunc("/r/{room}/api/", s.roomAPI)
	return mux
}

// createRoom mints one Room and answers with its room-scoped URL.
func (s *Server) createRoom(r *http.Request) (any, error) {
	if !s.isOwner(r) {
		return nil, transport.ErrForbidden
	}
	roomID := "room_" + randomHex(4)
	room := relational.RoomRow{ID: roomID, CreatedAt: s.now().UTC()}
	if err := s.db.Create(&room).Error; err != nil {
		return nil, err
	}
	return createdRoom{
		ID:      room.ID,
		Created: room.CreatedAt,
		Server:  baseURL(r) + "/r/" + room.ID,
	}, nil
}

// roomAPI dispatches the Room contract of one Room. Unknown rooms answer 404
// without leaking anything else.
func (s *Server) roomAPI(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("room")
	store, err := relational.New(s.db, roomID)
	if err != nil {
		transport.Wrap(func(*http.Request) (any, error) { return nil, err })(w, r)
		return
	}
	if _, err := store.Room(r.Context()); err != nil {
		transport.Wrap(func(*http.Request) (any, error) { return nil, err })(w, r)
		return
	}
	room := transport.NewServer(store)
	room.Auth = &transport.Auth{Control: s.control, OwnerTokenHash: s.ownerTokenHash}
	http.StripPrefix("/r/"+roomID, room.Handler()).ServeHTTP(w, r)
}

// createdRoom is the response of POST /api/rooms. Server is the room-scoped
// URL that TEAROOM_SERVER points at from then on.
type createdRoom struct {
	ID      string    `json:"id"`
	Created time.Time `json:"created"`
	Server  string    `json:"server"`
}

func (s *Server) isOwner(r *http.Request) bool {
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	token, ok := strings.CutPrefix(value, "Bearer ")
	if !ok {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(control.HashToken(token)), []byte(s.ownerTokenHash)) == 1
}

func baseURL(r *http.Request) string {
	scheme := "http"
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		scheme = forwarded
	} else if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func randomHex(bytes int) string {
	raw := make([]byte, bytes)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return hex.EncodeToString(raw)
}
