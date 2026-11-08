// Package cli exposes Tearoom as the room-tree command surface: named rooms
// in one workspace, append-only records, pure reads, and notification lines.
// It owns no session state; every shared invariant lives in core.Store.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

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

const rootHelp = `tearoom — a durable room tree for agents

Usage:
  tearoom <command> [flags]

Commands:
  room      create rooms in the workspace tree
  role      define and inspect reusable collaborator roles
  collab    run or inspect the local hc collaboration dispatcher
  send      append one record to a room
  read      read records: a room, a subtree, a tail
  tree      print the skeleton: parents and children
  watch     print a notification line per new record
  server    serve the workspace over local HTTP
  skill     print the agent guide: the journey and the rules

Options:
  --version show build and store format information

Environment:
  TEAROOM_ACTOR    default signature for --as
  TEAROOM_SERVER   local server URL; omit for the nearest workspace

Use "tearoom <command> --help" for more information.
`

const serverHelp = `Run the local room service.

Usage:
  tearoom server <command> [flags]

Commands:
  start   serve the nearest workspace over loopback HTTP
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
	case "role":
		err = a.runRole(args[1:])
	case "collab":
		err = a.runCollab(args[1:])
	case "send":
		err = a.runSend(args[1:])
	case "read":
		err = a.runRead(args[1:])
	case "tree":
		err = a.runTree(args[1:])
	case "watch":
		err = a.runWatch(args[1:])
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

func (a *App) resolveRoom(ctx context.Context, st core.Store, flagValue string) (string, error) {
	name := strings.TrimSpace(flagValue)
	if name != "" {
		return name, nil
	}
	rooms, err := st.Rooms(ctx)
	if err != nil {
		return "", err
	}
	if len(rooms) == 1 {
		return rooms[0].Name, nil
	}
	if len(rooms) == 0 {
		return "", core.ErrNoRoom
	}
	return "", fmt.Errorf("this workspace has %d rooms; pass --room (see 'tearoom tree')", len(rooms))
}

// openStore targets TEAROOM_SERVER when set and otherwise opens the nearest
// local workspace. Room creation never runs through this path: rooms are
// created locally.
func (a *App) openStore() (core.Store, error) {
	if server := strings.TrimRight(strings.TrimSpace(os.Getenv("TEAROOM_SERVER")), "/"); server != "" {
		if err := transport.RequireSafeURL(server); err != nil {
			return nil, err
		}
		return transport.NewClient(server), nil
	}
	return store.Open(a.Dir)
}

func (a *App) context() context.Context {
	if a.Context != nil {
		return a.Context
	}
	return context.Background()
}

// parseBoundary accepts only the presentation address e<seq>.
func isHelp(value string) bool { return value == "-h" || value == "--help" }

func isVersion(value string) bool { return value == "--version" }
