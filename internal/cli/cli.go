// Package cli exposes Tearoom as the room and event command surface: explicit
// admission, pure caller-owned reads, and JSON output. It owns no session
// state; every shared invariant lives in core.Store.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/williamfzc/tearoom/internal/core"
	"github.com/williamfzc/tearoom/internal/store"
	"github.com/williamfzc/tearoom/internal/transport"
	"github.com/williamfzc/tearoom/internal/version"
)

const (
	ExitOK    = 0
	ExitError = 1
	ExitStore = 4
)

const rootHelp = `tearoom — a durable room for agents

Usage:
  tearoom <command> [flags]

Commands:
  room      init and join the nearest room
  send      append one event
  read      read events after an explicit boundary
  server    serve the nearest room over HTTP
  skill     print the agent guide: the journey and the rules

Options:
  --version show build and store format information

Environment:
  TEAROOM_ACTOR    stable actor identity
  TEAROOM_SERVER   shared server or room URL; omit for the nearest local room
  TEAROOM_TOKEN    bearer credential for writes and owner operations

Use "tearoom <command> --help" for more information.
`

const roomHelp = `Manage the local room workspace.

Usage:
  tearoom room <command> [flags]

Commands:
  init      create a room and admit the creator
  join      admit an actor as an explicit member
`

const serverHelp = `Run a shared room service.

Usage:
  tearoom server <command> [flags]

Commands:
  start   serve the nearest room over HTTP
`

type App struct {
	Out     io.Writer
	Err     io.Writer
	Dir     string
	Context context.Context
}

func New() *App {
	dir, _ := os.Getwd()
	return &App{Out: os.Stdout, Err: os.Stderr, Dir: dir, Context: context.Background()}
}

func (a *App) Run(args []string) int {
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Fprint(a.Out, rootHelp)
		return ExitOK
	}
	if isVersion(args[0]) {
		fmt.Fprintf(a.Out, "tearoom version=%s commit=%s store-format=%d\n", version.Version, version.Commit, store.FormatVersion())
		return ExitOK
	}
	var err error
	switch args[0] {
	case "room":
		err = a.runRoom(args[1:])
	case "send":
		err = a.runSend(args[1:])
	case "read":
		err = a.runRead(args[1:])
	case "server":
		err = a.runServer(args[1:])
	case "skill":
		err = a.runSkill(args[1:])
	default:
		fmt.Fprintf(a.Err, "unknown command %q\n\n%s", args[0], rootHelp)
		return ExitError
	}
	if err == nil || errors.Is(err, flag.ErrHelp) {
		return ExitOK
	}
	fmt.Fprintln(a.Err, err)
	switch {
	case errors.Is(err, core.ErrNoRoom), strings.Contains(err.Error(), "server unreachable"):
		return ExitStore
	default:
		return ExitError
	}
}

func (a *App) runRoom(args []string) error {
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Fprint(a.Out, roomHelp)
		return nil
	}
	switch args[0] {
	case "init":
		return a.roomInit(args[1:])
	case "join":
		return a.roomJoin(args[1:])
	default:
		return fmt.Errorf("unknown room command %q", args[0])
	}
}

// roomInit atomically creates a room and admits the creator: one command, one
// actor. The room identity comes from the directory that owns .tearoom; there
// is no --name flag.
func (a *App) roomInit(args []string) error {
	const help = `Create a room in the current directory and admit the creator.

Usage:
  tearoom room init [--as <actor>]

Flags:
  --as string        actor identity; defaults to TEAROOM_ACTOR
  -h, --help         show help
`
	flags := newFlags("room init", a.Out, help)
	as := flags.String("as", "", "")
	if err := parseNoArgs(flags, args); err != nil {
		return err
	}
	actor, err := actorID(*as)
	if err != nil {
		return err
	}
	if os.Getenv("TEAROOM_SERVER") != "" {
		return errors.New("room init is local; unset TEAROOM_SERVER")
	}
	st, err := store.Init(a.Dir, actor)
	if err != nil {
		return err
	}
	room, err := st.Room(context.Background())
	if err != nil {
		return err
	}
	return json.NewEncoder(a.Out).Encode(room)
}

// roomJoin is explicit admission within the trust domain of one OS user.
func (a *App) roomJoin(args []string) error {
	const help = `Join the nearest room as an explicit member.

Usage:
  tearoom room join <actor>

Arguments:
  <actor>            stable actor identity to admit

Flags:
  -h, --help         show help
`
	flags := newFlags("room join", a.Out, help)
	if err := parse(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: tearoom room join <actor>")
	}
	actor := strings.TrimSpace(flags.Arg(0))
	if actor == "" {
		return errors.New("actor identity is required")
	}
	st, err := a.openStore()
	if err != nil {
		return err
	}
	admitted, err := st.AdmitActor(context.Background(), actor)
	if err != nil {
		return err
	}
	return json.NewEncoder(a.Out).Encode(admitted)
}

const sendHelp = `Append one event to the nearest room.

Plain text is encoded as a JSON string; --json passes the raw JSON content.
The appended event is printed as one JSON object.

Usage:
  tearoom send <text> [flags]
  tearoom send --json <json> [flags]

Flags:
  --as string             actor identity; defaults to TEAROOM_ACTOR
  --json string           raw JSON content instead of positional text
  --idempotency-key string  retry identity; an equal retry returns the original event
  -h, --help              show help
`

func (a *App) runSend(args []string) error {
	flags := newFlags("send", a.Out, sendHelp)
	as := flags.String("as", "", "")
	raw := flags.String("json", "", "")
	key := flags.String("idempotency-key", "", "")
	if err := parse(flags, args); err != nil {
		return err
	}
	text := strings.Join(flags.Args(), " ")
	var content json.RawMessage
	switch {
	case *raw != "" && text != "":
		return errors.New("provide exactly one of positional text or --json")
	case *raw != "":
		content = json.RawMessage(*raw)
	case text != "":
		encoded, err := json.Marshal(text)
		if err != nil {
			return err
		}
		content = encoded
	default:
		return errors.New("provide positional text or --json content")
	}
	actor, err := actorID(*as)
	if err != nil {
		return err
	}
	st, err := a.openStore()
	if err != nil {
		return err
	}
	event, err := st.Append(context.Background(), actor, core.AppendRequest{
		Content:        content,
		IdempotencyKey: *key,
	})
	if err != nil {
		return err
	}
	return json.NewEncoder(a.Out).Encode(event)
}

const readHelp = `Read events from the nearest room, one JSON object per line.

An empty result prints nothing and exits 0. The read is pure: nothing in the
Room changes; truncation or polling for later events belongs to the caller.

Usage:
  tearoom read [--as <actor>] [--after e42]

Flags:
  --as string        actor identity; defaults to TEAROOM_ACTOR
  --after string     exclusive boundary, the presentation address e<seq>
  -h, --help         show help
`

func (a *App) runRead(args []string) error {
	flags := newFlags("read", a.Out, readHelp)
	as := flags.String("as", "", "")
	after := flags.String("after", "", "")
	if err := parseNoArgs(flags, args); err != nil {
		return err
	}
	boundary, err := parseBoundary(*after)
	if err != nil {
		return err
	}
	actor, err := actorID(*as)
	if err != nil {
		return err
	}
	st, err := a.openStore()
	if err != nil {
		return err
	}
	ctx := a.Context
	if ctx == nil {
		ctx = context.Background()
	}
	events, err := st.Read(ctx, actor, boundary)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(a.Out)
	for _, event := range events {
		if err := encoder.Encode(event); err != nil {
			return err
		}
	}
	return nil
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

func (a *App) serverStart(args []string) error {
	const help = `Serve the nearest room over HTTP on the loopback interface.

P0 refuses non-loopback listen addresses: the HTTP contract carries no
authentication, so the service must not be exposed to a network.

Usage:
  tearoom server start [flags]

Flags:
  --address string      listen address (default "127.0.0.1:8787")
  -h, --help            show help
`
	flags := newFlags("server start", a.Out, help)
	address := flags.String("address", "127.0.0.1:8787", "")
	if err := parseNoArgs(flags, args); err != nil {
		return err
	}
	if err := requireLoopback(*address); err != nil {
		return err
	}
	st, err := store.Open(a.Dir)
	if err != nil {
		return fmt.Errorf("open room under %s: %w", a.Dir, err)
	}
	defer closeStore(st)
	server := &http.Server{Addr: *address, Handler: transport.NewServer(st).Handler(), ReadHeaderTimeout: 10 * time.Second}
	root := a.Dir
	if r, ok := st.(interface{ Root() string }); ok {
		root = r.Root()
	}
	fmt.Fprintf(a.Err, "tearoom serving %s on %s\n", root, *address)
	ctx := a.Context
	if ctx == nil {
		ctx = context.Background()
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.ListenAndServe() }()
	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return err
		}
		if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

func closeStore(st core.Store) {
	if closer, ok := st.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
}

// requireLoopback rejects any listen address whose host is not a loopback
// address. The default of the old server, ":8787" (all interfaces), is refused.
func requireLoopback(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid listen address %q: %w", address, err)
	}
	if host == "" {
		return fmt.Errorf("refusing listen address %q: P0 serves loopback only; use 127.0.0.1:8787", address)
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	return fmt.Errorf("refusing listen address %q: P0 serves loopback only; use 127.0.0.1:8787", address)
}

// openStore targets TEAROOM_SERVER when set and otherwise opens the nearest
// local room. room init never runs through this path: room creation is local.
func (a *App) openStore() (core.Store, error) {
	if server := strings.TrimRight(strings.TrimSpace(os.Getenv("TEAROOM_SERVER")), "/"); server != "" {
		if err := transport.RequireSafeURL(server); err != nil {
			return nil, err
		}
		return transport.NewClient(server, strings.TrimSpace(os.Getenv("TEAROOM_TOKEN"))), nil
	}
	return store.Open(a.Dir)
}

// parseBoundary accepts only the presentation address e<seq>.
func parseBoundary(value string) (uint64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	if !strings.HasPrefix(value, "e") {
		return 0, fmt.Errorf("invalid --after %q: use the presentation address e<seq>", value)
	}
	seqText := strings.TrimPrefix(value, "e")
	if seqText == "" {
		return 0, fmt.Errorf("invalid --after %q: use the presentation address e<seq>", value)
	}
	seq, err := strconv.ParseUint(seqText, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid --after %q: use the presentation address e<seq>", value)
	}
	return seq, nil
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

func isHelp(value string) bool { return value == "-h" || value == "--help" }

func isVersion(value string) bool { return value == "--version" }
