// skill.go renders the agent orientation guide: the room-tree journey and the
// rules that hold at every command, carried by the binary so no skill file
// has to be distributed. Exact syntax stays with per-command help and the
// CLI contract; this text only teaches the journey.
package cli

import (
	"fmt"
)

// skillText is the agent-facing orientation printed by `tearoom skill`. It
// works before any setup: it reads no Room and takes no flags.
const skillText = `tearoom agent guide

Tearoom is a durable room tree. Every room is one span of context with one
ordered, append-only record stream; rooms link through single parents, and
whoever reads a room can pick up the work — across agents, tools, and
process restarts. A signature is an unverified label. The CLI is the whole
interface.

The journey:
  1. Record the goal            tearoom room init --name main-line
                                tearoom send --text "the goal and acceptance"
  2. Split into children        tearoom room init --name implementation --parent main-line
  3. Work inside one room       tearoom send --text "..." --room implementation
  4. Hand off in writing        what was done, what is blocked, what the next
                                worker should do first
  5. The next worker reads      tearoom read --room implementation --tail 20
                                and continues. Verify in another room, never
                                in the one that did the work.

Every agent, every session:
  export TEAROOM_ACTOR=<signature>              a plain label, chosen once
  tearoom tree                                  the skeleton: parents and children
  tearoom read --room <room> [--after e<N>] [--tail 5] [--subtree]
  tearoom send --text <text> | --json <json> [--room <room>]

Rules that hold everywhere:
- Records form one dense, ordered, durable stream per room. The Store
  assigns each position; callers never pick one.
- Reads are pure and repeatable. Keep your own boundary in --after; Tearoom
  stores no cursor or acknowledgement for you.
- The signature is never verified. Quoting a record is room-name#seq in
  plain text; tools do not parse it.
- An empty read is success: no output, exit 0.
- If a send may have gone through, retry with the same --idempotency-key and
  you get the original record back instead of a duplicate.
- Notifications say only "there is something new": tearoom watch --json
  prints one envelope per new record. Deciding who wakes and how belongs to
  an external waker.

Exact flags live with each command: run tearoom <command> --help.
`

// runSkill prints the agent guide. It owns no Room state and takes no flags,
// so an Agent can call it before any setup to learn the surface.
func (a *App) runSkill(args []string) error {
	const help = `Print the agent guide: the room-tree journey and the rules that hold
at every command.

Usage:
  tearoom skill
`
	flags := newFlags("skill", a.Out, help)
	if err := parseNoArgs(flags, args); err != nil {
		return err
	}
	fmt.Fprint(a.Out, skillText)
	return nil
}
