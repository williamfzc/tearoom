// The same contract battery as the SQLite suite, against a real PostgreSQL
// when the operator provides one. Gated by TEAROOM_TEST_PG so `make check`
// runs anywhere and the PostgreSQL suite runs wherever a database exists.
// Each subtest gets a fresh random room id in the shared database and cleans
// up after itself.
package relational_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/williamfzc/tearoom/internal/control"
	"github.com/williamfzc/tearoom/internal/core"
	"github.com/williamfzc/tearoom/internal/relational"
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
	db, err := relational.OpenPostgres(pgDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := relational.Migrate(db); err != nil {
		t.Fatal(err)
	}
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	roomID := "pgtest-" + hex.EncodeToString(raw)
	st, err := relational.New(db, roomID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&relational.RoomRow{ID: roomID, CreatedAt: time.Now().UTC(), Head: 0}).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Where("room_id = ?", roomID).Delete(&relational.IdempotencyRow{})
		db.Where("room_id = ?", roomID).Delete(&relational.EventRow{})
		db.Where("room_id = ?", roomID).Delete(&relational.ActorRow{})
		db.Where("id = ?", roomID).Delete(&relational.RoomRow{})
	})
	return st
}

func TestPostgreSQLStoreContract(t *testing.T) {
	ctx := context.Background()
	// A reachability check up front gives a clear failure when the DSN is
	// wrong instead of a confusing per-subtest failure.
	db, err := relational.OpenPostgres(pgDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Exec("SELECT 1").Error; err != nil {
		t.Fatalf("cannot reach PostgreSQL at TEAROOM_TEST_PG: %v", err)
	}
	core.StoreContract(t, newPGStore)
}

// TestPostgreSQLCredentialControl proves the credential schema and policy on
// PostgreSQL: issue for an admitted actor, bearer verification, and immediate
// revocation ride the same additive table as SQLite.
func TestPostgreSQLCredentialControl(t *testing.T) {
	db, err := relational.OpenPostgres(pgDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := relational.Migrate(db); err != nil {
		t.Fatal(err)
	}
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	roomID := "pgcred-" + hex.EncodeToString(raw)
	st, err := relational.New(db, roomID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AdmitActor(context.Background(), "bob"); err != nil {
		t.Fatal(err)
	}
	ctl := control.New(db, nil)
	issued, err := ctl.Issue(context.Background(), roomID, "bob", time.Hour)
	if err != nil {
		t.Fatalf("issue = %v", err)
	}
	actor, err := ctl.Verify(context.Background(), roomID, issued.Token)
	if err != nil || actor != "bob" {
		t.Fatalf("verify = %q, %v", actor, err)
	}
	if err := ctl.Revoke(context.Background(), roomID, issued.ID); err != nil {
		t.Fatalf("revoke = %v", err)
	}
	if _, err := ctl.Verify(context.Background(), roomID, issued.Token); err == nil {
		t.Fatal("revoked credential verified")
	}
}
