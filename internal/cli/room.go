// room.go is the room surface: create a room in the workspace tree, under a
// parent when given. Room creation is local; there is no join and no
// credential step.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/williamfzc/tearoom/internal/core"
	"github.com/williamfzc/tearoom/internal/store"
)

const roomHelp = `Create rooms in the workspace tree.

Usage:
  tearoom room init --name <name> [--parent <room>]

The first room init in a directory creates the workspace (.tearoom); later
inits add rooms to the same tree. A room without --parent is a root; with
--parent it records the named room as its parent. Room names are unique
within the workspace.
`

func (a *App) runRoom(args []string) error {
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Fprint(a.Out, roomHelp)
		return nil
	}
	switch args[0] {
	case "init":
		return a.roomInit(args[1:])
	default:
		return fmt.Errorf("unknown room command %q", args[0])
	}
}

// roomInit creates one room. When no workspace exists yet, the workspace is
// created first; the room then joins its tree, recording its parent when
// --parent names one.
func (a *App) roomInit(args []string) error {
	const help = `Create a room in the workspace tree.

Usage:
  tearoom room init --name <name> [--parent <room>]

Flags:
  --name string      the room name; unique within the workspace
  --parent string    the parent room; omitted for a root
  -h, --help         show help
`
	flags := newFlags("room init", a.Out, help)
	name := flags.String("name", "", "")
	parent := flags.String("parent", "", "")
	if err := parseNoArgs(flags, args); err != nil {
		return err
	}
	if strings.TrimSpace(*name) == "" {
		return errors.New("--name is required: rooms are addressed by name")
	}
	st, err := store.Open(a.Dir)
	if errors.Is(err, core.ErrNoRoom) {
		// No workspace anywhere above: create one here.
		st, err = store.Init(a.Dir)
	}
	if err != nil {
		return err
	}
	room, err := st.CreateRoom(a.context(), *name, *parent)
	if err != nil {
		return err
	}
	return json.NewEncoder(a.Out).Encode(room)
}
