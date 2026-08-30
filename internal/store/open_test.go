// Backend selection tests: Open must return the backend its marker declares
// and reject every older layout loudly; Init must refuse an unknown backend
// and an existing room.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/williamfzc/tearoom/internal/core"
	"github.com/williamfzc/tearoom/internal/relational"
)

func TestOpenReturnsFileBackend(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(dir, BackendFile); err != nil {
		t.Fatal(err)
	}
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.(*File); !ok {
		t.Fatalf("file marker opened %T, want *File", st)
	}
	room, err := st.Room(context.Background())
	if err != nil || room.ID == "" {
		t.Fatalf("room = %+v, %v", room, err)
	}
}

func TestOpenReturnsSQLiteBackend(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(dir, BackendSQLite); err != nil {
		t.Fatal(err)
	}
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.(*relational.Store); !ok {
		t.Fatalf("sqlite marker opened %T, want *relational.Store", st)
	}
	// The dispatched store answers the full contract.
	ctx := context.Background()
	room, err := st.Room(ctx)
	if err != nil || room.ID == "" {
		t.Fatalf("room = %+v, %v", room, err)
	}
	if _, err := st.AdmitActor(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Append(ctx, "a", core.AppendRequest{Content: json.RawMessage(`"via dispatch"`)}); err != nil {
		t.Fatal(err)
	}
	events, err := st.Read(ctx, "a", 0, 0)
	if err != nil || len(events) != 1 || events[0].Seq != 1 {
		t.Fatalf("events = %+v, %v", events, err)
	}
}

func TestOpenRejectsOldLayouts(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, Dir)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		marker *marker
	}{
		{name: "markerless old file layout", marker: nil},
		{name: "pre-cutover backend marker", marker: &marker{Backend: "sqlite"}},
		{name: "format 1 marker", marker: &marker{Format: 1, Backend: "file"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
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

func TestInitRejectsUnknownBackend(t *testing.T) {
	if _, err := Init(t.TempDir(), Backend("oracle")); err == nil {
		t.Fatal("unknown backend accepted by Init")
	}
}

func TestInitRefusesExistingRoom(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(dir, BackendFile); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(dir, BackendFile); err == nil {
		t.Fatal("second Init over an existing room accepted")
	}
}

func TestFindWalksUpAndFailsOutsideARoom(t *testing.T) {
	dir := t.TempDir()
	if _, err := Find(dir); !errors.Is(err, core.ErrNoRoom) {
		t.Fatalf("Find outside a room = %v, want %v", err, core.ErrNoRoom)
	}
	nested := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(dir, BackendFile); err != nil {
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
