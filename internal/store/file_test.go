// File store tests: the shared core conformance battery plus layout,
// admission, and in-process concurrency invariants specific to the file
// backend.
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/williamfzc/tearoom/internal/core"
)

func newTestStore(t *testing.T) *File {
	t.Helper()
	st, err := Init(t.TempDir(), BackendFile)
	if err != nil {
		t.Fatal(err)
	}
	return st.(*File)
}

func admit(t *testing.T, file *File, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if _, err := file.AdmitActor(context.Background(), id); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFileStoreConformsToCoreContract(t *testing.T) {
	core.StoreContract(t, func(t *testing.T) core.Store {
		return newTestStore(t)
	})
}

func TestInitWritesFormat2Marker(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(dir, BackendFile); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, Dir, markerName))
	if err != nil {
		t.Fatal(err)
	}
	var m marker
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m.Format != 2 || m.Backend != string(BackendFile) {
		t.Fatalf("marker = %+v, want format 2 file", m)
	}
}

func TestLayoutHasNoCursorOrSessionState(t *testing.T) {
	file := newTestStore(t)
	admit(t, file, "a")
	ctx := context.Background()
	if _, err := file.Append(ctx, "a", core.AppendRequest{Content: json.RawMessage(`"x"`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Read(ctx, "a", 0, 0); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(file.Root())
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"store.json":   true,
		"room.json":    true,
		"actors.json":  true,
		"events.jsonl": true,
		"lock":         true,
	}
	for _, entry := range entries {
		if !allowed[entry.Name()] {
			t.Fatalf("unexpected file in room: %s", entry.Name())
		}
	}
}

func TestRoomIdentityFromDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "planning")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := Init(dir, BackendFile)
	if err != nil {
		t.Fatal(err)
	}
	room, err := st.Room(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if room.ID != "planning" {
		t.Fatalf("room id = %q, want %q", room.ID, "planning")
	}
}

func TestAdmitJoiningAgainReturnsExistingActor(t *testing.T) {
	file := newTestStore(t)
	first, err := file.AdmitActor(context.Background(), "planner")
	if err != nil {
		t.Fatal(err)
	}
	again, err := file.AdmitActor(context.Background(), "planner")
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != first.ID || !again.Joined.Equal(first.Joined) {
		t.Fatalf("re-join changed the actor: %+v then %+v", first, again)
	}
	actors, err := file.Actors(context.Background())
	if err != nil || len(actors) != 1 {
		t.Fatalf("actors = %+v, %v", actors, err)
	}
}

func TestAppendRejectsInvalidContent(t *testing.T) {
	file := newTestStore(t)
	admit(t, file, "a")
	ctx := context.Background()
	if _, err := file.Append(ctx, "a", core.AppendRequest{Content: json.RawMessage("")}); err == nil {
		t.Fatal("empty content accepted")
	}
	if _, err := file.Append(ctx, "a", core.AppendRequest{Content: json.RawMessage("{")}); err == nil {
		t.Fatal("invalid JSON content accepted")
	}
}

func TestConcurrentAppendIsDenseInProcess(t *testing.T) {
	file := newTestStore(t)
	const writers = 12
	ids := make([]string, writers)
	for i := range ids {
		ids[i] = fmt.Sprintf("actor-%d", i)
	}
	admit(t, file, ids...)

	var group sync.WaitGroup
	start := make(chan struct{})
	results := make(chan error, writers)
	for _, actor := range ids {
		group.Add(1)
		go func(actor string) {
			defer group.Done()
			<-start
			_, err := file.Append(context.Background(), actor, core.AppendRequest{
				Content: json.RawMessage(fmt.Sprintf(`"from %s"`, actor)),
			})
			results <- err
		}(actor)
	}
	close(start)
	group.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}

	events, err := file.Read(context.Background(), ids[0], 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != writers {
		t.Fatalf("events = %d, want %d", len(events), writers)
	}
	for i, event := range events {
		if event.Seq != uint64(i+1) {
			t.Fatalf("sequence not dense: event %d has seq %d", i, event.Seq)
		}
	}
}
