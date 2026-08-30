// The same contract battery as the SQLite suite, against a real PostgreSQL
// when the operator provides one. Gated by TEAROOM_TEST_PG so `make check`
// runs anywhere and the PostgreSQL suite runs wherever a database exists.
// Each subtest gets a fresh random room id in the shared database and cleans
// up after itself.
package relational

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"

	"github.com/williamfzc/tearoom/internal/core"
)

func pgDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEAROOM_TEST_PG")
	if dsn == "" {
		t.Skip("TEAROOM_TEST_PG not set; skipping the PostgreSQL suite")
	}
	return dsn
}

func newPGStore(t *testing.T) core.Store {
	t.Helper()
	db, err := OpenPostgres(pgDSN(t))
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
	st := New(db, roomID)
	if err := db.Create(&RoomRow{ID: roomID, CreatedAt: st.now().UTC(), Head: 0}).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Where("room_id = ?", roomID).Delete(&IdempotencyRow{})
		db.Where("room_id = ?", roomID).Delete(&EventRow{})
		db.Where("room_id = ?", roomID).Delete(&ActorRow{})
		db.Where("id = ?", roomID).Delete(&RoomRow{})
	})
	return st
}

func TestPostgreSQLStoreContract(t *testing.T) {
	ctx := context.Background()
	// A reachability check up front gives a clear failure when the DSN is
	// wrong instead of a confusing per-subtest failure.
	db, err := OpenPostgres(pgDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Exec("SELECT 1").Error; err != nil {
		t.Fatalf("cannot reach PostgreSQL at TEAROOM_TEST_PG: %v", err)
	}
	core.StoreContract(t, newPGStore)
}
