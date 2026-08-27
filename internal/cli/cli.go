// Package cli implements the tea command surface.
//
// Two rules shape everything here. Exit codes are part of the contract, because
// agents branch on them without parsing prose — a rejected claim is exit 2, an
// expected outcome, not a crash. And every command takes --json, because the
// primary caller is a program, not a person.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/williamfzc/tearoom/internal/core"
	"github.com/williamfzc/tearoom/internal/store"
	"github.com/williamfzc/tearoom/internal/transport"
	"github.com/williamfzc/tearoom/internal/view"
)

// Exit codes are contractual. Distinct codes let an agent decide between waiting,
// switching tasks and bailing out without any text parsing.
const (
	ExitOK       = 0
	ExitUsage    = 1
	ExitConflict = 2 // lease held by someone else
	ExitTimeout  = 3 // wait expired with no change
	ExitStore    = 4 // store unreachable
)

type App struct {
	Out io.Writer
	Err io.Writer
	// Dir is the working directory used to discover the workspace.
	Dir string
	// Now is injectable for tests.
	Now func() time.Time
}

func New() *App {
	wd, _ := os.Getwd()
	return &App{Out: os.Stdout, Err: os.Stderr, Dir: wd, Now: time.Now}
}

func (a *App) now() time.Time {
	if a.Now == nil {
		return time.Now()
	}
	return a.Now()
}

const usage = `tea — coordination for multi-agent workspaces

  tea init      bind this directory to a workspace
  tea join      register or resume this agent's identity
  tea post      open a thread
  tea reply     append an event to a thread
  tea inbox     threads needing this agent's attention
  tea context   compacted thread context for resuming work
  tea claim     reserve a resource before touching it
  tea release   release a held lease
  tea wait      block until something changes
  tea gate      request human approval, or decide one

  tea status    agents, threads, leases, what needs a human
  tea threads   all threads
  tea leases    active leases
  tea ledger    audit trail

Identity comes from TEA_AGENT or --agent. Set TEA_SERVER to use a remote store.
Every command accepts --json.
`

func (a *App) Run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(a.Err, usage)
		return ExitUsage
	}
	cmd, rest := args[0], args[1:]

	switch cmd {
	case "-h", "--help", "help":
		fmt.Fprint(a.Out, usage)
		return ExitOK
	case "init":
		return a.cmdInit(rest)
	case "join":
		return a.cmdJoin(rest)
	case "post":
		return a.cmdPost(rest)
	case "reply":
		return a.cmdReply(rest)
	case "inbox":
		return a.cmdInbox(rest)
	case "context":
		return a.cmdContext(rest)
	case "claim":
		return a.cmdClaim(rest)
	case "release":
		return a.cmdRelease(rest)
	case "wait":
		return a.cmdWait(rest)
	case "gate":
		return a.cmdGate(rest)
	case "status":
		return a.cmdStatus(rest)
	case "threads":
		return a.cmdThreads(rest)
	case "leases":
		return a.cmdLeases(rest)
	case "ledger":
		return a.cmdLedger(rest)
	default:
		fmt.Fprintf(a.Err, "unknown command %q\n\n%s", cmd, usage)
		return ExitUsage
	}
}

// open resolves the store. TEA_SERVER switching between a local file store and a
// remote one is the payoff of having a single Store interface: no command below
// this line knows or cares which it got.
func (a *App) open() (core.Store, error) {
	if base := os.Getenv("TEA_SERVER"); base != "" {
		return transport.NewClient(strings.TrimSuffix(base, "/"), os.Getenv("TEA_TOKEN")), nil
	}
	return store.Open(a.Dir)
}

// agentID resolves identity, which must be explicit. Defaulting to the username or
// hostname would silently merge two agents on one machine into one identity, and
// then leases would stop meaning anything.
func agentID(flagVal string) (string, error) {
	if flagVal != "" {
		return flagVal, nil
	}
	if v := os.Getenv("TEA_AGENT"); v != "" {
		return v, nil
	}
	return "", errors.New("no agent identity: set TEA_AGENT or pass --agent")
}

func (a *App) fail(err error) int {
	var ce *core.ConflictError
	switch {
	case errors.As(err, &ce):
		fmt.Fprintf(a.Err, "conflict: %s\n", ce.Error())
		return ExitConflict
	case errors.Is(err, core.ErrNoWorkspace):
		fmt.Fprintln(a.Err, "no workspace here: run 'tea init' first")
		return ExitStore
	case errors.Is(err, core.ErrNeedEvidence):
		fmt.Fprintln(a.Err, "refusing to resolve: attach evidence with --evidence first")
		return ExitUsage
	case strings.Contains(err.Error(), "unreachable"):
		fmt.Fprintf(a.Err, "%v\n", err)
		return ExitStore
	default:
		fmt.Fprintf(a.Err, "error: %v\n", err)
		return ExitUsage
	}
}

func (a *App) emit(asJSON bool, v any, human func()) int {
	if asJSON {
		enc := json.NewEncoder(a.Out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(v); err != nil {
			return a.fail(err)
		}
		return ExitOK
	}
	human()
	return ExitOK
}

func fs(name string) (*flag.FlagSet, *bool, *string) {
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	asJSON := f.Bool("json", false, "machine-readable output")
	agent := f.String("agent", "", "agent identity (default $TEA_AGENT)")
	return f, asJSON, agent
}

// parse works around Go's flag package stopping at the first positional argument.
//
// Without this, `tea post "fix login" --json` silently folds --json into the title
// and returns human output, which an agent then fails to parse. Since the natural
// way to write these commands is subject-first, flags are hoisted ahead of
// positionals before parsing.
func parse(f *flag.FlagSet, args []string) error {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
			continue
		}
		flags = append(flags, a)
		// A flag needing a value consumes the next token, unless it was given as
		// --flag=value or is a boolean.
		if !strings.Contains(a, "=") && needsValue(f, a) && i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return f.Parse(append(flags, positional...))
}

// needsValue reports whether a flag takes a separate value token. Booleans do not,
// so `--json t1` must not eat t1.
func needsValue(f *flag.FlagSet, arg string) bool {
	name := strings.TrimLeft(arg, "-")
	fl := f.Lookup(name)
	if fl == nil {
		return false
	}
	bf, ok := fl.Value.(interface{ IsBoolFlag() bool })
	return !(ok && bf.IsBoolFlag())
}

// --- commands ---

func (a *App) cmdInit(args []string) int {
	f, asJSON, _ := fs("init")
	name := f.String("name", "", "workspace name")
	if err := parse(f, args); err != nil {
		return ExitUsage
	}
	n := *name
	if n == "" {
		n = filepathBase(a.Dir)
	}
	st, err := store.Init(a.Dir, n)
	if err != nil {
		return a.fail(err)
	}
	ws, err := st.Workspace(context.Background())
	if err != nil {
		return a.fail(err)
	}
	return a.emit(*asJSON, map[string]any{"workspace": ws, "path": st.Root()}, func() {
		fmt.Fprintf(a.Out, "workspace %q ready at %s\n", ws.Name, st.Root())
	})
}

func (a *App) cmdJoin(args []string) int {
	f, asJSON, agent := fs("join")
	role := f.String("role", "", "role label, e.g. implementer")
	if err := parse(f, args); err != nil {
		return ExitUsage
	}
	id, err := agentID(*agent)
	if err != nil {
		return a.fail(err)
	}
	st, err := a.open()
	if err != nil {
		return a.fail(err)
	}
	ag, err := st.UpsertAgent(context.Background(), id, *role)
	if err != nil {
		return a.fail(err)
	}
	return a.emit(*asJSON, map[string]any{"agent": ag}, func() {
		fmt.Fprintf(a.Out, "joined as %s", ag.ID)
		if ag.Role != "" {
			fmt.Fprintf(a.Out, " (%s)", ag.Role)
		}
		fmt.Fprintln(a.Out)
	})
}

func (a *App) cmdPost(args []string) int {
	f, asJSON, agent := fs("post")
	body := f.String("body", "", "opening event body")
	if err := parse(f, args); err != nil {
		return ExitUsage
	}
	title := strings.Join(f.Args(), " ")
	if title == "" {
		fmt.Fprintln(a.Err, "usage: tea post <title> [--body ...]")
		return ExitUsage
	}
	id, err := agentID(*agent)
	if err != nil {
		return a.fail(err)
	}
	st, err := a.open()
	if err != nil {
		return a.fail(err)
	}
	t, err := st.CreateThread(context.Background(), id, title, *body)
	if err != nil {
		return a.fail(err)
	}
	return a.emit(*asJSON, map[string]any{"thread": t}, func() {
		fmt.Fprintf(a.Out, "%s  %s\n", t.ID, t.Title)
	})
}

func (a *App) cmdReply(args []string) int {
	f, asJSON, agent := fs("reply")
	body := f.String("body", "", "event body")
	status := f.String("status", "", "new status: open|claimed|blocked|resolved")
	var evidence multiFlag
	f.Var(&evidence, "evidence", "evidence path or URL (repeatable)")
	if err := parse(f, args); err != nil {
		return ExitUsage
	}
	if f.NArg() < 1 {
		fmt.Fprintln(a.Err, "usage: tea reply <thread> --body ... [--status ...] [--evidence ...]")
		return ExitUsage
	}
	id, err := agentID(*agent)
	if err != nil {
		return a.fail(err)
	}
	st, err := a.open()
	if err != nil {
		return a.fail(err)
	}
	t, err := st.AppendEvent(context.Background(), f.Arg(0), id, *body, core.ThreadStatus(*status), evidence)
	if err != nil {
		return a.fail(err)
	}
	return a.emit(*asJSON, map[string]any{"thread": t}, func() {
		fmt.Fprintf(a.Out, "%s  %s  (%d events)\n", t.ID, t.Status, len(t.Events))
	})
}

func (a *App) cmdInbox(args []string) int {
	f, asJSON, agent := fs("inbox")
	scope := f.String("scope", "mine", "mine|all")
	if err := parse(f, args); err != nil {
		return ExitUsage
	}
	id, err := agentID(*agent)
	if err != nil {
		return a.fail(err)
	}
	st, err := a.open()
	if err != nil {
		return a.fail(err)
	}
	b, err := a.buildBoard(st)
	if err != nil {
		return a.fail(err)
	}

	var picked []*view.ThreadState
	for _, ts := range b.Threads {
		if ts.Thread.Status == core.StatusResolved {
			continue
		}
		// "Needs my action" is the whole point. An inbox that returns everything
		// is just ls, and costs the agent tokens to filter itself.
		mine := ts.Thread.Owner == id ||
			(ts.Thread.Owner == "" && ts.Thread.Status == core.StatusOpen) ||
			ts.Blocked || ts.WaitingOnHuman
		if *scope == "all" || mine {
			picked = append(picked, ts)
		}
	}
	return a.emit(*asJSON, map[string]any{"threads": picked}, func() {
		if len(picked) == 0 {
			fmt.Fprintln(a.Out, "inbox empty")
			return
		}
		for _, ts := range picked {
			marks := []string{}
			if ts.WaitingOnHuman {
				marks = append(marks, "needs-human")
			}
			if ts.Stalled {
				marks = append(marks, "stalled:"+ts.StalledBy)
			}
			owner := ts.Thread.Owner
			if owner == "" {
				owner = "-"
			}
			line := fmt.Sprintf("%-4s %-9s %-10s %s", ts.Thread.ID, ts.Thread.Status, owner, ts.Thread.Title)
			if len(marks) > 0 {
				line += "  [" + strings.Join(marks, " ") + "]"
			}
			fmt.Fprintln(a.Out, line)
		}
	})
}

func (a *App) cmdContext(args []string) int {
	f, asJSON, _ := fs("context")
	full := f.Bool("full", false, "return the entire event log")
	keep := f.Int("keep", 5, "recent events to keep verbatim")
	if err := parse(f, args); err != nil {
		return ExitUsage
	}
	if f.NArg() < 1 {
		fmt.Fprintln(a.Err, "usage: tea context <thread> [--full]")
		return ExitUsage
	}
	st, err := a.open()
	if err != nil {
		return a.fail(err)
	}
	ctx := context.Background()
	t, err := st.Thread(ctx, f.Arg(0))
	if err != nil {
		return a.fail(err)
	}
	leases, err := st.Leases(ctx)
	if err != nil {
		return a.fail(err)
	}
	now := a.now()
	var mine []*core.Lease
	for _, l := range leases {
		if l.Thread == t.ID && !l.Expired(now) {
			mine = append(mine, l)
		}
	}
	gates, err := st.Gates(ctx)
	if err != nil {
		return a.fail(err)
	}
	var gate *core.Gate
	for _, g := range gates {
		if g.Thread == t.ID && g.State == core.GatePending {
			gate = g
			break
		}
	}

	if *full {
		return a.emit(*asJSON, map[string]any{"thread": t, "leases": mine, "gate": gate}, func() {
			a.printThread(t, mine, gate)
		})
	}
	c := view.CompactThread(t, mine, gate, *keep)
	return a.emit(*asJSON, c, func() {
		fmt.Fprintf(a.Out, "%s  %s  %s\n", c.Thread.ID, c.Thread.Status, c.Thread.Title)
		if c.Summary != "" {
			fmt.Fprintf(a.Out, "  … %s\n", c.Summary)
		}
		for _, e := range c.Recent {
			fmt.Fprintf(a.Out, "  #%d %-8s %s\n", e.Seq, e.Agent, e.Body)
		}
		for _, l := range c.Leases {
			fmt.Fprintf(a.Out, "  lease %s held by %s\n", l.Resource, l.Holder)
		}
		if c.Gate != nil {
			fmt.Fprintf(a.Out, "  gate %s pending: %s\n", c.Gate.ID, c.Gate.Reason)
		}
	})
}

func (a *App) printThread(t *core.Thread, leases []*core.Lease, gate *core.Gate) {
	fmt.Fprintf(a.Out, "%s  %s  %s\n", t.ID, t.Status, t.Title)
	for _, e := range t.Events {
		fmt.Fprintf(a.Out, "  #%d %-8s %s\n", e.Seq, e.Agent, e.Body)
		for _, ev := range e.Evidence {
			fmt.Fprintf(a.Out, "        evidence: %s\n", ev)
		}
	}
	for _, l := range leases {
		fmt.Fprintf(a.Out, "  lease %s held by %s\n", l.Resource, l.Holder)
	}
	if gate != nil {
		fmt.Fprintf(a.Out, "  gate %s pending: %s\n", gate.ID, gate.Reason)
	}
}

func (a *App) cmdClaim(args []string) int {
	f, asJSON, agent := fs("claim")
	ttl := f.Duration("ttl", core.DefaultLeaseTTL, "lease duration")
	thread := f.String("thread", "", "associate with a thread")
	if err := parse(f, args); err != nil {
		return ExitUsage
	}
	if f.NArg() < 1 {
		fmt.Fprintln(a.Err, "usage: tea claim <resource> [--ttl 15m] [--thread t1]")
		return ExitUsage
	}
	id, err := agentID(*agent)
	if err != nil {
		return a.fail(err)
	}
	st, err := a.open()
	if err != nil {
		return a.fail(err)
	}
	l, err := st.Claim(context.Background(), id, f.Arg(0), *thread, *ttl)
	if err != nil {
		return a.fail(err) // conflict becomes exit 2, with holder and expiry
	}
	return a.emit(*asJSON, map[string]any{"lease": l}, func() {
		fmt.Fprintf(a.Out, "held %s until %s\n", l.Resource, l.Expires.Local().Format("15:04:05"))
	})
}

func (a *App) cmdRelease(args []string) int {
	f, asJSON, agent := fs("release")
	if err := parse(f, args); err != nil {
		return ExitUsage
	}
	if f.NArg() < 1 {
		fmt.Fprintln(a.Err, "usage: tea release <resource>")
		return ExitUsage
	}
	id, err := agentID(*agent)
	if err != nil {
		return a.fail(err)
	}
	st, err := a.open()
	if err != nil {
		return a.fail(err)
	}
	if err := st.Release(context.Background(), id, f.Arg(0)); err != nil {
		return a.fail(err)
	}
	return a.emit(*asJSON, map[string]any{"released": f.Arg(0)}, func() {
		fmt.Fprintf(a.Out, "released %s\n", f.Arg(0))
	})
}

// cmdWait polls rather than pushes. M0 chooses polling deliberately: a blocking
// long-poll or websocket is more machinery for the same observable behaviour at
// this scale, and the interval is invisible next to how long agent turns take.
func (a *App) cmdWait(args []string) int {
	f, asJSON, agent := fs("wait")
	timeout := f.Duration("timeout", 5*time.Minute, "give up after this long")
	interval := f.Duration("interval", 2*time.Second, "poll interval")
	lease := f.String("lease", "", "wait until this resource is claimable")
	gate := f.String("gate", "", "wait until this gate is decided")
	if err := parse(f, args); err != nil {
		return ExitUsage
	}
	st, err := a.open()
	if err != nil {
		return a.fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	var threadID string
	var baseline int
	if f.NArg() > 0 {
		threadID = f.Arg(0)
		t, err := st.Thread(ctx, threadID)
		if err != nil {
			return a.fail(err)
		}
		baseline = len(t.Events)
	}
	if threadID == "" && *lease == "" && *gate == "" {
		fmt.Fprintln(a.Err, "usage: tea wait <thread> | --lease <resource> | --gate <id>")
		return ExitUsage
	}
	me, _ := agentID(*agent)

	for {
		// Report *why* it woke, so the caller does not need a second query.
		if threadID != "" {
			t, err := st.Thread(ctx, threadID)
			if err == nil && len(t.Events) > baseline {
				last := t.Events[len(t.Events)-1]
				return a.emit(*asJSON, map[string]any{"reason": "thread.event", "thread": t, "event": last}, func() {
					fmt.Fprintf(a.Out, "%s: %s said %q (status %s)\n", t.ID, last.Agent, last.Body, t.Status)
				})
			}
		}
		if *gate != "" {
			g, err := st.Gate(ctx, *gate)
			if err == nil && g.State != core.GatePending {
				return a.emit(*asJSON, map[string]any{"reason": "gate." + string(g.State), "gate": g}, func() {
					fmt.Fprintf(a.Out, "gate %s %s by %s\n", g.ID, g.State, g.DecidedBy)
				})
			}
		}
		if *lease != "" {
			leases, err := st.Leases(ctx)
			if err == nil {
				now := a.now()
				blocked := false
				for _, l := range leases {
					if l.Expired(now) || l.Holder == me {
						continue
					}
					if l.Resource == *lease {
						blocked = true
						break
					}
				}
				if !blocked {
					return a.emit(*asJSON, map[string]any{"reason": "lease.free", "resource": *lease}, func() {
						fmt.Fprintf(a.Out, "%s is claimable\n", *lease)
					})
				}
			}
		}

		select {
		case <-ctx.Done():
			fmt.Fprintln(a.Err, "timeout: nothing changed")
			return ExitTimeout
		case <-time.After(*interval):
		}
	}
}

func (a *App) cmdGate(args []string) int {
	// Subcommands first so `tea gate approve g1` does not read as a reason string.
	if len(args) > 0 {
		switch args[0] {
		case "approve", "reject":
			return a.decideGate(args[0], args[1:])
		case "list":
			return a.listGates(args[1:])
		}
	}
	f, asJSON, agent := fs("gate")
	thread := f.String("thread", "", "thread this gate blocks")
	if err := parse(f, args); err != nil {
		return ExitUsage
	}
	reason := strings.Join(f.Args(), " ")
	if reason == "" {
		fmt.Fprintln(a.Err, "usage: tea gate <reason> [--thread t1] | tea gate approve|reject <id>")
		return ExitUsage
	}
	id, err := agentID(*agent)
	if err != nil {
		return a.fail(err)
	}
	st, err := a.open()
	if err != nil {
		return a.fail(err)
	}
	ctx := context.Background()
	g, err := st.CreateGate(ctx, id, reason, *thread)
	if err != nil {
		return a.fail(err)
	}
	// The store blocks the thread as part of CreateGate; doing it here too would
	// duplicate the invariant and let the two paths drift.
	return a.emit(*asJSON, map[string]any{"gate": g}, func() {
		fmt.Fprintf(a.Out, "gate %s open, waiting on a human: %s\n", g.ID, g.Reason)
	})
}

func (a *App) decideGate(decision string, args []string) int {
	f, asJSON, agent := fs("gate " + decision)
	note := f.String("note", "", "note for the ledger")
	if err := parse(f, args); err != nil {
		return ExitUsage
	}
	if f.NArg() < 1 {
		fmt.Fprintf(a.Err, "usage: tea gate %s <id> [--note ...]\n", decision)
		return ExitUsage
	}
	by, err := agentID(*agent)
	if err != nil {
		by = "human" // a human at a terminal should not need to export TEA_AGENT
	}
	st, err := a.open()
	if err != nil {
		return a.fail(err)
	}
	state := core.GateApproved
	if decision == "reject" {
		state = core.GateRejected
	}
	ctx := context.Background()
	g, err := st.DecideGate(ctx, f.Arg(0), state, by, *note)
	if err != nil {
		return a.fail(err)
	}
	return a.emit(*asJSON, map[string]any{"gate": g}, func() {
		fmt.Fprintf(a.Out, "gate %s %s\n", g.ID, g.State)
	})
}

func (a *App) listGates(args []string) int {
	f, asJSON, _ := fs("gate list")
	if err := parse(f, args); err != nil {
		return ExitUsage
	}
	st, err := a.open()
	if err != nil {
		return a.fail(err)
	}
	gates, err := st.Gates(context.Background())
	if err != nil {
		return a.fail(err)
	}
	return a.emit(*asJSON, map[string]any{"gates": gates}, func() {
		if len(gates) == 0 {
			fmt.Fprintln(a.Out, "no gates")
			return
		}
		for _, g := range gates {
			fmt.Fprintf(a.Out, "%-12s %-9s %-6s %s\n", g.ID, g.State, g.Thread, g.Reason)
		}
	})
}

func (a *App) buildBoard(st core.Store) (*view.Board, error) {
	ctx := context.Background()
	ws, err := st.Workspace(ctx)
	if err != nil {
		return nil, err
	}
	agents, err := st.Agents(ctx)
	if err != nil {
		return nil, err
	}
	threads, err := st.Threads(ctx)
	if err != nil {
		return nil, err
	}
	leases, err := st.Leases(ctx)
	if err != nil {
		return nil, err
	}
	gates, err := st.Gates(ctx)
	if err != nil {
		return nil, err
	}
	return view.Build(view.Input{
		Workspace: ws, Agents: agents, Threads: threads,
		Leases: leases, Gates: gates, Now: a.now(),
	}), nil
}

func (a *App) cmdStatus(args []string) int {
	f, asJSON, _ := fs("status")
	if err := parse(f, args); err != nil {
		return ExitUsage
	}
	st, err := a.open()
	if err != nil {
		return a.fail(err)
	}
	b, err := a.buildBoard(st)
	if err != nil {
		return a.fail(err)
	}
	return a.emit(*asJSON, b, func() {
		fmt.Fprintf(a.Out, "workspace %s\n\n", b.Workspace.Name)

		// Attention goes first and is the only section that must be read. Burying
		// it under healthy rows is how oversight tools become wallpaper.
		if len(b.Attention) > 0 {
			fmt.Fprintln(a.Out, "NEEDS ATTENTION")
			for _, at := range b.Attention {
				fmt.Fprintf(a.Out, "  [%s] %s\n      %s\n", at.Kind, at.Label, at.Detail)
			}
			fmt.Fprintln(a.Out)
		} else {
			fmt.Fprint(a.Out, "nothing needs a human\n\n")
		}

		fmt.Fprintln(a.Out, "AGENTS")
		for _, as := range b.Agents {
			state := "live"
			if as.Stale {
				state = "STALE"
			}
			fmt.Fprintf(a.Out, "  %-10s %-6s %-5s idle %-6s holding %d\n",
				as.Agent.ID, as.Agent.Role, state, as.Idle, len(as.Holding))
		}

		fmt.Fprintln(a.Out, "\nTHREADS")
		for _, ts := range b.Threads {
			owner := ts.Thread.Owner
			if owner == "" {
				owner = "-"
			}
			fmt.Fprintf(a.Out, "  %-4s %-9s %-10s %s\n", ts.Thread.ID, ts.Thread.Status, owner, ts.Thread.Title)
		}
	})
}

func (a *App) cmdThreads(args []string) int {
	f, asJSON, _ := fs("threads")
	if err := parse(f, args); err != nil {
		return ExitUsage
	}
	st, err := a.open()
	if err != nil {
		return a.fail(err)
	}
	list, err := st.Threads(context.Background())
	if err != nil {
		return a.fail(err)
	}
	return a.emit(*asJSON, map[string]any{"threads": list}, func() {
		for _, t := range list {
			owner := t.Owner
			if owner == "" {
				owner = "-"
			}
			fmt.Fprintf(a.Out, "%-4s %-9s %-10s %s\n", t.ID, t.Status, owner, t.Title)
		}
	})
}

func (a *App) cmdLeases(args []string) int {
	f, asJSON, _ := fs("leases")
	if err := parse(f, args); err != nil {
		return ExitUsage
	}
	st, err := a.open()
	if err != nil {
		return a.fail(err)
	}
	list, err := st.Leases(context.Background())
	if err != nil {
		return a.fail(err)
	}
	now := a.now()
	live := make([]*core.Lease, 0, len(list))
	for _, l := range list {
		if !l.Expired(now) {
			live = append(live, l)
		}
	}
	return a.emit(*asJSON, map[string]any{"leases": live}, func() {
		if len(live) == 0 {
			fmt.Fprintln(a.Out, "no active leases")
			return
		}
		for _, l := range live {
			fmt.Fprintf(a.Out, "%-24s %-10s expires %s\n",
				l.Resource, l.Holder, l.Expires.Local().Format("15:04:05"))
		}
	})
}

func (a *App) cmdLedger(args []string) int {
	f, asJSON, _ := fs("ledger")
	tail := f.Int("tail", 20, "entries to show")
	if err := parse(f, args); err != nil {
		return ExitUsage
	}
	st, err := a.open()
	if err != nil {
		return a.fail(err)
	}
	entries, err := st.Ledger(context.Background(), *tail)
	if err != nil {
		return a.fail(err)
	}
	return a.emit(*asJSON, map[string]any{"ledger": entries}, func() {
		for _, e := range entries {
			fmt.Fprintf(a.Out, "%s %-10s %-16s %s\n",
				e.At.Local().Format("15:04:05"), e.Agent, e.Action, e.Target)
		}
	})
}

// multiFlag collects a repeatable string flag.
type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

func filepathBase(p string) string {
	p = strings.TrimRight(p, "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	if p == "" {
		return "workspace"
	}
	return p
}
