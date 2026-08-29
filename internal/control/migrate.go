// Versioned control-plane migrations, mirroring the coordination schema's
// discipline: every change is appended with its own version, never edited
// in place. The ctl_schema_versions table keeps this list separate from the
// coordination schema_versions table in a shared database.
package control

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

type ctlSchemaVersion struct {
	Version   int       `gorm:"primaryKey"`
	AppliedAt time.Time `gorm:"not null"`
}

func (ctlSchemaVersion) TableName() string { return "ctl_schema_versions" }

var migrations = []struct {
	version int
	run     func(*gorm.DB) error
}{
	{1, func(db *gorm.DB) error {
		return db.AutoMigrate(&Account{}, &Membership{}, &Credential{})
	}},
}

// Migrate upgrades a database to the current control-plane schema. Hosted
// servers and the hosted CLI run it alongside relational.Migrate on open.
func Migrate(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.AutoMigrate(&ctlSchemaVersion{}); err != nil {
			return err
		}
		var applied []int
		if err := tx.Model(&ctlSchemaVersion{}).Pluck("version", &applied).Error; err != nil {
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
				return fmt.Errorf("control migration %d: %w", m.version, err)
			}
			if err := tx.Create(&ctlSchemaVersion{Version: m.version, AppliedAt: time.Now().UTC()}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
