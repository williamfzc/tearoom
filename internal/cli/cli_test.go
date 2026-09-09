// CLI tests protect command routing, help discovery, machine-safe arguments,
// and the exit-code contract for the target command surface.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/williamfzc/tearoom/internal/core"
	"github.com/williamfzc/tearoom/internal/hosted"
	"github.com/williamfzc/tearoom/internal/relational"
	"github.com/williamfzc/tearoom/internal/store"
	"github.com/williamfzc/tearoom/internal/transport"
	"github.com/williamfzc/tearoom/internal/version"
)

func TestVersionReportsBuildMetadataAndStoreFormat(t *testing.T) {
	originalVersion, originalCommit := version.Version, version.Commit
	version.Version, version.Commit = "1.2.3", "abc1234"
	t.Cleanup(func() { version.Version, version.Commit = originalVersion, originalCommit })

	var out bytes.Buffer
	app := &App{Out: &out, Err: io.Discard, Dir: t.TempDir()}
	if code := app.Run([]string{"--version"}); code != ExitOK {
		t.Fatalf("--version exit = %d", code)
	}
	want := "tearoom version=1.2.3 commit=abc1234 store-format=2\n"
	if out.String() != want {
		t.Fatalf("--version output = %q, want %q", out.String(), want)
	}

}

func TestRootHelpDocumentsVersionOption(t *testing.T) {
	var out bytes.Buffer
	app := &App{Out: &out, Err: io.Discard, Dir: t.TempDir()}
	if code := app.Run([]string{"--help"}); code != ExitOK {
		t.Fatalf("--help exit = %d", code)
	}
	if !strings.Contains(out.String(), "--version") {
		t.Fatalf("root help = %q, want --version", out.String())
	}
}

func TestSkillPrintsAgentGuideWithoutArgs(t *testing.T) {
	var out bytes.Buffer
	app := &App{Out: &out, Err: io.Discard, Dir: t.TempDir()}
	if code := app.Run([]string{"skill"}); code != ExitOK {
		t.Fatalf("skill exit = %d", code)
	}
	guide := out.String()
	for _, want := range []string{
		"TEAROOM_ACTOR",
		"TEAROOM_SERVER",
		"room init",
		"tearoom read [--after e<N>]",
		"--idempotency-key",
		"tearoom <command> --help",
	} {
		if !strings.Contains(guide, want) {
			t.Fatalf("skill guide = %q, want it to contain %q", guide, want)
		}
	}

	// The guide is orientation, not a Room read: it must not depend on one.
	app = &App{Out: &out, Err: io.Discard, Dir: t.TempDir()}
	out.Reset()
	if code := app.Run([]string{"skill", "extra"}); code != ExitError {
		t.Fatalf("skill extra exit = %d, want %d", code, ExitError)
	}

	var root bytes.Buffer
	app = &App{Out: &root, Err: io.Discard, Dir: t.TempDir()}
	if code := app.Run([]string{"--help"}); code != ExitOK {
		t.Fatalf("--help exit = %d", code)
	}
	if !strings.Contains(root.String(), "skill") {
		t.Fatalf("root help = %q, want it to route to skill", root.String())
	}
}

func TestRootAndCommandHelp(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"--help"}, "tearoom <command>"},
		{[]string{"room", "--help"}, "tearoom room init"},
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
		if test.args[0] == "--help" && strings.Contains(out.String(), "help      show help") {
			t.Fatalf("root help still advertises removed help command: %q", out.String())
		}
	}
}

func TestRoomJoinHelpUsesPositionalActor(t *testing.T) {
	var out bytes.Buffer
	app := &App{Out: &out, Err: io.Discard, Dir: t.TempDir()}
	if code := app.Run([]string{"room", "join", "--help"}); code != ExitOK {
		t.Fatalf("room join --help exit = %d", code)
	}
	if !strings.Contains(out.String(), "tearoom room join <actor>") || strings.Contains(out.String(), "--as") {
		t.Fatalf("room join help = %q", out.String())
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
		{"help"},
		{"room", "info"},
		{"room", "members"},
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
		{"send", "hello", "--to", "bob", "--as", "alice"},
		{"read", "--limit", "2", "--as", "alice"},
		{"read", "--wait", "5s", "--as", "alice"},
		{"room", "init", "--as", "alice", "--store", "file"},
		{"server", "start", "--directory", t.TempDir()},
	} {
		app := &App{Out: io.Discard, Err: io.Discard, Dir: t.TempDir()}
		if code := app.Run(args); code != ExitError {
			t.Fatalf("%v exit = %d, want %d", args, code, ExitError)
		}
	}
}

func TestRootHelpCommandIsRemovedButSendHelpIsText(t *testing.T) {
	app := &App{Out: io.Discard, Err: io.Discard, Dir: t.TempDir()}
	if code := app.Run([]string{"help"}); code != ExitError {
		t.Fatalf("help exit = %d, want %d", code, ExitError)
	}
	dir := initRoom(t, "planner")
	var out bytes.Buffer
	app = &App{Out: &out, Err: io.Discard, Dir: dir}
	if code := app.Run([]string{"send", "help", "--as", "planner"}); code != ExitOK {
		t.Fatalf("send help exit = %d, output %s", code, out.String())
	}
	var event core.Event
	if err := json.Unmarshal(out.Bytes(), &event); err != nil || string(event.Content) != `"help"` {
		t.Fatalf("send help output = %q", out.String())
	}
	// `--` makes a help-looking positional argument literal as well.
	out.Reset()
	if code := app.Run([]string{"send", "--as", "planner", "--", "--help"}); code != ExitOK {
		t.Fatalf("send -- --help exit = %d, output %s", code, out.String())
	}
	if err := json.Unmarshal(out.Bytes(), &event); err != nil || string(event.Content) != `"--help"` {
		t.Fatalf("send literal help output = %q", out.String())
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

func TestRoomInitWithoutActorFails(t *testing.T) {
	app := &App{Out: io.Discard, Err: io.Discard, Dir: t.TempDir()}
	t.Setenv("TEAROOM_ACTOR", "")
	if code := app.Run([]string{"room", "init"}); code != ExitError {
		t.Fatalf("room init without an actor exit = %d, want %d", code, ExitError)
	}
}

func TestJoinAdmitsActors(t *testing.T) {
	dir := t.TempDir()
	app := &App{Out: io.Discard, Err: io.Discard, Dir: dir}
	if code := app.Run([]string{"room", "init", "--as", "planner"}); code != ExitOK {
		t.Fatalf("room init exit = %d", code)
	}
	var joinOut bytes.Buffer
	joinApp := &App{Out: &joinOut, Err: io.Discard, Dir: dir}
	if code := joinApp.Run([]string{"room", "join", "developer"}); code != ExitOK {
		t.Fatalf("room join exit = %d", code)
	}
	var actor core.Actor
	if err := json.Unmarshal(joinOut.Bytes(), &actor); err != nil || actor.ID != "developer" {
		t.Fatalf("room join output = %q", joinOut.String())
	}
	// Repeating the join returns the existing membership.
	var repeatOut bytes.Buffer
	repeat := &App{Out: &repeatOut, Err: io.Discard, Dir: dir}
	if code := repeat.Run([]string{"room", "join", "developer"}); code != ExitOK {
		t.Fatalf("repeat join exit = %d", code)
	}
	var repeated core.Actor
	if err := json.Unmarshal(repeatOut.Bytes(), &repeated); err != nil || repeated.ID != "developer" {
		t.Fatalf("repeat join output = %q", repeatOut.String())
	}
}

func TestRoomJoinRequiresPositionalActor(t *testing.T) {
	dir := initRoom(t, "planner")
	t.Setenv("TEAROOM_ACTOR", "from-environment")
	app := &App{Out: io.Discard, Err: io.Discard, Dir: dir}
	if code := app.Run([]string{"room", "join"}); code != ExitError {
		t.Fatalf("join without positional actor exit = %d, want %d", code, ExitError)
	}
	if code := app.Run([]string{"room", "join", "--as", "bob"}); code != ExitError {
		t.Fatalf("join with --as exit = %d, want %d", code, ExitError)
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
	for _, after := range []string{"e1"} {
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

func TestReadAfterRequiresPresentationAddress(t *testing.T) {
	dir := initRoom(t, "planner")
	app := &App{Out: io.Discard, Err: io.Discard, Dir: dir}
	if code := app.Run([]string{"read", "--after", "1", "--as", "planner"}); code != ExitError {
		t.Fatalf("bare --after exit = %d, want %d", code, ExitError)
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

func TestServerStartRefusesNonLoopback(t *testing.T) {
	for _, address := range []string{":8787", "0.0.0.0:8787", "192.168.1.10:8787"} {
		dir := initRoom(t, "planner")
		app := &App{Out: io.Discard, Err: io.Discard, Dir: dir}
		if code := app.Run([]string{"server", "start", "--address", address}); code != ExitError {
			t.Fatalf("server start %s exit = %d, want %d", address, code, ExitError)
		}
	}
}

func TestServerStartHostedModeStopsOnCancellation(t *testing.T) {
	t.Setenv("TEAROOM_DSN", filepath.Join(t.TempDir(), "hosted.db"))
	t.Setenv("TEAROOM_OWNER_TOKEN", "tr_owner_cli_test")
	ctx, cancel := context.WithCancel(context.Background())
	app := &App{Out: io.Discard, Err: io.Discard, Dir: t.TempDir(), Context: ctx}
	result := make(chan error, 1)
	go func() { result <- app.serverStart([]string{"--address", "127.0.0.1:0"}) }()
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("hosted serverStart error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("hosted serverStart did not stop after context cancellation")
	}
}

func TestServerStartRefusesHostedWithoutOwnerToken(t *testing.T) {
	t.Setenv("TEAROOM_DSN", filepath.Join(t.TempDir(), "hosted.db"))
	app := &App{Out: io.Discard, Err: io.Discard, Dir: t.TempDir()}
	if code := app.Run([]string{"server", "start"}); code != ExitError {
		t.Fatalf("hosted without owner token exit = %d, want %d", code, ExitError)
	}
}

func TestServerStartStopsOnContextCancellation(t *testing.T) {
	dir := initRoom(t, "planner")
	ctx, cancel := context.WithCancel(context.Background())
	app := &App{Out: io.Discard, Err: io.Discard, Dir: dir, Context: ctx}
	result := make(chan error, 1)
	go func() { result <- app.serverStart([]string{"--address", "127.0.0.1:0"}) }()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("serverStart error = %v, want clean cancellation", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serverStart did not stop after context cancellation")
	}
}

type closableStore struct {
	core.Store
	closed bool
}

func (s *closableStore) Close() error {
	s.closed = true
	return nil
}

func TestCloseStoreClosesClosableBackend(t *testing.T) {
	backend := &closableStore{}
	closeStore(backend)
	if !backend.closed {
		t.Fatal("closeStore did not close a closable backend")
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
	if code := joiner.Run([]string{"room", "join", "bob"}); code != ExitOK {
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

func TestRemoteJourneyThroughCLI(t *testing.T) {
	// A hosted service over a SQLite deployment database stands in for the
	// remote deployment; the journey is identical over PostgreSQL.
	db, err := relational.OpenDatabase(filepath.Join(t.TempDir(), "journey.db"))
	if err != nil {
		t.Fatal(err)
	}
	hostedServer, err := hosted.New(db, "tr_owner_journey", nil)
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(hostedServer.Handler())
	t.Cleanup(httpServer.Close)

	// The owner creates the room and gets its room URL back.
	ownerHome := t.TempDir()
	out := &bytes.Buffer{}
	owner := &App{Out: out, Err: io.Discard, Dir: ownerHome}
	t.Setenv("TEAROOM_TOKEN", "tr_owner_journey")
	if code := owner.Run([]string{"room", "init", "--server", httpServer.URL, "--as", "planner"}); code != ExitOK {
		t.Fatalf("remote room init exit = %d, output %s", code, out.String())
	}
	var created struct {
		ID     string `json:"id"`
		Server string `json:"server"`
	}
	if err := json.Unmarshal(out.Bytes(), &created); err != nil || created.Server == "" {
		t.Fatalf("remote init output = %q", out.String())
	}

	// Every following command targets the room URL with the right bearer.
	t.Setenv("TEAROOM_SERVER", created.Server)
	t.Setenv("TEAROOM_TOKEN", "tr_owner_journey")
	t.Setenv("TEAROOM_ACTOR", "planner")
	app := &App{Out: io.Discard, Err: io.Discard, Dir: ownerHome}
	if code := app.Run([]string{"room", "join", "bob"}); code != ExitOK {
		t.Fatal("owner join failed")
	}

	// Issue bob a credential; the plaintext token prints exactly once.
	issueOut := &bytes.Buffer{}
	issuer := &App{Out: issueOut, Err: io.Discard, Dir: ownerHome}
	if code := issuer.Run([]string{"room", "credential", "issue", "--actor", "bob", "--expires", "24h"}); code != ExitOK {
		t.Fatalf("credential issue exit = %d, output %s", code, issueOut.String())
	}
	var issued struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(issueOut.Bytes(), &issued); err != nil || !strings.HasPrefix(issued.Token, "tr_") {
		t.Fatalf("issued = %q", issueOut.String())
	}

	// bob writes with the credential.
	bobHome := t.TempDir()
	t.Setenv("TEAROOM_TOKEN", issued.Token)
	t.Setenv("TEAROOM_ACTOR", "bob")
	bob := &App{Out: io.Discard, Err: io.Discard, Dir: bobHome}
	if code := bob.Run([]string{"send", "from bob"}); code != ExitOK {
		t.Fatal("credentialed send failed")
	}

	// The owner revokes; bob's next write fails without touching history.
	t.Setenv("TEAROOM_TOKEN", "tr_owner_journey")
	t.Setenv("TEAROOM_ACTOR", "planner")
	if code := app.Run([]string{"room", "credential", "revoke", issued.ID}); code != ExitOK {
		t.Fatal("revoke failed")
	}
	t.Setenv("TEAROOM_TOKEN", issued.Token)
	revoked := &App{Out: io.Discard, Err: io.Discard, Dir: bobHome}
	if code := revoked.Run([]string{"send", "after revoke"}); code != ExitError {
		t.Fatal("revoked credential write succeeded")
	}

	// Reads stay open with no token at all.
	t.Setenv("TEAROOM_TOKEN", "")
	reader := &App{Out: io.Discard, Err: io.Discard, Dir: bobHome}
	if code := reader.Run([]string{"read"}); code != ExitOK {
		t.Fatal("open read failed")
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
