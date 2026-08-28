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
	file, err := store.Init(t.TempDir(), "test")
	if err != nil {
		t.Fatal(err)
	}
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
