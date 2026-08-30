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
  help      show help

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
	case "help":
		err = a.runHelp(args[1:])
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

func (a *App) runHelp(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(a.Out, rootHelp)
		return nil
	}
	switch args[0] {
	case "room":
		fmt.Fprint(a.Out, roomHelp)
	case "send":
		fmt.Fprint(a.Out, sendHelp)
	case "read":
		fmt.Fprint(a.Out, readHelp)
	case "server":
		fmt.Fprint(a.Out, serverHelp)
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
  tearoom room join [--as <actor>]

Flags:
  --as string        actor identity; defaults to TEAROOM_ACTOR
  -h, --help         show help
`
	flags := newFlags("room join", a.Out, help)
	as := flags.String("as", "", "")
	if err := parseNoArgs(flags, args); err != nil {
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
  --directory string    directory containing the room (default ".")
  -h, --help            show help
`
	flags := newFlags("server start", a.Out, help)
	address := flags.String("address", "127.0.0.1:8787", "")
	directory := flags.String("directory", a.Dir, "")
	if err := parseNoArgs(flags, args); err != nil {
		return err
	}
	if err := requireLoopback(*address); err != nil {
		return err
	}
	st, err := store.Open(*directory)
	if err != nil {
		return fmt.Errorf("open room under %s: %w", *directory, err)
	}
	server := &http.Server{Addr: *address, Handler: transport.NewServer(st).Handler(), ReadHeaderTimeout: 10 * time.Second}
	root := *directory
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

// parseBoundary accepts the presentation address e<seq> or a bare sequence.
func parseBoundary(value string) (uint64, error) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "e")
	if value == "" {
		return 0, nil
	}
	seq, err := strconv.ParseUint(value, 10, 64)
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

// stringList collects a repeatable flag value.
type stringList struct {
	values []string
}

func (s *stringList) String() string { return strings.Join(s.values, ",") }

func (s *stringList) Set(value string) error {
	s.values = append(s.values, value)
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
