// The same battery as the SQLite suite, against a real PostgreSQL when the
// operator provides one. Gated by TEAROOM_TEST_PG so `make check` runs
// anywhere and the PG suite runs wherever a database exists. Each test uses
// a fresh random room id in the shared database and cleans up after itself.
package relational

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
)

func pgDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEAROOM_TEST_PG")
	if dsn == "" {
		t.Skip("TEAROOM_TEST_PG not set; skipping the PostgreSQL suite")
	}
	return dsn
}

func newPGStore(t *testing.T) *Store {
	t.Helper()
	db, err := OpenURL(pgDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	roomID := "pgtest-" + hex.EncodeToString(raw)
	store := New(db, roomID)
	if err := db.Create(&RoomRow{ID: roomID, Name: "pg battery"}).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Where("room_id = ?", roomID).Delete(&EventRow{})
		db.Where("room_id = ?", roomID).Delete(&ActorRow{})
		db.Where("id = ?", roomID).Delete(&RoomRow{})
	})
	return store
}

func TestPostgreSQLBattery(t *testing.T) {
	ctx := context.Background()
	// A room exists check up front gives a clear failure when the DSN is
	// wrong instead of seven confusing per-test failures.
	db, err := OpenURL(pgDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Exec("SELECT 1").Error; err != nil {
		t.Fatalf("cannot reach PostgreSQL at TEAROOM_TEST_PG: %v", err)
	}
	battery(t, newPGStore)
}
