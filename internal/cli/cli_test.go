// CLI tests protect command routing, help discovery, machine-safe arguments,
// and the exit-code contract for the target command surface.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/williamfzc/tearoom/internal/core"
	"github.com/williamfzc/tearoom/internal/store"
	"github.com/williamfzc/tearoom/internal/transport"
)

func TestRootAndCommandHelp(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"--help"}, "tearoom <command>"},
		{[]string{"room", "--help"}, "tearoom room <command>"},
		{[]string{"send", "--help"}, "tearoom send <text>"},
		{[]string{"read", "--help"}, "--after"},
		{[]string{"server", "--help"}, "tearoom server <command>"},
	} {
		var out bytes.Buffer
		app := &App{Out: &out, Err: io.Discard, Dir: t.TempDir()}
		if code := app.Run(test.args); code != ExitOK {
			t.Fatalf("%v exit = %d", test.args, code)
		}
		if !strings.Contains(out.String(), test.want) {
			t.Fatalf("%v output %q does not contain %q", test.args, out.String(), test.want)
		}
	}
}

func TestRemovedCommandsAreRejected(t *testing.T) {
	for _, args := range [][]string{
		{"event"},
		{"event", "send", "hello"},
		{"event", "list"},
		{"event", "watch"},
		{"list"},
		{"watch"},
		{"hosted"},
		{"actor"},
		{"room", "info"},
	} {
		app := &App{Out: io.Discard, Err: io.Discard, Dir: t.TempDir()}
		if code := app.Run(args); code != ExitError {
			t.Fatalf("%v exit = %d, want %d", args, code, ExitError)
		}
	}
}

func TestRemovedFlagsAreRejected(t *testing.T) {
	for _, args := range [][]string{
		{"send", "hello", "--cursor", "3", "--as", "alice"},
		{"send", "hello", "--body", "x", "--as", "alice"},
		{"send", "hello", "--data", "{}", "--as", "alice"},
		{"send", "hello", "--type", "message", "--as", "alice"},
		{"send", "hello", "--caused-by", "e1", "--as", "alice"},
		{"send", "hello", "--id", "k", "--as", "alice"},
		{"read", "--cursor", "3", "--as", "alice"},
		{"read", "--type", "message", "--as", "alice"},
	} {
		app := &App{Out: io.Discard, Err: io.Discard, Dir: t.TempDir()}
		if code := app.Run(args); code != ExitError {
			t.Fatalf("%v exit = %d, want %d", args, code, ExitError)
		}
	}
}

func TestRoomInitAdmitsCreator(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	app := &App{Out: &out, Err: io.Discard, Dir: dir}
	if code := app.Run([]string{"room", "init", "--as", "planner"}); code != ExitOK {
		t.Fatalf("room init exit = %d, output %s", code, out.String())
	}
	var room core.Room
	if err := json.Unmarshal(out.Bytes(), &room); err != nil || room.ID == "" {
		t.Fatalf("room init output = %q", out.String())
	}
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	actors, err := st.Actors(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(actors) != 1 || actors[0].ID != "planner" {
		t.Fatalf("actors after init = %+v, want exactly planner", actors)
	}
}

func TestRoomInitStoreChoice(t *testing.T) {
	for _, backend := range []string{"file", "sqlite"} {
		dir := t.TempDir()
		app := &App{Out: io.Discard, Err: io.Discard, Dir: dir}
		if code := app.Run([]string{"room", "init", "--as", "a", "--store", backend}); code != ExitOK {
			t.Fatalf("room init --store %s exit = %d", backend, code)
		}
		st, err := store.Open(dir)
		if err != nil {
			t.Fatalf("open %s room: %v", backend, err)
		}
		if _, err := st.Room(context.Background()); err != nil {
			t.Fatalf("room %s: %v", backend, err)
		}
	}
}

func TestRoomInitWithoutActorFails(t *testing.T) {
	app := &App{Out: io.Discard, Err: io.Discard, Dir: t.TempDir()}
	t.Setenv("TEAROOM_ACTOR", "")
	if code := app.Run([]string{"room", "init"}); code != ExitError {
		t.Fatalf("room init without an actor exit = %d, want %d", code, ExitError)
	}
}

func TestJoinAndMembers(t *testing.T) {
	dir := t.TempDir()
	app := &App{Out: io.Discard, Err: io.Discard, Dir: dir}
	if code := app.Run([]string{"room", "init", "--as", "planner"}); code != ExitOK {
		t.Fatalf("room init exit = %d", code)
	}
	var joinOut bytes.Buffer
	joinApp := &App{Out: &joinOut, Err: io.Discard, Dir: dir}
	if code := joinApp.Run([]string{"room", "join", "--as", "developer"}); code != ExitOK {
		t.Fatalf("room join exit = %d", code)
	}
	var actor core.Actor
	if err := json.Unmarshal(joinOut.Bytes(), &actor); err != nil || actor.ID != "developer" {
		t.Fatalf("room join output = %q", joinOut.String())
	}
	var members bytes.Buffer
	membersApp := &App{Out: &members, Err: io.Discard, Dir: dir}
	if code := membersApp.Run([]string{"room", "members"}); code != ExitOK {
		t.Fatalf("room members exit = %d", code)
	}
	var actors []core.Actor
	if err := json.Unmarshal(members.Bytes(), &actors); err != nil || len(actors) != 2 {
		t.Fatalf("room members output = %q", members.String())
	}
}

func TestSendTextEncodesJSONString(t *testing.T) {
	dir := initRoom(t, "planner")
	var out bytes.Buffer
	app := &App{Out: &out, Err: io.Discard, Dir: dir}
	if code := app.Run([]string{"send", "review this", "--as", "planner"}); code != ExitOK {
		t.Fatalf("send exit = %d, output %s", code, out.String())
	}
	var event core.Event
	if err := json.Unmarshal(out.Bytes(), &event); err != nil {
		t.Fatalf("send output = %q", out.String())
	}
	if event.Seq != 1 || event.Actor != "planner" {
		t.Fatalf("event = %+v", event)
	}
	if string(event.Content) != `"review this"` {
		t.Fatalf("content = %s, want the JSON string", event.Content)
	}
}

func TestSendJSONPassesRaw(t *testing.T) {
	dir := initRoom(t, "planner")
	var out bytes.Buffer
	app := &App{Out: &out, Err: io.Discard, Dir: dir}
	if code := app.Run([]string{"send", "--json", `{"plan":"review worktree A","n":1}`, "--as", "planner"}); code != ExitOK {
		t.Fatalf("send --json exit = %d", code)
	}
	var event core.Event
	if err := json.Unmarshal(out.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	if string(event.Content) != `{"plan":"review worktree A","n":1}` {
		t.Fatalf("content = %s, want the raw JSON", event.Content)
	}
}

func TestSendRequiresActor(t *testing.T) {
	dir := initRoom(t, "planner")
	app := &App{Out: io.Discard, Err: io.Discard, Dir: dir}
	t.Setenv("TEAROOM_ACTOR", "")
	if code := app.Run([]string{"send", "hello"}); code != ExitError {
		t.Fatalf("send without an actor exit = %d, want %d", code, ExitError)
	}
}

func TestSendToRepeatable(t *testing.T) {
	dir := initRoom(t, "planner")
	for _, member := range []string{"bob", "carol"} {
		app := &App{Out: io.Discard, Err: io.Discard, Dir: dir}
		if code := app.Run([]string{"room", "join", "--as", member}); code != ExitOK {
			t.Fatalf("join %s exit = %d", member, code)
		}
	}
	var out bytes.Buffer
	app := &App{Out: &out, Err: io.Discard, Dir: dir}
	if code := app.Run([]string{"send", "hi", "--to", "bob", "--to", "carol", "--as", "planner"}); code != ExitOK {
		t.Fatalf("send exit = %d", code)
	}
	var event core.Event
	if err := json.Unmarshal(out.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	if len(event.To) != 2 || event.To[0] != "bob" || event.To[1] != "carol" {
		t.Fatalf("to = %v, want [bob carol]", event.To)
	}
}

func TestSendUnjoinedActorRejected(t *testing.T) {
	dir := initRoom(t, "planner")
	app := &App{Out: io.Discard, Err: io.Discard, Dir: dir}
	if code := app.Run([]string{"send", "hello", "--as", "stranger"}); code != ExitError {
		t.Fatalf("unjoined send exit = %d, want %d", code, ExitError)
	}
}

func TestReadPrintsJSONL(t *testing.T) {
	dir := initRoom(t, "planner")
	app := &App{Out: io.Discard, Err: io.Discard, Dir: dir}
	if code := app.Run([]string{"send", "one", "--as", "planner"}); code != ExitOK {
		t.Fatalf("send one exit = %d", code)
	}
	if code := app.Run([]string{"send", "two", "--as", "planner"}); code != ExitOK {
		t.Fatalf("send two exit = %d", code)
	}
	var out bytes.Buffer
	reader := &App{Out: &out, Err: io.Discard, Dir: dir}
	if code := reader.Run([]string{"read", "--as", "planner"}); code != ExitOK {
		t.Fatalf("read exit = %d", code)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("read output = %q, want 2 JSONL lines", out.String())
	}
	for _, line := range lines {
		var event core.Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("read line %q: %v", line, err)
		}
	}
}

func TestReadEmptyPrintsNothing(t *testing.T) {
	dir := initRoom(t, "planner")
	var out bytes.Buffer
	app := &App{Out: &out, Err: io.Discard, Dir: dir}
	if code := app.Run([]string{"read", "--as", "planner"}); code != ExitOK {
		t.Fatalf("empty read exit = %d, want 0", code)
	}
	if out.Len() != 0 {
		t.Fatalf("empty read output = %q, want nothing", out.String())
	}
}

func TestReadAfterBoundary(t *testing.T) {
	dir := initRoom(t, "planner")
	app := &App{Out: io.Discard, Err: io.Discard, Dir: dir}
	for i := 1; i <= 3; i++ {
		if code := app.Run([]string{"send", "m", "--as", "planner"}); code != ExitOK {
			t.Fatalf("send %d exit = %d", i, code)
		}
	}
	for _, after := range []string{"e1", "1"} {
		var out bytes.Buffer
		reader := &App{Out: &out, Err: io.Discard, Dir: dir}
		if code := reader.Run([]string{"read", "--after", after, "--as", "planner"}); code != ExitOK {
			t.Fatalf("read --after %s exit = %d", after, code)
		}
		lines := strings.Split(strings.TrimSpace(out.String()), "\n")
		if len(lines) != 2 {
			t.Fatalf("read --after %s = %q, want 2 lines", after, out.String())
		}
		var first core.Event
		if err := json.Unmarshal([]byte(lines[0]), &first); err != nil || first.Seq != 2 {
			t.Fatalf("read --after %s first event = %s", after, lines[0])
		}
	}
}

func TestReadLimit(t *testing.T) {
	dir := initRoom(t, "planner")
	app := &App{Out: io.Discard, Err: io.Discard, Dir: dir}
	for i := 1; i <= 3; i++ {
		if code := app.Run([]string{"send", "m", "--as", "planner"}); code != ExitOK {
			t.Fatalf("send exit = %d", code)
		}
	}
	var out bytes.Buffer
	reader := &App{Out: &out, Err: io.Discard, Dir: dir}
	if code := reader.Run([]string{"read", "--limit", "2", "--as", "planner"}); code != ExitOK {
		t.Fatalf("read --limit exit = %d", code)
	}
	if n := strings.Count(strings.TrimSpace(out.String()), "\n") + 1; n != 2 {
		t.Fatalf("read --limit 2 = %q, want 2 lines", out.String())
	}
}

func TestReadRepeatedHasNoSideEffects(t *testing.T) {
	dir := initRoom(t, "planner")
	app := &App{Out: io.Discard, Err: io.Discard, Dir: dir}
	if code := app.Run([]string{"send", "m", "--as", "planner"}); code != ExitOK {
		t.Fatalf("send exit = %d", code)
	}
	for i := 0; i < 2; i++ {
		var out bytes.Buffer
		reader := &App{Out: &out, Err: io.Discard, Dir: dir}
		if code := reader.Run([]string{"read", "--as", "planner"}); code != ExitOK {
			t.Fatalf("read %d exit = %d", i, code)
		}
		if lines := strings.Split(strings.TrimSpace(out.String()), "\n"); len(lines) != 1 {
			t.Fatalf("read %d = %q, want 1 line", i, out.String())
		}
	}
}

func TestReadWaitTimeout(t *testing.T) {
	dir := initRoom(t, "planner")
	var out bytes.Buffer
	app := &App{Out: &out, Err: io.Discard, Dir: dir}
	if code := app.Run([]string{"read", "--wait", "200ms", "--as", "planner"}); code != ExitTimeout {
		t.Fatalf("read --wait timeout exit = %d, want %d", code, ExitTimeout)
	}
	if out.Len() != 0 {
		t.Fatalf("read --wait timeout output = %q, want nothing", out.String())
	}
}

func TestReadWaitReturnsExistingEvents(t *testing.T) {
	dir := initRoom(t, "planner")
	app := &App{Out: io.Discard, Err: io.Discard, Dir: dir}
	if code := app.Run([]string{"send", "m", "--as", "planner"}); code != ExitOK {
		t.Fatalf("send exit = %d", code)
	}
	var out bytes.Buffer
	reader := &App{Out: &out, Err: io.Discard, Dir: dir}
	if code := reader.Run([]string{"read", "--wait", "5s", "--as", "planner"}); code != ExitOK {
		t.Fatalf("read --wait with events exit = %d", code)
	}
	if lines := strings.Split(strings.TrimSpace(out.String()), "\n"); len(lines) != 1 {
		t.Fatalf("read --wait output = %q, want 1 line", out.String())
	}
}

func TestReadWaitSeesLaterAppend(t *testing.T) {
	dir := initRoom(t, "planner")
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	app := &App{Out: &out, Err: io.Discard, Dir: dir}
	done := make(chan int, 1)
	go func() { done <- app.Run([]string{"read", "--wait", "10s", "--as", "planner"}) }()
	time.Sleep(300 * time.Millisecond)
	if _, err := st.Append(context.Background(), "planner", core.AppendRequest{Content: json.RawMessage(`"late"`)}); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-done:
		if code != ExitOK {
			t.Fatalf("read --wait exit = %d", code)
		}
		if !strings.Contains(out.String(), `"late"`) {
			t.Fatalf("read --wait output = %q, missing the later event", out.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("read --wait did not return after a later append")
	}
}

func TestServerStartRefusesNonLoopback(t *testing.T) {
	for _, address := range []string{":8787", "0.0.0.0:8787", "192.168.1.10:8787"} {
		dir := initRoom(t, "planner")
		app := &App{Out: io.Discard, Err: io.Discard, Dir: dir}
		if code := app.Run([]string{"server", "start", "--address", address}); code != ExitError {
			t.Fatalf("server start %s exit = %d, want %d", address, code, ExitError)
		}
	}
}

func TestRemoteServerRefusesNonLoopback(t *testing.T) {
	t.Setenv("TEAROOM_SERVER", "http://192.0.2.1:8787")
	var errOut bytes.Buffer
	app := &App{Out: io.Discard, Err: &errOut, Dir: t.TempDir()}
	if code := app.Run([]string{"send", "hello", "--as", "planner"}); code != ExitError {
		t.Fatalf("send exit = %d, want %d", code, ExitError)
	}
	if !strings.Contains(errOut.String(), "loopback") {
		t.Fatalf("error = %q, want loopback validation", errOut.String())
	}
}

func TestReadWaitHonorsCancellation(t *testing.T) {
	dir := initRoom(t, "planner")
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app := &App{Out: io.Discard, Err: io.Discard, Dir: dir}
	_, err = app.readWithWait(ctx, st, "planner", 0, 0, time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("readWithWait error = %v, want context cancellation", err)
	}
}

func TestServerModeRoundTrip(t *testing.T) {
	dir := initRoom(t, "planner")
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(transport.NewServer(st).Handler())
	defer server.Close()
	t.Setenv("TEAROOM_SERVER", server.URL)

	sender := &App{Out: io.Discard, Err: io.Discard, Dir: t.TempDir()}
	if code := sender.Run([]string{"send", "hello", "--as", "planner"}); code != ExitOK {
		t.Fatalf("http send exit = %d", code)
	}
	joiner := &App{Out: io.Discard, Err: io.Discard, Dir: t.TempDir()}
	if code := joiner.Run([]string{"room", "join", "--as", "bob"}); code != ExitOK {
		t.Fatalf("http join exit = %d", code)
	}
	var out bytes.Buffer
	reader := &App{Out: &out, Err: io.Discard, Dir: t.TempDir()}
	if code := reader.Run([]string{"read", "--as", "bob"}); code != ExitOK {
		t.Fatalf("http read exit = %d", code)
	}
	if lines := strings.Split(strings.TrimSpace(out.String()), "\n"); len(lines) != 1 {
		t.Fatalf("http read = %q, want 1 line", out.String())
	}
	// Unjoined actors are rejected over HTTP too.
	stranger := &App{Out: io.Discard, Err: io.Discard, Dir: t.TempDir()}
	if code := stranger.Run([]string{"read", "--as", "stranger"}); code != ExitError {
		t.Fatalf("http unjoined read exit = %d, want %d", code, ExitError)
	}
}

func TestIdempotencyConflictExitsOne(t *testing.T) {
	dir := initRoom(t, "planner")
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(transport.NewServer(st).Handler())
	defer server.Close()
	t.Setenv("TEAROOM_SERVER", server.URL)

	app := &App{Out: io.Discard, Err: io.Discard, Dir: t.TempDir()}
	if code := app.Run([]string{"send", "--json", `{"plan":1}`, "--idempotency-key", "k1", "--as", "planner"}); code != ExitOK {
		t.Fatalf("first append exit = %d", code)
	}
	var out bytes.Buffer
	retry := &App{Out: &out, Err: io.Discard, Dir: t.TempDir()}
	if code := retry.Run([]string{"send", "--json", `{"plan":2}`, "--idempotency-key", "k1", "--as", "planner"}); code != ExitError {
		t.Fatalf("conflicting retry exit = %d, want %d (HTTP 409)", code, ExitError)
	}
	// An equal retry returns the original event and exits 0.
	var ok bytes.Buffer
	equal := &App{Out: &ok, Err: io.Discard, Dir: t.TempDir()}
	if code := equal.Run([]string{"send", "--json", `{"plan": 1}`, "--idempotency-key", "k1", "--as", "planner"}); code != ExitOK {
		t.Fatalf("equal retry exit = %d, want 0", code)
	}
	var event core.Event
	if err := json.Unmarshal(ok.Bytes(), &event); err != nil || event.Seq != 1 {
		t.Fatalf("equal retry returned %s, want the original event", ok.String())
	}
}

func TestParseAcceptsFlagsAfterText(t *testing.T) {
	dir := initRoom(t, "planner")
	var out bytes.Buffer
	app := &App{Out: &out, Err: io.Discard, Dir: dir}
	if code := app.Run([]string{"send", "hello", "world", "--as", "planner"}); code != ExitOK {
		t.Fatalf("send exit = %d, output %s", code, out.String())
	}
	var event core.Event
	if err := json.Unmarshal(out.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	if string(event.Content) != `"hello world"` {
		t.Fatalf("content = %s, want the joined positional text", event.Content)
	}
}

func TestParseRejectsUnknownFlag(t *testing.T) {
	dir := initRoom(t, "planner")
	app := &App{Out: io.Discard, Err: io.Discard, Dir: dir}
	if code := app.Run([]string{"send", "hello", "--fromat", "json", "--as", "planner"}); code != ExitError {
		t.Fatalf("unknown flag exit = %d, want %d", code, ExitError)
	}
}

func initRoom(t *testing.T, creator string) string {
	t.Helper()
	dir := t.TempDir()
	app := &App{Out: io.Discard, Err: io.Discard, Dir: dir}
	if code := app.Run([]string{"room", "init", "--as", creator}); code != ExitOK {
		t.Fatalf("room init exit = %d", code)
	}
	return dir
}
