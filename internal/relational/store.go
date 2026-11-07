// Package relational implements core.Store over a SQL database. SQLite is the
// local file backend; the same schema and transaction contract targets
// PostgreSQL for the gated battery and future multi-host serving. One append
// transaction serializes on the target Room's row, so concurrent appends to
// one room receive a dense sequence on both backends while different rooms
// never wait on each other.
package relational

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/williamfzc/tearoom/internal/core"
	"gorm.io/gorm"
)

// DSN parameters: WAL lets readers run alongside one writer, and busy_timeout
// serializes concurrent SQLite writers instead of failing them.
const dsnPragmas = "?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)"

// Store implements core.Store over one workspace database holding the whole
// room tree. Many Stores may share one *gorm.DB; rooms are keyed by name.
type Store struct {
	root string
	db   *gorm.DB
	now  func() time.Time
}

// Init creates a fresh workspace database. The workspace starts with no
// rooms; the first room is created through CreateRoom.
func Init(root string) (*Store, error) {
	path := filepath.Join(root, "room.db")
	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("workspace database already exists at %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	db, err := openSQLite(path)
	if err != nil {
		return nil, err
	}
	s := &Store{root: root, db: db, now: time.Now}
	if err := Migrate(db); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

// Open opens the workspace database under root, running the format gate and
// the one-time format-2 upgrade on the way in. A missing database is no
// workspace; a database with tables but no marker is refused — there is no
// import beyond the one documented migration step.
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
	if err := Migrate(db); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

func openSQLite(path string) (*gorm.DB, error) {
	return gorm.Open(sqlite.Open("file:"+path+dsnPragmas), &gorm.Config{})
}

// New wraps an existing database connection. The caller is responsible for
// schema creation (Migrate); this constructor only refuses a database whose
// marker is not the current format. The PostgreSQL battery uses this.
func New(db *gorm.DB) (*Store, error) {
	s := &Store{db: db, now: time.Now}
	if err := s.gate(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

// Close releases the underlying database connection.
func (s *Store) Close() error {
	db, err := s.db.DB()
	if err != nil {
		return err
	}
	return db.Close()
}

// Root returns the .tearoom directory backing this store.
func (s *Store) Root() string { return s.root }

// CreateRoom creates one node of the tree. The name must be unused; the
// parent, when given, must already exist. The parent link is written once,
// here — nothing ever writes into another room afterwards.
func (s *Store) CreateRoom(ctx context.Context, name, parent string) (*core.Room, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("room name is required")
	}
	parent = strings.TrimSpace(parent)
	created := s.now().UTC()
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if parent != "" {
			var prow RoomRow
			if err := tx.Where("name = ?", parent).Limit(1).Find(&prow).Error; err != nil {
				return err
			}
			if prow.Name == "" {
				return fmt.Errorf("parent room %q: %w", parent, core.ErrNotFound)
			}
		}
		var existing RoomRow
		if err := tx.Where("name = ?", name).Limit(1).Find(&existing).Error; err != nil {
			return err
		}
		if existing.Name != "" {
			return fmt.Errorf("room %q already exists: %w", name, core.ErrConflict)
		}
		row := RoomRow{Name: name, CreatedAt: created, Head: 0}
		if parent != "" {
			row.Parent = &parent
		}
		return tx.Create(&row).Error
	})
	if err != nil {
		return nil, err
	}
	return &core.Room{Name: name, Parent: parent, Created: created}, nil
}

// Rooms lists every Room of the tree. The child list of any room is derived
// from this listing at read time and is never cached.
func (s *Store) Rooms(ctx context.Context) ([]core.Room, error) {
	var rows []RoomRow
	if err := s.db.WithContext(ctx).Model(&RoomRow{}).Order("name").Find(&rows).Error; err != nil {
		return nil, err
	}
	rooms := make([]core.Room, 0, len(rows))
	for _, row := range rows {
		rooms = append(rooms, core.Room{Name: row.Name, Parent: deref(row.Parent), Created: row.CreatedAt})
	}
	return rooms, nil
}

// Read returns the records of one room after the caller-owned exclusive
// boundary. Repeated reads are side-effect-free; no cursor is persisted.
func (s *Store) Read(ctx context.Context, room string, after uint64) ([]core.Record, error) {
	room = strings.TrimSpace(room)
	if _, err := s.headRow(ctx, room); err != nil {
		return nil, err
	}
	var rows []EventRow
	if err := s.db.WithContext(ctx).
		Where("room_id = ? AND seq > ?", room, after).
		Order("seq").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	records := make([]core.Record, 0, len(rows))
	for _, row := range rows {
		record, err := row.coreRecord(room)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

// Append accepts one contribution and assigns the next dense sequence of the
// target room. One transaction serializes every append on that room's row —
// the first write takes the SQLite database write lock or the PostgreSQL row
// lock — then resolves the idempotency key, reads the head, and inserts the
// immutable record, the ledger row, and the new head all-or-none. The author
// is stored verbatim after whitespace trimming; an empty author is valid.
func (s *Store) Append(ctx context.Context, room, author string, request core.AppendRequest) (*core.Record, error) {
	room = strings.TrimSpace(room)
	author = strings.TrimSpace(author)
	content, err := core.CompactJSON(request.Content)
	if err != nil {
		return nil, fmt.Errorf("invalid content: %w", err)
	}
	key := strings.TrimSpace(request.IdempotencyKey)
	fingerprint := core.Fingerprint(author, content)

	var appended *core.Record
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. Serialize appends on the room row before any read, so the head
		// each writer observes reflects every committed append. The update
		// also answers room existence in the same atomic step.
		lock := tx.Exec("UPDATE rooms SET head = head WHERE name = ?", room)
		if lock.Error != nil {
			return lock.Error
		}
		if lock.RowsAffected == 0 {
			return fmt.Errorf("room %q: %w", room, core.ErrNotFound)
		}

		// 2. Resolve an existing idempotency key: an equal fingerprint
		// returns the original record, a different one conflicts.
		if key != "" {
			var idem IdempotencyRow
			if err := tx.Where("room_id = ? AND author = ? AND key = ?", room, author, key).
				Limit(1).Find(&idem).Error; err != nil {
				return err
			}
			if idem.Key != "" {
				if !bytesEqual(idem.Fingerprint, fingerprint) {
					return fmt.Errorf("%w: idempotency key %q already describes another record", core.ErrConflict, key)
				}
				var original EventRow
				if err := tx.Where("room_id = ? AND seq = ?", room, idem.EventSeq).
					Limit(1).Find(&original).Error; err != nil {
					return err
				}
				if original.Seq == 0 {
					return fmt.Errorf("idempotency key %q lost its original record", key)
				}
				record, err := original.coreRecord(room)
				if err != nil {
					return err
				}
				appended = &record
				return nil
			}
		}

		// 3-5. Assign the next dense sequence and insert the immutable
		// record, the ledger row, and the new head in the same transaction.
		var roomRow RoomRow
		if err := tx.Where("name = ?", room).First(&roomRow).Error; err != nil {
			return err
		}
		seq := roomRow.Head + 1
		record := &core.Record{Room: room, Seq: seq, Author: author, At: s.now().UTC(), Content: content}
		row := EventRow{RoomID: room, Seq: seq, Author: author, At: record.At, Content: string(content)}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if key != "" {
			ledger := IdempotencyRow{RoomID: room, Author: author, Key: key, Fingerprint: fingerprint, EventSeq: seq}
			if err := tx.Create(&ledger).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&RoomRow{}).Where("name = ?", room).Update("head", seq).Error; err != nil {
			return err
		}
		appended = record
		return nil
	})
	if err != nil {
		return nil, err
	}
	return appended, nil
}

// headRow returns the room row for name or a not-found error.
func (s *Store) headRow(ctx context.Context, room string) (RoomRow, error) {
	var row RoomRow
	if err := s.db.WithContext(ctx).Where("name = ?", room).Limit(1).Find(&row).Error; err != nil {
		return RoomRow{}, err
	}
	if row.Name == "" {
		return RoomRow{}, fmt.Errorf("room %q: %w", room, core.ErrNotFound)
	}
	return row, nil
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

var _ core.Store = (*Store)(nil)
