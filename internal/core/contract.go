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
// pass: the single-parent tree, dense per-room append, pure caller-owned
// reads, and per-request idempotency. newStore returns a fresh empty
// workspace for each subtest. Store implementations run this battery from
// their own test suites.
func StoreContract(t *testing.T, newStore func(t *testing.T) Store) {
	t.Helper()
	ctx := context.Background()

	t.Run("CreateRoom", func(t *testing.T) {
		st := newStore(t)
		room, err := st.CreateRoom(ctx, "main-line", "")
		if err != nil {
			t.Fatal(err)
		}
		if room == nil || room.Name != "main-line" {
			t.Fatalf("created room = %+v", room)
		}
		if room.Parent != "" {
			t.Fatalf("root room records a parent: %+v", room)
		}
		if room.Created.IsZero() {
			t.Fatalf("room creation time missing: %+v", room)
		}
		rooms, err := st.Rooms(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(rooms) != 1 || rooms[0].Name != "main-line" {
			t.Fatalf("rooms = %+v, want exactly main-line", rooms)
		}
		// Names are the addresses: a duplicate is a conflict, and a child of
		// a missing room is refused.
		if _, err := st.CreateRoom(ctx, "main-line", ""); !errors.Is(err, ErrConflict) {
			t.Fatalf("duplicate room error = %v, want %v", err, ErrConflict)
		}
		if _, err := st.CreateRoom(ctx, "orphan", "missing"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("unknown parent error = %v, want %v", err, ErrNotFound)
		}
		if _, err := st.CreateRoom(ctx, "  ", ""); err == nil {
			t.Fatal("blank room name accepted")
		}
	})

	t.Run("ChildRecordsParent", func(t *testing.T) {
		st := newStore(t)
		if _, err := st.CreateRoom(ctx, "root", ""); err != nil {
			t.Fatal(err)
		}
		child, err := st.CreateRoom(ctx, "implementation", "root")
		if err != nil {
			t.Fatal(err)
		}
		if child.Parent != "root" {
			t.Fatalf("child = %+v, want parent root", child)
		}
		// The parent records nothing about the child: the child list is
		// derived from the rooms listing, never stored on the parent.
		rooms, err := st.Rooms(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(rooms) != 2 {
			t.Fatalf("rooms = %+v, want root and implementation", rooms)
		}
		var children []string
		for _, room := range rooms {
			if room.Parent == "root" {
				children = append(children, room.Name)
			}
		}
		if len(children) != 1 || children[0] != "implementation" {
			t.Fatalf("derived children of root = %v", children)
		}
	})

	t.Run("IndependentRoots", func(t *testing.T) {
		st := newStore(t)
		if _, err := st.CreateRoom(ctx, "one", ""); err != nil {
			t.Fatal(err)
		}
		if _, err := st.CreateRoom(ctx, "two", ""); err != nil {
			t.Fatal(err)
		}
		rooms, err := st.Rooms(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(rooms) != 2 {
			t.Fatalf("rooms = %+v, want two roots", rooms)
		}
		for _, room := range rooms {
			if room.Parent != "" {
				t.Fatalf("root %q records a parent: %+v", room.Name, room)
			}
		}
	})

	t.Run("DenseSequencePerRoom", func(t *testing.T) {
		st := newStore(t)
		if _, err := st.CreateRoom(ctx, "root", ""); err != nil {
			t.Fatal(err)
		}
		if _, err := st.CreateRoom(ctx, "child", "root"); err != nil {
			t.Fatal(err)
		}
		for i := 1; i <= 3; i++ {
			// The first append carries no prior read: ordinary appends never
			// depend on read progress.
			record, err := st.Append(ctx, "root", "a", AppendRequest{Content: json.RawMessage(`"e"`)})
			if err != nil {
				t.Fatal(err)
			}
			if record.Seq != uint64(i) {
				t.Fatalf("root seq = %d, want %d", record.Seq, i)
			}
			if record.Room != "root" || record.At.IsZero() {
				t.Fatalf("record does not carry its room and acceptance time: %+v", record)
			}
		}
		// A child's stream starts at 1 regardless of the parent's head: the
		// streams of different rooms are independent.
		first, err := st.Append(ctx, "child", "b", AppendRequest{Content: json.RawMessage(`"c"`)})
		if err != nil {
			t.Fatal(err)
		}
		if first.Seq != 1 {
			t.Fatalf("child seq = %d, want 1", first.Seq)
		}
		records := readAll(t, st, "root", 0)
		if len(records) != 3 {
			t.Fatalf("root history = %d records, want 3", len(records))
		}
		for i, record := range records {
			if record.Seq != uint64(i+1) {
				t.Fatalf("history not dense: record %d has seq %d", i, record.Seq)
			}
		}
	})

	t.Run("ConcurrentAppendDense", func(t *testing.T) {
		st := newStore(t)
		if _, err := st.CreateRoom(ctx, "busy", ""); err != nil {
			t.Fatal(err)
		}
		const writers = 8
		var group sync.WaitGroup
		results := make(chan error, writers)
		for i := 0; i < writers; i++ {
			group.Add(1)
			go func(i int) {
				defer group.Done()
				_, err := st.Append(ctx, "busy", fmt.Sprintf("w%d", i),
					AppendRequest{Content: json.RawMessage(fmt.Sprintf(`"from w%d"`, i))})
				results <- err
			}(i)
		}
		group.Wait()
		close(results)
		for err := range results {
			if err != nil {
				t.Fatalf("concurrent append: %v", err)
			}
		}
		records := readAll(t, st, "busy", 0)
		if len(records) != writers {
			t.Fatalf("records = %d, want %d", len(records), writers)
		}
		for i, record := range records {
			if record.Seq != uint64(i+1) {
				t.Fatalf("concurrent sequence not dense: record %d has seq %d", i, record.Seq)
			}
		}
	})

	t.Run("ReadBoundary", func(t *testing.T) {
		st := newStore(t)
		if _, err := st.CreateRoom(ctx, "room", ""); err != nil {
			t.Fatal(err)
		}
		for i := 1; i <= 5; i++ {
			if _, err := st.Append(ctx, "room", "a", AppendRequest{Content: json.RawMessage(fmt.Sprintf(`"m%d"`, i))}); err != nil {
				t.Fatal(err)
			}
		}
		after2 := readAll(t, st, "room", 2)
		if len(after2) != 3 || after2[0].Seq != 3 || after2[2].Seq != 5 {
			t.Fatalf("read after 2 = %+v, want seqs 3..5", after2)
		}
		// Repeated reads are side-effect-free and return identical history.
		again := readAll(t, st, "room", 2)
		if len(again) != 3 || again[0].Seq != 3 {
			t.Fatalf("repeated read changed results: %+v", again)
		}
		// The boundary is caller-owned: a later full read still sees everything.
		all := readAll(t, st, "room", 0)
		if len(all) != 5 {
			t.Fatalf("full read after bounded reads = %d records", len(all))
		}
		// Past the end is empty, not an error.
		if tail := readAll(t, st, "room", 5); len(tail) != 0 {
			t.Fatalf("read past the end = %+v", tail)
		}
		// An unknown room is a not-found error, never a silent empty read.
		if _, err := st.Read(ctx, "missing", 0); !errors.Is(err, ErrNotFound) {
			t.Fatalf("unknown room error = %v, want %v", err, ErrNotFound)
		}
	})

	t.Run("MissingAuthorAllowed", func(t *testing.T) {
		st := newStore(t)
		if _, err := st.CreateRoom(ctx, "room", ""); err != nil {
			t.Fatal(err)
		}
		record, err := st.Append(ctx, "room", "", AppendRequest{Content: json.RawMessage(`"anon"`)})
		if err != nil {
			t.Fatalf("append without a signature: %v", err)
		}
		if record.Author != "" {
			t.Fatalf("author = %q, want the empty signature", record.Author)
		}
		// A signature is a label: whitespace is trimmed, never validated.
		labeled, err := st.Append(ctx, "room", "  codex  ", AppendRequest{Content: json.RawMessage(`"signed"`)})
		if err != nil {
			t.Fatal(err)
		}
		if labeled.Author != "codex" {
			t.Fatalf("author = %q, want codex", labeled.Author)
		}
	})

	t.Run("Idempotency", func(t *testing.T) {
		st := newStore(t)
		if _, err := st.CreateRoom(ctx, "one", ""); err != nil {
			t.Fatal(err)
		}
		if _, err := st.CreateRoom(ctx, "two", ""); err != nil {
			t.Fatal(err)
		}

		// No key: every append creates a distinct record.
		if _, err := st.Append(ctx, "one", "a", AppendRequest{Content: json.RawMessage(`{"n":1}`)}); err != nil {
			t.Fatal(err)
		}
		if _, err := st.Append(ctx, "one", "a", AppendRequest{Content: json.RawMessage(`{"n":1}`)}); err != nil {
			t.Fatal(err)
		}
		records := readAll(t, st, "one", 0)
		if len(records) != 2 || records[0].Seq == records[1].Seq {
			t.Fatalf("no-key appends collapsed into %+v", records)
		}

		// Same key + same fingerprint: return the original record. Whitespace
		// in content must not change the fingerprint.
		first, err := st.Append(ctx, "one", "a", AppendRequest{IdempotencyKey: "k1", Content: json.RawMessage(`{"plan":1}`)})
		if err != nil {
			t.Fatal(err)
		}
		retry, err := st.Append(ctx, "one", "a", AppendRequest{IdempotencyKey: "k1", Content: json.RawMessage("{\n  \"plan\" : 1 \n}")})
		if err != nil {
			t.Fatal(err)
		}
		if retry.Seq != first.Seq || !retry.At.Equal(first.At) || !bytes.Equal(retry.Content, first.Content) {
			t.Fatalf("idempotent retry did not return the original record: first=%+v retry=%+v", first, retry)
		}
		if len(readAll(t, st, "one", 0)) != 3 {
			t.Fatal("idempotent retry created a new record")
		}

		// Same key + different content: conflict.
		if _, err := st.Append(ctx, "one", "a", AppendRequest{IdempotencyKey: "k1", Content: json.RawMessage(`{"plan":2}`)}); !errors.Is(err, ErrConflict) {
			t.Fatalf("key reuse with different content error = %v, want %v", err, ErrConflict)
		}

		// The key is scoped per (room, signature, key): another signature in
		// the same room, and the same signature in another room, may reuse it.
		if _, err := st.Append(ctx, "one", "b", AppendRequest{IdempotencyKey: "k1", Content: json.RawMessage(`{"plan":2}`)}); err != nil {
			t.Fatalf("key reuse by another signature: %v", err)
		}
		if _, err := st.Append(ctx, "two", "a", AppendRequest{IdempotencyKey: "k1", Content: json.RawMessage(`{"plan":3}`)}); err != nil {
			t.Fatalf("key reuse in another room: %v", err)
		}
	})

	t.Run("ConcurrentIdempotency", func(t *testing.T) {
		st := newStore(t)
		if _, err := st.CreateRoom(ctx, "room", ""); err != nil {
			t.Fatal(err)
		}
		const attempts = 8
		var group sync.WaitGroup
		results := make(chan *Record, attempts)
		errorsCh := make(chan error, attempts)
		for i := 0; i < attempts; i++ {
			group.Add(1)
			go func() {
				defer group.Done()
				record, err := st.Append(ctx, "room", "a", AppendRequest{
					IdempotencyKey: "concurrent-key",
					Content:        json.RawMessage(`{"same":true}`),
				})
				results <- record
				errorsCh <- err
			}()
		}
		group.Wait()
		close(results)
		close(errorsCh)
		var first *Record
		for record := range results {
			if record == nil {
				t.Fatal("concurrent idempotent append returned nil record")
			}
			if first == nil {
				first = record
			} else if record.Seq != first.Seq {
				t.Fatalf("concurrent idempotent sequences = %d and %d", first.Seq, record.Seq)
			}
		}
		for err := range errorsCh {
			if err != nil {
				t.Fatalf("concurrent idempotent append: %v", err)
			}
		}
		if records := readAll(t, st, "room", 0); len(records) != 1 {
			t.Fatalf("concurrent idempotent append created %d records", len(records))
		}
	})

	t.Run("ContentCompacted", func(t *testing.T) {
		st := newStore(t)
		if _, err := st.CreateRoom(ctx, "room", ""); err != nil {
			t.Fatal(err)
		}
		record, err := st.Append(ctx, "room", "a", AppendRequest{Content: json.RawMessage("{\n  \"a\" : 1,\n  \"b\": [ 1, 2 ]\n}")})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(record.Content, json.RawMessage(`{"a":1,"b":[1,2]}`)) {
			t.Fatalf("content not compacted for storage: %s", record.Content)
		}
		seen := readAll(t, st, "room", 0)
		if !bytes.Equal(seen[0].Content, record.Content) {
			t.Fatalf("stored content differs: %s vs %s", seen[0].Content, record.Content)
		}
	})
}

func readAll(t *testing.T, st Store, room string, after uint64) []Record {
	t.Helper()
	records, err := st.Read(context.Background(), room, after)
	if err != nil {
		t.Fatalf("read %s: %v", room, err)
	}
	return records
}
