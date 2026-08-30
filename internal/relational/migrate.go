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

// Migrate creates the format-2 schema on an existing connection: the five
// tables and the format marker. It is idempotent and safe to run on every
// open; the format gate in Store.Open runs first, so old databases are
// refused before any schema statement touches them.
func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(&MetaRow{}, &RoomRow{}, &ActorRow{}, &EventRow{}, &IdempotencyRow{}); err != nil {
		return err
	}
	var marker MetaRow
	if err := db.Where("key = ?", formatKey).Limit(1).Find(&marker).Error; err != nil {
		return err
	}
	if marker.Key != "" {
		return nil
	}
	return db.Create(&MetaRow{Key: formatKey, Value: strconv.Itoa(formatVersion)}).Error
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
	var marker MetaRow
	if err := s.db.WithContext(ctx).Where("key = ?", formatKey).Limit(1).Find(&marker).Error; err != nil {
		return err
	}
	want := strconv.Itoa(formatVersion)
	if marker.Key == "" || marker.Value != want {
		return fmt.Errorf(
			"incompatible database at %s: format %q, want %q; no automatic upgrade",
			s.root, marker.Value, want,
		)
	}
	return nil
}
