// Package resolve loads raw workspace state through a core.Store and hands it to
// the pure derivations in view.
//
// It exists because view must stay pure — no I/O, no clock — while every client
// needs the same five reads in the same order before it can derive anything. That
// sequence was written twice, once in the CLI and once in the HTTP server, which
// made it two copies of one fact. It now lives here, and both call it.
//
// The layer above (cli, transport) owns presentation and protocol. The layers below
// (store, view) own persistence and judgement. This package owns only the assembly
// between them, and deliberately holds no product opinion of its own.
package resolve

import (
	"context"
	"time"

	"github.com/williamfzc/tearoom/internal/core"
	"github.com/williamfzc/tearoom/internal/view"
)

// Clock supplies the current time. Callers pass their own so tests stay
// deterministic and the server and CLI can share one implementation.
type Clock func() time.Time

// Board reads the whole workspace and resolves it.
//
// The reads are sequential and fail fast: a board built from a partial workspace
// would misreport who is stuck, and a wrong answer here is worse than an error.
func Board(ctx context.Context, st core.Store, now Clock) (*view.Board, error) {
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
		Workspace: ws,
		Agents:    agents,
		Threads:   threads,
		Leases:    leases,
		Gates:     gates,
		Now:       now(),
	}), nil
}

// ThreadDetail is the raw material behind one thread: the thread itself, the leases
// still live against it, and the gate still waiting on a human.
//
// It is returned rather than rendered because two callers want different shapes
// from the same reads — `tea context --full` prints all of it, everything else
// compacts it. Fetching twice to serve those two shapes would be the duplication
// this package exists to remove.
type ThreadDetail struct {
	Thread *core.Thread
	Leases []*core.Lease
	Gate   *core.Gate
}

// Thread reads one thread and the live state attached to it.
//
// Expired leases and settled gates are filtered out here, once, so no caller has to
// remember that an expired lease is not a lease.
func Thread(ctx context.Context, st core.Store, id string, now Clock) (*ThreadDetail, error) {
	t, err := st.Thread(ctx, id)
	if err != nil {
		return nil, err
	}
	leases, err := st.Leases(ctx)
	if err != nil {
		return nil, err
	}
	at := now()
	d := &ThreadDetail{Thread: t}
	for _, l := range leases {
		if l.Thread == id && !l.Expired(at) {
			d.Leases = append(d.Leases, l)
		}
	}
	gates, err := st.Gates(ctx)
	if err != nil {
		return nil, err
	}
	for _, g := range gates {
		if g.Thread == id && g.State == core.GatePending {
			d.Gate = g
			break
		}
	}
	return d, nil
}

// Compact resolves a single thread into the smallest form an agent can act on,
// keeping at most keep trailing events.
func Compact(ctx context.Context, st core.Store, id string, keep int, now Clock) (*view.Compact, error) {
	d, err := Thread(ctx, st, id, now)
	if err != nil {
		return nil, err
	}
	return view.CompactThread(d.Thread, d.Leases, d.Gate, keep), nil
}
