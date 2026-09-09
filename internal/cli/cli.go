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
	ExitOK      = 0
	ExitError   = 1
	ExitTimeout = 3
	ExitStore   = 4
)

const rootHelp = `tearoom — a durable room for agents

Usage:
  tearoom <command> [flags]

Commands:
  room      init, join, and list members of the nearest room
  send      append one event
  read      read events, optionally waiting for later ones
  server    serve the nearest room over HTTP
  skill     print the agent guide: the journey and the rules

Options:
  --version show build and store format information

Environment:
  TEAROOM_ACTOR    stable actor identity
  TEAROOM_SERVER   shared server URL; omit for the nearest local room

Use "tearoom <command> --help" for more information.
`

const roomHelp = `Manage the local room workspace.

Usage:
  tearoom room <command> [flags]

Commands:
  init      create a room and admit the creator
  join      admit an actor as an explicit member
  members   list the room's admitted actors
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
	case errors.Is(err, errWaitTimeout):
		return ExitTimeout
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
	case "members":
		return a.roomMembers(args[1:])
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
  tearoom room init [--as <actor>] [--store file|sqlite]

Flags:
  --as string        actor identity; defaults to TEAROOM_ACTOR
  --store string     file | sqlite (default "file")
  -h, --help         show help
`
	flags := newFlags("room init", a.Out, help)
	as := flags.String("as", "", "")
	storeName := flags.String("store", string(store.BackendFile), "")
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
	backend, err := parseBackend(*storeName)
	if err != nil {
		return err
	}
	st, err := store.Init(a.Dir, backend, actor)
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

func (a *App) roomMembers(args []string) error {
	const help = `List the room's admitted actors.

Usage:
  tearoom room members

Flags:
  -h, --help         show help
`
	flags := newFlags("room members", a.Out, help)
	if err := parseNoArgs(flags, args); err != nil {
		return err
	}
	st, err := a.openStore()
	if err != nil {
		return err
	}
	actors, err := st.Actors(context.Background())
	if err != nil {
		return err
	}
	return json.NewEncoder(a.Out).Encode(actors)
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
  --to string             addressed member; repeatable
  --idempotency-key string  retry identity; an equal retry returns the original event
  -h, --help              show help
`

func (a *App) runSend(args []string) error {
	flags := newFlags("send", a.Out, sendHelp)
	as := flags.String("as", "", "")
	raw := flags.String("json", "", "")
	to := &stringList{}
	flags.Var(to, "to", "")
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
		To:             to.values,
		Content:        content,
		IdempotencyKey: *key,
	})
	if err != nil {
		if errors.Is(err, core.ErrUnknownActor) && strings.Contains(err.Error(), "recipient") {
			return fmt.Errorf("%w; run `tearoom room join <actor>` first", err)
		}
		return err
	}
	return json.NewEncoder(a.Out).Encode(event)
}

const readHelp = `Read events from the nearest room, one JSON object per line.

An empty result prints nothing and exits 0. --wait holds only the current
invocation: it returns as soon as an event past --after exists, and exits 3
when the bound elapses with nothing new.

Usage:
  tearoom read [--as <actor>] [--after e42] [--limit N] [--wait 5m]

Flags:
  --as string        actor identity; defaults to TEAROOM_ACTOR
  --after string     exclusive boundary, the presentation address e<seq>
  --limit int        maximum events; 0 means all
  --wait duration    hold the invocation for a later event (e.g. 5m)
  -h, --help         show help
`

var errWaitTimeout = errors.New("read wait elapsed without a later event")

func (a *App) runRead(args []string) error {
	flags := newFlags("read", a.Out, readHelp)
	as := flags.String("as", "", "")
	after := flags.String("after", "", "")
	limit := flags.Int("limit", 0, "")
	wait := flags.Duration("wait", 0, "")
	if err := parseNoArgs(flags, args); err != nil {
		return err
	}
	if *limit < 0 {
		return errors.New("--limit must be non-negative")
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
	events, err := a.readWithWait(ctx, st, actor, boundary, *limit, *wait)
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

// readWithWait keeps wait out of core.Store. A server target answers the wait
// in one held request; a local store is polled within this invocation. Either
// way the wait creates no cursor, subscription, or durable state.
func (a *App) readWithWait(ctx context.Context, st core.Store, actor string, after uint64, limit int, wait time.Duration) ([]core.Event, error) {
	if wait <= 0 {
		return st.Read(ctx, actor, after, limit)
	}
	type waiter interface {
		ReadWait(ctx context.Context, actor string, after uint64, limit int, wait time.Duration) ([]core.Event, error)
	}
	if remote, ok := st.(waiter); ok {
		events, err := remote.ReadWait(ctx, actor, after, limit, wait)
		if err != nil {
			return nil, err
		}
		if len(events) == 0 {
			return nil, errWaitTimeout
		}
		return events, nil
	}
	deadline := time.Now().Add(wait)
	for {
		events, err := st.Read(ctx, actor, after, limit)
		if err != nil {
			return nil, err
		}
		if len(events) > 0 {
			return events, nil
		}
		if remaining := time.Until(deadline); remaining <= 0 {
			return nil, errWaitTimeout
		} else {
			step := 500 * time.Millisecond
			if remaining < step {
				step = remaining
			}
			timer := time.NewTimer(step)
			select {
			case <-timer.C:
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return nil, ctx.Err()
			}
		}
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
		if err := transport.RequireLoopbackURL(server); err != nil {
			return nil, err
		}
		return transport.NewClient(server), nil
	}
	return store.Open(a.Dir)
}

func parseBackend(value string) (store.Backend, error) {
	backend := store.Backend(strings.ToLower(strings.TrimSpace(value)))
	switch backend {
	case store.BackendFile, store.BackendSQLite:
		return backend, nil
	default:
		return "", fmt.Errorf("unsupported --store %q; use file or sqlite", value)
	}
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
