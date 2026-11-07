// The format gate and schema creation. A database created before the tree
// format carries no usable format marker; opening it is refused with a clear
// incompatible error, because there is no import beyond the one-time
// format-2 → format-3 step. Schema creation is idempotent, so shared
// databases may run it on every open.
package relational

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"gorm.io/gorm"
)

// formatVersion is the current database layout: the room tree with per-room
// streams and the meta format marker. Format 2 — the v1 single-room model —
// upgrades once, on open.
const formatVersion = 3

// formatKey names the meta row that records the layout format.
const formatKey = "format"

// Migrate creates, verifies, or upgrades the schema to the current format.
// A completely empty database receives the format-3 schema and marker in one
// transaction. An existing database must carry a marker: format 2 upgrades
// once to format 3 (its single room becomes a root named by its old
// identifier, signatures are kept verbatim, and the actor and credential
// tables are dropped); format 3 is verified idempotently; any other marker —
// or a database with tables but no marker — is refused, never read or
// upgraded.
func Migrate(db *gorm.DB) error {
	if !db.Migrator().HasTable(&MetaRow{}) {
		return createFresh(db)
	}
	var marker MetaRow
	if err := db.Where("key = ?", formatKey).Limit(1).Find(&marker).Error; err != nil {
		return err
	}
	switch marker.Value {
	case strconv.Itoa(formatVersion):
		return db.AutoMigrate(&MetaRow{}, &RoomRow{}, &EventRow{}, &IdempotencyRow{})
	case "2":
		return migrateFormat2(db)
	default:
		return fmt.Errorf("incompatible database: format %q, want %q; no automatic upgrade",
			marker.Value, strconv.Itoa(formatVersion))
	}
}

// createFresh refuses a database that already holds pre-marker tables and
// otherwise creates the format-3 schema and marker in one transaction.
func createFresh(db *gorm.DB) error {
	for _, table := range []string{"schema_versions", "rooms", "actors", "credentials", "events", "append_idempotency"} {
		if db.Migrator().HasTable(table) {
			return fmt.Errorf("incompatible database: existing table %q has no format-%d marker; no automatic upgrade", table, formatVersion)
		}
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.AutoMigrate(&MetaRow{}, &RoomRow{}, &EventRow{}, &IdempotencyRow{}); err != nil {
			return err
		}
		return tx.Create(&MetaRow{Key: formatKey, Value: strconv.Itoa(formatVersion)}).Error
	})
}

// Migration shapes: the format-3 rows built beside the format-2 tables, then
// swapped in by rename. GORM derives dialect-correct types from these
// structs, so the same migration runs on SQLite and PostgreSQL.
type roomRowV3 struct {
	Name      string    `gorm:"primaryKey;size:255"`
	Parent    *string   `gorm:"size:255"`
	CreatedAt time.Time `gorm:"not null"`
	Head      uint64    `gorm:"not null"`
}

func (roomRowV3) TableName() string { return "rooms_v3" }

type eventRowV3 struct {
	RoomID  string    `gorm:"column:room_id;primaryKey;size:255"`
	Seq     uint64    `gorm:"primaryKey"`
	Author  string    `gorm:"not null"`
	At      time.Time `gorm:"not null"`
	Content string    `gorm:"not null"`
}

func (eventRowV3) TableName() string { return "events_v3" }

type idempotencyRowV3 struct {
	RoomID      string `gorm:"primaryKey;size:255"`
	Author      string `gorm:"primaryKey;size:255"`
	Key         string `gorm:"primaryKey;size:255"`
	Fingerprint []byte `gorm:"not null"`
	EventSeq    uint64 `gorm:"not null"`
}

func (idempotencyRowV3) TableName() string { return "append_idempotency_v3" }

// migrateFormat2 performs the one-time v1 → tree upgrade in one transaction:
// the single room row becomes a root named by its old identifier, records
// keep their author verbatim and lose the recipient column, the actor and
// credential tables are dropped, and the marker is rewritten. If anything
// fails, the transaction rolls back and the database stays at format 2.
func migrateFormat2(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.AutoMigrate(&roomRowV3{}, &eventRowV3{}, &idempotencyRowV3{}); err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO rooms_v3 (name, parent, created_at, head)
			SELECT id, NULL, created_at, head FROM rooms`).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO events_v3 (room_id, seq, author, at, content)
			SELECT room_id, seq, actor, at, content FROM events`).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO append_idempotency_v3 (room_id, author, key, fingerprint, event_seq)
			SELECT room_id, actor, key, fingerprint, event_seq FROM append_idempotency`).Error; err != nil {
			return err
		}
		m := tx.Migrator()
		if err := m.DropTable("rooms", "events", "append_idempotency"); err != nil {
			return err
		}
		for _, table := range []string{"actors", "credentials"} {
			if m.HasTable(table) {
				if err := m.DropTable(table); err != nil {
					return err
				}
			}
		}
		if err := m.RenameTable("rooms_v3", "rooms"); err != nil {
			return err
		}
		if err := m.RenameTable("events_v3", "events"); err != nil {
			return err
		}
		if err := m.RenameTable("append_idempotency_v3", "append_idempotency"); err != nil {
			return err
		}
		if err := tx.AutoMigrate(&MetaRow{}, &RoomRow{}, &EventRow{}, &IdempotencyRow{}); err != nil {
			return err
		}
		return tx.Model(&MetaRow{}).Where("key = ?", formatKey).Update("value", strconv.Itoa(formatVersion)).Error
	})
}

// gate refuses a database that predates the marker era. The marker table is
// the only accepted signature: databases without it — old schema_versions
// layouts or unrecognized shapes — are never read, migrated, or reused.
func (s *Store) gate(ctx context.Context) error {
	if !s.db.Migrator().HasTable(&MetaRow{}) {
		return fmt.Errorf(
			"incompatible database at %s: missing the format-%d marker (pre-cutover layout); no automatic upgrade",
			s.root, formatVersion,
		)
	}
	var marker MetaRow
	if err := s.db.WithContext(ctx).Where("key = ?", formatKey).Limit(1).Find(&marker).Error; err != nil {
		return err
	}
	if marker.Key == "" || marker.Value != strconv.Itoa(formatVersion) {
		return fmt.Errorf("incompatible database: format %q, want %q; no automatic upgrade",
			marker.Value, strconv.Itoa(formatVersion))
	}
	return nil
}
