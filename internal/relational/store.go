// Package relational implements core.Store over a GORM-managed database.
// SQLite is the local backend; the same models and transaction contract
// target PostgreSQL for hosted operation. Every Store invariant that flock
// protects in the file backend is enforced here by one append transaction.
package relational

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/williamfzc/tearoom/internal/core"
	"gorm.io/gorm"
)

// DSN parameters: WAL allows concurrent readers with one writer, and
// busy_timeout serializes concurrent appends instead of failing them.
const dsnPragmas = "?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)"

type Store struct {
	root  string
	db    *gorm.DB
	now   func() time.Time
	probe struct {
		mu   sync.Mutex
		head uint64
	}
}

// Init creates a new Room database under root (a .tearoom directory) and
// records the room row. Callers must have created root and written any
// backend marker before calling.
func Init(root, name string) (*Store, error) {
	s, err := openDB(root)
	if err != nil {
		return nil, err
	}
	if err := s.migrate(context.Background()); err != nil {
		return nil, err
	}
	room := RoomRow{ID: RoomID, Name: name, CreatedAt: s.now().UTC()}
	if err := s.db.Create(&room).Error; err != nil {
		return nil, err
	}
	return s, nil
}

// OpenAt opens an existing Room database under root, upgrading its schema if
// needed. A missing database file is reported as no room, like a missing
// room.json in the file backend.
func OpenAt(root string) (*Store, error) {
	if _, err := os.Stat(filepath.Join(root, "room.db")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, core.ErrNoRoom
		}
		return nil, err
	}
	s, err := openDB(root)
	if err != nil {
		return nil, err
	}
	if err := s.migrate(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

func openDB(root string) (*Store, error) {
	path := filepath.Join(root, "room.db")
	db, err := gorm.Open(sqlite.Open("file:"+path+dsnPragmas), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	return &Store{root: root, db: db, now: time.Now}, nil
}

func (s *Store) Root() string { return s.root }

func (s *Store) SetClock(now func() time.Time) { s.now = now }

func (s *Store) Room(ctx context.Context) (*core.Room, error) {
	var row RoomRow
	if err := s.db.WithContext(ctx).Where("id = ?", RoomID).Limit(1).Find(&row).Error; err != nil {
		return nil, err
	}
	if row.ID == "" {
		return nil, core.ErrNoRoom
	}
	return &core.Room{ID: row.ID, Name: row.Name, Created: row.CreatedAt}, nil
}

func (s *Store) UpsertActor(ctx context.Context, id, role string) (*core.Actor, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("actor id is required")
	}
	now := s.now().UTC()
	var actor *core.Actor
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row ActorRow
		if err := tx.Where("room_id = ? AND id = ?", RoomID, id).Limit(1).Find(&row).Error; err != nil {
			return err
		}
		if row.ID != "" {
			if role != "" {
				row.Role = role
			}
			row.LastSeenAt = now
			if err := tx.Save(&row).Error; err != nil {
				return err
			}
		} else {
			row = ActorRow{RoomID: RoomID, ID: id, Role: role, JoinedAt: now, LastSeenAt: now}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		actor = &core.Actor{ID: row.ID, Role: row.Role, Joined: row.JoinedAt, LastSeen: row.LastSeenAt}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return actor, nil
}

func (s *Store) Actors(ctx context.Context) ([]*core.Actor, error) {
	var rows []ActorRow
	if err := s.db.WithContext(ctx).Where("room_id = ?", RoomID).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	actors := make([]*core.Actor, 0, len(rows))
	for _, row := range rows {
		actors = append(actors, &core.Actor{ID: row.ID, Role: row.Role, Joined: row.JoinedAt, LastSeen: row.LastSeenAt})
	}
	return actors, nil
}

func (s *Store) Read(ctx context.Context, actor string, after uint64, limit int) (*core.Feed, error) {
	if err := s.requireActor(ctx, actor); err != nil {
		return nil, err
	}
	room, err := s.Room(ctx)
	if err != nil {
		return nil, err
	}
	feed := &core.Feed{Room: room, Cursor: after, Events: []core.Event{}}
	query := s.db.WithContext(ctx).
		Where("room_id = ? AND seq > ?", RoomID, after).
		Order("seq")
	if limit > 0 {
		// The SQL visibility predicate matches the canonical comma-joined
		// recipients. It can over-match (a comma inside an actor id), so the
		// exact core.Event.VisibleTo check below is the final filter; a
		// directed-heavy room may then return fewer than limit events.
		query = query.Where(
			"recipients = '' OR actor = ? OR ',' || recipients || ',' LIKE '%,' || ? || ',%'",
			actor, actor,
		).Limit(limit)
	}
	var rows []EventRow
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		event := row.coreEvent()
		feed.Cursor = event.Seq
		if event.VisibleTo(actor) {
			feed.Events = append(feed.Events, event)
		}
	}
	return feed, nil
}

// AppendEvent enforces the complete append contract in one transaction:
// lock the Room head, validate the Actor and recipients, resolve the
// idempotency key, compare the expected cursor, assign the next dense
// sequence, insert the immutable Event, and advance the head — all or none.
func (s *Store) AppendEvent(ctx context.Context, input core.AppendInput) (*core.Event, error) {
	input.Actor = strings.TrimSpace(input.Actor)
	input.Type = strings.TrimSpace(input.Type)
	input.CausedBy = strings.TrimSpace(input.CausedBy)
	input.To = core.NormalizeRecipients(input.To)
	if input.Actor == "" || input.Type == "" || strings.TrimSpace(input.Key) == "" {
		return nil, errors.New("actor, event type, and idempotency key are required")
	}
	if len(input.Data) > 0 && !json.Valid(input.Data) {
		return nil, errors.New("event data must be valid JSON")
	}

	var appended *core.Event
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. Lock the Room head. On SQLite the no-op update is the first
		// write, which serializes concurrent appends before any of them
		// reads; on PostgreSQL it takes the row lock.
		if err := tx.Exec("UPDATE rooms SET name = name WHERE id = ?", RoomID).Error; err != nil {
			return err
		}

		// 2. Validate the Actor and every recipient.
		if err := s.requireActorTx(tx, input.Actor); err != nil {
			return err
		}
		for _, recipient := range input.To {
			if err := s.requireActorTx(tx, recipient); err != nil {
				return fmt.Errorf("recipient %q: %w", recipient, err)
			}
		}

		// 3. Resolve an existing idempotency key.
		var existing EventRow
		if err := tx.Where("room_id = ? AND key = ?", RoomID, input.Key).Limit(1).Find(&existing).Error; err != nil {
			return err
		}
		if existing.Key != "" {
			if core.SameEvent(existing.coreEvent(), input) {
				event := existing.coreEvent()
				appended = &event
				return nil
			}
			return fmt.Errorf("%w: idempotency key %q already describes another event", core.ErrConflict, input.Key)
		}

		// 4. Compare the expected cursor with the current head.
		var room RoomRow
		if err := tx.Where("id = ?", RoomID).First(&room).Error; err != nil {
			return err
		}
		if input.Expected != room.Head {
			return &core.StaleWriteError{Expected: input.Expected, Actual: room.Head}
		}

		// 5-6. Assign the next dense sequence and insert the immutable Event.
		seq := room.Head + 1
		row := EventRow{
			RoomID:     RoomID,
			Seq:        seq,
			ID:         fmt.Sprintf("e%d", seq),
			Actor:      input.Actor,
			At:         s.now().UTC(),
			Type:       input.Type,
			Body:       input.Body,
			Data:       string(input.Data),
			Recipients: strings.Join(input.To, ","),
			CausedBy:   input.CausedBy,
			Key:        input.Key,
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}

		// 7. Advance the Room head in the same transaction.
		if err := tx.Model(&RoomRow{}).Where("id = ?", RoomID).Update("head", seq).Error; err != nil {
			return err
		}
		event := row.coreEvent()
		appended = &event
		return nil
	})
	if err != nil {
		return nil, err
	}
	return appended, nil
}

// Changed implements core.Probe: an indexed max(seq) aggregate instead of a
// full read, letting long-poll waiters poll a quiet Room almost for free.
func (s *Store) Changed(ctx context.Context) (bool, error) {
	var head uint64
	if err := s.db.WithContext(ctx).
		Model(&EventRow{}).
		Where("room_id = ?", RoomID).
		Select("COALESCE(MAX(seq), 0)").
		Scan(&head).Error; err != nil {
		return false, err
	}
	p := &s.probe
	p.mu.Lock()
	defer p.mu.Unlock()
	changed := head != p.head
	p.head = head
	return changed, nil
}

func (s *Store) requireActor(ctx context.Context, id string) error {
	return s.requireActorTx(s.db.WithContext(ctx), id)
}

func (s *Store) requireActorTx(tx *gorm.DB, id string) error {
	var count int64
	if err := tx.Model(&ActorRow{}).Where("room_id = ? AND id = ?", RoomID, id).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("%w: %s", core.ErrUnknownActor, id)
	}
	return nil
}

var _ core.Store = (*Store)(nil)
var _ core.Probe = (*Store)(nil)
