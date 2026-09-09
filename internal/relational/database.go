// database.go opens deployment databases by DSN: PostgreSQL for the remote
// profile, SQLite files for tests and single-host setups. The schema is
// Migrate's responsibility either way.
package relational

import (
	"strings"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// OpenPostgres opens a PostgreSQL connection by DSN for the gated battery.
func OpenPostgres(dsn string) (*gorm.DB, error) {
	return gorm.Open(postgres.Open(dsn), &gorm.Config{})
}

// OpenDatabase opens one shared deployment database by DSN: a postgres:// URL
// selects the remote profile, anything else is a SQLite file for tests and
// single-host setups. Migrate owns the schema either way.
func OpenDatabase(dsn string) (*gorm.DB, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		return gorm.Open(postgres.Open(dsn), &gorm.Config{})
	}
	return openSQLite(dsn)
}
