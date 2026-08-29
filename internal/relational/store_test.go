// The relational Store must answer the same contract as the file Store:
// these tests mirror internal/store/file_test.go against SQLite.
package relational

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/williamfzc/tearoom/internal/core"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Init(t.TempDir(), "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.db.Migrator().DropTable("events", "actors", "rooms") })
	return store
}

func join(t *testing.T, store *Store, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if _, err := store.UpsertActor(context.Background(), id, ""); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAppendRejectsStaleCursor(t *testing.T) {
	store := newTestStore(t)
	join(t, store, "a", "b")
	ctx := context.Background()

	if _, err := store.AppendEvent(ctx, core.AppendInput{Actor: "a", Expected: 0, Type: "message", Body: "first", Key: "one"}); err != nil {
		t.Fatal(err)
	}
	_, err := store.AppendEvent(ctx, core.AppendInput{Actor: "b", Expected: 0, Type: "message", Body: "I thought it was empty", Key: "two"})
	var stale *core.StaleWriteError
	if !errors.As(err, &stale) {
		t.Fatalf("stale append error = %v", err)
	}
	if stale.Expected != 0 || stale.Actual != 1 {
		t.Fatalf("stale cursors = %d/%d", stale.Expected, stale.Actual)
	}
}

func TestIdempotentRetryReturnsOriginalEvent(t *testing.T) {
	store := newTestStore(t)
	join(t, store, "a")
	input := core.AppendInput{Actor: "a", Expected: 0, Type: "message", Body: "once", Key: "stable-key"}
	first, err := store.AppendEvent(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	input.Expected = first.Seq
	retry, err := store.AppendEvent(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if retry.ID != first.ID {
		t.Fatalf("retry created %s, want %s", retry.ID, first.ID)
	}
	feed, _ := store.Read(context.Background(), "a", 0, 0)
	if len(feed.Events) != 1 {
		t.Fatalf("events = %d, want 1", len(feed.Events))
	}
}

func TestIdempotencyKeyReuseForOtherContentConflicts(t *testing.T) {
	store := newTestStore(t)
	join(t, store, "a")
	ctx := context.Background()
	if _, err := store.AppendEvent(ctx, core.AppendInput{Actor: "a", Expected: 0, Type: "message", Body: "first", Key: "key-1"}); err != nil {
		t.Fatal(err)
	}
	_, err := store.AppendEvent(ctx, core.AppendInput{Actor: "a", Expected: 1, Type: "message", Body: "different", Key: "key-1"})
	if !errors.Is(err, core.ErrConflict) {
		t.Fatalf("conflict error = %v", err)
	}
}

func TestPrivateEventVisibleOnlyToSenderAndRecipients(t *testing.T) {
	store := newTestStore(t)
	join(t, store, "seer", "wolf", "villager")
	event, err := store.AppendEvent(context.Background(), core.AppendInput{
		Actor: "seer", Expected: 0, Type: "role.assigned", Body: "wolf", To: []string{"wolf"}, Key: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{"seer", "wolf"} {
		feed, err := store.Read(context.Background(), actor, 0, 0)
		if err != nil || len(feed.Events) != 1 || feed.Events[0].ID != event.ID {
			t.Fatalf("%s did not see private event: %+v %v", actor, feed, err)
		}
	}
	feed, err := store.Read(context.Background(), "villager", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed.Events) != 0 || feed.Cursor != 1 {
		t.Fatalf("villager feed = %+v", feed)
	}
}

func TestConcurrentAppendHasOneWinnerPerCursor(t *testing.T) {
	store := newTestStore(t)
	const writers = 12
	ids := make([]string, writers)
	for i := range ids {
		ids[i] = string(rune('a' + i))
	}
	join(t, store, ids...)

	var group sync.WaitGroup
	start := make(chan struct{})
	results := make(chan error, writers)
	for _, actor := range ids {
		group.Add(1)
		go func(actor string) {
			defer group.Done()
			<-start
			_, err := store.AppendEvent(context.Background(), core.AppendInput{
				Actor: actor, Expected: 0, Type: "message", Body: actor, Key: actor,
			})
			results <- err
		}(actor)
	}
	close(start)
	group.Wait()
	close(results)

	wins, stale := 0, 0
	for err := range results {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, core.ErrStaleWrite):
			stale++
		default:
			t.Fatalf("unexpected result: %v", err)
		}
	}
	if wins != 1 || stale != writers-1 {
		t.Fatalf("wins=%d stale=%d", wins, stale)
	}
}

func TestUnknownActorCannotReadOrWrite(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.Read(context.Background(), "missing", 0, 0); !errors.Is(err, core.ErrUnknownActor) {
		t.Fatalf("read error = %v", err)
	}
	if _, err := store.AppendEvent(context.Background(), core.AppendInput{Actor: "missing", Type: "message", Body: "x", Key: "missing-actor"}); !errors.Is(err, core.ErrUnknownActor) {
		t.Fatalf("write error = %v", err)
	}
}

func TestChangedProbeTracksAppends(t *testing.T) {
	store := newTestStore(t)
	join(t, store, "a")
	ctx := context.Background()
	changed, err := store.Changed(ctx)
	if err != nil || changed {
		t.Fatalf("empty store Changed = %v, %v", changed, err)
	}
	changed, err = store.Changed(ctx)
	if err != nil || changed {
		t.Fatalf("quiet store Changed = %v, %v", changed, err)
	}
	if _, err := store.AppendEvent(ctx, core.AppendInput{Actor: "a", Expected: 0, Type: "message", Body: "x", Key: "one"}); err != nil {
		t.Fatal(err)
	}
	changed, err = store.Changed(ctx)
	if err != nil || !changed {
		t.Fatalf("store with one append Changed = %v, %v", changed, err)
	}
}
