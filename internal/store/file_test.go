package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/williamfzc/tearoom/internal/core"
)

func newTestStore(t *testing.T) *File {
	t.Helper()
	f, err := Init(t.TempDir(), "test")
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	return f
}

func TestOverlapDetection(t *testing.T) {
	// Exact-string comparison would let "src/**" and "src/auth/a.ts" both succeed,
	// which is the precise bug this product exists to prevent. These cases pin that
	// down.
	cases := []struct {
		a, b string
		want bool
	}{
		{"src/auth.ts", "src/auth.ts", true},
		{"src/**", "src/auth/a.ts", true},
		{"src/auth/a.ts", "src/**", true},
		{"src/**", "src", true},
		{"src/", "src/auth.ts", true},
		{"src/*.ts", "src/auth.ts", true},
		{"src/**", "test/a.ts", false},
		{"src/auth.ts", "src/user.ts", false},
		{"docs/**", "src/**", false},
	}
	for _, c := range cases {
		if got := overlaps(c.a, c.b); got != c.want {
			t.Errorf("overlaps(%q,%q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestClaimConflictIsActionable(t *testing.T) {
	f := newTestStore(t)
	ctx := context.Background()
	f.UpsertAgent(ctx, "a", "impl")
	f.UpsertAgent(ctx, "b", "verify")

	if _, err := f.Claim(ctx, "a", "src/**", "", time.Minute); err != nil {
		t.Fatalf("first claim: %v", err)
	}

	_, err := f.Claim(ctx, "b", "src/auth.ts", "", time.Minute)
	if err == nil {
		t.Fatal("overlapping claim succeeded; silent overwrite is possible")
	}
	if !errors.Is(err, core.ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
	// A bare rejection is useless: the agent cannot choose between waiting and
	// moving on without knowing who holds it and until when.
	var ce *core.ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("conflict lacks detail: %v", err)
	}
	if ce.Holder != "a" {
		t.Errorf("holder = %q, want a", ce.Holder)
	}
	if ce.Expires.IsZero() {
		t.Error("conflict has no expiry, so the caller cannot decide how long to wait")
	}
}

func TestReclaimOwnLeaseExtends(t *testing.T) {
	f := newTestStore(t)
	ctx := context.Background()
	f.UpsertAgent(ctx, "a", "impl")

	first, err := f.Claim(ctx, "a", "src/**", "", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.Claim(ctx, "a", "src/**", "", 10*time.Minute)
	if err != nil {
		t.Fatalf("re-claiming own lease failed: %v", err)
	}
	if !second.Expires.After(first.Expires) {
		t.Error("re-claim did not extend the lease")
	}
}

func TestStaleHolderIsPreemptable(t *testing.T) {
	f := newTestStore(t)
	ctx := context.Background()

	// Agent "dead" joins and claims, then time jumps past the staleness window.
	base := time.Now()
	f.SetClock(func() time.Time { return base })
	f.UpsertAgent(ctx, "dead", "impl")
	if _, err := f.Claim(ctx, "dead", "src/**", "", time.Hour); err != nil {
		t.Fatal(err)
	}

	f.SetClock(func() time.Time { return base.Add(core.StaleAfter + time.Minute) })
	f.UpsertAgent(ctx, "live", "impl")

	// The lease has not expired, but its holder is gone. Deadlock would be worse
	// than takeover here, so this must succeed.
	if _, err := f.Claim(ctx, "live", "src/**", "", time.Minute); err != nil {
		t.Fatalf("could not preempt a dead agent's lease: %v", err)
	}

	entries, _ := f.Ledger(ctx, 0)
	found := false
	for _, e := range entries {
		if e.Action == "lease.preempt" {
			found = true
		}
	}
	if !found {
		t.Error("preemption was not recorded; takeovers must be reviewable")
	}
}

func TestReleaseRequiresHolder(t *testing.T) {
	f := newTestStore(t)
	ctx := context.Background()
	f.UpsertAgent(ctx, "a", "impl")
	f.UpsertAgent(ctx, "b", "impl")
	f.Claim(ctx, "a", "src/**", "", time.Minute)

	err := f.Release(ctx, "b", "src/**")
	if !errors.Is(err, core.ErrNotHolder) {
		t.Fatalf("agent b released agent a's lease: %v", err)
	}
	if err := f.Release(ctx, "a", "src/**"); err != nil {
		t.Fatalf("holder could not release: %v", err)
	}
}

func TestResolveRequiresEvidence(t *testing.T) {
	f := newTestStore(t)
	ctx := context.Background()
	th, err := f.CreateThread(ctx, "a", "fix the thing", "")
	if err != nil {
		t.Fatal(err)
	}

	// "Done" with no proof is the most common way a multi-agent run silently goes
	// wrong, so this is refused.
	_, err = f.AppendEvent(ctx, th.ID, "a", "done", core.StatusResolved, nil)
	if !errors.Is(err, core.ErrNeedEvidence) {
		t.Fatalf("resolved without evidence: %v", err)
	}

	if _, err := f.AppendEvent(ctx, th.ID, "a", "tests pass", core.StatusResolved, []string{"ci.log"}); err != nil {
		t.Fatalf("resolve with evidence failed: %v", err)
	}
	got, _ := f.Thread(ctx, th.ID)
	if got.Status != core.StatusResolved {
		t.Errorf("status = %q, want resolved", got.Status)
	}
	if got.Owner != "" {
		t.Errorf("owner should clear on resolve, got %q", got.Owner)
	}
}

func TestClaimedThreadTracksOwner(t *testing.T) {
	f := newTestStore(t)
	ctx := context.Background()
	th, _ := f.CreateThread(ctx, "a", "task", "")
	updated, err := f.AppendEvent(ctx, th.ID, "worker", "on it", core.StatusClaimed, nil)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Owner != "worker" {
		t.Errorf("owner = %q, want worker", updated.Owner)
	}
}

func TestExpiredLeaseIsFree(t *testing.T) {
	f := newTestStore(t)
	ctx := context.Background()
	base := time.Now()
	f.SetClock(func() time.Time { return base })
	f.UpsertAgent(ctx, "a", "impl")
	f.Claim(ctx, "a", "src/**", "", time.Minute)

	f.SetClock(func() time.Time { return base.Add(2 * time.Minute) })
	f.UpsertAgent(ctx, "b", "impl")
	if _, err := f.Claim(ctx, "b", "src/**", "", time.Minute); err != nil {
		t.Fatalf("expired lease still blocking: %v", err)
	}
}

func TestThreadIDsAreShortAndSequential(t *testing.T) {
	f := newTestStore(t)
	ctx := context.Background()
	for i, want := range []string{"t1", "t2", "t3"} {
		th, err := f.CreateThread(ctx, "a", "task", "")
		if err != nil {
			t.Fatal(err)
		}
		if th.ID != want {
			t.Errorf("thread %d id = %q, want %q", i, th.ID, want)
		}
	}
}

func TestGateLifecycle(t *testing.T) {
	f := newTestStore(t)
	ctx := context.Background()
	g, err := f.CreateGate(ctx, "a", "needs prod migration", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if g.State != core.GatePending {
		t.Fatalf("new gate state = %q", g.State)
	}
	if _, err := f.DecideGate(ctx, g.ID, core.GateApproved, "human", "ok"); err != nil {
		t.Fatal(err)
	}
	// Deciding twice would let a rejected action be quietly re-approved.
	if _, err := f.DecideGate(ctx, g.ID, core.GateRejected, "human", ""); err == nil {
		t.Error("re-deciding a settled gate should fail")
	}
}

func TestLedgerIsAppendOnly(t *testing.T) {
	f := newTestStore(t)
	ctx := context.Background()
	f.CreateThread(ctx, "a", "one", "")
	f.CreateThread(ctx, "a", "two", "")

	entries, err := f.Ledger(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 2 {
		t.Fatalf("ledger has %d entries, want at least 2", len(entries))
	}
	for i := 1; i < len(entries); i++ {
		if entries[i].At.Before(entries[i-1].At) {
			t.Error("ledger is out of order; an audit trail must be sequential")
		}
	}
}

func TestFindWalksUpFromSubdirectory(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(dir, "test"); err != nil {
		t.Fatal(err)
	}
	sub := dir + "/a/b/c"
	if err := mkdirAll(sub); err != nil {
		t.Fatal(err)
	}
	// An agent invoked deep in a repo must still find the workspace, like git does.
	if _, err := Find(sub); err != nil {
		t.Fatalf("could not find workspace from subdirectory: %v", err)
	}
}

func TestInitRefusesToClobber(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(dir, "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(dir, "second"); err == nil {
		t.Error("re-init overwrote an existing workspace")
	}
}

// TestGateBlocksAndUnblocksItsThread is a regression test for a bug found by
// clicking Approve in the web console: the gate→thread transition lived in the CLI,
// so approving from the browser settled the gate but left the thread blocked
// forever. The invariant now lives in the store, which is the only place every
// client passes through.
func TestGateBlocksAndUnblocksItsThread(t *testing.T) {
	f := newTestStore(t)
	ctx := context.Background()
	th, _ := f.CreateThread(ctx, "impl", "run migration", "")

	g, err := f.CreateGate(ctx, "impl", "needs DBA sign-off", th.ID)
	if err != nil {
		t.Fatal(err)
	}
	blocked, _ := f.Thread(ctx, th.ID)
	if blocked.Status != core.StatusBlocked {
		t.Fatalf("opening a gate left the thread %q, want blocked", blocked.Status)
	}

	// Approving must resume the thread. No caller should have to remember to do
	// this separately.
	if _, err := f.DecideGate(ctx, g.ID, core.GateApproved, "human", "ok"); err != nil {
		t.Fatal(err)
	}
	resumed, _ := f.Thread(ctx, th.ID)
	if resumed.Status != core.StatusClaimed {
		t.Errorf("after approval thread is %q, want claimed — it would hang forever", resumed.Status)
	}
	// The work goes back to whoever asked; the human only pressed a button.
	if resumed.Owner != "impl" {
		t.Errorf("after approval owner = %q, want impl (the asker, not the approver)", resumed.Owner)
	}
}

func TestRejectedGateReopensThread(t *testing.T) {
	f := newTestStore(t)
	ctx := context.Background()
	th, _ := f.CreateThread(ctx, "impl", "risky change", "")
	g, _ := f.CreateGate(ctx, "impl", "needs approval", th.ID)

	if _, err := f.DecideGate(ctx, g.ID, core.GateRejected, "human", "not now"); err != nil {
		t.Fatal(err)
	}
	got, _ := f.Thread(ctx, th.ID)
	// Rejection should free the thread for a different approach, not strand it.
	if got.Status != core.StatusOpen {
		t.Errorf("after rejection thread is %q, want open", got.Status)
	}
	if got.Owner != "" {
		t.Errorf("rejected thread still owned by %q", got.Owner)
	}
}

func TestGateWithoutThreadIsHarmless(t *testing.T) {
	f := newTestStore(t)
	ctx := context.Background()
	// A standalone gate must not fail just because it has no thread attached.
	g, err := f.CreateGate(ctx, "impl", "general question", "")
	if err != nil {
		t.Fatalf("gate with no thread failed: %v", err)
	}
	if _, err := f.DecideGate(ctx, g.ID, core.GateApproved, "human", ""); err != nil {
		t.Fatalf("deciding a threadless gate failed: %v", err)
	}
}

func TestGateOnMissingThreadStillOpens(t *testing.T) {
	f := newTestStore(t)
	ctx := context.Background()
	// A stale thread id should not block the gate from being raised; losing the
	// human's attention is worse than a dangling reference.
	if _, err := f.CreateGate(ctx, "impl", "reason", "t999"); err != nil {
		t.Errorf("gate referencing a missing thread failed: %v", err)
	}
}
