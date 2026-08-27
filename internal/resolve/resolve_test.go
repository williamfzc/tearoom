// Contract tests for the assembly layer. They verify what resolve promises its
// callers — that expired leases and settled gates never reach the layer above, and
// that a partial read fails instead of producing a half-true board — rather than
// re-testing the derivations that view already owns.
package resolve

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/williamfzc/tearoom/internal/core"
)

var t0 = time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)

func clock() time.Time { return t0 }

// fake is a Store that returns whatever a test puts in it, and can be told to fail
// on one specific read.
type fake struct {
	ws      *core.Workspace
	agents  []*core.Agent
	threads []*core.Thread
	leases  []*core.Lease
	gates   []*core.Gate
	failOn  string
}

var errBoom = errors.New("store unavailable")

func (f *fake) fail(name string) error {
	if f.failOn == name {
		return errBoom
	}
	return nil
}

func (f *fake) Workspace(context.Context) (*core.Workspace, error) {
	return f.ws, f.fail("workspace")
}
func (f *fake) Agents(context.Context) ([]*core.Agent, error) { return f.agents, f.fail("agents") }
func (f *fake) Threads(context.Context) ([]*core.Thread, error) {
	return f.threads, f.fail("threads")
}
func (f *fake) Leases(context.Context) ([]*core.Lease, error) { return f.leases, f.fail("leases") }
func (f *fake) Gates(context.Context) ([]*core.Gate, error)   { return f.gates, f.fail("gates") }

func (f *fake) Thread(_ context.Context, id string) (*core.Thread, error) {
	if err := f.fail("thread"); err != nil {
		return nil, err
	}
	for _, t := range f.threads {
		if t.ID == id {
			return t, nil
		}
	}
	return nil, core.ErrNotFound
}

// The remaining Store methods are mutations; resolve only reads, so reaching one
// from this layer is a bug and should fail loudly rather than return a zero value.
func (f *fake) UpsertAgent(context.Context, string, string) (*core.Agent, error) {
	panic("resolve must not mutate: UpsertAgent")
}
func (f *fake) CreateThread(context.Context, string, string, string) (*core.Thread, error) {
	panic("resolve must not mutate: CreateThread")
}
func (f *fake) AppendEvent(context.Context, string, string, string, core.ThreadStatus, []string) (*core.Thread, error) {
	panic("resolve must not mutate: AppendEvent")
}
func (f *fake) Claim(context.Context, string, string, string, time.Duration) (*core.Lease, error) {
	panic("resolve must not mutate: Claim")
}
func (f *fake) Release(context.Context, string, string) error {
	panic("resolve must not mutate: Release")
}
func (f *fake) CreateGate(context.Context, string, string, string) (*core.Gate, error) {
	panic("resolve must not mutate: CreateGate")
}
func (f *fake) DecideGate(context.Context, string, core.GateState, string, string) (*core.Gate, error) {
	panic("resolve must not mutate: DecideGate")
}
func (f *fake) Gate(context.Context, string) (*core.Gate, error) {
	panic("resolve reads gates in bulk, not one by one")
}
func (f *fake) Append(context.Context, core.LedgerEntry) error {
	panic("resolve must not mutate: Append")
}
func (f *fake) Ledger(context.Context, int) ([]core.LedgerEntry, error) {
	panic("resolve does not read the ledger")
}

func base() *fake {
	return &fake{
		ws:      &core.Workspace{ID: "w1", Name: "test"},
		threads: []*core.Thread{{ID: "t1", Title: "work", Status: core.StatusClaimed, Owner: "impl"}},
	}
}

func TestThreadDropsExpiredLeases(t *testing.T) {
	f := base()
	f.leases = []*core.Lease{
		{Resource: "live/**", Thread: "t1", Holder: "impl", Expires: t0.Add(time.Hour)},
		{Resource: "dead/**", Thread: "t1", Holder: "impl", Expires: t0.Add(-time.Minute)},
	}
	d, err := Thread(context.Background(), f, "t1", clock)
	if err != nil {
		t.Fatal(err)
	}
	// An expired lease is not a lease. Letting one through would make the caller
	// believe a resource is held when anyone may take it.
	if len(d.Leases) != 1 || d.Leases[0].Resource != "live/**" {
		t.Errorf("leases = %+v, want only live/**", d.Leases)
	}
}

func TestThreadIgnoresOtherThreadsLeases(t *testing.T) {
	f := base()
	f.leases = []*core.Lease{
		{Resource: "mine/**", Thread: "t1", Holder: "impl", Expires: t0.Add(time.Hour)},
		{Resource: "theirs/**", Thread: "t2", Holder: "other", Expires: t0.Add(time.Hour)},
	}
	d, err := Thread(context.Background(), f, "t1", clock)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Leases) != 1 || d.Leases[0].Resource != "mine/**" {
		t.Errorf("leases = %+v, want only this thread's", d.Leases)
	}
}

func TestThreadReturnsOnlyPendingGate(t *testing.T) {
	f := base()
	f.gates = []*core.Gate{
		{ID: "g1", Thread: "t1", State: core.GateApproved, Reason: "old"},
		{ID: "g2", Thread: "t1", State: core.GatePending, Reason: "current"},
	}
	d, err := Thread(context.Background(), f, "t1", clock)
	if err != nil {
		t.Fatal(err)
	}
	// A settled gate is history. Surfacing it would tell an agent to wait for a
	// decision that has already been made.
	if d.Gate == nil || d.Gate.ID != "g2" {
		t.Errorf("gate = %+v, want the pending one", d.Gate)
	}
}

func TestThreadHasNoGateWhenAllSettled(t *testing.T) {
	f := base()
	f.gates = []*core.Gate{{ID: "g1", Thread: "t1", State: core.GateRejected}}
	d, err := Thread(context.Background(), f, "t1", clock)
	if err != nil {
		t.Fatal(err)
	}
	if d.Gate != nil {
		t.Errorf("gate = %+v, want nil once settled", d.Gate)
	}
}

func TestThreadPropagatesMissing(t *testing.T) {
	if _, err := Thread(context.Background(), base(), "nope", clock); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// A board assembled from a partial read would misreport who is stuck, which is
// worse than returning an error. Each read must be able to abort the whole thing.
func TestBoardFailsFastOnAnyRead(t *testing.T) {
	for _, stage := range []string{"workspace", "agents", "threads", "leases", "gates"} {
		f := base()
		f.failOn = stage
		if _, err := Board(context.Background(), f, clock); !errors.Is(err, errBoom) {
			t.Errorf("failure in %s read: err = %v, want it propagated", stage, err)
		}
	}
}

func TestThreadFailsFastOnAnyRead(t *testing.T) {
	for _, stage := range []string{"thread", "leases", "gates"} {
		f := base()
		f.failOn = stage
		if _, err := Thread(context.Background(), f, "t1", clock); !errors.Is(err, errBoom) {
			t.Errorf("failure in %s read: err = %v, want it propagated", stage, err)
		}
	}
}

func TestBoardUsesTheSuppliedClock(t *testing.T) {
	f := base()
	f.agents = []*core.Agent{{ID: "impl", Role: "implementer", LastSeen: t0.Add(-time.Hour)}}
	b, err := Board(context.Background(), f, clock)
	if err != nil {
		t.Fatal(err)
	}
	// Passing the clock in is what keeps this layer testable; if it silently called
	// time.Now the staleness below would depend on the wall clock.
	if len(b.Agents) != 1 || !b.Agents[0].Stale {
		t.Error("an agent unseen for an hour should be stale at the injected time")
	}
}

func TestCompactGoesThroughThread(t *testing.T) {
	f := base()
	f.threads[0].Events = []core.Event{
		{Seq: 1, Agent: "impl", Body: "one"},
		{Seq: 2, Agent: "impl", Body: "two"},
		{Seq: 3, Agent: "impl", Body: "three"},
	}
	f.leases = []*core.Lease{{Resource: "gone/**", Thread: "t1", Expires: t0.Add(-time.Minute)}}
	c, err := Compact(context.Background(), f, "t1", 2, clock)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Recent) != 2 {
		t.Errorf("kept %d events, want 2", len(c.Recent))
	}
	// The filtering promise must hold through Compact too, not just Thread.
	if len(c.Leases) != 0 {
		t.Errorf("compact carried an expired lease: %+v", c.Leases)
	}
}
