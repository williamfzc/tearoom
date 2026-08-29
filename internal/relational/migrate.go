// Versioned schema migrations. Every new schema change is appended here with
// its own version, never edited in place, so a Room created by an older
// binary upgrades forward when a newer binary opens it.
package relational

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// schemaVersion records which migrations a database has already applied.
type schemaVersion struct {
	Version   int       `gorm:"primaryKey"`
	AppliedAt time.Time `gorm:"not null"`
}

func (schemaVersion) TableName() string { return "schema_versions" }

var migrations = []struct {
	version int
	run     func(*gorm.DB) error
}{
	{1, func(db *gorm.DB) error {
		return db.AutoMigrate(&RoomRow{}, &ActorRow{}, &EventRow{})
	}},
}

func (s *Store) migrate(ctx context.Context) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.AutoMigrate(&schemaVersion{}); err != nil {
			return err
		}
		var applied []int
		if err := tx.Model(&schemaVersion{}).Pluck("version", &applied).Error; err != nil {
			return err
		}
		done := make(map[int]bool, len(applied))
		for _, version := range applied {
			done[version] = true
		}
		for _, m := range migrations {
			if done[m.version] {
				continue
			}
			if err := m.run(tx); err != nil {
				return fmt.Errorf("migration %d: %w", m.version, err)
			}
			if err := tx.Create(&schemaVersion{Version: m.version, AppliedAt: time.Now().UTC()}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
