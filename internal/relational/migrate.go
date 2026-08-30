// The format gate and schema creation. A database created before the cutover
// carries no format marker; opening it is refused with a clear incompatible
// error, because there is no import or automatic upgrade. Schema creation is
// idempotent, so shared databases may run it on every open.
package relational

import (
	"context"
	"fmt"
	"strconv"

	"gorm.io/gorm"
)

// formatVersion is the only database layout accepted: the cutover Room model
// with the append_idempotency ledger and the meta format marker. Any other
// layout is refused on open.
const formatVersion = 2

// formatKey names the meta row that records the layout format.
const formatKey = "format"

// Migrate creates or advances the format-2 schema. A completely empty
// database receives the schema and marker in one transaction. An existing
// database must already carry the exact format-2 marker before any schema
// mutation; old, missing, or wrong markers are refused.
func Migrate(db *gorm.DB) error {
	if db.Migrator().HasTable(&MetaRow{}) {
		if err := checkMarker(db); err != nil {
			return err
		}
		return db.AutoMigrate(&MetaRow{}, &RoomRow{}, &ActorRow{}, &EventRow{}, &IdempotencyRow{})
	}
	for _, table := range []string{"schema_versions", "rooms", "actors", "events", "append_idempotency"} {
		if db.Migrator().HasTable(table) {
			return fmt.Errorf("incompatible database: existing table %q has no format-%d marker; no automatic upgrade", table, formatVersion)
		}
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.AutoMigrate(&MetaRow{}, &RoomRow{}, &ActorRow{}, &EventRow{}, &IdempotencyRow{}); err != nil {
			return err
		}
		return tx.Create(&MetaRow{Key: formatKey, Value: strconv.Itoa(formatVersion)}).Error
	})
}

func checkMarker(db *gorm.DB) error {
	var marker MetaRow
	if err := db.Where("key = ?", formatKey).Limit(1).Find(&marker).Error; err != nil {
		return err
	}
	want := strconv.Itoa(formatVersion)
	if marker.Key == "" || marker.Value != want {
		return fmt.Errorf("incompatible database: format %q, want %q; no automatic upgrade", marker.Value, want)
	}
	return nil
}

// gate refuses a database that predates the cutover. The marker table is the
// only accepted signature: databases without it — old schema_versions layouts
// or unrecognized shapes — are never read, migrated, or reused.
func (s *Store) gate(ctx context.Context) error {
	if !s.db.Migrator().HasTable(&MetaRow{}) {
		return fmt.Errorf(
			"incompatible database at %s: missing the format-%d marker (pre-cutover layout); no automatic upgrade",
			s.root, formatVersion,
		)
	}
	return checkMarker(s.db.WithContext(ctx))
}
