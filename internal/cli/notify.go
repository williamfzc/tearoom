// notify.go is the observation surface: the skeleton of the tree and the
// notification stream. watch emits one envelope per new record and decides
// nothing; waking someone belongs to an external waker.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/williamfzc/tearoom/internal/core"
)

const treeHelp = `Print the workspace skeleton: every root with its descendants.

Usage:
  tearoom tree

Flags:
  -h, --help    show help
`

func (a *App) runTree(args []string) error {
	flags := newFlags("tree", a.Out, treeHelp)
	if err := parseNoArgs(flags, args); err != nil {
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
	fmt.Fprint(a.Out, renderTree(rooms))
	return nil
}

const watchHelp = `Print one line per new record, until interrupted.

The line is a notification envelope: which room, which sequence, which
signature, when. It carries no content and requests nothing — deciding who is
woken and how belongs to an external waker.

Usage:
  tearoom watch [--room <room>] [--json]

Flags:
  --room string   watch one room; the default watches every room of the workspace
  --json          print the envelope as one JSON object per line
  -h, --help      show help
`

func (a *App) runWatch(args []string) error {
	flags := newFlags("watch", a.Out, watchHelp)
	roomFlag := flags.String("room", "", "")
	asJSON := flags.Bool("json", false, "")
	if err := parseNoArgs(flags, args); err != nil {
		return err
	}
	filter := strings.TrimSpace(*roomFlag)
	ctx := a.context()
	st, err := a.openStore()
	if err != nil {
		return err
	}
	if filter != "" {
		// Fail fast on an unknown room instead of watching nothing forever.
		if _, err := st.Read(ctx, filter, 0); err != nil {
			return err
		}
	}
	encoder := json.NewEncoder(a.Out)
	heads := map[string]uint64{}
	primed := map[string]bool{}
	emit := func(n core.Notification) error {
		if *asJSON {
			return encoder.Encode(n)
		}
		_, err := fmt.Fprintf(a.Out, "%s #%d %s %s\n", n.Room, n.Seq, n.Author, n.At.UTC().Format(time.RFC3339))
		return err
	}
	for {
		rooms, err := st.Rooms(ctx)
		if err != nil {
			return err
		}
		for _, room := range rooms {
			if filter != "" && room.Name != filter {
				continue
			}
			records, err := st.Read(ctx, room.Name, heads[room.Name])
			if err != nil {
				return err
			}
			for _, record := range records {
				if primed[room.Name] {
					if err := emit(record.Notification()); err != nil {
						return err
					}
				}
				heads[room.Name] = record.Seq
			}
			// The first sighting primes the head without emitting history.
			primed[room.Name] = true
		}
		if err := ctxSleep(ctx, watchInterval); err != nil {
			return nil
		}
	}
}

// subtreeOf returns the room and all its descendants, depth-first by name.
func subtreeOf(ctx context.Context, st core.Store, root string) ([]string, error) {
	rooms, err := st.Rooms(ctx)
	if err != nil {
		return nil, err
	}
	children := map[string][]string{}
	exists := false
	for _, room := range rooms {
		if room.Name == root {
			exists = true
		}
		children[room.Parent] = append(children[room.Parent], room.Name)
	}
	if !exists {
		return nil, fmt.Errorf("room %q: %w", root, core.ErrNotFound)
	}
	order := []string{}
	var walk func(name string)
	walk = func(name string) {
		order = append(order, name)
		for _, child := range children[name] {
			walk(child)
		}
	}
	walk(root)
	return order, nil
}

// renderTree lays the forest out: every root with its descendants,
// depth-first by name.
func renderTree(rooms []core.Room) string {
	children := map[string][]string{}
	roots := []string{}
	for _, room := range rooms {
		if room.Parent == "" {
			roots = append(roots, room.Name)
		} else {
			children[room.Parent] = append(children[room.Parent], room.Name)
		}
	}
	var out strings.Builder
	var walk func(name, prefix, connector string)
	walk = func(name, prefix, connector string) {
		out.WriteString(prefix + connector + name + "\n")
		// Siblings extend the prefix with a full vertical bar; a last child
		// leaves blank space instead.
		var childBase string
		switch connector {
		case "├── ":
			childBase = prefix + "│   "
		case "└── ":
			childBase = prefix + "    "
		default:
			childBase = prefix
		}
		kids := children[name]
		for i, child := range kids {
			childConnector := "├── "
			if i == len(kids)-1 {
				childConnector = "└── "
			}
			walk(child, childBase, childConnector)
		}
	}
	for _, root := range roots {
		walk(root, "", "")
	}
	return out.String()
}

// resolveRoom selects the target room: the flag when given, otherwise the
// only room of the workspace; an ambiguous workspace is a refusal, never a
// guess.

func ctxSleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

const watchInterval = 500 * time.Millisecond
