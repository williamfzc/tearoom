// room.go is the room surface: create a room in the workspace tree, under a
// parent when given. Room creation is local; there is no join and no
// credential step.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/williamfzc/tearoom/internal/collab"
	"github.com/williamfzc/tearoom/internal/core"
	"github.com/williamfzc/tearoom/internal/store"
)

const roomHelp = `Create rooms in the workspace tree.

Usage:
  tearoom room init --name <name> [--parent <room>]
  tearoom room roles --room <name> [--add <role,...>] [--remove <role,...>]

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
	case "roles":
		return a.roomRoles(args[1:])
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
  tearoom room roles --room <name> [--add <role,...>] [--remove <role,...>]

Flags:
  --name string      the room name; unique within the workspace
  --parent string    the parent room; omitted for a root
  --role string      reusable role(s) to configure, comma-separated
  -h, --help         show help
`
	flags := newFlags("room init", a.Out, help)
	name := flags.String("name", "", "")
	parent := flags.String("parent", "", "")
	roles := flags.String("role", "", "")
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
	var selected []string
	if strings.TrimSpace(*roles) != "" {
		selected, err = parseMentions(*roles)
		if err != nil {
			return err
		}
		root, findErr := store.Find(a.Dir)
		if findErr != nil {
			return findErr
		}
		config, loadErr := collab.Load(root)
		if loadErr != nil {
			return loadErr
		}
		for _, role := range selected {
			if _, ok := config.Roles[role]; !ok {
				return fmt.Errorf("unknown role %q; define it before creating a room with roles", role)
			}
		}
	}
	room, err := st.CreateRoom(a.context(), *name, *parent)
	if err != nil {
		return err
	}
	if strings.TrimSpace(*roles) != "" {
		root, err := store.Find(a.Dir)
		if err != nil {
			return err
		}
		err = collab.Update(root, func(config *collab.Config) error {
			if _, exists := config.Rooms[room.Name]; exists {
				return fmt.Errorf("collaborators already configured for room %q", room.Name)
			}
			config.Rooms[room.Name] = collab.Room{Roles: selected}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return json.NewEncoder(a.Out).Encode(room)
}

func (a *App) roomRoles(args []string) error {
	const help = `List or change roles configured for a room.

Usage:
  tearoom room roles --room <name> [--add <role,...>] [--remove <role,...>] [--status]

--status queries hc for each role's panel; without it the list is offline.
`
	flags := newFlags("room roles", a.Out, help)
	room := flags.String("room", "", "")
	add := flags.String("add", "", "")
	remove := flags.String("remove", "", "")
	status := flags.Bool("status", false, "")
	if err := parseNoArgs(flags, args); err != nil {
		return err
	}
	if strings.TrimSpace(*room) == "" {
		return errors.New("--room is required")
	}
	if *add == "" && *remove == "" {
		st, err := a.openStore()
		if err != nil {
			return err
		}
		rooms, err := st.Rooms(a.context())
		if err != nil {
			return err
		}
		found := false
		for _, entry := range rooms {
			found = found || entry.Name == *room
		}
		if !found {
			return fmt.Errorf("room %q: %w", *room, core.ErrNotFound)
		}
		return a.printRoomRoles(*room, *status)
	}
	root, err := store.Find(a.Dir)
	if err != nil {
		return err
	}
	st, err := a.openStore()
	if err != nil {
		return err
	}
	rooms, err := st.Rooms(a.context())
	if err != nil {
		return err
	}
	found := false
	for _, entry := range rooms {
		found = found || entry.Name == *room
	}
	if !found {
		return fmt.Errorf("room %q: %w", *room, core.ErrNotFound)
	}
	adds, err := parseMentions(*add)
	if err != nil {
		return err
	}
	removes, err := parseMentions(*remove)
	if err != nil {
		return err
	}
	if err := collab.Update(root, func(config *collab.Config) error {
		current := map[string]bool{}
		for _, name := range config.Rooms[*room].Roles {
			current[name] = true
		}
		for _, name := range adds {
			if _, ok := config.Roles[name]; !ok {
				return fmt.Errorf("unknown role %q; define it first with 'tearoom role define'", name)
			}
			current[name] = true
		}
		for _, name := range removes {
			delete(current, name)
		}
		selected := make([]string, 0, len(current))
		for name := range current {
			selected = append(selected, name)
		}
		sort.Strings(selected)
		config.Rooms[*room] = collab.Room{Roles: selected}
		return nil
	}); err != nil {
		return err
	}
	return a.printRoomRoles(*room, *status)
}

func (a *App) printRoomRoles(room string, showStatus bool) error {
	root, err := store.Find(a.Dir)
	if err != nil {
		return err
	}
	config, err := collab.Load(root)
	if err != nil {
		return err
	}
	roles := config.Rooms[room].SortedRoles()
	encoder := json.NewEncoder(a.Out)
	for _, name := range roles {
		entry := map[string]any{"room": room, "role": name, "definition": config.Roles[name]}
		if showStatus {
			panel, err := collab.PanelName(root, room, name)
			if err != nil {
				return err
			}
			entry["panel"] = panel
			status, err := collab.NewHCRuntime("hc").PanelStatus(a.context(), panel)
			if err != nil {
				entry["status"] = collab.PanelUnknown
				entry["status_error"] = err.Error()
			} else {
				entry["status"] = status
			}
		}
		if err := encoder.Encode(entry); err != nil {
			return err
		}
	}
	return nil
}
