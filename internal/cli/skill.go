// skill.go renders the agent orientation guide: the Room journey and the
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

Tearoom is a durable shared Room. One participant appends work; another
pulls the same ordered history, continues it, and appends the result for
the next participant. A human is just another Actor, and the CLI is the
whole interface.

The journey:
  1. An Actor appends work        tearoom send "status"  or  --json <json>
  2. Another Actor pulls it       tearoom read [--after e<N>]
  3. That Actor continues         tearoom send --json '{"result": ...}'
  4. Anyone repeats 2 and 3 from their own boundary.

Setup, once, in the project directory:
  tearoom room init --as <actor>      create the Room here, admit the creator
  tearoom room join <actor>           admit each further participant
  tearoom server start                optional: serve the Room on 127.0.0.1:8787

Every Actor, every session:
  export TEAROOM_ACTOR=<your actor>             a stable identity, chosen once
  export TEAROOM_SERVER=http://127.0.0.1:8787   omit to use the nearest local Room
  tearoom read [--after e<N>] [--limit N] [--wait 5m]
  tearoom send <text>  |  tearoom send --json <json>  [--to <member>]...

Rules that hold everywhere:
- Events form one dense, ordered, durable history. The Store assigns each
  position; callers never pick one.
- Reads are pure and repeatable. Keep your own boundary in --after; Tearoom
  stores no cursor, unread marker, or acknowledgement for you.
- Every member sees every Event. --to is public addressing, not secrecy.
- An empty read is success: no output, exit 0.
- If a send may have gone through, retry with the same --idempotency-key and
  you get the original Event back instead of a duplicate.

Exact flags live with each command: run tearoom <command> --help.
`

// runSkill prints the agent guide. It owns no Room state and takes no flags,
// so an Agent can call it before any setup to learn the surface.
func (a *App) runSkill(args []string) error {
	const help = `Print the agent guide: the Room journey and the rules that hold
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
