// Package relational implements core.Store over a SQL database. SQLite is the
// local file backend; the same schema and transaction contract targets
// PostgreSQL for the gated battery and future hosted operation. One append
// transaction serializes on the Room row, so concurrent appends receive a
// dense sequence on both backends.
package relational

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/williamfzc/tearoom/internal/core"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// DSN parameters: WAL lets readers run alongside one writer, and busy_timeout
// serializes concurrent SQLite writers instead of failing them.
const dsnPragmas = "?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)"

// Store implements core.Store over one Room in a SQL database. roomID is the
// tenant key: many Stores may share one *gorm.DB as long as their room ids
// differ.
type Store struct {
	root   string
	db     *gorm.DB
	roomID string
	now    func() time.Time
}

// Init creates a fresh format-2 Room database under root (a .tearoom
// directory). The Room identity is the directory that owns root, matching the
// file backend, so both backends answer the same identity for one workspace.
func Init(root string) (*Store, error) {
	path := filepath.Join(root, "room.db")
	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("room database already exists at %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	db, err := openSQLite(path)
	if err != nil {
		return nil, err
	}
	if err := Migrate(db); err != nil {
		return nil, err
	}
	s := &Store{root: root, db: db, roomID: filepath.Base(filepath.Dir(root)), now: time.Now}
	room := core.Room{ID: s.roomID, Created: s.now().UTC()}
	if err := db.Create(&RoomRow{ID: room.ID, CreatedAt: room.Created, Head: 0}).Error; err != nil {
		return nil, err
	}
	return s, nil
}

// Open opens the format-2 Room database under root. A missing database is no
// room; a database without the format marker predates the cutover and is
// refused — there is no import or automatic upgrade.
func Open(root string) (*Store, error) {
	path := filepath.Join(root, "room.db")
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, core.ErrNoRoom
		}
		return nil, err
	}
	db, err := openSQLite(path)
	if err != nil {
		return nil, err
	}
	s := &Store{root: root, db: db, now: time.Now}
	if err := s.gate(context.Background()); err != nil {
		return nil, err
	}
	if _, err := s.Room(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

func openSQLite(path string) (*gorm.DB, error) {
	return gorm.Open(sqlite.Open("file:"+path+dsnPragmas), &gorm.Config{})
}

// OpenPostgres opens a PostgreSQL connection by DSN for the gated battery.
func OpenPostgres(dsn string) (*gorm.DB, error) {
	return gorm.Open(postgres.Open(dsn), &gorm.Config{})
}

// New wraps an existing database connection for one Room. roomID is the
// tenant key: many Stores may share one *gorm.DB as long as their room ids
// differ. The caller is responsible for schema creation (Migrate) and the
// Room row; the PostgreSQL battery and future hosted operation use this.
func New(db *gorm.DB, roomID string) *Store {
	return &Store{db: db, roomID: roomID, now: time.Now}
}

// Room returns the Room identity this database holds.
func (s *Store) Room(ctx context.Context) (*core.Room, error) {
	var row RoomRow
	if err := s.db.WithContext(ctx).Where("id = ?", s.roomID).Limit(1).Find(&row).Error; err != nil {
		return nil, err
	}
	if row.ID == "" {
		return nil, core.ErrNoRoom
	}
	return &core.Room{ID: row.ID, Created: row.CreatedAt}, nil
}

// AdmitActor explicitly admits an actor; joining an already-admitted actor
// returns the existing one unchanged. Admission serializes on the Room row so
// two concurrent joins of the same actor cannot double-insert.
func (s *Store) AdmitActor(ctx context.Context, actorID string) (*core.Actor, error) {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, errors.New("actor id is required")
	}
	var admitted *core.Actor
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("UPDATE rooms SET head = head WHERE id = ?", s.roomID).Error; err != nil {
			return err
		}
		var row ActorRow
		if err := tx.Where("room_id = ? AND id = ?", s.roomID, actorID).Limit(1).Find(&row).Error; err != nil {
			return err
		}
		if row.ID != "" {
			admitted = &core.Actor{ID: row.ID, Joined: row.JoinedAt}
			return nil
		}
		actor := &core.Actor{ID: actorID, Joined: s.now().UTC()}
		if err := tx.Create(&ActorRow{RoomID: s.roomID, ID: actor.ID, JoinedAt: actor.Joined}).Error; err != nil {
			return err
		}
		admitted = actor
		return nil
	})
	if err != nil {
		return nil, err
	}
	return admitted, nil
}

// Actors lists every admitted actor.
func (s *Store) Actors(ctx context.Context) ([]core.Actor, error) {
	var rows []ActorRow
	if err := s.db.WithContext(ctx).Where("room_id = ?", s.roomID).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	actors := make([]core.Actor, 0, len(rows))
	for _, row := range rows {
		actors = append(actors, core.Actor{ID: row.ID, Joined: row.JoinedAt})
	}
	return actors, nil
}

// Read returns the events after the caller-owned exclusive boundary for an
// admitted actor. Repeated reads are side-effect-free; no cursor is persisted.
func (s *Store) Read(ctx context.Context, actorID string, after uint64, limit int) ([]core.Event, error) {
	actorID = strings.TrimSpace(actorID)
	if err := s.requireActor(ctx, actorID); err != nil {
		return nil, err
	}
	query := s.db.WithContext(ctx).
		Where("room_id = ? AND seq > ?", s.roomID, after).
		Order("seq")
	if limit > 0 {
		query = query.Limit(limit)
	}
	var rows []EventRow
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	events := make([]core.Event, 0, len(rows))
	for _, row := range rows {
		events = append(events, row.coreEvent())
	}
	return events, nil
}

// Append accepts one contribution and assigns the next dense room sequence.
// One transaction serializes every append on the Room row — the first write
// takes the SQLite database write lock or the PostgreSQL row lock — then
// validates admission, resolves the idempotency key, reads the head, and
// inserts the immutable Event, the ledger row, and the new head all-or-none.
func (s *Store) Append(ctx context.Context, actorID string, request core.AppendRequest) (*core.Event, error) {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, errors.New("actor id is required")
	}
	content, err := core.CompactJSON(request.Content)
	if err != nil {
		return nil, fmt.Errorf("invalid content: %w", err)
	}
	to := core.NormalizeTo(request.To)
	key := strings.TrimSpace(request.IdempotencyKey)
	fingerprint := core.Fingerprint(actorID, to, content)
	toJSON, err := json.Marshal(to)
	if err != nil {
		return nil, fmt.Errorf("encode to: %w", err)
	}

	var appended *core.Event
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. Serialize appends on the Room row before any read, so the head
		// each writer observes reflects every committed append.
		if err := tx.Exec("UPDATE rooms SET head = head WHERE id = ?", s.roomID).Error; err != nil {
			return err
		}

		// 2. Admission.
		if err := s.requireActorTx(tx, actorID); err != nil {
			return err
		}

		// 3. Resolve an existing idempotency key: an equal fingerprint
		// returns the original Event, a different one conflicts.
		if key != "" {
			var idem IdempotencyRow
			if err := tx.Where("room_id = ? AND actor = ? AND key = ?", s.roomID, actorID, key).
				Limit(1).Find(&idem).Error; err != nil {
				return err
			}
			if idem.Key != "" {
				if !bytes.Equal(idem.Fingerprint, fingerprint) {
					return fmt.Errorf("%w: idempotency key %q already describes another event", core.ErrConflict, key)
				}
				var original EventRow
				if err := tx.Where("room_id = ? AND seq = ?", s.roomID, idem.EventSeq).
					Limit(1).Find(&original).Error; err != nil {
					return err
				}
				if original.Seq == 0 {
					return fmt.Errorf("idempotency key %q lost its original event", key)
				}
				event := original.coreEvent()
				appended = &event
				return nil
			}
		}

		// 4-6. Assign the next dense sequence and insert the immutable Event,
		// the ledger row, and the new head in the same transaction.
		var room RoomRow
		if err := tx.Where("id = ?", s.roomID).First(&room).Error; err != nil {
			return err
		}
		seq := room.Head + 1
		event := &core.Event{Seq: seq, Actor: actorID, At: s.now().UTC(), To: to, Content: content}
		row := EventRow{RoomID: s.roomID, Seq: seq, Actor: actorID, At: event.At, To: string(toJSON), Content: string(content)}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if key != "" {
			ledger := IdempotencyRow{RoomID: s.roomID, Actor: actorID, Key: key, Fingerprint: fingerprint, EventSeq: seq}
			if err := tx.Create(&ledger).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&RoomRow{}).Where("id = ?", s.roomID).Update("head", seq).Error; err != nil {
			return err
		}
		appended = event
		return nil
	})
	if err != nil {
		return nil, err
	}
	return appended, nil
}

func (s *Store) requireActor(ctx context.Context, id string) error {
	return s.requireActorTx(s.db.WithContext(ctx), id)
}

func (s *Store) requireActorTx(tx *gorm.DB, id string) error {
	var count int64
	if err := tx.Model(&ActorRow{}).Where("room_id = ? AND id = ?", s.roomID, id).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("%w: %s", core.ErrUnknownActor, id)
	}
	return nil
}

var _ core.Store = (*Store)(nil)
