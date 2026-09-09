// The Store conformance battery: one executable statement of this package's
// storage contract, run by every Store implementation from its own tests.
package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
)

// StoreContract runs the conformance battery every Store implementation must
// pass: admission, dense ordered append, membership visibility, public To
// addressing, caller-owned read boundaries, and per-request idempotency.
// newStore returns a fresh initialized room for each subtest. Store
// implementations run this battery from their own test suites.
func StoreContract(t *testing.T, newStore func(t *testing.T) Store) {
	t.Helper()
	ctx := context.Background()

	t.Run("Room", func(t *testing.T) {
		st := newStore(t)
		room, err := st.Room(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if room == nil || room.ID == "" {
			t.Fatalf("room identity missing: %+v", room)
		}
		if room.Created.IsZero() {
			t.Fatalf("room creation time missing: %+v", room)
		}
	})

	t.Run("AdmitActor", func(t *testing.T) {
		st := newStore(t)
		actor, err := st.AdmitActor(ctx, "planner")
		if err != nil {
			t.Fatal(err)
		}
		if actor.ID != "planner" || actor.Joined.IsZero() {
			t.Fatalf("admitted actor = %+v", actor)
		}
		again, err := st.AdmitActor(ctx, "planner")
		if err != nil {
			t.Fatal(err)
		}
		if again.ID != actor.ID || !again.Joined.Equal(actor.Joined) {
			t.Fatalf("re-joining changed the actor: %+v then %+v", actor, again)
		}
		actors, err := st.Actors(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(actors) != 1 || actors[0].ID != "planner" {
			t.Fatalf("actors = %+v, want exactly planner", actors)
		}
	})

	t.Run("UnjoinedActorRejected", func(t *testing.T) {
		st := newStore(t)
		if _, err := st.Read(ctx, "stranger", 0); !errors.Is(err, ErrUnknownActor) {
			t.Fatalf("unjoined read error = %v, want %v", err, ErrUnknownActor)
		}
		if _, err := st.Append(ctx, "stranger", AppendRequest{Content: json.RawMessage(`"hi"`)}); !errors.Is(err, ErrUnknownActor) {
			t.Fatalf("unjoined append error = %v, want %v", err, ErrUnknownActor)
		}
	})

	t.Run("UnjoinedRecipientRejected", func(t *testing.T) {
		st := newStore(t)
		admit(t, st, "sender")
		if _, err := st.Append(ctx, "sender", AppendRequest{
			To: []string{"missing"}, Content: json.RawMessage(`"hi"`),
		}); !errors.Is(err, ErrUnknownActor) {
			t.Fatalf("unknown recipient error = %v, want %v", err, ErrUnknownActor)
		}
		if events := readAll(t, st, "sender", 0); len(events) != 0 {
			t.Fatalf("unknown recipient appended events: %+v", events)
		}
	})

	t.Run("DenseSequence", func(t *testing.T) {
		st := newStore(t)
		admit(t, st, "a", "b")
		for i := 1; i <= 3; i++ {
			event, err := st.Append(ctx, "a", AppendRequest{Content: json.RawMessage(`"e"`)})
			if err != nil {
				t.Fatal(err)
			}
			if event.Seq != uint64(i) {
				t.Fatalf("seq = %d, want %d", event.Seq, i)
			}
		}
		events := readAll(t, st, "b", 0)
		if len(events) != 3 {
			t.Fatalf("events = %d, want 3", len(events))
		}
		for i, event := range events {
			if event.Seq != uint64(i+1) {
				t.Fatalf("history not dense: event %d has seq %d", i, event.Seq)
			}
		}
	})

	t.Run("ConcurrentAppendDense", func(t *testing.T) {
		st := newStore(t)
		const writers = 8
		ids := make([]string, writers)
		for i := range ids {
			ids[i] = fmt.Sprintf("w%d", i)
		}
		admit(t, st, ids...)
		var group sync.WaitGroup
		results := make(chan error, writers)
		for _, id := range ids {
			group.Add(1)
			go func(id string) {
				defer group.Done()
				_, err := st.Append(ctx, id, AppendRequest{Content: json.RawMessage(fmt.Sprintf(`"from %s"`, id))})
				results <- err
			}(id)
		}
		group.Wait()
		close(results)
		for err := range results {
			if err != nil {
				t.Fatalf("concurrent append: %v", err)
			}
		}
		events := readAll(t, st, ids[0], 0)
		if len(events) != writers {
			t.Fatalf("events = %d, want %d", len(events), writers)
		}
		for i, event := range events {
			if event.Seq != uint64(i+1) {
				t.Fatalf("concurrent sequence not dense: event %d has seq %d", i, event.Seq)
			}
		}
	})

	t.Run("MembershipVisibility", func(t *testing.T) {
		st := newStore(t)
		admit(t, st, "planner", "bob", "carol")
		if _, err := st.Append(ctx, "planner", AppendRequest{To: []string{"bob"}, Content: json.RawMessage(`{"plan":1}`)}); err != nil {
			t.Fatal(err)
		}
		if _, err := st.Append(ctx, "bob", AppendRequest{Content: json.RawMessage(`"ok"`)}); err != nil {
			t.Fatal(err)
		}
		for _, member := range []string{"planner", "bob", "carol"} {
			events := readAll(t, st, member, 0)
			if len(events) != 2 {
				t.Fatalf("%s sees %d events, want 2: %+v", member, len(events), events)
			}
		}
		// To is public addressing: an event addressed to bob is visible to carol.
		carol := readAll(t, st, "carol", 0)
		if !bytes.Equal(carol[0].Content, json.RawMessage(`{"plan":1}`)) {
			t.Fatalf("carol missed the event addressed to bob: %+v", carol)
		}
	})

	t.Run("ReadBoundary", func(t *testing.T) {
		st := newStore(t)
		admit(t, st, "a")
		for i := 1; i <= 5; i++ {
			if _, err := st.Append(ctx, "a", AppendRequest{Content: json.RawMessage(fmt.Sprintf(`"m%d"`, i))}); err != nil {
				t.Fatal(err)
			}
		}
		after2 := readAll(t, st, "a", 2)
		if len(after2) != 3 || after2[0].Seq != 3 || after2[2].Seq != 5 {
			t.Fatalf("read after 2 = %+v, want seqs 3..5", after2)
		}
		// Repeated reads are side-effect-free and return identical history.
		again := readAll(t, st, "a", 2)
		if len(again) != 3 || again[0].Seq != 3 {
			t.Fatalf("repeated read changed results: %+v", again)
		}
		// The boundary is caller-owned: a later full read still sees everything.
		all := readAll(t, st, "a", 0)
		if len(all) != 5 {
			t.Fatalf("full read after bounded reads = %d events", len(all))
		}
		// Past the end is empty, not an error.
		if tail := readAll(t, st, "a", 5); len(tail) != 0 {
			t.Fatalf("read past the end = %+v", tail)
		}
	})

	t.Run("AppendWithoutPriorRead", func(t *testing.T) {
		st := newStore(t)
		admit(t, st, "a")
		if _, err := st.Append(ctx, "a", AppendRequest{Content: json.RawMessage(`"first"`)}); err != nil {
			t.Fatalf("ordinary append depends on a prior read: %v", err)
		}
	})

	t.Run("Idempotency", func(t *testing.T) {
		st := newStore(t)
		admit(t, st, "a", "b")

		// No key: every append creates a distinct event.
		if _, err := st.Append(ctx, "a", AppendRequest{Content: json.RawMessage(`{"n":1}`)}); err != nil {
			t.Fatal(err)
		}
		if _, err := st.Append(ctx, "a", AppendRequest{Content: json.RawMessage(`{"n":1}`)}); err != nil {
			t.Fatal(err)
		}
		events := readAll(t, st, "a", 0)
		if len(events) != 2 || events[0].Seq == events[1].Seq {
			t.Fatalf("no-key appends collapsed into %+v", events)
		}

		// Same key + same fingerprint: return the original event. Whitespace
		// in content must not change the fingerprint.
		first, err := st.Append(ctx, "a", AppendRequest{IdempotencyKey: "k1", Content: json.RawMessage(`{"plan":1}`)})
		if err != nil {
			t.Fatal(err)
		}
		retry, err := st.Append(ctx, "a", AppendRequest{IdempotencyKey: "k1", Content: json.RawMessage("{\n  \"plan\" : 1 \n}")})
		if err != nil {
			t.Fatal(err)
		}
		if retry.Seq != first.Seq || !retry.At.Equal(first.At) || !bytes.Equal(retry.Content, first.Content) {
			t.Fatalf("idempotent retry did not return the original event: first=%+v retry=%+v", first, retry)
		}
		if len(readAll(t, st, "a", 0)) != 3 {
			t.Fatal("idempotent retry created a new event")
		}

		// Same key + same content with a different To ordering: same fingerprint.
		addr, err := st.Append(ctx, "a", AppendRequest{IdempotencyKey: "k2", To: []string{"b"}, Content: json.RawMessage(`"hi"`)})
		if err != nil {
			t.Fatal(err)
		}
		retryAddr, err := st.Append(ctx, "a", AppendRequest{IdempotencyKey: "k2", To: []string{"b"}, Content: json.RawMessage(`"hi"`)})
		if err != nil {
			t.Fatal(err)
		}
		if retryAddr.Seq != addr.Seq || !bytes.Equal(retryAddr.Content, addr.Content) {
			t.Fatalf("addressed retry did not return the original event: %+v vs %+v", addr, retryAddr)
		}

		// Same key + different fingerprint: conflict.
		if _, err := st.Append(ctx, "a", AppendRequest{IdempotencyKey: "k1", Content: json.RawMessage(`{"plan":2}`)}); !errors.Is(err, ErrConflict) {
			t.Fatalf("key reuse with different content error = %v, want %v", err, ErrConflict)
		}

		// The key is scoped per (room, actor, key): another actor may reuse it.
		if _, err := st.Append(ctx, "b", AppendRequest{IdempotencyKey: "k1", Content: json.RawMessage(`{"plan":2}`)}); err != nil {
			t.Fatalf("key reuse by another actor: %v", err)
		}
	})

	t.Run("ConcurrentIdempotency", func(t *testing.T) {
		st := newStore(t)
		admit(t, st, "a")
		const attempts = 8
		var group sync.WaitGroup
		results := make(chan *Event, attempts)
		errorsCh := make(chan error, attempts)
		for i := 0; i < attempts; i++ {
			group.Add(1)
			go func() {
				defer group.Done()
				event, err := st.Append(ctx, "a", AppendRequest{
					IdempotencyKey: "concurrent-key",
					Content:        json.RawMessage(`{"same":true}`),
				})
				results <- event
				errorsCh <- err
			}()
		}
		group.Wait()
		close(results)
		close(errorsCh)
		var first *Event
		for event := range results {
			if event == nil {
				t.Fatal("concurrent idempotent append returned nil event")
			}
			if first == nil {
				first = event
			} else if event.Seq != first.Seq {
				t.Fatalf("concurrent idempotent sequences = %d and %d", first.Seq, event.Seq)
			}
		}
		for err := range errorsCh {
			if err != nil {
				t.Fatalf("concurrent idempotent append: %v", err)
			}
		}
		if events := readAll(t, st, "a", 0); len(events) != 1 {
			t.Fatalf("concurrent idempotent append created %d events", len(events))
		}
	})

	t.Run("ContentCompacted", func(t *testing.T) {
		st := newStore(t)
		admit(t, st, "a")
		event, err := st.Append(ctx, "a", AppendRequest{Content: json.RawMessage("{\n  \"a\" : 1,\n  \"b\": [ 1, 2 ]\n}")})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(event.Content, json.RawMessage(`{"a":1,"b":[1,2]}`)) {
			t.Fatalf("content not compacted for storage: %s", event.Content)
		}
		seen := readAll(t, st, "a", 0)
		if !bytes.Equal(seen[0].Content, event.Content) {
			t.Fatalf("stored content differs: %s vs %s", seen[0].Content, event.Content)
		}
	})

	t.Run("ToNormalized", func(t *testing.T) {
		st := newStore(t)
		admit(t, st, "a", "bob", "carol")
		event, err := st.Append(ctx, "a", AppendRequest{To: []string{"carol", "bob", "bob", "carol"}, Content: json.RawMessage(`"m"`)})
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"bob", "carol"}; !equalStrings(event.To, want) {
			t.Fatalf("to = %v, want %v", event.To, want)
		}
	})

	t.Run("ToKeepsLiteralCommaIdentity", func(t *testing.T) {
		st := newStore(t)
		admit(t, st, "sender", "a,b")
		event, err := st.Append(ctx, "sender", AppendRequest{
			To: []string{" a,b ", "a,b"}, IdempotencyKey: "comma-key", Content: json.RawMessage(`"m"`),
		})
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"a,b"}; !equalStrings(event.To, want) {
			t.Fatalf("to = %v, want literal comma identity %v", event.To, want)
		}
		retry, err := st.Append(ctx, "sender", AppendRequest{
			To: []string{"a,b"}, IdempotencyKey: "comma-key", Content: json.RawMessage(`"m"`),
		})
		if err != nil || retry.Seq != event.Seq {
			t.Fatalf("literal comma identity retry = %+v, %v; want original seq %d", retry, err, event.Seq)
		}
	})
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func admit(t *testing.T, st Store, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if _, err := st.AdmitActor(context.Background(), id); err != nil {
			t.Fatalf("admit %s: %v", id, err)
		}
	}
}

func readAll(t *testing.T, st Store, actor string, after uint64) []Event {
	t.Helper()
	events, err := st.Read(context.Background(), actor, after)
	if err != nil {
		t.Fatalf("read %s: %v", actor, err)
	}
	return events
}
