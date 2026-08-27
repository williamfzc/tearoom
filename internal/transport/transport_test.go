package transport

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/williamfzc/tearoom/internal/core"
	"github.com/williamfzc/tearoom/internal/store"
	"github.com/williamfzc/tearoom/internal/view"
)

// newPair wires a real file store behind a real HTTP server and returns a client
// pointing at it. No mocks: the point is to prove the remote path behaves like the
// local one, and a mock would only prove the mock matches itself.
func newPair(t *testing.T) (*Client, *store.File) {
	t.Helper()
	f, err := store.Init(t.TempDir(), "test")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewServer(f).Handler())
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, ""), f
}

// TestClientSatisfiesStore is the isomorphism claim in executable form: the remote
// client must be substitutable for the local store everywhere.
func TestClientSatisfiesStore(t *testing.T) {
	var _ core.Store = (*Client)(nil)
	var _ core.Store = (*store.File)(nil)
}

func TestRoundTripThroughHTTP(t *testing.T) {
	c, _ := newPair(t)
	ctx := context.Background()

	if _, err := c.UpsertAgent(ctx, "impl", "implementer"); err != nil {
		t.Fatal(err)
	}
	th, err := c.CreateThread(ctx, "impl", "fix login", "fails on retry")
	if err != nil {
		t.Fatal(err)
	}
	if th.ID != "t1" {
		t.Errorf("thread id = %q, want t1", th.ID)
	}

	if _, err := c.AppendEvent(ctx, th.ID, "impl", "patched", core.StatusClaimed, nil); err != nil {
		t.Fatal(err)
	}
	got, err := c.Thread(ctx, th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != core.StatusClaimed || got.Owner != "impl" {
		t.Errorf("status=%q owner=%q after claim", got.Status, got.Owner)
	}
	if len(got.Events) != 2 {
		t.Errorf("events = %d, want 2", len(got.Events))
	}
}

// TestConflictSurvivesTheWire is the important one. A conflict must arrive at the
// client as a typed error carrying holder and expiry, otherwise a remote agent
// cannot decide whether to wait or move on — and errors.Is must still work, or
// every caller regresses to string matching.
func TestConflictSurvivesTheWire(t *testing.T) {
	c, _ := newPair(t)
	ctx := context.Background()
	c.UpsertAgent(ctx, "a", "impl")
	c.UpsertAgent(ctx, "b", "impl")

	if _, err := c.Claim(ctx, "a", "src/**", "", time.Minute); err != nil {
		t.Fatal(err)
	}
	_, err := c.Claim(ctx, "b", "src/auth.ts", "", time.Minute)
	if err == nil {
		t.Fatal("overlapping remote claim succeeded")
	}
	if !errors.Is(err, core.ErrConflict) {
		t.Fatalf("errors.Is broke across HTTP: %v", err)
	}
	var ce *core.ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("conflict lost its type over the wire: %v", err)
	}
	if ce.Holder != "a" {
		t.Errorf("holder = %q, want a", ce.Holder)
	}
	if ce.Expires.IsZero() {
		t.Error("expiry did not survive serialisation")
	}
}

func TestSentinelErrorsSurviveTheWire(t *testing.T) {
	c, _ := newPair(t)
	ctx := context.Background()

	if _, err := c.Thread(ctx, "nope"); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("ErrNotFound lost: %v", err)
	}

	th, _ := c.CreateThread(ctx, "a", "task", "")
	_, err := c.AppendEvent(ctx, th.ID, "a", "done", core.StatusResolved, nil)
	if !errors.Is(err, core.ErrNeedEvidence) {
		t.Errorf("ErrNeedEvidence lost: %v", err)
	}

	c.UpsertAgent(ctx, "a", "impl")
	c.UpsertAgent(ctx, "b", "impl")
	c.Claim(ctx, "a", "x/**", "", time.Minute)
	if err := c.Release(ctx, "b", "x/**"); !errors.Is(err, core.ErrNotHolder) {
		t.Errorf("ErrNotHolder lost: %v", err)
	}
}

// TestBoardEndpointRunsSharedView proves the console gets a board derived by the
// same code the CLI uses, rather than logic reimplemented in the frontend.
func TestBoardEndpointRunsSharedView(t *testing.T) {
	c, f := newPair(t)
	ctx := context.Background()

	base := time.Now()
	f.SetClock(func() time.Time { return base })
	c.UpsertAgent(ctx, "dead", "impl")
	th, _ := c.CreateThread(ctx, "dead", "half-done work", "")
	c.AppendEvent(ctx, th.ID, "dead", "working", core.StatusClaimed, nil)

	// Advance past staleness on both the store and the server's clock.
	later := base.Add(core.StaleAfter + time.Minute)
	f.SetClock(func() time.Time { return later })
	srv := NewServer(f)
	srv.Now = func() time.Time { return later }
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	c2 := NewClient(ts.URL, "")

	var b view.Board
	if err := c2.GetJSON(ctx, "/api/board", &b); err != nil {
		t.Fatal(err)
	}
	if len(b.Threads) != 1 {
		t.Fatalf("board has %d threads, want 1", len(b.Threads))
	}
	if !b.Threads[0].Stalled {
		t.Error("server-side view did not flag the stalled thread")
	}
	found := false
	for _, at := range b.Attention {
		if at.Kind == view.Stalled {
			found = true
		}
	}
	if !found {
		t.Error("attention list did not survive JSON round-trip to the console")
	}
}

func TestGateFlowOverHTTP(t *testing.T) {
	c, _ := newPair(t)
	ctx := context.Background()
	c.UpsertAgent(ctx, "a", "impl")
	th, _ := c.CreateThread(ctx, "a", "risky change", "")

	g, err := c.CreateGate(ctx, "a", "needs prod approval", th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if g.State != core.GatePending {
		t.Fatalf("gate state = %q", g.State)
	}
	decided, err := c.DecideGate(ctx, g.ID, core.GateApproved, "human", "reviewed")
	if err != nil {
		t.Fatal(err)
	}
	if decided.State != core.GateApproved || decided.DecidedBy != "human" {
		t.Errorf("decision did not persist: %+v", decided)
	}
}

func TestUnreachableServerFailsLoudly(t *testing.T) {
	// A transport failure must never look like a successful no-op: an agent that
	// believes it holds a lease it does not hold is worse than a crash.
	c := NewClient("http://127.0.0.1:1", "")
	c.HTTP.Timeout = 500 * time.Millisecond
	_, err := c.Claim(context.Background(), "a", "src/**", "", time.Minute)
	if err == nil {
		t.Fatal("claim against a dead server reported success")
	}
	if !contains(err.Error(), "unreachable") {
		t.Errorf("error should say the server is unreachable, got: %v", err)
	}
}

func TestLedgerOverHTTP(t *testing.T) {
	c, _ := newPair(t)
	ctx := context.Background()
	c.CreateThread(ctx, "a", "one", "")
	c.CreateThread(ctx, "a", "two", "")

	entries, err := c.Ledger(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 2 {
		t.Errorf("ledger returned %d entries, want at least 2", len(entries))
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
