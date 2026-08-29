// File store contract tests cover the invariants every client receives.
package store

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/williamfzc/tearoom/internal/core"
)

func newTestStore(t *testing.T) *File {
	t.Helper()
	st, err := Init(t.TempDir(), "test", BackendFile)
	if err != nil {
		t.Fatal(err)
	}
	return st.(*File)
}

func join(t *testing.T, file *File, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if _, err := file.UpsertActor(context.Background(), id, ""); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAppendRejectsStaleCursor(t *testing.T) {
	file := newTestStore(t)
	join(t, file, "a", "b")
	ctx := context.Background()

	if _, err := file.AppendEvent(ctx, core.AppendInput{Actor: "a", Expected: 0, Type: "message", Body: "first", Key: "one"}); err != nil {
		t.Fatal(err)
	}
	_, err := file.AppendEvent(ctx, core.AppendInput{Actor: "b", Expected: 0, Type: "message", Body: "I thought it was empty", Key: "two"})
	var stale *core.StaleWriteError
	if !errors.As(err, &stale) {
		t.Fatalf("stale append error = %v", err)
	}
	if stale.Expected != 0 || stale.Actual != 1 {
		t.Fatalf("stale cursors = %d/%d", stale.Expected, stale.Actual)
	}
}

func TestIdempotentRetryReturnsOriginalEvent(t *testing.T) {
	file := newTestStore(t)
	join(t, file, "a")
	input := core.AppendInput{Actor: "a", Expected: 0, Type: "message", Body: "once", Key: "stable-key"}
	first, err := file.AppendEvent(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	input.Expected = first.Seq
	retry, err := file.AppendEvent(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if retry.ID != first.ID {
		t.Fatalf("retry created %s, want %s", retry.ID, first.ID)
	}
	feed, _ := file.Read(context.Background(), "a", 0, 0)
	if len(feed.Events) != 1 {
		t.Fatalf("events = %d, want 1", len(feed.Events))
	}
}

func TestPrivateEventVisibleOnlyToSenderAndRecipients(t *testing.T) {
	file := newTestStore(t)
	join(t, file, "seer", "wolf", "villager")
	event, err := file.AppendEvent(context.Background(), core.AppendInput{
		Actor: "seer", Expected: 0, Type: "role.assigned", Body: "wolf", To: []string{"wolf"}, Key: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{"seer", "wolf"} {
		feed, err := file.Read(context.Background(), actor, 0, 0)
		if err != nil || len(feed.Events) != 1 || feed.Events[0].ID != event.ID {
			t.Fatalf("%s did not see private event: %+v %v", actor, feed, err)
		}
	}
	feed, err := file.Read(context.Background(), "villager", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed.Events) != 0 || feed.Cursor != 1 {
		t.Fatalf("villager feed = %+v", feed)
	}
}

func TestConcurrentAppendHasOneWinnerPerCursor(t *testing.T) {
	file := newTestStore(t)
	const writers = 12
	ids := make([]string, writers)
	for i := range ids {
		ids[i] = string(rune('a' + i))
	}
	join(t, file, ids...)

	var group sync.WaitGroup
	start := make(chan struct{})
	results := make(chan error, writers)
	for i, actor := range ids {
		group.Add(1)
		go func(i int, actor string) {
			defer group.Done()
			<-start
			_, err := file.AppendEvent(context.Background(), core.AppendInput{
				Actor: actor, Expected: 0, Type: "message", Body: actor, Key: actor,
			})
			results <- err
		}(i, actor)
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
	file := newTestStore(t)
	if _, err := file.Read(context.Background(), "missing", 0, 0); !errors.Is(err, core.ErrUnknownActor) {
		t.Fatalf("read error = %v", err)
	}
	if _, err := file.AppendEvent(context.Background(), core.AppendInput{Actor: "missing", Type: "message", Body: "x", Key: "missing-actor"}); !errors.Is(err, core.ErrUnknownActor) {
		t.Fatalf("write error = %v", err)
	}
}
