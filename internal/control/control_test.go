// Control contract tests: credentials are minted only for admitted actors,
// bound to their room, refused once expired or revoked, and stored only as
// hashes. These run against SQLite through the public relational API.
package control

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/williamfzc/tearoom/internal/core"
	"github.com/williamfzc/tearoom/internal/relational"
)

func newControl(t *testing.T) (*Control, *time.Time) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "control.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := relational.Migrate(db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	return New(db, func() time.Time { return now }), &now
}

func admit(t *testing.T, db *gorm.DB, room, actor string) {
	t.Helper()
	st, err := relational.New(db, room)
	if err != nil {
		t.Fatal(err)
	}
	// The store wraps the shared test database; never close it here.
	if _, err := st.AdmitActor(context.Background(), actor); err != nil {
		t.Fatal(err)
	}
}

func TestIssueRequiresAdmittedActor(t *testing.T) {
	c, _ := newControl(t)
	if _, err := c.Issue(context.Background(), "room", "stranger", 24*time.Hour); !errors.Is(err, core.ErrUnknownActor) {
		t.Fatalf("issue for unadmitted actor = %v, want core.ErrUnknownActor", err)
	}
}

func TestIssueMintsRoomBoundPrintableCredentials(t *testing.T) {
	c, now := newControl(t)
	db := c.db
	admit(t, db, "room", "bob")
	admit(t, db, "other", "carol")

	first, err := c.Issue(context.Background(), "room", "bob", 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.Issue(context.Background(), "room", "bob", 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(first.Token, "tr_") || !strings.HasPrefix(first.ID, "cred_") {
		t.Fatalf("issued = %+v, want tr_ token and cred_ id", first)
	}
	if first.Token == second.Token || first.ID == second.ID {
		t.Fatalf("two issues collided: %+v vs %+v", first, second)
	}
	if !first.ExpiresAt.Equal((*now).Add(24 * time.Hour)) {
		t.Fatalf("expires_at = %v, want %v", first.ExpiresAt, (*now).Add(24*time.Hour))
	}

	// Room binding: bob's token says nothing in another room.
	if _, err := c.Issue(context.Background(), "other", "carol", time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Verify(context.Background(), "other", first.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("cross-room verify = %v, want ErrUnauthorized", err)
	}
	if _, err := c.Verify(context.Background(), "room", first.Token); err != nil {
		t.Fatalf("same-room verify = %v", err)
	}
}

func TestExpiredCredentialRefused(t *testing.T) {
	c, now := newControl(t)
	admit(t, c.db, "room", "bob")
	issued, err := c.Issue(context.Background(), "room", "bob", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Verify(context.Background(), "room", issued.Token); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(2 * time.Hour)
	if _, err := c.Verify(context.Background(), "room", issued.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expired verify = %v, want ErrUnauthorized", err)
	}
}

func TestRevokeBlocksImmediatelyAndIsIdempotent(t *testing.T) {
	c, _ := newControl(t)
	admit(t, c.db, "room", "bob")
	issued, err := c.Issue(context.Background(), "room", "bob", 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Revoke(context.Background(), "room", issued.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Verify(context.Background(), "room", issued.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked verify = %v, want ErrUnauthorized", err)
	}
	// Revoking again is idempotent; an unknown credential is not.
	if err := c.Revoke(context.Background(), "room", issued.ID); err != nil {
		t.Fatalf("repeat revoke = %v, want nil", err)
	}
	if err := c.Revoke(context.Background(), "room", "cred_missing"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("unknown revoke = %v, want core.ErrNotFound", err)
	}
}

func TestTokenIsStoredOnlyAsHash(t *testing.T) {
	c, _ := newControl(t)
	admit(t, c.db, "room", "bob")
	issued, err := c.Issue(context.Background(), "room", "bob", 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := c.db.Table("credentials").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("credential rows = %d, want 1", len(rows))
	}
	for column, value := range rows[0] {
		if text, ok := value.(string); ok && strings.Contains(text, issued.Token) {
			t.Fatalf("column %q leaks the plaintext token", column)
		}
	}
}
