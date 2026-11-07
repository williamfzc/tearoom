// The relational Store must answer the same contract as every other backend:
// the core conformance battery runs against SQLite here and against
// PostgreSQL in pg_test.go. Backend-specific gates the battery does not
// cover — the format gate, the one-time format-2 migration, and input
// validation — are proven on top.
package relational

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/williamfzc/tearoom/internal/core"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestSQLiteStoreContract(t *testing.T) {
	core.StoreContract(t, func(t *testing.T) core.Store {
		return newTestStore(t)
	})
}

func TestInitRefusesExistingDatabase(t *testing.T) {
	root := t.TempDir()
	if _, err := Init(root); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(root); err == nil {
		t.Fatal("second Init over an existing database accepted")
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
	st, err := Init(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.db.Model(&MetaRow{}).Where("key = ?", formatKey).Update("value", "1").Error; err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
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
	if _, err := New(db); err == nil || !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("New ungated database error = %v", err)
	}
}

func TestSchemaIsOnlyTheFormatThreeShape(t *testing.T) {
	st := newTestStore(t)
	tables, err := st.db.Migrator().GetTables()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"meta": true, "rooms": true, "events": true, "append_idempotency": true,
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
	wantColumns := []string{"room_id", "seq", "author", "at", "content"}
	if strings.Join(names, ",") != strings.Join(wantColumns, ",") {
		t.Fatalf("events columns = %v, want %v", names, wantColumns)
	}
}

func TestAppendRejectsInvalidContent(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if _, err := st.CreateRoom(ctx, "room", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Append(ctx, "room", "a", core.AppendRequest{Content: json.RawMessage("")}); err == nil {
		t.Fatal("empty content accepted")
	}
	if _, err := st.Append(ctx, "room", "a", core.AppendRequest{Content: json.RawMessage("{")}); err == nil {
		t.Fatal("invalid JSON content accepted")
	}
}

func TestReadCorruptContentFailsClosed(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.CreateRoom(context.Background(), "room", ""); err != nil {
		t.Fatal(err)
	}
	row := EventRow{RoomID: "room", Seq: 1, Author: "a", At: time.Now().UTC(), Content: "{"}
	if err := st.db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := st.Read(context.Background(), "room", 0); err == nil {
		t.Fatal("corrupt content was accepted")
	}
}

// TestOpenMigratesFormat2 proves the one-time v1 → tree migration: the old
// single room becomes a root named by its old identifier, records keep their
// author verbatim and lose the recipient column, the actor and credential
// tables are dropped, and the ledger survives.
func TestOpenMigratesFormat2(t *testing.T) {
	root := t.TempDir()
	db, err := openSQLite(filepath.Join(root, "room.db"))
	if err != nil {
		t.Fatal(err)
	}
	// The ledger row carries the fingerprint the original append computed,
	// so the retry below resolves to the original record instead of
	// conflicting.
	fingerprint := hex.EncodeToString(core.Fingerprint("planner", json.RawMessage(`"hello"`)))
	statements := []string{
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`INSERT INTO meta (key, value) VALUES ('format', '2')`,
		`CREATE TABLE rooms (id TEXT PRIMARY KEY, created_at TIMESTAMP NOT NULL, head BIGINT NOT NULL)`,
		`INSERT INTO rooms (id, created_at, head) VALUES ('legacy', '2026-01-01T00:00:00Z', 2)`,
		`CREATE TABLE actors (room_id TEXT NOT NULL, id TEXT NOT NULL, joined_at TIMESTAMP NOT NULL, PRIMARY KEY (room_id, id))`,
		`INSERT INTO actors (room_id, id, joined_at) VALUES ('legacy', 'planner', '2026-01-01T00:00:00Z')`,
		`CREATE TABLE credentials (id TEXT PRIMARY KEY, room_id TEXT NOT NULL, actor TEXT NOT NULL, token_hash TEXT NOT NULL, expires_at TIMESTAMP NOT NULL, created_at TIMESTAMP NOT NULL)`,
		`CREATE TABLE events (room_id TEXT NOT NULL, seq BIGINT NOT NULL, actor TEXT NOT NULL, at TIMESTAMP NOT NULL, "to" TEXT NOT NULL, content TEXT NOT NULL, PRIMARY KEY (room_id, seq))`,
		`INSERT INTO events (room_id, seq, actor, at, "to", content) VALUES
			('legacy', 1, 'planner', '2026-01-01T00:01:00Z', '[]', '"hello"'),
			('legacy', 2, 'bob', '2026-01-01T00:02:00Z', '["planner"]', '"hi"')`,
		`CREATE TABLE append_idempotency (room_id TEXT NOT NULL, actor TEXT NOT NULL, key TEXT NOT NULL, fingerprint BLOB NOT NULL, event_seq BIGINT NOT NULL, PRIMARY KEY (room_id, actor, key))`,
		`INSERT INTO append_idempotency (room_id, actor, key, fingerprint, event_seq) VALUES ('legacy', 'planner', 'k1', x'` + fingerprint + `', 1)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	if sqlDB, err := db.DB(); err != nil {
		t.Fatal(err)
	} else if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}

	// Opening the format-2 workspace performs the one-time upgrade.
	st, err := Open(root)
	if err != nil {
		t.Fatalf("open a format-2 workspace: %v", err)
	}
	defer st.Close()

	// The old room is a parentless root named by its old identifier.
	rooms, err := st.Rooms(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rooms) != 1 || rooms[0].Name != "legacy" || rooms[0].Parent != "" {
		t.Fatalf("rooms after migration = %+v, want the legacy room as a root", rooms)
	}

	// Records keep their author verbatim; the recipient column is gone.
	records, err := st.Read(context.Background(), "legacy", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("records after migration = %+v, want both", records)
	}
	if records[0].Author != "planner" || records[1].Author != "bob" || records[1].Seq != 2 {
		t.Fatalf("authors not preserved: %+v", records)
	}

	// The ledger survives so retries still resolve after the upgrade.
	retry, err := st.Append(context.Background(), "legacy", "planner",
		core.AppendRequest{IdempotencyKey: "k1", Content: json.RawMessage(`"hello"`)})
	if err != nil {
		t.Fatalf("ledger did not survive the migration: %v", err)
	}
	if retry.Seq != 1 {
		t.Fatalf("retry resolved to seq %d, want the original 1", retry.Seq)
	}

	// The migration is one-time: the dropped tables stay gone.
	tables, err := st.db.Migrator().GetTables()
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		if table == "actors" || table == "credentials" || strings.HasSuffix(table, "_v3") {
			t.Fatalf("migration left table %q behind", table)
		}
	}

	// Reopening sees the current format directly.
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := Open(root)
	if err != nil {
		t.Fatalf("reopen after migration: %v", err)
	}
	if err := again.Close(); err != nil {
		t.Fatal(err)
	}
}
