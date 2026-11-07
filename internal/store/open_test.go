// Workspace selection tests: Open must return the sqlite workspace its
// marker declares, upgrade a format-2 workspace once, and reject every other
// older layout loudly; Init must refuse an existing workspace and leave no
// temporary siblings behind.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/williamfzc/tearoom/internal/core"
	"github.com/williamfzc/tearoom/internal/relational"
)

func TestOpenReturnsSQLiteWorkspace(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(dir); err != nil {
		t.Fatal(err)
	}
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.(*relational.Store); !ok {
		t.Fatalf("sqlite marker opened %T, want *relational.Store", st)
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
}

func TestOpenRejectsOldLayouts(t *testing.T) {
	cases := []struct {
		name   string
		marker *marker
	}{
		{name: "markerless old layout", marker: nil},
		{name: "retired file backend marker", marker: &marker{Format: formatVersion, Backend: "file"}},
		{name: "format 1 marker", marker: &marker{Format: 1, Backend: "sqlite"}},
		{name: "format 4 marker", marker: &marker{Format: 4, Backend: "sqlite"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			root := filepath.Join(dir, Dir)
			if err := os.MkdirAll(root, 0o755); err != nil {
				t.Fatal(err)
			}
			// A relational database backs every case, so the refusal comes
			// from the marker and not from a missing database.
			if _, err := relational.Init(root); err != nil {
				t.Fatal(err)
			}
			if c.marker == nil {
				if err := os.Remove(filepath.Join(root, markerName)); err != nil && !errors.Is(err, os.ErrNotExist) {
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

// TestOpenUpgradesFormat2WorkspaceOnce proves the one-time v1 → tree
// migration at the workspace boundary: the old single room becomes a root,
// its records keep their signatures, and the marker file is rewritten.
func TestOpenUpgradesFormat2WorkspaceOnce(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, Dir)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := relational.Init(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := st.CreateRoom(ctx, "v1-room", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Append(ctx, "v1-room", "planner", core.AppendRequest{Content: json.RawMessage(`"work"`)}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	// Roll the workspace back to the v1 declaration.
	if err := writeMarker(root, marker{Format: 2, Backend: "sqlite"}); err != nil {
		t.Fatal(err)
	}

	upgraded, err := Open(dir)
	if err != nil {
		t.Fatalf("open a format-2 workspace: %v", err)
	}
	records, err := upgraded.Read(ctx, "v1-room", 0)
	if err != nil || len(records) != 1 || records[0].Author != "planner" {
		t.Fatalf("records after upgrade = %+v, %v", records, err)
	}
	rooms, err := upgraded.Rooms(ctx)
	if err != nil || len(rooms) != 1 || rooms[0].Parent != "" {
		t.Fatalf("rooms after upgrade = %+v, %v", rooms, err)
	}
	data, err := os.ReadFile(filepath.Join(root, markerName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), fmt.Sprintf("%d", formatVersion)) {
		t.Fatalf("marker file was not rewritten: %s", data)
	}
	if closer, ok := upgraded.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil {
			t.Fatal(err)
		}
	}

	// The upgrade is one-time: the next open reads the current format.
	again, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen after upgrade: %v", err)
	}
	if closer, ok := again.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil {
			t.Fatal(err)
		}
	}
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
