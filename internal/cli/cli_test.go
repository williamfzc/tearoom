package cli

import (
	"flag"
	"io"
	"strings"
	"testing"
)

// Go's flag package stops parsing at the first positional argument, so
// `tea post "title" --json` silently swallowed --json as part of the title. Agents
// write flags in whatever order reads naturally, so parse() reorders first. These
// tests pin that behaviour down.

func TestParseAcceptsFlagsAfterPositionals(t *testing.T) {
	f := flag.NewFlagSet("post", flag.ContinueOnError)
	f.SetOutput(io.Discard) // silence usage text on expected errors
	asJSON := f.Bool("json", false, "")
	body := f.String("body", "", "")

	if err := parse(f, []string{"hello world", "--json", "--body", "b"}); err != nil {
		t.Fatal(err)
	}
	if !*asJSON {
		t.Error("--json after a positional was swallowed; agents hit this immediately")
	}
	if *body != "b" {
		t.Errorf("body = %q, want b", *body)
	}
	if f.Arg(0) != "hello world" {
		t.Errorf("positional = %q, want the title intact", f.Arg(0))
	}
}

func TestParseAcceptsFlagsBeforePositionals(t *testing.T) {
	f := flag.NewFlagSet("post", flag.ContinueOnError)
	f.SetOutput(io.Discard) // silence usage text on expected errors
	asJSON := f.Bool("json", false, "")
	body := f.String("body", "", "")

	// The conventional order must keep working after the reordering fix.
	if err := parse(f, []string{"--body", "b", "--json", "second thread"}); err != nil {
		t.Fatal(err)
	}
	if !*asJSON || *body != "b" || f.Arg(0) != "second thread" {
		t.Errorf("json=%v body=%q arg=%q", *asJSON, *body, f.Arg(0))
	}
}

func TestParseKeepsMultiWordPositionals(t *testing.T) {
	f := flag.NewFlagSet("post", flag.ContinueOnError)
	f.SetOutput(io.Discard) // silence usage text on expected errors
	asJSON := f.Bool("json", false, "")

	if err := parse(f, []string{"fix", "the", "flaky", "test", "--json"}); err != nil {
		t.Fatal(err)
	}
	if !*asJSON {
		t.Error("trailing --json lost")
	}
	if got := strings.Join(f.Args(), " "); got != "fix the flaky test" {
		t.Errorf("positionals = %q", got)
	}
}

func TestParseRespectsDoubleDash(t *testing.T) {
	f := flag.NewFlagSet("post", flag.ContinueOnError)
	f.SetOutput(io.Discard) // silence usage text on expected errors
	asJSON := f.Bool("json", false, "")

	// After --, everything is literal. A title that happens to contain --json must
	// survive verbatim, or agents cannot post about flags.
	if err := parse(f, []string{"--", "a title with --json inside"}); err != nil {
		t.Fatal(err)
	}
	if *asJSON {
		t.Error("--json after -- was treated as a flag")
	}
	if f.Arg(0) != "a title with --json inside" {
		t.Errorf("positional = %q", f.Arg(0))
	}
}

func TestParseHandlesValueFlagsWithEquals(t *testing.T) {
	f := flag.NewFlagSet("claim", flag.ContinueOnError)
	f.SetOutput(io.Discard) // silence usage text on expected errors
	ttl := f.String("ttl", "", "")

	if err := parse(f, []string{"src/**", "--ttl=30m"}); err != nil {
		t.Fatal(err)
	}
	if *ttl != "30m" {
		t.Errorf("ttl = %q, want 30m", *ttl)
	}
	if f.Arg(0) != "src/**" {
		t.Errorf("resource = %q", f.Arg(0))
	}
}

func TestParseDoesNotEatPositionalAfterBoolFlag(t *testing.T) {
	f := flag.NewFlagSet("post", flag.ContinueOnError)
	f.SetOutput(io.Discard) // silence usage text on expected errors
	asJSON := f.Bool("json", false, "")

	// A bool flag takes no value, so the token after it is a positional, not its
	// argument. Getting this wrong would silently drop the title.
	if err := parse(f, []string{"--json", "my title"}); err != nil {
		t.Fatal(err)
	}
	if !*asJSON || f.Arg(0) != "my title" {
		t.Errorf("json=%v arg=%q", *asJSON, f.Arg(0))
	}
}

func TestParseValueFlagStillTakesItsValue(t *testing.T) {
	f := flag.NewFlagSet("claim", flag.ContinueOnError)
	f.SetOutput(io.Discard) // silence usage text on expected errors
	ttl := f.String("ttl", "", "")

	// The space-separated form must not mistake 30m for a positional.
	if err := parse(f, []string{"--ttl", "30m", "src/**"}); err != nil {
		t.Fatal(err)
	}
	if *ttl != "30m" {
		t.Errorf("ttl = %q", *ttl)
	}
	if f.Arg(0) != "src/**" {
		t.Errorf("resource = %q, want src/**", f.Arg(0))
	}
}

func TestParseRejectsUnknownFlag(t *testing.T) {
	f := flag.NewFlagSet("post", flag.ContinueOnError)
	f.SetOutput(io.Discard) // silence usage text on expected errors
	f.Bool("json", false, "")

	// A typo must fail loudly rather than being treated as part of the title.
	if err := parse(f, []string{"title", "--jsonn"}); err == nil {
		t.Error("unknown flag accepted; a typo would silently change behaviour")
	}
}
