// The relational Store must answer the same contract as the file Store: the
// core conformance battery runs against SQLite here and against PostgreSQL in
// pg_test.go. Backend-specific gates the battery does not cover — the format
// gate, the schema shape, and input validation — are proven on top.
package relational

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/williamfzc/tearoom/internal/core"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Init(t.TempDir(), "planner")
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func admit(t *testing.T, st *Store, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if _, err := st.AdmitActor(context.Background(), id); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSQLiteStoreContract(t *testing.T) {
	core.StoreContract(t, func(t *testing.T) core.Store {
		return newTestStore(t)
	})
}

func TestInitRefusesExistingDatabase(t *testing.T) {
	root := t.TempDir()
	if _, err := Init(root, "planner"); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(root, "planner"); err == nil {
		t.Fatal("second Init over an existing database accepted")
	}
}

func TestRoomIdentityFromDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "planning")
	root := filepath.Join(dir, ".tearoom")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := Init(root, "planner")
	if err != nil {
		t.Fatal(err)
	}
	room, err := st.Room(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if room.ID != "planning" || room.Created.IsZero() {
		t.Fatalf("room = %+v, want id planning with a creation time", room)
	}
}

func TestInitAdmitsCreatorAtomically(t *testing.T) {
	st, err := Init(t.TempDir(), "planner")
	if err != nil {
		t.Fatal(err)
	}
	actors, err := st.Actors(context.Background())
	if err != nil || len(actors) != 1 || actors[0].ID != "planner" {
		t.Fatalf("initial actors = %+v, %v", actors, err)
	}
}

func TestOpenMissingDatabaseIsNoRoom(t *testing.T) {
	if _, err := Open(t.TempDir()); !errors.Is(err, core.ErrNoRoom) {
		t.Fatalf("Open on an empty root = %v, want %v", err, core.ErrNoRoom)
	}
}

// TestOpenRefusesPreCutoverDatabase simulates the old layout: a schema_versions
// bookkeeping table and no format marker. It must be refused without being
// read or migrated.
func TestOpenRefusesPreCutoverDatabase(t *testing.T) {
	root := t.TempDir()
	db, err := openSQLite(filepath.Join(root, "room.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE schema_versions (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO schema_versions (version, applied_at) VALUES (1, '2026-01-01T00:00:00Z')`).Error; err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = Open(root)
	if err == nil {
		t.Fatal("pre-cutover database opened without error")
	}
	message := err.Error()
	if !strings.Contains(message, "incompatible") || !strings.Contains(message, "no automatic upgrade") {
		t.Fatalf("refusal is not a clear incompatible error: %v", err)
	}
}

func TestOpenRefusesWrongFormatMarker(t *testing.T) {
	root := t.TempDir()
	st, err := Init(root, "planner")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.db.Model(&MetaRow{}).Where("key = ?", formatKey).Update("value", "1").Error; err != nil {
		t.Fatal(err)
	}
	_, err = Open(root)
	if err == nil {
		t.Fatal("database with a wrong format marker opened without error")
	}
	if !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("wrong-format refusal is not a clear incompatible error: %v", err)
	}
}

func TestMigrateRefusesWrongMarkerBeforeMutation(t *testing.T) {
	root := t.TempDir()
	db, err := openSQLite(filepath.Join(root, "room.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&MetaRow{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&MetaRow{Key: formatKey, Value: "1"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err == nil || !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("Migrate wrong marker error = %v", err)
	}
	if db.Migrator().HasTable(&EventRow{}) {
		t.Fatal("Migrate mutated schema after rejecting wrong marker")
	}
}

func TestNewRefusesUngatedDatabase(t *testing.T) {
	root := t.TempDir()
	db, err := openSQLite(filepath.Join(root, "room.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(db, "room"); err == nil || !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("New ungated database error = %v", err)
	}
}

func TestSchemaIsOnlyTheFormatTwoShape(t *testing.T) {
	st := newTestStore(t)
	tables, err := st.db.Migrator().GetTables()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"meta": true, "rooms": true, "actors": true,
		"events": true, "append_idempotency": true,
	}
	if len(tables) != len(want) {
		t.Fatalf("tables = %v, want only %v", tables, want)
	}
	for _, table := range tables {
		if !want[table] {
			t.Fatalf("unexpected table %q", table)
		}
	}
	// The old Event shape must not exist: PRAGMA table_info names the real
	// columns, where a word like "key" cannot be confused with "PRIMARY KEY".
	var columns []struct {
		Name string `gorm:"column:name"`
	}
	if err := st.db.Raw("PRAGMA table_info(events)").Scan(&columns).Error; err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(columns))
	for _, column := range columns {
		names = append(names, column.Name)
	}
	wantColumns := []string{"room_id", "seq", "actor", "at", "to", "content"}
	if strings.Join(names, ",") != strings.Join(wantColumns, ",") {
		t.Fatalf("events columns = %v, want %v", names, wantColumns)
	}
}

func TestAppendRejectsInvalidContent(t *testing.T) {
	st := newTestStore(t)
	admit(t, st, "a")
	ctx := context.Background()
	if _, err := st.Append(ctx, "a", core.AppendRequest{Content: json.RawMessage("")}); err == nil {
		t.Fatal("empty content accepted")
	}
	if _, err := st.Append(ctx, "a", core.AppendRequest{Content: json.RawMessage("{")}); err == nil {
		t.Fatal("invalid JSON content accepted")
	}
}

func TestAppendRejectsEmptyActor(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.Append(context.Background(), "  ", core.AppendRequest{Content: json.RawMessage(`"x"`)}); err == nil {
		t.Fatal("blank actor accepted")
	}
}

func TestReadCorruptRecipientsFailsClosed(t *testing.T) {
	st := newTestStore(t)
	admit(t, st, "planner")
	row := EventRow{
		RoomID: st.roomID, Seq: 1, Actor: "planner",
		At: time.Now().UTC(), To: "[", Content: `"bad"`,
	}
	if err := st.db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := st.Read(context.Background(), "planner", 0, 0); err == nil {
		t.Fatal("corrupt recipient JSON was accepted")
	}
}

func TestReadCorruptEventMembershipFailsClosed(t *testing.T) {
	tests := map[string]EventRow{
		"ghost author": {
			Seq: 1, Actor: "ghost", At: time.Now().UTC(), To: "[]", Content: `"ok"`,
		},
		"ghost recipient": {
			Seq: 1, Actor: "planner", At: time.Now().UTC(), To: `["ghost"]`, Content: `"ok"`,
		},
	}
	for name, row := range tests {
		t.Run(name, func(t *testing.T) {
			st := newTestStore(t)
			row.RoomID = st.roomID
			if err := st.db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := st.Read(context.Background(), "planner", 0, 0); err == nil {
				t.Fatal("event with a non-member identity was accepted")
			}
		})
	}
}
