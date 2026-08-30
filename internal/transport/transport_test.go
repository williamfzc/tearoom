// Transport contract tests prove the HTTP surface against the new Room
// contract: join, unjoined rejection, dense order, public To addressing, pure
// reads, idempotent retry and conflict, invocation-scoped wait, the unified
// error body, and rejection of the old append fields.
package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/williamfzc/tearoom/internal/core"
	"github.com/williamfzc/tearoom/internal/store"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func newPair(t *testing.T) (*Client, *store.File) {
	t.Helper()
	st, err := store.Init(t.TempDir(), store.BackendFile, "planner")
	if err != nil {
		t.Fatal(err)
	}
	file := st.(*store.File)
	server := httptest.NewServer(NewServer(file).Handler())
	t.Cleanup(server.Close)
	return NewClient(server.URL), file
}

func TestClientSatisfiesStore(t *testing.T) {
	var _ core.Store = (*Client)(nil)
	var _ core.Store = (*store.File)(nil)
}

func TestClientReadWaitRetriesPastServerCap(t *testing.T) {
	var requests int
	client := NewClient("http://tearoom.test")
	client.HTTP.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		if got := request.URL.Query().Get("wait"); got != "60" {
			t.Fatalf("request %d wait = %q, want 60", requests, got)
		}
		body := "[]"
		if requests == 2 {
			body = `[{"seq":1,"actor":"a","at":"2026-01-01T00:00:00Z","content":"ok"}]`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})
	events, err := client.ReadWait(context.Background(), "a", 0, 0, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || len(events) != 1 || events[0].Seq != 1 {
		t.Fatalf("requests = %d, events = %+v", requests, events)
	}
}

func TestClientReadWaitHonorsSubsecondBudget(t *testing.T) {
	client := NewClient("http://tearoom.test")
	client.HTTP.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("[]")),
			Request:    request,
		}, nil
	})
	started := time.Now()
	events, err := client.ReadWait(context.Background(), "a", 0, 0, 20*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("events = %+v, want empty", events)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("wait took %s, want under one second", elapsed)
	}
}

func TestRequireLoopbackURL(t *testing.T) {
	for _, test := range []struct {
		url string
		ok  bool
	}{
		{"http://127.0.0.1:8787", true},
		{"http://[::1]:8787", true},
		{"http://localhost:8787", true},
		{"http://192.0.2.1:8787", false},
		{"https://example.test", false},
	} {
		t.Run(test.url, func(t *testing.T) {
			err := RequireLoopbackURL(test.url)
			if (err == nil) != test.ok {
				t.Fatalf("RequireLoopbackURL(%q) error = %v, want ok=%v", test.url, err, test.ok)
			}
		})
	}
}

func TestRoomAndJoinThroughHTTP(t *testing.T) {
	client, _ := newPair(t)
	ctx := context.Background()

	room, err := client.Room(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if room.ID == "" || room.Created.IsZero() {
		t.Fatalf("room = %+v", room)
	}

	actor, err := client.AdmitActor(ctx, "planner")
	if err != nil {
		t.Fatal(err)
	}
	if actor.ID != "planner" || actor.Joined.IsZero() {
		t.Fatalf("actor = %+v", actor)
	}
	again, err := client.AdmitActor(ctx, "planner")
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != actor.ID || !again.Joined.Equal(actor.Joined) {
		t.Fatalf("re-join changed the actor: %+v then %+v", actor, again)
	}

	actors, err := client.Actors(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(actors) != 1 || actors[0].ID != "planner" {
		t.Fatalf("actors = %+v", actors)
	}
}

func TestUnjoinedActorRejected(t *testing.T) {
	client, _ := newPair(t)
	ctx := context.Background()
	if _, err := client.Read(ctx, "stranger", 0, 0); !errors.Is(err, core.ErrUnknownActor) {
		t.Fatalf("read error = %v, want %v", err, core.ErrUnknownActor)
	}
	if _, err := client.Append(ctx, "stranger", core.AppendRequest{Content: json.RawMessage(`"hi"`)}); !errors.Is(err, core.ErrUnknownActor) {
		t.Fatalf("append error = %v, want %v", err, core.ErrUnknownActor)
	}
}

func TestDenseOrderAndPublicToOverHTTP(t *testing.T) {
	client, _ := newPair(t)
	ctx := context.Background()
	for _, actor := range []string{"a", "b", "c"} {
		if _, err := client.AdmitActor(ctx, actor); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.Append(ctx, "a", core.AppendRequest{To: []string{"b"}, Content: json.RawMessage(`{"plan":1}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Append(ctx, "b", core.AppendRequest{Content: json.RawMessage(`"ok"`)}); err != nil {
		t.Fatal(err)
	}

	// Every member reads the same dense history.
	for _, actor := range []string{"a", "b", "c"} {
		events, err := client.Read(ctx, actor, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 2 {
			t.Fatalf("%s sees %d events, want 2", actor, len(events))
		}
		for i, event := range events {
			if event.Seq != uint64(i+1) {
				t.Fatalf("%s history not dense: event %d has seq %d", actor, i, event.Seq)
			}
		}
	}

	// To is public addressing: the event addressed to b is visible to c.
	events, err := client.Read(ctx, "c", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(events[0].Content, json.RawMessage(`{"plan":1}`)) || len(events[0].To) != 1 || events[0].To[0] != "b" {
		t.Fatalf("c missed the event addressed to b: %+v", events[0])
	}
}

func TestPureReadsAndBoundariesOverHTTP(t *testing.T) {
	client, _ := newPair(t)
	ctx := context.Background()
	if _, err := client.AdmitActor(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 5; i++ {
		if _, err := client.Append(ctx, "a", core.AppendRequest{Content: json.RawMessage(fmt.Sprintf(`"m%d"`, i))}); err != nil {
			t.Fatal(err)
		}
	}

	after2, err := client.Read(ctx, "a", 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(after2) != 3 || after2[0].Seq != 3 || after2[2].Seq != 5 {
		t.Fatalf("read after 2 = %+v, want seqs 3..5", after2)
	}
	// Repeated reads are side-effect-free and return identical history.
	again, err := client.Read(ctx, "a", 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 3 || again[0].Seq != 3 {
		t.Fatalf("repeated read changed results: %+v", again)
	}
	limited, err := client.Read(ctx, "a", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 2 || limited[0].Seq != 2 || limited[1].Seq != 3 {
		t.Fatalf("limited read = %+v, want seqs 2..3", limited)
	}
	tail, err := client.Read(ctx, "a", 5, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(tail) != 0 {
		t.Fatalf("read past the end = %+v", tail)
	}
}

func TestIdempotentRetryAndConflictOverHTTP(t *testing.T) {
	client, _ := newPair(t)
	ctx := context.Background()
	if _, err := client.AdmitActor(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	first, err := client.Append(ctx, "a", core.AppendRequest{IdempotencyKey: "k1", Content: json.RawMessage(`{"plan":1}`)})
	if err != nil {
		t.Fatal(err)
	}
	retry, err := client.Append(ctx, "a", core.AppendRequest{IdempotencyKey: "k1", Content: json.RawMessage(`{"plan":1}`)})
	if err != nil {
		t.Fatal(err)
	}
	if retry.Seq != first.Seq || !retry.At.Equal(first.At) || !bytes.Equal(retry.Content, first.Content) {
		t.Fatalf("retry did not return the original event: first=%+v retry=%+v", first, retry)
	}
	// The retry created no new event.
	events, err := client.Read(ctx, "a", 0, 0)
	if err != nil || len(events) != 1 {
		t.Fatalf("events after retry = %+v, %v", events, err)
	}

	// Same key, different content: the conflict sentinel survives the wire.
	if _, err := client.Append(ctx, "a", core.AppendRequest{IdempotencyKey: "k1", Content: json.RawMessage(`{"plan":2}`)}); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("conflict error = %v, want %v", err, core.ErrConflict)
	}
	// The key is scoped per actor: another actor may reuse it.
	if _, err := client.AdmitActor(ctx, "b"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Append(ctx, "b", core.AppendRequest{IdempotencyKey: "k1", Content: json.RawMessage(`{"plan":2}`)}); err != nil {
		t.Fatalf("key reuse by another actor: %v", err)
	}
	// No key: repeated appends produce distinct events.
	e1, err := client.Append(ctx, "a", core.AppendRequest{Content: json.RawMessage(`"x"`)})
	if err != nil {
		t.Fatal(err)
	}
	e2, err := client.Append(ctx, "a", core.AppendRequest{Content: json.RawMessage(`"x"`)})
	if err != nil {
		t.Fatal(err)
	}
	if e1.Seq == e2.Seq {
		t.Fatalf("no-key appends collapsed into %d", e1.Seq)
	}
}

func TestWaitReturnsWhenEventAppends(t *testing.T) {
	client, _ := newPair(t)
	ctx := context.Background()
	for _, actor := range []string{"a", "b"} {
		if _, err := client.AdmitActor(ctx, actor); err != nil {
			t.Fatal(err)
		}
	}
	got := make(chan []core.Event, 1)
	go func() {
		events, err := client.ReadWait(ctx, "b", 0, 0, 5*time.Second)
		if err != nil {
			t.Error(err)
			return
		}
		got <- events
	}()
	time.Sleep(150 * time.Millisecond)
	if _, err := client.Append(ctx, "a", core.AppendRequest{Content: json.RawMessage(`"wake"`)}); err != nil {
		t.Fatal(err)
	}
	select {
	case events := <-got:
		if len(events) != 1 || events[0].Seq != 1 || !bytes.Equal(events[0].Content, json.RawMessage(`"wake"`)) {
			t.Fatalf("events = %+v", events)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wait did not return after an append")
	}
}

func TestWaitTimesOutEmpty(t *testing.T) {
	client, _ := newPair(t)
	ctx := context.Background()
	if _, err := client.AdmitActor(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	before := time.Now()
	events, err := client.ReadWait(ctx, "a", 0, 0, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("events = %+v, want none", events)
	}
	if elapsed := time.Since(before); elapsed < 1500*time.Millisecond {
		t.Fatalf("empty wait returned too early: %s", elapsed)
	}
}

func TestWaitShortCircuitsOnAvailableEvents(t *testing.T) {
	client, _ := newPair(t)
	ctx := context.Background()
	if _, err := client.AdmitActor(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Append(ctx, "a", core.AppendRequest{Content: json.RawMessage(`"already"`)}); err != nil {
		t.Fatal(err)
	}
	before := time.Now()
	events, err := client.ReadWait(ctx, "a", 0, 0, 5*time.Second)
	if err != nil || len(events) != 1 {
		t.Fatalf("events = %+v, err = %v", events, err)
	}
	if elapsed := time.Since(before); elapsed > time.Second {
		t.Fatalf("short circuit took %s", elapsed)
	}
}

func TestAppendRejectsOldFields(t *testing.T) {
	client, _ := newPair(t)
	ctx := context.Background()
	if _, err := client.AdmitActor(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	// The removed fields of the old wire format, plus idempotency_key, which
	// belongs in the header only.
	oldFields := []string{"actor", "expected", "type", "body", "data", "caused_by", "key", "id", "idempotency_key"}
	for _, field := range oldFields {
		t.Run(field, func(t *testing.T) {
			body := fmt.Sprintf(`{"content":"\"x\"","%s":1}`, field)
			request, err := http.NewRequest("POST", client.Base+"/api/events", strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set(actorHeader, "a")
			response, err := client.HTTP.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusBadRequest {
				t.Fatalf("old field %q: status %d, want 400", field, response.StatusCode)
			}
			var envelope errorEnvelope
			if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Error.Code != "invalid" {
				t.Fatalf("old field %q: code %q, want invalid", field, envelope.Error.Code)
			}
		})
	}
}

func TestAppendRejectsTrailingJSON(t *testing.T) {
	client, _ := newPair(t)
	if _, err := client.AdmitActor(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, client.Base+"/api/events", strings.NewReader(`{"content":"x"}{"type":"legacy"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set(actorHeader, "a")
	response, err := client.HTTP.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.StatusCode)
	}
	var envelope errorEnvelope
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != "invalid" {
		t.Fatalf("code = %q, want invalid", envelope.Error.Code)
	}
}

func TestStrictErrorBody(t *testing.T) {
	client, _ := newPair(t)
	ctx := context.Background()
	if _, err := client.AdmitActor(ctx, "a"); err != nil {
		t.Fatal(err)
	}

	get := func(path, actor string) *http.Response {
		t.Helper()
		request, err := http.NewRequest(http.MethodGet, client.Base+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if actor != "" {
			request.Header.Set(actorHeader, actor)
		}
		response, err := client.HTTP.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	check := func(response *http.Response, wantStatus int, wantCode string) {
		t.Helper()
		defer response.Body.Close()
		if response.StatusCode != wantStatus {
			t.Fatalf("status = %d, want %d", response.StatusCode, wantStatus)
		}
		var envelope errorEnvelope
		if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Error.Code != wantCode || envelope.Error.Message == "" {
			t.Fatalf("envelope = %+v, want code %q with a message", envelope, wantCode)
		}
	}

	// Unknown actor: 401.
	check(get("/api/events?after=0&limit=0", "stranger"), http.StatusUnauthorized, "unknown_actor")
	// Missing actor header: 400.
	check(get("/api/events?after=0&limit=0", ""), http.StatusBadRequest, "invalid")
	// Malformed parameter: 400.
	check(get("/api/events?after=nope", "a"), http.StatusBadRequest, "invalid")

	// Malformed append body: 400.
	request, _ := http.NewRequest(http.MethodPost, client.Base+"/api/events", strings.NewReader(`{"content":`))
	request.Header.Set(actorHeader, "a")
	response, err := client.HTTP.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	check(response, http.StatusBadRequest, "invalid")

	// Wrong method: 405.
	request, _ = http.NewRequest(http.MethodPost, client.Base+"/api/room", nil)
	response, err = client.HTTP.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	check(response, http.StatusMethodNotAllowed, "method_not_allowed")

	// Idempotency-key conflict: 409, reserved for conflict only.
	if _, err := client.Append(ctx, "a", core.AppendRequest{IdempotencyKey: "k", Content: json.RawMessage(`"once"`)}); err != nil {
		t.Fatal(err)
	}
	request, _ = http.NewRequest(http.MethodPost, client.Base+"/api/events", strings.NewReader(`{"content":"\"twice\""}`))
	request.Header.Set(actorHeader, "a")
	request.Header.Set(idempotencyHeader, "k")
	response, err = client.HTTP.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	check(response, http.StatusConflict, "conflict")
}

func TestUnreachableServerFailsLoudly(t *testing.T) {
	client := NewClient("http://127.0.0.1:1")
	client.HTTP.Timeout = 100 * time.Millisecond
	_, err := client.Room(context.Background())
	if err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("error = %v", err)
	}
}
