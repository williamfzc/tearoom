// Backend selection tests: Open must return the implementation the marker
// declares, and Init must refuse an unknown backend loudly.
package store

import (
	"context"
	"testing"

	"github.com/williamfzc/tearoom/internal/relational"
)

func TestOpenReturnsBackendFromMarker(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(dir, "sqlite-room", BackendSQLite); err != nil {
		t.Fatal(err)
	}
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.(*relational.Store); !ok {
		t.Fatalf("sqlite marker opened %T, want *relational.Store", st)
	}
	if room, err := st.Room(context.Background()); err != nil || room.Name != "sqlite-room" {
		t.Fatalf("room = %+v, %v", room, err)
	}
}

func TestOpenDefaultsToFileWithoutMarker(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(dir, "file-room", BackendFile); err != nil {
		t.Fatal(err)
	}
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.(*File); !ok {
		t.Fatalf("markerless room opened %T, want *File", st)
	}
}

func TestOpenRejectsUnknownBackend(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(dir, "x", BackendSQLite); err != nil {
		t.Fatal(err)
	}
	root, err := Find(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeMarker(root, marker{Backend: "oracle"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("unknown backend marker opened without error")
	}
}

func TestInitRejectsUnknownBackend(t *testing.T) {
	if _, err := Init(t.TempDir(), "x", Backend("oracle")); err == nil {
		t.Fatal("unknown backend accepted by Init")
	}
}
