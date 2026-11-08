// collaboration.go connects the local CLI to the application-layer dispatcher.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/williamfzc/tearoom/internal/collab"
	"github.com/williamfzc/tearoom/internal/store"
)

func (a *App) runCollab(args []string) error {
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Fprint(a.Out, `Run or inspect the local collaboration dispatcher.

Usage:
  tearoom collab run
  tearoom collab status
  tearoom collab resolve --room <room> --seq <seq> --role <role> --outcome <delivered|skip|retry>

Retry additionally requires --confirm because an ambiguous task may already
have run and could repeat side effects. Resolution is allowed only for an
unknown delivery.
`)
		return nil
	}
	root, err := store.Find(a.Dir)
	if err != nil {
		return err
	}
	runtime := collab.NewHCRuntime("hc")
	dispatcher := collab.NewDispatcher(root, nil, runtime)
	switch args[0] {
	case "run":
		if len(args) != 1 {
			return fmt.Errorf("unexpected arguments: %v", args[1:])
		}
		if _, err := exec.LookPath("hc"); err != nil {
			return errors.New("hc is required for collaboration; install and authenticate it before running")
		}
		ctx, stop := signal.NotifyContext(a.context(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		st, err := store.Open(a.Dir)
		if err != nil {
			return err
		}
		dispatcher = collab.NewDispatcher(root, st, runtime)
		return dispatcher.Run(ctx, 500*time.Millisecond)
	case "status":
		if len(args) != 1 {
			return fmt.Errorf("unexpected arguments: %v", args[1:])
		}
		intents, err := dispatcher.Status()
		if err != nil {
			return err
		}
		if intents == nil {
			intents = []collab.Intent{}
		}
		return json.NewEncoder(a.Out).Encode(intents)
	case "resolve":
		return a.resolveDelivery(args[1:], dispatcher)
	default:
		fmt.Fprintf(a.Out, "unknown collab command %q\n", args[0])
		return fmt.Errorf("unknown collab command %q", args[0])
	}
}
