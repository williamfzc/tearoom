// read.go is the read surface: a room, a subtree, a tail, and the caller
// boundary. The read is pure; truncation and polling belong to the caller.
package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"
)

const readHelp = `Read records from the room tree, one JSON object per line.

An empty result prints nothing and exits 0. The read is pure: nothing in the
workspace changes; truncation or polling for later records belongs to the
caller.

Usage:
  tearoom read [--room <room>] [--after e42] [--tail 5] [--subtree]

Flags:
  --room string    room to read; defaults to the only room of the workspace
  --after string   exclusive boundary, the presentation address e<seq>
  --tail int       print only the last N records of each selected room;
                   explicit --tail 0 prints nothing, omitting the flag
                   prints all records
  --subtree        read the room and all its descendants, depth-first
  -h, --help       show help
`

func (a *App) runRead(args []string) error {
	flags := newFlags("read", a.Out, readHelp)
	roomFlag := flags.String("room", "", "")
	after := flags.String("after", "", "")
	tail := flags.Int("tail", 0, "")
	subtree := flags.Bool("subtree", false, "")
	if err := parseNoArgs(flags, args); err != nil {
		return err
	}
	if *tail < 0 {
		return errors.New("--tail must be a non-negative integer")
	}
	var tailSet bool
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "tail" {
			tailSet = true
		}
	})
	boundary, err := parseBoundary(*after)
	if err != nil {
		return err
	}
	st, err := a.openStore()
	if err != nil {
		return err
	}
	ctx := a.context()
	room, err := a.resolveRoom(ctx, st, *roomFlag)
	if err != nil {
		return err
	}
	rooms := []string{room}
	if *subtree {
		if rooms, err = subtreeOf(ctx, st, room); err != nil {
			return err
		}
	}
	encoder := json.NewEncoder(a.Out)
	for _, name := range rooms {
		records, err := st.Read(ctx, name, boundary)
		if err != nil {
			return err
		}
		if tailSet && len(records) > *tail {
			records = records[len(records)-*tail:]
		}
		for _, record := range records {
			if err := encoder.Encode(record); err != nil {
				return err
			}
		}
	}
	return nil
}

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

// signature resolves the record author: a plain label, never validated. An
// unset label is a valid empty signature.
