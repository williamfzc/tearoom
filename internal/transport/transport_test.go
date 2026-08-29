// Transport contract tests prove remote Store behavior matches local behavior.
package transport

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/williamfzc/tearoom/internal/core"
	"github.com/williamfzc/tearoom/internal/store"
)

func newPair(t *testing.T) (*Client, *store.File) {
	t.Helper()
	st, err := store.Init(t.TempDir(), "test", store.BackendFile)
	if err != nil {
		t.Fatal(err)
	}
	file := st.(*store.File)
	server := httptest.NewServer(NewServer(file).Handler())
	t.Cleanup(server.Close)
	return NewClient(server.URL, ""), file
}

func TestClientSatisfiesStore(t *testing.T) {
	var _ core.Store = (*Client)(nil)
	var _ core.Store = (*store.File)(nil)
}

func TestRoundTripAndVisibilityThroughHTTP(t *testing.T) {
	client, _ := newPair(t)
	ctx := context.Background()
	for _, actor := range []string{"a", "b", "c"} {
		if _, err := client.UpsertActor(ctx, actor, ""); err != nil {
			t.Fatal(err)
		}
	}
	event, err := client.AppendEvent(ctx, core.AppendInput{Actor: "a", Expected: 0, Type: "message", Body: "private", To: []string{"b"}, Key: "one"})
	if err != nil {
		t.Fatal(err)
	}
	if event.Seq != 1 {
		t.Fatalf("seq = %d", event.Seq)
	}
	for _, actor := range []string{"a", "b"} {
		feed, err := client.Read(ctx, actor, 0, 0)
		if err != nil || len(feed.Events) != 1 {
			t.Fatalf("%s feed = %+v, %v", actor, feed, err)
		}
	}
	feed, err := client.Read(ctx, "c", 0, 0)
	if err != nil || len(feed.Events) != 0 || feed.Cursor != 1 {
		t.Fatalf("c feed = %+v, %v", feed, err)
	}
}

func TestStaleWriteSurvivesTheWire(t *testing.T) {
	client, _ := newPair(t)
	ctx := context.Background()
	client.UpsertActor(ctx, "a", "")
	client.UpsertActor(ctx, "b", "")
	client.AppendEvent(ctx, core.AppendInput{Actor: "a", Expected: 0, Type: "message", Body: "first", Key: "one"})
	_, err := client.AppendEvent(ctx, core.AppendInput{Actor: "b", Expected: 0, Type: "message", Body: "stale", Key: "two"})
	if !errors.Is(err, core.ErrStaleWrite) || !errors.Is(err, core.ErrConflict) {
		t.Fatalf("wire error = %v", err)
	}
	var stale *core.StaleWriteError
	if !errors.As(err, &stale) || stale.Actual != 1 {
		t.Fatalf("typed stale error = %#v", err)
	}
}

func TestLongPollReturnsWhenEventAppends(t *testing.T) {
	client, _ := newPair(t)
	ctx := context.Background()
	for _, actor := range []string{"a", "b"} {
		if _, err := client.UpsertActor(ctx, actor, ""); err != nil {
			t.Fatal(err)
		}
	}
	got := make(chan *core.Feed, 1)
	go func() {
		feed, err := client.ReadWait(ctx, "b", 0, 0, 5*time.Second)
		if err != nil {
			t.Error(err)
			return
		}
		got <- feed
	}()
	time.Sleep(150 * time.Millisecond)
	if _, err := client.AppendEvent(ctx, core.AppendInput{Actor: "a", Expected: 0, Type: "message", Body: "wake", Key: "wake-key"}); err != nil {
		t.Fatal(err)
	}
	select {
	case feed := <-got:
		if feed.Cursor != 1 || len(feed.Events) != 1 || feed.Events[0].Body != "wake" {
			t.Fatalf("feed = %+v", feed)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("long-poll did not return after an append")
	}
}

func TestLongPollReturnsEmptyFeedAfterWait(t *testing.T) {
	client, _ := newPair(t)
	ctx := context.Background()
	if _, err := client.UpsertActor(ctx, "a", ""); err != nil {
		t.Fatal(err)
	}
	before := time.Now()
	feed, err := client.ReadWait(ctx, "a", 0, 0, 250*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if feed.Cursor != 0 || len(feed.Events) != 0 {
		t.Fatalf("feed = %+v", feed)
	}
	if elapsed := time.Since(before); elapsed < 200*time.Millisecond {
		t.Fatalf("empty long-poll returned too early: %s", elapsed)
	}
}

func TestLongPollShortCircuitsOnAvailableEvents(t *testing.T) {
	client, _ := newPair(t)
	ctx := context.Background()
	if _, err := client.UpsertActor(ctx, "a", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := client.AppendEvent(ctx, core.AppendInput{Actor: "a", Expected: 0, Type: "message", Body: "already", Key: "one"}); err != nil {
		t.Fatal(err)
	}
	before := time.Now()
	feed, err := client.ReadWait(ctx, "a", 0, 0, 5*time.Second)
	if err != nil || len(feed.Events) != 1 {
		t.Fatalf("feed = %+v, err = %v", feed, err)
	}
	if elapsed := time.Since(before); elapsed > time.Second {
		t.Fatalf("short circuit took %s", elapsed)
	}
}

func TestLongPollPicksUpOutOfBandAppend(t *testing.T) {
	client, file := newPair(t)
	ctx := context.Background()
	for _, actor := range []string{"a", "b"} {
		if _, err := client.UpsertActor(ctx, actor, ""); err != nil {
			t.Fatal(err)
		}
	}
	got := make(chan *core.Feed, 1)
	go func() {
		feed, err := client.ReadWait(ctx, "b", 0, 0, 4*time.Second)
		if err != nil {
			t.Error(err)
			return
		}
		got <- feed
	}()
	time.Sleep(150 * time.Millisecond)
	// A direct file append bypasses the server's broadcaster and is only
	// observable through the idle fallback probe.
	if _, err := file.AppendEvent(ctx, core.AppendInput{Actor: "a", Expected: 0, Type: "message", Body: "direct", Key: "direct-key"}); err != nil {
		t.Fatal(err)
	}
	select {
	case feed := <-got:
		if len(feed.Events) != 1 || feed.Events[0].Body != "direct" {
			t.Fatalf("feed = %+v", feed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("long-poll missed an out-of-band append")
	}
}

func TestUnreachableServerFailsLoudly(t *testing.T) {
	client := NewClient("http://127.0.0.1:1", "")
	client.HTTP.Timeout = 100 * time.Millisecond
	_, err := client.Room(context.Background())
	if err == nil || !contains(err.Error(), "unreachable") {
		t.Fatalf("error = %v", err)
	}
}

func contains(value, part string) bool {
	return len(value) >= len(part) && (value == part || containsAt(value, part))
}
func containsAt(value, part string) bool {
	for i := 0; i+len(part) <= len(value); i++ {
		if value[i:i+len(part)] == part {
			return true
		}
	}
	return false
}
