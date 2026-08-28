// Package cli exposes Tearoom as a small workspace and event command surface.
// It owns local session convenience while Store owns every shared invariant.
package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/williamfzc/tearoom/internal/core"
	"github.com/williamfzc/tearoom/internal/store"
	"github.com/williamfzc/tearoom/internal/transport"
)

const (
	ExitOK       = 0
	ExitUsage    = 1
	ExitConflict = 2
	ExitTimeout  = 3
	ExitStore    = 4
)

const rootHelp = `tearoom — a durable room for agents

Usage:
  tearoom <command> [subcommand] [flags]

Commands:
  room      manage the local workspace
  server    run a shared room service
  event     send, list, and watch events

Environment:
  TEAROOM_ACTOR    stable actor identity
  TEAROOM_SERVER   shared server URL; omit for the nearest local room
  TEAROOM_TOKEN    bearer token for a server that requires one
  TEAROOM_HOME     client state directory; defaults to the user config directory

Use "tearoom <command> --help" for more information.
`

const roomHelp = `Manage the local room workspace.

Usage:
  tearoom room <command> [flags]

Commands:
  init    create a room in the current directory
  info    show the current room
`

const serverHelp = `Run a shared room service.

Usage:
  tearoom server <command> [flags]

Commands:
  start   serve a local room over HTTP
`

const eventHelp = `Exchange ordered events.

Usage:
  tearoom event <command> [flags]

Commands:
  send    send a text or JSON event
  list    list currently available events and exit
  watch   wait for later events
`

type App struct {
	Out io.Writer
	Err io.Writer
	Dir string
}

type session struct {
	Actor  string `json:"actor"`
	Cursor uint64 `json:"cursor"`
}

func New() *App {
	dir, _ := os.Getwd()
	return &App{Out: os.Stdout, Err: os.Stderr, Dir: dir}
}

func (a *App) Run(args []string) int {
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Fprint(a.Out, rootHelp)
		return ExitOK
	}
	var err error
	switch args[0] {
	case "room":
		err = a.runRoom(args[1:])
	case "server":
		err = a.runServer(args[1:])
	case "event":
		err = a.runEvent(args[1:])
	case "help":
		err = a.runHelp(args[1:])
	default:
		fmt.Fprintf(a.Err, "unknown command %q\n\n%s", args[0], rootHelp)
		return ExitUsage
	}
	if err == nil || errors.Is(err, flag.ErrHelp) {
		return ExitOK
	}
	fmt.Fprintln(a.Err, err)
	switch {
	case errors.Is(err, core.ErrStaleWrite), errors.Is(err, core.ErrConflict):
		return ExitConflict
	case errors.Is(err, errTimeout):
		return ExitTimeout
	case errors.Is(err, core.ErrNoRoom), strings.Contains(err.Error(), "server unreachable"):
		return ExitStore
	default:
		return ExitUsage
	}
}

func (a *App) runHelp(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(a.Out, rootHelp)
		return nil
	}
	switch args[0] {
	case "room":
		fmt.Fprint(a.Out, roomHelp)
	case "server":
		fmt.Fprint(a.Out, serverHelp)
	case "event":
		fmt.Fprint(a.Out, eventHelp)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
	return nil
}

func (a *App) runRoom(args []string) error {
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Fprint(a.Out, roomHelp)
		return nil
	}
	switch args[0] {
	case "init":
		return a.roomInit(args[1:])
	case "info":
		return a.roomInfo(args[1:])
	default:
		return fmt.Errorf("unknown room command %q", args[0])
	}
}

func (a *App) runServer(args []string) error {
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Fprint(a.Out, serverHelp)
		return nil
	}
	if args[0] != "start" {
		return fmt.Errorf("unknown server command %q", args[0])
	}
	return a.serverStart(args[1:])
}

func (a *App) runEvent(args []string) error {
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Fprint(a.Out, eventHelp)
		return nil
	}
	switch args[0] {
	case "send":
		return a.eventSend(args[1:])
	case "list":
		return a.eventList(args[1:])
	case "watch":
		return a.eventWatch(args[1:])
	default:
		return fmt.Errorf("unknown event command %q", args[0])
	}
}

func (a *App) roomInit(args []string) error {
	const help = `Create a room in the current directory.

Usage:
  tearoom room init [flags]

Flags:
  --name string      room name; defaults to the directory name
  --format string    json | text (default "json")
  -h, --help         show help
`
	flags := newFlags("room init", a.Out, help)
	name := flags.String("name", "", "")
	format := flags.String("format", "json", "")
	if err := parseNoArgs(flags, args); err != nil {
		return err
	}
	if err := validateFormat(*format); err != nil {
		return err
	}
	if os.Getenv("TEAROOM_SERVER") != "" {
		return errors.New("room init is local; unset TEAROOM_SERVER")
	}
	file, err := store.Init(a.Dir, *name)
	if err != nil {
		return err
	}
	room, err := file.Room(context.Background())
	if err != nil {
		return err
	}
	return output(a.Out, room, *format, fmt.Sprintf("room %s created\n", room.Name))
}

func (a *App) roomInfo(args []string) error {
	const help = `Show the current room.

Usage:
  tearoom room info [flags]

Flags:
  --format string    json | text (default "json")
  -h, --help         show help
`
	flags := newFlags("room info", a.Out, help)
	format := flags.String("format", "json", "")
	if err := parseNoArgs(flags, args); err != nil {
		return err
	}
	if err := validateFormat(*format); err != nil {
		return err
	}
	st, err := a.openStore()
	if err != nil {
		return err
	}
	room, err := st.Room(context.Background())
	if err != nil {
		return err
	}
	return output(a.Out, room, *format, fmt.Sprintf("%s\n", room.Name))
}

func (a *App) serverStart(args []string) error {
	const help = `Serve a local room over HTTP.

Usage:
  tearoom server start [flags]

Flags:
  --address string      listen address (default ":8787")
  --directory string    directory containing the room (default ".")
  -h, --help            show help
`
	flags := newFlags("server start", a.Out, help)
	address := flags.String("address", ":8787", "")
	directory := flags.String("directory", a.Dir, "")
	if err := parseNoArgs(flags, args); err != nil {
		return err
	}
	file, err := store.Open(*directory)
	if err != nil {
		return fmt.Errorf("open room under %s: %w", *directory, err)
	}
	server := &http.Server{Addr: *address, Handler: transport.NewServer(file).Handler(), ReadHeaderTimeout: 10 * time.Second}
	fmt.Fprintf(a.Err, "tearoom serving %s on %s\n", file.Root(), *address)
	return server.ListenAndServe()
}

func (a *App) eventList(args []string) error {
	const help = `List currently available events and exit.

The saved session cursor is used and advanced automatically.

Usage:
  tearoom event list [flags]

Flags:
  --as string        actor identity; defaults to TEAROOM_ACTOR
  --cursor uint      override the saved cursor
  --limit int        maximum visible events; 0 means all
  --format string    json | text (default "json")
  -h, --help         show help
`
	flags := newFlags("event list", a.Out, help)
	as := flags.String("as", "", "")
	cursor := flags.Uint64("cursor", 0, "")
	limit := flags.Int("limit", 0, "")
	format := flags.String("format", "json", "")
	if err := parseNoArgs(flags, args); err != nil {
		return err
	}
	if *limit < 0 {
		return errors.New("--limit must be non-negative")
	}
	if err := validateFormat(*format); err != nil {
		return err
	}
	st, actor, state, statePath, err := a.prepare(*as)
	if err != nil {
		return err
	}
	after := state.Cursor
	if flagWasSet(flags, "cursor") {
		after = *cursor
	}
	feed, err := st.Read(context.Background(), actor, after, *limit)
	if err != nil {
		return err
	}
	state.Cursor = feed.Cursor
	if err := writeSession(statePath, state); err != nil {
		return err
	}
	return outputFeed(a.Out, feed, *format)
}

func (a *App) eventSend(args []string) error {
	const help = `Send an event.

A plain text Event needs no protocol flags:
  tearoom event send "review worktree A"

The saved session cursor protects against stale writes. Advanced flags override
Event protocol fields but do not fetch a newer cursor implicitly.

Usage:
  tearoom event send <text> [flags]
  tearoom event send --body <text> [flags]
  tearoom event send --data <json> [flags]

Flags:
  --as string            actor identity; defaults to TEAROOM_ACTOR
  --body string          text body instead of positional text
  --data string          JSON data instead of text
  --type string          event type (default "message")
  --to string            comma-separated recipients
  --caused-by string     causing Event id
  --cursor uint          override the saved expected cursor
  --id string            override the derived idempotency key
  --format string        json | text (default "json")
  -h, --help             show help
`
	flags := newFlags("event send", a.Out, help)
	as := flags.String("as", "", "")
	body := flags.String("body", "", "")
	data := flags.String("data", "", "")
	typeName := flags.String("type", "message", "")
	to := flags.String("to", "", "")
	causedBy := flags.String("caused-by", "", "")
	cursor := flags.Uint64("cursor", 0, "")
	id := flags.String("id", "", "")
	format := flags.String("format", "json", "")
	if err := parse(flags, args); err != nil {
		return err
	}
	if err := validateFormat(*format); err != nil {
		return err
	}
	text := strings.Join(flags.Args(), " ")
	provided := 0
	for _, value := range []string{text, *body, *data} {
		if value != "" {
			provided++
		}
	}
	if provided != 1 {
		return errors.New("provide exactly one of positional text, --body, or --data")
	}
	if *body != "" {
		text = *body
	}
	st, actor, state, statePath, err := a.prepare(*as)
	if err != nil {
		return err
	}
	expected := state.Cursor
	if flagWasSet(flags, "cursor") {
		expected = *cursor
	}
	input := core.AppendInput{Actor: actor, Expected: expected, Type: *typeName, Body: text, To: splitCSV(*to), CausedBy: *causedBy}
	if *data != "" {
		input.Data = json.RawMessage(*data)
	}
	input.Key = strings.TrimSpace(*id)
	if input.Key == "" {
		input.Key = deriveRequestID(input)
	}
	event, err := st.AppendEvent(context.Background(), input)
	if err != nil {
		return err
	}
	state.Cursor = event.Seq
	if err := writeSession(statePath, state); err != nil {
		return err
	}
	return output(a.Out, event, *format, fmt.Sprintf("event %s sent at cursor %d\n", event.ID, event.Seq))
}

var errTimeout = errors.New("watch timed out")

func (a *App) eventWatch(args []string) error {
	const help = `Wait for later events.

The saved session cursor is used and advanced automatically.

Usage:
  tearoom event watch [flags]

Flags:
  --as string             actor identity; defaults to TEAROOM_ACTOR
  --cursor uint           override the saved cursor
  --timeout duration      maximum wait (default 5m)
  --poll-interval duration  polling interval (default 250ms)
  --format string         json | text (default "json")
  -h, --help              show help
`
	flags := newFlags("event watch", a.Out, help)
	as := flags.String("as", "", "")
	cursor := flags.Uint64("cursor", 0, "")
	timeout := flags.Duration("timeout", 5*time.Minute, "")
	interval := flags.Duration("poll-interval", 250*time.Millisecond, "")
	format := flags.String("format", "json", "")
	if err := parseNoArgs(flags, args); err != nil {
		return err
	}
	if *timeout <= 0 || *interval <= 0 {
		return errors.New("--timeout and --poll-interval must be positive")
	}
	if err := validateFormat(*format); err != nil {
		return err
	}
	st, actor, state, statePath, err := a.prepare(*as)
	if err != nil {
		return err
	}
	after := state.Cursor
	if flagWasSet(flags, "cursor") {
		after = *cursor
	}
	deadline := time.Now().Add(*timeout)
	for {
		feed, err := st.Read(context.Background(), actor, after, 0)
		if err != nil {
			return err
		}
		if feed.Cursor > after {
			state.Cursor = feed.Cursor
			if err := writeSession(statePath, state); err != nil {
				return err
			}
			return outputFeed(a.Out, feed, *format)
		}
		if !time.Now().Before(deadline) {
			return errTimeout
		}
		time.Sleep(*interval)
	}
}

func (a *App) prepare(flagActor string) (core.Store, string, session, string, error) {
	actor, err := actorID(flagActor)
	if err != nil {
		return nil, "", session{}, "", err
	}
	st, err := a.openStore()
	if err != nil {
		return nil, "", session{}, "", err
	}
	if _, err := st.UpsertActor(context.Background(), actor, ""); err != nil {
		return nil, "", session{}, "", err
	}
	path, err := a.sessionPath(actor)
	if err != nil {
		return nil, "", session{}, "", err
	}
	state, err := readSession(path, actor)
	if err != nil {
		return nil, "", session{}, "", err
	}
	return st, actor, state, path, nil
}

func (a *App) openStore() (core.Store, error) {
	if server := strings.TrimRight(strings.TrimSpace(os.Getenv("TEAROOM_SERVER")), "/"); server != "" {
		return transport.NewClient(server, os.Getenv("TEAROOM_TOKEN")), nil
	}
	return store.Open(a.Dir)
}

func (a *App) sessionPath(actor string) (string, error) {
	var scope string
	if server := strings.TrimRight(strings.TrimSpace(os.Getenv("TEAROOM_SERVER")), "/"); server != "" {
		scope = "server:" + server
	} else {
		root, err := store.Find(a.Dir)
		if err != nil {
			return "", err
		}
		scope = "room:" + root
	}
	sum := sha256.Sum256([]byte(scope + "\x00" + actor))
	name := hex.EncodeToString(sum[:16]) + ".json"
	return filepath.Join(sessionHome(), "sessions", name), nil
}

func sessionHome() string {
	if root := strings.TrimSpace(os.Getenv("TEAROOM_HOME")); root != "" {
		return root
	}
	if root, err := os.UserConfigDir(); err == nil {
		return filepath.Join(root, "tearoom")
	}
	return filepath.Join(os.TempDir(), "tearoom")
}

func readSession(path, actor string) (session, error) {
	state := session{Actor: actor}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, fmt.Errorf("read session state: %w", err)
	}
	if state.Actor != actor {
		return state, errors.New("session actor mismatch")
	}
	return state, nil
}

func writeSession(path string, state session) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".session-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func deriveRequestID(input core.AppendInput) string {
	copy := input
	copy.Key = ""
	data, _ := json.Marshal(copy)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:16])
}

func actorID(flagValue string) (string, error) {
	value := strings.TrimSpace(flagValue)
	if value == "" {
		value = strings.TrimSpace(os.Getenv("TEAROOM_ACTOR"))
	}
	if value == "" {
		return "", errors.New("actor identity is required: set TEAROOM_ACTOR or pass --as")
	}
	return value, nil
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.Split(value, ",")
}

func outputFeed(out io.Writer, feed *core.Feed, format string) error {
	if format == "json" {
		return json.NewEncoder(out).Encode(feed)
	}
	fmt.Fprintf(out, "cursor %d\n", feed.Cursor)
	for _, event := range feed.Events {
		target := ""
		if len(event.To) > 0 {
			target = " -> " + strings.Join(event.To, ",")
		}
		fmt.Fprintf(out, "%d %s%s %s", event.Seq, event.Actor, target, event.Type)
		if event.Body != "" {
			fmt.Fprintf(out, ": %s", event.Body)
		}
		fmt.Fprintln(out)
	}
	return nil
}

func output(out io.Writer, value any, format, text string) error {
	if format == "json" {
		return json.NewEncoder(out).Encode(value)
	}
	_, err := io.WriteString(out, text)
	return err
}

func validateFormat(value string) error {
	if value != "json" && value != "text" {
		return fmt.Errorf("unsupported --format %q; use json or text", value)
	}
	return nil
}

func parseNoArgs(flags *flag.FlagSet, args []string) error {
	if err := parse(flags, args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	return nil
}

func isHelp(value string) bool { return value == "help" || value == "-h" || value == "--help" }

func newFlags(name string, out io.Writer, help string) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(out)
	flags.Usage = func() { fmt.Fprint(out, help) }
	return flags
}

func flagWasSet(flags *flag.FlagSet, name string) bool {
	found := false
	flags.Visit(func(current *flag.Flag) {
		if current.Name == name {
			found = true
		}
	})
	return found
}

// parse permits flags in any order while preserving -- as a literal boundary.
func parse(flags *flag.FlagSet, args []string) error {
	var options, positionals []string
	literal := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if literal {
			positionals = append(positionals, arg)
			continue
		}
		if arg == "--" {
			literal = true
			continue
		}
		if isHelp(arg) {
			flags.Usage()
			return flag.ErrHelp
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positionals = append(positionals, arg)
			continue
		}
		name := strings.TrimLeft(strings.SplitN(arg, "=", 2)[0], "-")
		defined := flags.Lookup(name)
		if defined == nil {
			return fmt.Errorf("flag provided but not defined: --%s", name)
		}
		options = append(options, arg)
		if strings.Contains(arg, "=") || isBoolFlag(defined) {
			continue
		}
		if i+1 >= len(args) {
			return fmt.Errorf("flag needs an argument: --%s", name)
		}
		i++
		options = append(options, args[i])
	}
	return flags.Parse(append(options, positionals...))
}

func isBoolFlag(value *flag.Flag) bool {
	type boolFlag interface{ IsBoolFlag() bool }
	flagValue, ok := value.Value.(boolFlag)
	return ok && flagValue.IsBoolFlag()
}
