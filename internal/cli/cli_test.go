// CLI tests protect command routing, help discovery, machine-safe arguments,
// and the exit-code contract for the room-tree command surface.
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
	"github.com/williamfzc/tearoom/internal/relational"
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
	want := "tearoom version=1.2.3 commit=abc1234 store-format=3\n"
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
		"room init",
		"tearoom read --room <room> [--after e<N>]",
		"--idempotency-key",
		"tearoom watch --json",
		"tearoom <command> --help",
	} {
		if !strings.Contains(guide, want) {
			t.Fatalf("skill guide = %q, want it to contain %q", guide, want)
		}
	}

	// The guide is orientation, not a Room read: it must not depend on one.
	out.Reset()
	app = &App{Out: &out, Err: io.Discard, Dir: t.TempDir()}
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
		{[]string{"room", "--help"}, "tearoom room init --name"},
		{[]string{"send", "--help"}, "--text"},
		{[]string{"read", "--help"}, "--after"},
		{[]string{"tree", "--help"}, "tearoom tree"},
		{[]string{"watch", "--help"}, "--json"},
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
		{"list"},
		{"hosted"},
		{"actor"},
		{"help"},
		{"room", "join"},
		{"room", "join", "bob"},
		{"room", "credential"},
		{"room", "credential", "issue", "--actor", "bob", "--expires", "24h"},
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
	dir := initRoom(t, "main-line")
	for _, args := range [][]string{
		{"send", "hello", "--cursor", "3", "--as", "alice"},
		{"send", "hello", "--body", "x", "--as", "alice"},
		{"send", "hello", "--to", "bob", "--as", "alice"},
		{"read", "--limit", "2"},
		{"read", "--wait", "5s"},
		{"room", "init", "--as", "alice", "--store", "file"},
		{"server", "start", "--directory", t.TempDir()},
	} {
		app := &App{Out: io.Discard, Err: io.Discard, Dir: dir}
		if code := app.Run(args); code != ExitError {
			t.Fatalf("%v exit = %d, want %d", args, code, ExitError)
		}
	}
}

func TestRoomInitCreatesNamedRoot(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	app := &App{Out: &out, Err: io.Discard, Dir: dir}
	if code := app.Run([]string{"room", "init", "--name", "main-line"}); code != ExitOK {
		t.Fatalf("room init exit = %d, output %s", code, out.String())
	}
	var room core.Room
	if err := json.Unmarshal(out.Bytes(), &room); err != nil {
		t.Fatalf("room init output = %q", out.String())
	}
	if room.Name != "main-line" || room.Parent != "" || room.Created.IsZero() {
		t.Fatalf("room = %+v, want the named root with a creation time", room)
	}
}

func TestRoomInitCreatesChildAndRefusesBadParents(t *testing.T) {
	dir := t.TempDir()
	app := &App{Out: io.Discard, Err: io.Discard, Dir: dir}
	if code := app.Run([]string{"room", "init", "--name", "main-line"}); code != ExitOK {
		t.Fatalf("first init exit = %d", code)
	}
	var out bytes.Buffer
	child := &App{Out: &out, Err: io.Discard, Dir: dir}
	if code := child.Run([]string{"room", "init", "--name", "implementation", "--parent", "main-line"}); code != ExitOK {
		t.Fatalf("child init exit = %d, output %s", code, out.String())
	}
	var room core.Room
	if err := json.Unmarshal(out.Bytes(), &room); err != nil || room.Parent != "main-line" {
		t.Fatalf("child = %q", out.String())
	}
	// The same workspace holds both rooms.
	st, err := relational.Open(filepath.Join(dir, ".tearoom"))
	if err != nil {
		t.Fatal(err)
	}
	list, err := st.Rooms(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("rooms = %+v, want two in one workspace", list)
	}

	if code := child.Run([]string{"room", "init", "--name", "orphan", "--parent", "missing"}); code != ExitError {
		t.Fatalf("unknown parent exit = %d, want %d", code, ExitError)
	}
	if code := child.Run([]string{"room", "init", "--name", "implementation"}); code != ExitError {
		t.Fatalf("duplicate name exit = %d, want %d", code, ExitError)
	}
	if code := child.Run([]string{"room", "init", "--name", " "}); code != ExitError {
		t.Fatalf("blank name exit = %d, want %d", code, ExitError)
	}
}

func TestRoomInitRequiresName(t *testing.T) {
	app := &App{Out: io.Discard, Err: io.Discard, Dir: t.TempDir()}
	if code := app.Run([]string{"room", "init"}); code != ExitError {
		t.Fatalf("room init without a name exit = %d, want %d", code, ExitError)
	}
}

func TestSendAcceptsTextJSONAndPositional(t *testing.T) {
	dir := initRoom(t, "main-line")
	cases := []struct {
		args    []string
		content string
	}{
		{[]string{"send", "review this"}, `"review this"`},
		{[]string{"send", "--text", "by flag"}, `"by flag"`},
		{[]string{"send", "--json", `{"plan":"review"}`}, `{"plan":"review"}`},
		{[]string{"send", "hello", "world", "--as", "codex"}, `"hello world"`},
	}
	for i, c := range cases {
		var out bytes.Buffer
		app := &App{Out: &out, Err: io.Discard, Dir: dir}
		if code := app.Run(c.args); code != ExitOK {
			t.Fatalf("send %v exit = %d, output %s", c.args, code, out.String())
		}
		var record core.Record
		if err := json.Unmarshal(out.Bytes(), &record); err != nil {
			t.Fatalf("send output = %q", out.String())
		}
		if string(record.Content) != c.content {
			t.Fatalf("send %d content = %s, want %s", i, record.Content, c.content)
		}
		if record.Room != "main-line" || record.Seq != uint64(i+1) {
			t.Fatalf("send %d record = %+v", i, record)
		}
	}
	// Two content sources at once are a refusal, never a merge.
	app := &App{Out: io.Discard, Err: io.Discard, Dir: dir}
	if code := app.Run([]string{"send", "--text", "one", "--json", `"two"`}); code != ExitError {
		t.Fatalf("text and json exit = %d, want %d", code, ExitError)
	}
	if code := app.Run([]string{"send", "--text", "one", "two"}); code != ExitError {
		t.Fatalf("text and positional exit = %d, want %d", code, ExitError)
	}
}

func TestSendSignatureIsOptional(t *testing.T) {
	dir := initRoom(t, "main-line")
	var out bytes.Buffer
	app := &App{Out: &out, Err: io.Discard, Dir: dir}
	t.Setenv("TEAROOM_ACTOR", "")
	if code := app.Run([]string{"send", "anonymous"}); code != ExitOK {
		t.Fatalf("send without a signature exit = %d, output %s", code, out.String())
	}
	var record core.Record
	if err := json.Unmarshal(out.Bytes(), &record); err != nil || record.Author != "" {
		t.Fatalf("record = %q, want an empty signature", out.String())
	}

	var env bytes.Buffer
	envApp := &App{Out: &env, Err: io.Discard, Dir: dir}
	t.Setenv("TEAROOM_ACTOR", "from-environment")
	if code := envApp.Run([]string{"send", "labeled"}); code != ExitOK {
		t.Fatalf("send with env signature exit = %d", code)
	}
	var labeled core.Record
	if err := json.Unmarshal(env.Bytes(), &labeled); err != nil || labeled.Author != "from-environment" {
		t.Fatalf("record = %q, want the environment signature", env.String())
	}
}

func TestSendResolvesTheOnlyRoom(t *testing.T) {
	dir := initRoom(t, "solo")
	var out bytes.Buffer
	app := &App{Out: &out, Err: io.Discard, Dir: dir}
	if code := app.Run([]string{"send", "hello", "--as", "codex"}); code != ExitOK {
		t.Fatalf("send exit = %d, output %s", code, out.String())
	}
	var record core.Record
	if err := json.Unmarshal(out.Bytes(), &record); err != nil || record.Room != "solo" {
		t.Fatalf("record = %q, want it to land in solo", out.String())
	}
}

func TestSendReadRefuseAmbiguousRooms(t *testing.T) {
	dir := initRoom(t, "one")
	app := &App{Out: io.Discard, Err: io.Discard, Dir: dir}
	if code := app.Run([]string{"room", "init", "--name", "two"}); code != ExitOK {
		t.Fatalf("second room exit = %d", code)
	}
	if code := app.Run([]string{"send", "hello"}); code != ExitError {
		t.Fatalf("send without --room exit = %d, want %d", code, ExitError)
	}
	if code := app.Run([]string{"read"}); code != ExitError {
		t.Fatalf("read without --room exit = %d, want %d", code, ExitError)
	}
}

func TestSendUnknownRoomFails(t *testing.T) {
	dir := initRoom(t, "main-line")
	app := &App{Out: io.Discard, Err: io.Discard, Dir: dir}
	if code := app.Run([]string{"send", "hello", "--room", "missing"}); code != ExitError {
		t.Fatalf("send to an unknown room exit = %d, want %d", code, ExitError)
	}
}

func TestReadPrintsJSONL(t *testing.T) {
	dir := initRoom(t, "main-line")
	app := &App{Out: io.Discard, Err: io.Discard, Dir: dir}
	for _, text := range []string{"one", "two"} {
		if code := app.Run([]string{"send", text, "--as", "planner"}); code != ExitOK {
			t.Fatalf("send %s exit = %d", text, code)
		}
	}
	var out bytes.Buffer
	reader := &App{Out: &out, Err: io.Discard, Dir: dir}
	if code := reader.Run([]string{"read", "--room", "main-line"}); code != ExitOK {
		t.Fatalf("read exit = %d", code)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("read output = %q, want 2 JSONL lines", out.String())
	}
	for _, line := range lines {
		var record core.Record
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("read line %q: %v", line, err)
		}
	}
}

func TestReadEmptyPrintsNothing(t *testing.T) {
	dir := initRoom(t, "main-line")
	var out bytes.Buffer
	app := &App{Out: &out, Err: io.Discard, Dir: dir}
	if code := app.Run([]string{"read"}); code != ExitOK {
		t.Fatalf("empty read exit = %d, want 0", code)
	}
	if out.Len() != 0 {
		t.Fatalf("empty read output = %q, want nothing", out.String())
	}
}

func TestReadAfterBoundary(t *testing.T) {
	dir := initRoom(t, "main-line")
	app := &App{Out: io.Discard, Err: io.Discard, Dir: dir}
	for i := 1; i <= 3; i++ {
		if code := app.Run([]string{"send", "m", "--as", "planner"}); code != ExitOK {
			t.Fatalf("send %d exit = %d", i, code)
		}
	}
	var out bytes.Buffer
	reader := &App{Out: &out, Err: io.Discard, Dir: dir}
	if code := reader.Run([]string{"read", "--after", "e1"}); code != ExitOK {
		t.Fatalf("read --after exit = %d", code)
	}
	var records []core.Record
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var record core.Record
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("read line %q: %v", line, err)
		}
		records = append(records, record)
	}
	if len(records) != 2 || records[0].Seq != 2 || records[1].Seq != 3 {
		t.Fatalf("read --after e1 = %+v, want seqs 2 and 3", records)
	}

	// A bare number is not the presentation address.
	app = &App{Out: io.Discard, Err: io.Discard, Dir: dir}
	if code := app.Run([]string{"read", "--after", "1"}); code != ExitError {
		t.Fatalf("bare --after exit = %d, want %d", code, ExitError)
	}
}

func TestReadRepeatedHasNoSideEffects(t *testing.T) {
	dir := initRoom(t, "main-line")
	app := &App{Out: io.Discard, Err: io.Discard, Dir: dir}
	if code := app.Run([]string{"send", "m", "--as", "planner"}); code != ExitOK {
		t.Fatalf("send exit = %d", code)
	}
	for i := 0; i < 2; i++ {
		var out bytes.Buffer
		reader := &App{Out: &out, Err: io.Discard, Dir: dir}
		if code := reader.Run([]string{"read"}); code != ExitOK {
			t.Fatalf("read %d exit = %d", i, code)
		}
		if lines := strings.Split(strings.TrimSpace(out.String()), "\n"); len(lines) != 1 {
			t.Fatalf("read %d = %q, want 1 line", i, out.String())
		}
	}
}

func TestReadTailKeepsTheLastRecords(t *testing.T) {
	dir := initRoom(t, "main-line")
	app := &App{Out: io.Discard, Err: io.Discard, Dir: dir}
	for i := 1; i <= 5; i++ {
		if code := app.Run([]string{"send", "m"}); code != ExitOK {
			t.Fatalf("send %d exit = %d", i, code)
		}
	}
	var out bytes.Buffer
	reader := &App{Out: &out, Err: io.Discard, Dir: dir}
	if code := reader.Run([]string{"read", "--tail", "2"}); code != ExitOK {
		t.Fatalf("read --tail exit = %d", code)
	}
	var seqs []uint64
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var record core.Record
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		seqs = append(seqs, record.Seq)
	}
	if len(seqs) != 2 || seqs[0] != 4 || seqs[1] != 5 {
		t.Fatalf("tail = %v, want seqs 4 and 5", seqs)
	}
}

func TestReadSubtreeWalksTheBranch(t *testing.T) {
	dir := t.TempDir()
	app := &App{Out: io.Discard, Err: io.Discard, Dir: dir}
	for _, args := range [][]string{
		{"room", "init", "--name", "main-line"},
		{"room", "init", "--name", "implementation", "--parent", "main-line"},
		{"room", "init", "--name", "login-page", "--parent", "implementation"},
		{"room", "init", "--name", "review", "--parent", "main-line"},
	} {
		if code := app.Run(args); code != ExitOK {
			t.Fatalf("%v exit = %d", args, code)
		}
	}
	for _, room := range []string{"implementation", "login-page", "review"} {
		if code := app.Run([]string{"send", "note", "--room", room}); code != ExitOK {
			t.Fatalf("send to %s exit = %d", room, code)
		}
	}
	var out bytes.Buffer
	reader := &App{Out: &out, Err: io.Discard, Dir: dir}
	if code := reader.Run([]string{"read", "--room", "implementation", "--subtree"}); code != ExitOK {
		t.Fatalf("subtree read exit = %d", code)
	}
	var rooms []string
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var record core.Record
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		rooms = append(rooms, record.Room)
	}
	// Depth-first: implementation, then its child, never review.
	if strings.Join(rooms, ",") != "implementation,login-page" {
		t.Fatalf("subtree rooms = %v, want implementation,login-page", rooms)
	}
}

func TestTreePrintsTheSkeleton(t *testing.T) {
	dir := t.TempDir()
	app := &App{Out: io.Discard, Err: io.Discard, Dir: dir}
	for _, args := range [][]string{
		{"room", "init", "--name", "main-line"},
		{"room", "init", "--name", "implementation", "--parent", "main-line"},
		{"room", "init", "--name", "review", "--parent", "main-line"},
		{"room", "init", "--name", "login-page", "--parent", "implementation"},
	} {
		if code := app.Run(args); code != ExitOK {
			t.Fatalf("%v exit = %d", args, code)
		}
	}
	var out bytes.Buffer
	tree := &App{Out: &out, Err: io.Discard, Dir: dir}
	if code := tree.Run([]string{"tree"}); code != ExitOK {
		t.Fatalf("tree exit = %d", code)
	}
	want := "main-line\n├── implementation\n│   └── login-page\n└── review\n"
	if out.String() != want {
		t.Fatalf("tree = %q, want %q", out.String(), want)
	}
}

func TestWatchEmitsNotificationsOnAppend(t *testing.T) {
	dir := initRoom(t, "main-line")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out bytes.Buffer
	watcher := &App{Out: &out, Err: io.Discard, Dir: dir, Context: ctx}
	done := make(chan int, 1)
	go func() { done <- watcher.Run([]string{"watch", "--json"}) }()

	// Let the watcher prime its heads, send, and let at least one full tick
	// deliver the notification. The output buffer is read only after the
	// watcher has stopped, so no goroutine writes it concurrently.
	time.Sleep(1200 * time.Millisecond)
	sender := &App{Out: io.Discard, Err: io.Discard, Dir: dir}
	if code := sender.Run([]string{"send", "hello", "--as", "codex"}); code != ExitOK {
		cancel()
		t.Fatalf("send exit = %d", code)
	}
	time.Sleep(1200 * time.Millisecond)
	cancel()
	select {
	case code := <-done:
		if code != ExitOK {
			t.Fatalf("watch exit = %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("watch did not stop after context cancellation")
	}

	var notification core.Notification
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &notification); err != nil {
		t.Fatalf("watch output = %q", out.String())
	}
	if notification.Room != "main-line" || notification.Seq != 1 || notification.Author != "codex" {
		t.Fatalf("notification = %+v", notification)
	}
	if strings.Contains(out.String(), "hello") {
		t.Fatalf("watch leaked record content: %q", out.String())
	}
}

func TestServerStartRefusesNonLoopback(t *testing.T) {
	for _, address := range []string{":8787", "0.0.0.0:8787", "192.168.1.10:8787"} {
		dir := initRoom(t, "main-line")
		app := &App{Out: io.Discard, Err: io.Discard, Dir: dir}
		if code := app.Run([]string{"server", "start", "--address", address}); code != ExitError {
			t.Fatalf("server start %s exit = %d, want %d", address, code, ExitError)
		}
	}
}

func TestServerStartStopsOnContextCancellation(t *testing.T) {
	dir := initRoom(t, "main-line")
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
	case <-time.After(3 * time.Second):
		t.Fatal("serverStart did not stop after context cancellation")
	}
}

func TestServerModeRoundTrip(t *testing.T) {
	dir := initRoom(t, "main-line")
	st, err := relational.Open(dir + "/.tearoom")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	server := httptest.NewServer(transport.NewServer(st).Handler())
	defer server.Close()
	t.Setenv("TEAROOM_SERVER", server.URL)

	sender := &App{Out: io.Discard, Err: io.Discard, Dir: t.TempDir()}
	if code := sender.Run([]string{"send", "hello", "--as", "planner"}); code != ExitOK {
		t.Fatalf("http send exit = %d", code)
	}
	var out bytes.Buffer
	reader := &App{Out: &out, Err: io.Discard, Dir: t.TempDir()}
	if code := reader.Run([]string{"read", "--room", "main-line"}); code != ExitOK {
		t.Fatalf("http read exit = %d", code)
	}
	if lines := strings.Split(strings.TrimSpace(out.String()), "\n"); len(lines) != 1 {
		t.Fatalf("http read = %q, want 1 line", out.String())
	}
	// An unknown room is a refusal over HTTP too.
	stranger := &App{Out: io.Discard, Err: io.Discard, Dir: t.TempDir()}
	if code := stranger.Run([]string{"read", "--room", "missing"}); code != ExitError {
		t.Fatalf("http unknown-room read exit = %d, want %d", code, ExitError)
	}
}

func TestIdempotencyConflictExitsOne(t *testing.T) {
	dir := initRoom(t, "main-line")
	st, err := relational.Open(dir + "/.tearoom")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	server := httptest.NewServer(transport.NewServer(st).Handler())
	defer server.Close()
	t.Setenv("TEAROOM_SERVER", server.URL)

	app := &App{Out: io.Discard, Err: io.Discard, Dir: t.TempDir()}
	if code := app.Run([]string{"send", "--json", `{"plan":1}`, "--idempotency-key", "k1", "--as", "planner"}); code != ExitOK {
		t.Fatalf("first append exit = %d", code)
	}
	retry := &App{Out: io.Discard, Err: io.Discard, Dir: t.TempDir()}
	if code := retry.Run([]string{"send", "--json", `{"plan":2}`, "--idempotency-key", "k1", "--as", "planner"}); code != ExitError {
		t.Fatalf("conflicting retry exit = %d, want %d (HTTP 409)", code, ExitError)
	}
	// An equal retry returns the original record and exits 0.
	var ok bytes.Buffer
	equal := &App{Out: &ok, Err: io.Discard, Dir: t.TempDir()}
	if code := equal.Run([]string{"send", "--json", `{"plan": 1}`, "--idempotency-key", "k1", "--as", "planner"}); code != ExitOK {
		t.Fatalf("equal retry exit = %d, want 0", code)
	}
	var record core.Record
	if err := json.Unmarshal(ok.Bytes(), &record); err != nil || record.Seq != 1 {
		t.Fatalf("equal retry returned %s, want the original record", ok.String())
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

func TestParseAcceptsFlagsAfterText(t *testing.T) {
	dir := initRoom(t, "main-line")
	var out bytes.Buffer
	app := &App{Out: &out, Err: io.Discard, Dir: dir}
	if code := app.Run([]string{"send", "hello", "--as", "codex"}); code != ExitOK {
		t.Fatalf("send exit = %d, output %s", code, out.String())
	}
	var record core.Record
	if err := json.Unmarshal(out.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if string(record.Content) != `"hello"` || record.Author != "codex" {
		t.Fatalf("record = %+v", record)
	}
}

func TestParseRejectsUnknownFlag(t *testing.T) {
	dir := initRoom(t, "main-line")
	app := &App{Out: io.Discard, Err: io.Discard, Dir: dir}
	if code := app.Run([]string{"send", "hello", "--fromat", "json", "--as", "codex"}); code != ExitError {
		t.Fatalf("unknown flag exit = %d, want %d", code, ExitError)
	}
}

func initRoom(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	app := &App{Out: io.Discard, Err: io.Discard, Dir: dir}
	if code := app.Run([]string{"room", "init", "--name", name}); code != ExitOK {
		t.Fatalf("room init exit = %d", code)
	}
	return dir
}
