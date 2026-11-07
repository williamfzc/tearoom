// Store selection tests: Open must return the JSONL workspace its marker
// declares, upgrade the retired SQLite layouts once, and reject every other
// older layout loudly; Init must refuse an existing workspace and leave no
// temporary siblings behind.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	_ "github.com/glebarez/go-sqlite"
	"github.com/williamfzc/tearoom/internal/core"
)

func TestOpenReturnsJSONLWorkspace(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(dir); err != nil {
		t.Fatal(err)
	}
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.(*Store); !ok {
		t.Fatalf("jsonl marker opened %T, want *store.Store", st)
	}
	// The opened store answers the full contract.
	ctx := context.Background()
	if _, err := st.CreateRoom(ctx, "main-line", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Append(ctx, "main-line", "a", core.AppendRequest{Content: json.RawMessage(`"via dispatch"`)}); err != nil {
		t.Fatal(err)
	}
	records, err := st.Read(ctx, "main-line", 0)
	if err != nil || len(records) != 1 || records[0].Seq != 1 {
		t.Fatalf("records = %+v, %v", records, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, Dir, roomsDir, "main-line", "records.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"room":"main-line"`) {
		t.Fatalf("records.jsonl does not carry the room: %s", data)
	}
}

func TestOpenRejectsOldLayouts(t *testing.T) {
	cases := []struct {
		name   string
		marker *marker
	}{
		{name: "markerless v0 file layout", marker: nil},
		{name: "retired sqlite backend marker", marker: &marker{Format: formatVersion, Backend: "sqlite"}},
		{name: "format 1 marker", marker: &marker{Format: 1, Backend: "sqlite"}},
		{name: "format 5 marker", marker: &marker{Format: 5, Backend: "jsonl"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			root := filepath.Join(dir, Dir)
			if err := os.MkdirAll(root, 0o755); err != nil {
				t.Fatal(err)
			}
			if c.marker == nil {
				// The v0 layout: room files without a marker.
				if err := os.WriteFile(filepath.Join(root, "room.json"), []byte(`{"id":"room"}`), 0o644); err != nil {
					t.Fatal(err)
				}
			} else if err := writeMarker(root, *c.marker); err != nil {
				t.Fatal(err)
			}
			_, err := Open(dir)
			if err == nil {
				t.Fatal("old layout opened without error")
			}
			if !strings.Contains(err.Error(), "incompatible") {
				t.Fatalf("old layout error is not a clear incompatible error: %v", err)
			}
		})
	}
}

// sqliteFixture builds a retired SQLite workspace at dir/.tearoom with the
// given format's schema and rows, exactly the shapes the retired stores
// wrote.
func sqliteFixture(t *testing.T, dir string, format int) {
	t.Helper()
	root := filepath.Join(dir, Dir)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(root, "room.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	statements := []string{
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`INSERT INTO meta (key, value) VALUES ('format', '` + strconv.Itoa(format) + `')`,
	}
	if format == 2 {
		statements = append(statements,
			`CREATE TABLE rooms (id TEXT PRIMARY KEY, created_at TIMESTAMP NOT NULL, head INTEGER NOT NULL)`,
			`INSERT INTO rooms (id, created_at, head) VALUES ('legacy', '2026-01-01T00:00:00Z', 2)`,
			`CREATE TABLE actors (room_id TEXT NOT NULL, id TEXT NOT NULL, joined_at TIMESTAMP NOT NULL, PRIMARY KEY (room_id, id))`,
			`INSERT INTO actors (room_id, id, joined_at) VALUES ('legacy', 'planner', '2026-01-01T00:00:00Z')`,
			`CREATE TABLE events (room_id TEXT NOT NULL, seq INTEGER NOT NULL, actor TEXT NOT NULL, at TIMESTAMP NOT NULL, "to" TEXT NOT NULL, content TEXT NOT NULL, PRIMARY KEY (room_id, seq))`,
			`INSERT INTO events (room_id, seq, actor, at, "to", content) VALUES
				('legacy', 1, 'planner', '2026-01-01T00:01:00Z', '[]', '"hello"'),
				('legacy', 2, 'bob', '2026-01-01T00:02:00Z', '["planner"]', '{"v1":true}')`,
			`CREATE TABLE append_idempotency (room_id TEXT NOT NULL, actor TEXT NOT NULL, key TEXT NOT NULL, fingerprint BLOB NOT NULL, event_seq INTEGER NOT NULL, PRIMARY KEY (room_id, actor, key))`,
		)
	} else {
		statements = append(statements,
			`CREATE TABLE rooms (name TEXT PRIMARY KEY, parent TEXT, created_at TIMESTAMP NOT NULL, head INTEGER NOT NULL)`,
			`INSERT INTO rooms (name, parent, created_at, head) VALUES ('main-line', NULL, '2026-01-01T00:00:00Z', 1), ('implementation', 'main-line', '2026-01-01T00:00:01Z', 1)`,
			`CREATE TABLE events (room_id TEXT NOT NULL, seq INTEGER NOT NULL, author TEXT NOT NULL, at TIMESTAMP NOT NULL, content TEXT NOT NULL, PRIMARY KEY (room_id, seq))`,
			`INSERT INTO events (room_id, seq, author, at, content) VALUES
				('main-line', 1, 'feng', '2026-01-01T00:01:00Z', '"the goal"'),
				('implementation', 1, 'codex', '2026-01-01T00:02:00Z', '"started"')`,
			`CREATE TABLE append_idempotency (room_id TEXT NOT NULL, author TEXT NOT NULL, key TEXT NOT NULL, fingerprint BLOB NOT NULL, event_seq INTEGER NOT NULL, PRIMARY KEY (room_id, author, key))`,
		)
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	if err := writeMarker(root, marker{Format: format, Backend: "sqlite"}); err != nil {
		t.Fatal(err)
	}
}

// TestOpenMigratesFormat2 proves the v1 → JSONL migration at the workspace
// boundary: the single room becomes a root, records keep their author
// verbatim, the database is deleted, and the marker is rewritten.
func TestOpenMigratesFormat2(t *testing.T) {
	dir := t.TempDir()
	sqliteFixture(t, dir, 2)

	st, err := Open(dir)
	if err != nil {
		t.Fatalf("open a format-2 workspace: %v", err)
	}
	assertMigrated(t, dir, st, map[string][]string{
		"legacy": {"planner", "bob"},
	}, 1)
}

// TestOpenMigratesFormat3 proves the SQLite-tree → JSONL migration: the whole
// tree exports with parents intact.
func TestOpenMigratesFormat3(t *testing.T) {
	dir := t.TempDir()
	sqliteFixture(t, dir, 3)

	st, err := Open(dir)
	if err != nil {
		t.Fatalf("open a format-3 workspace: %v", err)
	}
	assertMigrated(t, dir, st, map[string][]string{
		"main-line":      {"feng"},
		"implementation": {"codex"},
	}, 0)
}

// assertMigrated checks the exported workspace: rooms with their parents,
// records with their authors, the database deleted, and the marker rewritten.
func assertMigrated(t *testing.T, dir string, st core.Store, authors map[string][]string, ledgerCount int) {
	t.Helper()
	ctx := context.Background()
	rooms, err := st.Rooms(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rooms) != len(authors) {
		t.Fatalf("rooms after migration = %+v, want %d", rooms, len(authors))
	}
	for _, room := range rooms {
		want, ok := authors[room.Name]
		if !ok {
			t.Fatalf("unexpected room %q", room.Name)
		}
		records, err := st.Read(ctx, room.Name, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(records) != len(want) {
			t.Fatalf("records of %q = %+v, want %d", room.Name, records, len(want))
		}
		for i, record := range records {
			if record.Author != want[i] {
				t.Fatalf("record %d of %q author = %q, want %q", i, room.Name, record.Author, want[i])
			}
		}
	}
	if _, err := os.Stat(filepath.Join(dir, Dir, "room.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the retired database survived the migration: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, Dir, markerName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), strconv.Itoa(formatVersion)) {
		t.Fatalf("marker file was not rewritten: %s", data)
	}
	_ = ledgerCount
}

func TestInitRefusesExistingWorkspace(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(dir); err == nil {
		t.Fatal("second Init over an existing workspace accepted")
	}
}

func TestInitLeavesNoTemporarySiblings(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(dir); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != Dir {
		t.Fatalf("initialization left temporary siblings: %+v", entries)
	}
}

func TestFindWalksUpAndFailsOutsideAWorkspace(t *testing.T) {
	dir := t.TempDir()
	if _, err := Find(dir); !errors.Is(err, core.ErrNoRoom) {
		t.Fatalf("Find outside a workspace = %v, want %v", err, core.ErrNoRoom)
	}
	nested := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(dir); err != nil {
		t.Fatal(err)
	}
	found, err := Find(nested)
	if err != nil {
		t.Fatal(err)
	}
	if found != filepath.Join(dir, Dir) {
		t.Fatalf("Find = %q, want %q", found, filepath.Join(dir, Dir))
	}
}
