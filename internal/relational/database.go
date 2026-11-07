// database.go opens deployment databases by DSN: PostgreSQL for the gated
// battery. The schema is Migrate's responsibility.
package relational

import (
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// OpenPostgres opens a PostgreSQL connection by DSN for the gated battery.
func OpenPostgres(dsn string) (*gorm.DB, error) {
	return gorm.Open(postgres.Open(dsn), &gorm.Config{})
}
