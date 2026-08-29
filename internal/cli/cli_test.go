// CLI tests protect command routing, help discovery, and machine-safe arguments.
package cli

import (
	"bytes"
	"flag"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/williamfzc/tearoom/internal/core"
	"github.com/williamfzc/tearoom/internal/store"
)

func TestRootAndGroupHelp(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"--help"}, "tearoom <command> [subcommand]"},
		{[]string{"room", "--help"}, "tearoom room <command>"},
		{[]string{"server", "--help"}, "tearoom server <command>"},
		{[]string{"event", "--help"}, "tearoom event <command>"},
		{[]string{"event", "send", "--help"}, "--body string"},
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
	for _, args := range [][]string{{"actor"}, {"room", "get"}, {"room", "serve"}, {"event", "create"}} {
		app := &App{Out: io.Discard, Err: io.Discard, Dir: t.TempDir()}
		if code := app.Run(args); code != ExitUsage {
			t.Fatalf("%v exit = %d, want %d", args, code, ExitUsage)
		}
	}
}

func TestOldRequestIDFlagIsRejected(t *testing.T) {
	app := &App{Out: io.Discard, Err: io.Discard, Dir: t.TempDir()}
	if code := app.Run([]string{"event", "send", "hello", "--request-id", "value"}); code != ExitUsage {
		t.Fatalf("--request-id exit = %d, want %d", code, ExitUsage)
	}
}

func TestEventSendAcceptsBodyAndID(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("TEAROOM_HOME", home)
	if _, err := store.Init(dir, "test", store.BackendFile); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	app := &App{Out: &out, Err: io.Discard, Dir: dir}
	if code := app.Run([]string{"event", "send", "--as", "alice", "--body", "hello", "--id", "request-1"}); code != ExitOK {
		t.Fatalf("event send exit = %d", code)
	}
	if !strings.Contains(out.String(), `"body":"hello"`) || !strings.Contains(out.String(), `"key":"request-1"`) {
		t.Fatalf("event send output = %s", out.String())
	}
}

func TestEventSendRejectsMultipleBodies(t *testing.T) {
	app := &App{Out: io.Discard, Err: io.Discard, Dir: t.TempDir()}
	if code := app.Run([]string{"event", "send", "hello", "--body", "other", "--as", "alice"}); code != ExitUsage {
		t.Fatalf("event send exit = %d, want %d", code, ExitUsage)
	}
}

func TestRequestIDDependsOnSavedCursorAndContent(t *testing.T) {
	base := coreInput("a", 4, "hello")
	first := deriveRequestID(base)
	if first != deriveRequestID(base) {
		t.Fatal("same input produced a different request id")
	}
	base.Expected++
	if first == deriveRequestID(base) {
		t.Fatal("different cursor produced the same request id")
	}
	base.Expected--
	base.Body = "other"
	if first == deriveRequestID(base) {
		t.Fatal("different content produced the same request id")
	}
}

func coreInput(actor string, cursor uint64, body string) core.AppendInput {
	return core.AppendInput{Actor: actor, Expected: cursor, Type: "message", Body: body}
}

func TestSessionPathScopesByServerAndActor(t *testing.T) {
	t.Setenv("TEAROOM_HOME", t.TempDir())
	app := &App{Dir: t.TempDir()}
	t.Setenv("TEAROOM_SERVER", "http://room-a.example")
	first, err := app.sessionPath("alice")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEAROOM_SERVER", "http://room-b.example")
	second, err := app.sessionPath("alice")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEAROOM_SERVER", "http://room-a.example")
	third, err := app.sessionPath("bob")
	if err != nil {
		t.Fatal(err)
	}
	if first == second || first == third {
		t.Fatalf("session paths are not independently scoped: %q %q %q", first, second, third)
	}
}

func TestLocalSessionPathUsesRoomAndActor(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TEAROOM_HOME", home)
	t.Setenv("TEAROOM_SERVER", "")
	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	if _, err := store.Init(firstRoot, "first", store.BackendFile); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Init(secondRoot, "second", store.BackendFile); err != nil {
		t.Fatal(err)
	}
	first, err := (&App{Dir: firstRoot}).sessionPath("alice")
	if err != nil {
		t.Fatal(err)
	}
	second, err := (&App{Dir: secondRoot}).sessionPath("alice")
	if err != nil {
		t.Fatal(err)
	}
	third, err := (&App{Dir: firstRoot}).sessionPath("bob")
	if err != nil {
		t.Fatal(err)
	}
	if first == second || first == third || !strings.HasPrefix(first, home) {
		t.Fatalf("local session paths are not independently scoped: %q %q %q", first, second, third)
	}
}

func TestSessionRoundTrip(t *testing.T) {
	path := t.TempDir() + "/nested/session.json"
	want := session{Actor: "alice", Cursor: 7}
	if err := writeSession(path, want); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("session mode = %o, want 600", info.Mode().Perm())
	}
	got, err := readSession(path, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("session = %+v, want %+v", got, want)
	}
}

func TestParseAcceptsFlagsAfterText(t *testing.T) {
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	format := flags.String("format", "json", "")
	cursor := flags.Uint64("cursor", 0, "")
	if err := parse(flags, []string{"hello", "world", "--format", "text", "--cursor", "4"}); err != nil {
		t.Fatal(err)
	}
	if *format != "text" || *cursor != 4 || strings.Join(flags.Args(), " ") != "hello world" {
		t.Fatalf("format=%s cursor=%d args=%q", *format, *cursor, flags.Args())
	}
}

func TestParseRejectsUnknownFlag(t *testing.T) {
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.String("format", "json", "")
	if err := parse(flags, []string{"--fromat", "json"}); err == nil {
		t.Fatal("unknown flag accepted")
	}
}
