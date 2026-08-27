//go:build js && wasm

// Command tea-web is the console, compiled to WebAssembly.
//
// This is the concrete payoff of building both ends in Go. The console does not
// reimplement "who is stuck", "is this waiting on a human", or urgency ordering:
// it imports the same internal/view package the CLI uses, so the terminal and the
// browser cannot drift apart. The only thing here that is browser-specific is DOM
// construction.
package main

import (
	"context"
	"strconv"
	"strings"
	"syscall/js"
	"time"

	"github.com/williamfzc/tearoom/internal/core"
	"github.com/williamfzc/tearoom/internal/transport"
	"github.com/williamfzc/tearoom/internal/view"
)

var (
	doc    = js.Global().Get("document")
	client *transport.Client

	// lastPaint fingerprints what is currently on screen. Repainting only on real
	// change keeps text selection alive, stops the DOM churning under the reader,
	// and avoids re-creating event callbacks every tick.
	lastPaint string

	// liveFuncs holds the callbacks attached to the current DOM. js.FuncOf
	// allocations are never collected until released, so a rebuild without this
	// leaks one per button per poll — invisible on a desk, fatal on a wallboard
	// left open for a week.
	liveFuncs []js.Func
)

func main() {
	base := js.Global().Get("location").Get("origin").String()
	client = transport.NewClient(base, "")

	render()
	// Poll instead of pushing. A human glances at this a few times an hour, so a
	// websocket buys nothing and costs reconnection logic on both ends.
	go func() {
		for range time.Tick(3 * time.Second) {
			render()
		}
	}()
	select {} // keep the wasm instance alive
}

func render() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	b, err := fetchBoard(ctx)
	root := doc.Call("getElementById", "app")
	if err != nil {
		// Show the failure, but only repaint if it is new, so a persistently down
		// server does not strobe the page.
		if lastPaint != "err:"+err.Error() {
			lastPaint = "err:" + err.Error()
			replaceChildren(root, errorCard(err))
		}
		return
	}

	// Compare everything except the clock. The timestamp alone changing is not a
	// reason to throw away the DOM the human is currently reading.
	fp := fingerprint(b)
	if fp == lastPaint {
		touchTimestamp(b)
		return
	}
	lastPaint = fp

	root.Set("innerHTML", "")
	releaseFuncs()
	root.Call("appendChild", header(b))

	// Attention first, and visually loudest. If a human has to scan past healthy
	// rows to find the problem, the console has failed at its one job.
	if len(b.Attention) > 0 {
		root.Call("appendChild", attentionSection(b.Attention))
	} else {
		root.Call("appendChild", calmCard())
	}
	root.Call("appendChild", agentsSection(b.Agents))
	root.Call("appendChild", threadsSection(b.Threads))
}

// fingerprint captures the meaningful state of the board, deliberately excluding
// the "updated" clock and the idle durations that tick every second. Including
// those would make every poll look like a change and defeat the whole point.
func fingerprint(b *view.Board) string {
	var sb strings.Builder
	sb.WriteString(b.Workspace.Name)
	for _, at := range b.Attention {
		sb.WriteString("|a:" + at.Kind.String() + at.Label + at.Gate)
	}
	for _, as := range b.Agents {
		sb.WriteString("|g:" + as.Agent.ID + as.Agent.Role + strconv.FormatBool(as.Stale) +
			strconv.Itoa(len(as.Holding)))
	}
	for _, ts := range b.Threads {
		sb.WriteString("|t:" + ts.Thread.ID + string(ts.Thread.Status) + ts.Thread.Title +
			ts.Thread.Owner + strconv.FormatBool(ts.Stalled) + strconv.FormatBool(ts.WaitingOnHuman))
	}
	return sb.String()
}

// touchTimestamp refreshes just the clock so the page still visibly proves it is
// live, without rebuilding anything around it.
func touchTimestamp(b *view.Board) {
	stamp := doc.Call("getElementById", "stamp")
	if !stamp.IsNull() && !stamp.IsUndefined() {
		stamp.Set("textContent", "updated "+b.At.Local().Format("15:04:05"))
	}
}

func replaceChildren(root js.Value, child js.Value) {
	root.Set("innerHTML", "")
	releaseFuncs()
	root.Call("appendChild", child)
}

// releaseFuncs frees callbacks belonging to the DOM we are about to discard.
func releaseFuncs() {
	for _, f := range liveFuncs {
		f.Release()
	}
	liveFuncs = nil
}

// fetchBoard asks the server for an already-resolved board. The server runs
// view.Build; we deserialise into the same struct type. One derivation, two
// renderers.
func fetchBoard(ctx context.Context) (*view.Board, error) {
	var b view.Board
	if err := client.GetJSON(ctx, "/api/board", &b); err != nil {
		return nil, err
	}
	return &b, nil
}

func el(tag string, class string) js.Value {
	e := doc.Call("createElement", tag)
	if class != "" {
		e.Set("className", class)
	}
	return e
}

func text(tag, class, s string) js.Value {
	e := el(tag, class)
	e.Set("textContent", s)
	return e
}

func header(b *view.Board) js.Value {
	h := el("header", "hdr")
	h.Call("appendChild", text("h1", "", b.Workspace.Name))
	stamp := text("span", "muted", "updated "+b.At.Local().Format("15:04:05"))
	stamp.Set("id", "stamp")
	h.Call("appendChild", stamp)
	return h
}

func calmCard() js.Value {
	c := el("section", "card calm")
	c.Call("appendChild", text("h2", "", "Nothing needs a human"))
	c.Call("appendChild", text("p", "muted", "Agents are working or idle. No gates, stalls or deadlocks."))
	return c
}

func errorCard(err error) js.Value {
	c := el("section", "card bad")
	c.Call("appendChild", text("h2", "", "Cannot reach the workspace"))
	c.Call("appendChild", text("p", "muted", err.Error()))
	return c
}

func attentionSection(items []view.Attention) js.Value {
	s := el("section", "card urgent")
	s.Call("appendChild", text("h2", "", "Needs attention ("+strconv.Itoa(len(items))+")"))
	for _, at := range items {
		row := el("div", "att att-"+at.Kind.String())
		row.Call("appendChild", text("span", "tag", at.Kind.String()))
		body := el("div", "att-body")
		body.Call("appendChild", text("div", "att-label", at.Label))
		body.Call("appendChild", text("div", "muted", at.Detail))
		row.Call("appendChild", body)

		// The console's entire write surface is approve / reject. Anything more and
		// it starts becoming the chat app we are deliberately not building.
		if at.Gate != "" {
			row.Call("appendChild", gateButtons(at.Gate))
		}
		s.Call("appendChild", row)
	}
	return s
}

func gateButtons(gateID string) js.Value {
	wrap := el("div", "actions")
	for _, spec := range []struct {
		label string
		state core.GateState
		class string
	}{
		{"Approve", core.GateApproved, "btn ok"},
		{"Reject", core.GateRejected, "btn no"},
	} {
		st := spec.state
		btn := text("button", spec.class, spec.label)
		cb := js.FuncOf(func(js.Value, []js.Value) any {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				client.DecideGate(ctx, gateID, st, "human", "decided in console")
				render()
			}()
			return nil
		})
		liveFuncs = append(liveFuncs, cb)
		btn.Call("addEventListener", "click", cb)
		wrap.Call("appendChild", btn)
	}
	return wrap
}

func agentsSection(agents []*view.AgentState) js.Value {
	s := el("section", "card")
	s.Call("appendChild", text("h2", "", "Agents"))
	for _, as := range agents {
		row := el("div", "row")
		dot := el("span", "dot")
		if as.Stale {
			dot.Set("className", "dot stale")
		}
		row.Call("appendChild", dot)
		row.Call("appendChild", text("span", "name", as.Agent.ID))
		row.Call("appendChild", text("span", "muted", as.Agent.Role))
		row.Call("appendChild", text("span", "muted", "idle "+as.Idle.String()))
		if n := len(as.Holding); n > 0 {
			row.Call("appendChild", text("span", "tag", "holding "+strconv.Itoa(n)))
		}
		s.Call("appendChild", row)
	}
	return s
}

func threadsSection(threads []*view.ThreadState) js.Value {
	s := el("section", "card")
	s.Call("appendChild", text("h2", "", "Threads"))
	for _, ts := range threads {
		row := el("div", "row")
		row.Call("appendChild", text("span", "tid", ts.Thread.ID))
		row.Call("appendChild", text("span", "status status-"+string(ts.Thread.Status), string(ts.Thread.Status)))
		row.Call("appendChild", text("span", "title", ts.Thread.Title))
		owner := ts.Thread.Owner
		if owner == "" {
			owner = "unowned"
		}
		row.Call("appendChild", text("span", "muted", owner))
		if ts.Stalled {
			row.Call("appendChild", text("span", "tag bad", "stalled"))
		}
		if ts.WaitingOnHuman {
			row.Call("appendChild", text("span", "tag warn", "needs human"))
		}
		s.Call("appendChild", row)
	}
	return s
}
