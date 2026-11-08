// collab_resolve.go exposes explicit human resolution for ambiguous deliveries.
package cli

import (
	"errors"
	"fmt"

	"github.com/williamfzc/tearoom/internal/collab"
)

func (a *App) resolveDelivery(args []string, dispatcher *collab.Dispatcher) error {
	const help = `Resolve an ambiguous delivery from tearoom collab status.

Usage:
  tearoom collab resolve --room <room> --seq <seq> --role <role> --outcome <delivered|skip|retry> [--confirm]

Outcomes:
  delivered  confirm input reached hc; wait for a Room receipt without resending
  skip       close this delivery and allow the role's next queued message
  retry      requeue; may repeat work or external effects, requires --confirm
`
	flags := newFlags("collab resolve", a.Out, help)
	room := flags.String("room", "", "")
	seq := flags.Uint64("seq", 0, "")
	role := flags.String("role", "", "")
	outcome := flags.String("outcome", "", "")
	confirm := flags.Bool("confirm", false, "")
	if err := parseNoArgs(flags, args); err != nil {
		return err
	}
	if *room == "" || *seq == 0 || *role == "" || *outcome == "" {
		return errors.New("--room, positive --seq, --role, and --outcome are required")
	}
	if *outcome == "retry" && !*confirm {
		return errors.New("retry may repeat work or side effects; inspect collab status and pass --confirm to requeue")
	}
	if *confirm && *outcome != "retry" {
		return fmt.Errorf("--confirm is only valid with --outcome retry")
	}
	if err := dispatcher.Resolve(*room, *seq, *role, *outcome, *confirm); err != nil {
		return err
	}
	_, err := fmt.Fprintf(a.Out, "resolved %s#%d @%s as %s\n", *room, *seq, *role, *outcome)
	return err
}
