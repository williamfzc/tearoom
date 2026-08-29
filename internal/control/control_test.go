// The control plane over an in-memory database: account, membership, and
// credential lifecycle. The coordination side is exercised through the
// relational tables a hosted server actually shares.
package control

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/williamfzc/tearoom/internal/relational"
	"gorm.io/gorm"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := relational.OpenURL(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := relational.Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Migrator().DropTable("ctl_credentials", "ctl_memberships", "ctl_accounts",
			"events", "actors", "rooms", "schema_versions", "ctl_schema_versions")
	})
	return db
}

func TestMigrateIsIdempotent(t *testing.T) {
	db := newTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

func TestDevLoginCreatesOnceAndReadsBack(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	first, err := DevLogin(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	again, err := DevLogin(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != again.ID || first.Login != devLogin {
		t.Fatalf("accounts differ across DevLogin: %+v vs %+v", first, again)
	}
}

func TestCreateRoomBindsOwnerAndRowsThrough(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	owner, err := DevLogin(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	roomID, err := CreateRoom(ctx, db, owner.ID, "ops")
	if err != nil {
		t.Fatal(err)
	}
	if roomID == "" {
		t.Fatal("empty room id")
	}
	membership, err := MembershipOf(ctx, db, owner.ID, roomID)
	if err != nil {
		t.Fatal(err)
	}
	if membership.Role != RoleOwner {
		t.Fatalf("role = %s, want %s", membership.Role, RoleOwner)
	}
	rooms, err := RoomsOf(ctx, db, owner.ID)
	if err != nil || len(rooms) != 1 || rooms[0].ID != roomID {
		t.Fatalf("RoomsOf = %+v, %v", rooms, err)
	}
	if _, err := MembershipOf(ctx, db, owner.ID, "nope"); !errors.Is(err, ErrNotMember) {
		t.Fatalf("foreign room membership = %v", err)
	}
}

func TestIssueVerifyRevoke(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	owner, _ := DevLogin(ctx, db)
	roomID, err := CreateRoom(ctx, db, owner.ID, "ops")
	if err != nil {
		t.Fatal(err)
	}

	token, cred, err := Issue(ctx, db, roomID, "alice", CapRead+","+CapAppend, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(token) <= len(TokenPrefix) || cred.Hash != HashToken(token) {
		t.Fatalf("token/hash mismatch: %q %q", token, cred.Hash)
	}

	verified, err := Verify(ctx, db, token)
	if err != nil {
		t.Fatal(err)
	}
	if verified.ActorID != "alice" || !HasCaps(verified.Capabilities, CapRead, CapAppend) {
		t.Fatalf("verified = %+v", verified)
	}

	if err := Revoke(ctx, db, roomID, cred.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(ctx, db, token); !errors.Is(err, ErrCredentialRevoked) {
		t.Fatalf("revoked verify = %v", err)
	}
	if _, err := Verify(ctx, db, "tr_not-a-token"); !errors.Is(err, ErrCredentialUnknown) {
		t.Fatalf("unknown verify = %v", err)
	}
}

func TestExpiredCredentialIsRejected(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	owner, _ := DevLogin(ctx, db)
	roomID, _ := CreateRoom(ctx, db, owner.ID, "ops")

	now := time.Now().UTC()
	token, _, err := issue(ctx, db, roomID, "alice", CapRead, 30*time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(ctx, db, token); err != nil {
		t.Fatalf("fresh credential rejected: %v", err)
	}
	token2, _, err := issue(ctx, db, roomID, "alice", CapRead, 30*time.Minute, now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(ctx, db, token2); !errors.Is(err, ErrCredentialExpired) {
		t.Fatalf("expired credential = %v", err)
	}
}

func TestCapabilityParsingDropsUnknownTokens(t *testing.T) {
	caps := ParseCaps("read,explode,append,READ")
	if !caps[CapRead] || !caps[CapAppend] || caps["explode"] || caps["READ"] {
		t.Fatalf("parsed = %v", caps)
	}
	if HasCaps("read", CapAppend) || !HasCaps("read,append", CapRead, CapAppend) {
		t.Fatal("HasCaps disagrees with ParseCaps")
	}
}

func TestIssueRejectsEmptyOrUnknownCapabilities(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	owner, _ := DevLogin(ctx, db)
	roomID, _ := CreateRoom(ctx, db, owner.ID, "ops")

	for _, caps := range []string{"", "explode", ","} {
		if _, _, err := Issue(ctx, db, roomID, "alice", caps, 0); err == nil {
			t.Fatalf("caps %q accepted", caps)
		}
	}
}
