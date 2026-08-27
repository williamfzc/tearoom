package view

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/williamfzc/tearoom/internal/core"
)

var t0 = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

func agent(id string, lastSeen time.Time) *core.Agent {
	return &core.Agent{ID: id, Role: "worker", Joined: t0, LastSeen: lastSeen}
}

// TestStalledThreadIsDetected covers the single most valuable derivation in the
// product: a thread claimed by an agent that has died. No individual record says
// so — it only appears by joining thread ownership against agent liveness, which
// is exactly why this logic must live in one shared place rather than being
// reimplemented per frontend.
func TestStalledThreadIsDetected(t *testing.T) {
	b := Build(Input{
		Workspace: &core.Workspace{Name: "w"},
		Agents:    []*core.Agent{agent("dead", t0.Add(-30*time.Minute))},
		Threads: []*core.Thread{{
			ID: "t1", Title: "refactor auth", Status: core.StatusClaimed,
			Owner: "dead", Updated: t0.Add(-25 * time.Minute),
		}},
		Now: t0,
	})

	if len(b.Threads) != 1 || !b.Threads[0].Stalled {
		t.Fatal("thread owned by a stale agent was not marked stalled")
	}
	if b.Threads[0].StalledBy != "dead" {
		t.Errorf("StalledBy = %q, want dead", b.Threads[0].StalledBy)
	}
	if !hasKind(b.Attention, Stalled) {
		t.Error("a stalled thread must reach the attention list; nobody will finish it")
	}
}

func TestLiveAgentThreadIsNotStalled(t *testing.T) {
	b := Build(Input{
		Workspace: &core.Workspace{Name: "w"},
		Agents:    []*core.Agent{agent("live", t0.Add(-1*time.Minute))},
		Threads: []*core.Thread{{
			ID: "t1", Status: core.StatusClaimed, Owner: "live", Updated: t0,
		}},
		Now: t0,
	})
	if b.Threads[0].Stalled {
		t.Error("a thread owned by a live agent must not be flagged")
	}
	if len(b.Attention) != 0 {
		t.Errorf("healthy workspace produced %d attention items", len(b.Attention))
	}
}

// TestAttentionOrdering pins the urgency ordering. A human scanning this list acts
// on the first row, so a gate they can clear must outrank a lease that will expire
// on its own.
func TestAttentionOrdering(t *testing.T) {
	b := Build(Input{
		Workspace: &core.Workspace{Name: "w"},
		Agents:    []*core.Agent{agent("dead", t0.Add(-30*time.Minute))},
		Threads: []*core.Thread{
			{ID: "t1", Status: core.StatusClaimed, Owner: "dead", Updated: t0},
			{ID: "t2", Status: core.StatusBlocked, Owner: "dead", Updated: t0},
		},
		Leases: []*core.Lease{{
			ID: "l1", Resource: "src/**", Holder: "dead",
			Acquired: t0.Add(-20 * time.Minute), Expires: t0.Add(time.Hour),
		}},
		Gates: []*core.Gate{{
			ID: "g1", Reason: "prod migration", Thread: "t2",
			State: core.GatePending, AskedBy: "dead", Asked: t0.Add(-5 * time.Minute),
		}},
		Now: t0,
	})

	if len(b.Attention) < 3 {
		t.Fatalf("expected several attention items, got %d", len(b.Attention))
	}
	if b.Attention[0].Kind != NeedsApproval {
		t.Errorf("first attention item = %v, want needs-approval", b.Attention[0].Kind)
	}
	for i := 1; i < len(b.Attention); i++ {
		if b.Attention[i].Kind < b.Attention[i-1].Kind {
			t.Fatal("attention list is not sorted by urgency")
		}
	}
}

func TestGateOnThreadMarksWaitingOnHuman(t *testing.T) {
	b := Build(Input{
		Workspace: &core.Workspace{Name: "w"},
		Agents:    []*core.Agent{agent("a", t0)},
		Threads:   []*core.Thread{{ID: "t1", Status: core.StatusBlocked, Owner: "a", Updated: t0}},
		Gates: []*core.Gate{{
			ID: "g1", Reason: "needs approval", Thread: "t1",
			State: core.GatePending, AskedBy: "a", Asked: t0,
		}},
		Now: t0,
	})
	ts := b.Threads[0]
	if !ts.WaitingOnHuman || ts.Gate == nil {
		t.Fatal("thread with a pending gate must be marked as waiting on a human")
	}
	// One problem should produce one row, not both a gate alert and a generic
	// "blocked" alert for the same thread.
	blocked := 0
	for _, at := range b.Attention {
		if at.Kind == Blocked {
			blocked++
		}
	}
	if blocked != 0 {
		t.Errorf("gate-blocked thread also raised %d generic blocked alerts", blocked)
	}
}

func TestDecidedGateClearsAttention(t *testing.T) {
	b := Build(Input{
		Workspace: &core.Workspace{Name: "w"},
		Agents:    []*core.Agent{agent("a", t0)},
		Threads:   []*core.Thread{{ID: "t1", Status: core.StatusClaimed, Owner: "a", Updated: t0}},
		Gates: []*core.Gate{{
			ID: "g1", Thread: "t1", State: core.GateApproved,
			AskedBy: "a", Asked: t0.Add(-time.Hour), DecidedBy: "human",
		}},
		Now: t0,
	})
	if hasKind(b.Attention, NeedsApproval) {
		t.Error("an already-decided gate must not keep asking for a human")
	}
}

func TestExpiredLeasesExcluded(t *testing.T) {
	b := Build(Input{
		Workspace: &core.Workspace{Name: "w"},
		Agents:    []*core.Agent{agent("a", t0)},
		Leases: []*core.Lease{
			{ID: "l1", Resource: "old/**", Holder: "a", Expires: t0.Add(-time.Minute)},
			{ID: "l2", Resource: "new/**", Holder: "a", Expires: t0.Add(time.Minute)},
		},
		Now: t0,
	})
	if n := len(b.Agents[0].Holding); n != 1 {
		t.Fatalf("agent holds %d leases, want 1 (expired must be filtered)", n)
	}
	if b.Agents[0].Holding[0].Resource != "new/**" {
		t.Errorf("wrong lease retained: %s", b.Agents[0].Holding[0].Resource)
	}
}

func TestStaleAgentsSortFirst(t *testing.T) {
	b := Build(Input{
		Workspace: &core.Workspace{Name: "w"},
		Agents: []*core.Agent{
			agent("alive", t0),
			agent("zdead", t0.Add(-time.Hour)),
		},
		Now: t0,
	})
	// Alphabetically "alive" precedes "zdead"; the stale one must still come first
	// because it is the one something is wrong with.
	if b.Agents[0].Agent.ID != "zdead" {
		t.Errorf("first agent = %q, want the stale one", b.Agents[0].Agent.ID)
	}
}

// TestDeadlockDetection verifies we report circular waits rather than resolving
// them. Auto-breaking a cycle means choosing a victim, and choosing wrong loses
// work, so a human gets the call.
func TestDeadlockDetection(t *testing.T) {
	b := Build(Input{
		Workspace: &core.Workspace{Name: "w"},
		Agents:    []*core.Agent{agent("a", t0), agent("b", t0)},
		Threads: []*core.Thread{
			{ID: "t1", Status: core.StatusBlocked, Owner: "a", Updated: t0,
				Events: []core.Event{{Seq: 1, Agent: "a", Body: "waiting-on: db/**"}}},
			{ID: "t2", Status: core.StatusBlocked, Owner: "b", Updated: t0,
				Events: []core.Event{{Seq: 1, Agent: "b", Body: "waiting-on: api/**"}}},
		},
		Leases: []*core.Lease{
			{ID: "l1", Resource: "api/**", Holder: "a", Expires: t0.Add(time.Hour)},
			{ID: "l2", Resource: "db/**", Holder: "b", Expires: t0.Add(time.Hour)},
		},
		Now: t0,
	})
	if len(b.Deadlocks) == 0 {
		t.Fatal("mutual wait was not detected as a deadlock")
	}
	if !hasKind(b.Attention, Deadlock) {
		t.Error("a deadlock must surface in the attention list")
	}
}

func TestNoFalseDeadlock(t *testing.T) {
	b := Build(Input{
		Workspace: &core.Workspace{Name: "w"},
		Agents:    []*core.Agent{agent("a", t0), agent("b", t0)},
		Threads: []*core.Thread{
			{ID: "t1", Status: core.StatusBlocked, Owner: "a", Updated: t0,
				Events: []core.Event{{Seq: 1, Agent: "a", Body: "waiting-on: db/**"}}},
		},
		Leases: []*core.Lease{
			{ID: "l1", Resource: "db/**", Holder: "b", Expires: t0.Add(time.Hour)},
		},
		Now: t0,
	})
	// One-way waiting is normal operation, not a deadlock.
	if len(b.Deadlocks) != 0 {
		t.Errorf("one-way wait reported as deadlock: %v", b.Deadlocks)
	}
}

// TestCompactThreadReducesPayload covers the token-cost lever: naive re-reading of
// a thread grows linearly with its length, so the default must be compacted and
// full history must be opt-in.
func TestCompactThreadReducesPayload(t *testing.T) {
	th := &core.Thread{ID: "t1", Title: "long one", Status: core.StatusClaimed}
	for i := 1; i <= 20; i++ {
		e := core.Event{Seq: i, Agent: "a", At: t0, Body: "step"}
		if i == 3 {
			e.Evidence = []string{"log.txt"}
			e.Status = core.StatusClaimed
		}
		th.Events = append(th.Events, e)
	}

	c := CompactThread(th, nil, nil, 5)
	if len(c.Recent) != 5 {
		t.Fatalf("kept %d recent events, want 5", len(c.Recent))
	}
	if c.Omitted != 15 {
		t.Errorf("omitted = %d, want 15", c.Omitted)
	}
	if c.Summary == "" {
		t.Error("omitted events must be summarised, not silently dropped")
	}
	// The last event must be present: an agent resuming work needs the most recent
	// state above all.
	if c.Recent[len(c.Recent)-1].Seq != 20 {
		t.Errorf("last kept event seq = %d, want 20", c.Recent[len(c.Recent)-1].Seq)
	}
}

func TestCompactShortThreadKeepsEverything(t *testing.T) {
	th := &core.Thread{ID: "t1", Events: []core.Event{
		{Seq: 1, Agent: "a", Body: "one"},
		{Seq: 2, Agent: "b", Body: "two"},
	}}
	c := CompactThread(th, nil, nil, 5)
	if len(c.Recent) != 2 || c.Omitted != 0 || c.Summary != "" {
		t.Error("a short thread must pass through unmodified")
	}
}

func TestBuildIsPure(t *testing.T) {
	in := Input{
		Workspace: &core.Workspace{Name: "w"},
		Agents:    []*core.Agent{agent("a", t0)},
		Threads:   []*core.Thread{{ID: "t1", Status: core.StatusOpen, Updated: t0}},
		Now:       t0,
	}
	// Same input twice must yield identical output, or the CLI and the console can
	// disagree while looking at the same state.
	a, b := Build(in), Build(in)
	if len(a.Attention) != len(b.Attention) || len(a.Threads) != len(b.Threads) {
		t.Error("Build is not deterministic")
	}
}

func hasKind(items []Attention, k AttentionKind) bool {
	for _, it := range items {
		if it.Kind == k {
			return true
		}
	}
	return false
}

// TestAttentionKindJSONRoundTrip guards the API surface. The ordinal exists only
// for sorting; if it leaked into JSON, clients would start depending on constant
// order and a harmless reorder would break them.
func TestAttentionKindJSONRoundTrip(t *testing.T) {
	for _, k := range []AttentionKind{NeedsApproval, Deadlock, Stalled, StaleLease, Blocked} {
		b, err := json.Marshal(k)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) == "0" || string(b)[0] != '"' {
			t.Errorf("kind %v serialised as %s, want a quoted name", k, b)
		}
		var back AttentionKind
		if err := json.Unmarshal(b, &back); err != nil {
			t.Fatalf("unmarshal %s: %v", b, err)
		}
		if back != k {
			t.Errorf("round trip changed %v into %v", k, back)
		}
	}
}

func TestBoardSurvivesJSONRoundTrip(t *testing.T) {
	in := Input{
		Workspace: &core.Workspace{Name: "w"},
		Agents:    []*core.Agent{agent("dead", t0.Add(-30*time.Minute))},
		Threads: []*core.Thread{{
			ID: "t1", Title: "work", Status: core.StatusClaimed,
			Owner: "dead", Updated: t0.Add(-20 * time.Minute),
		}},
		Now: t0,
	}
	b := Build(in)
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	// The console decodes into this same struct; if the round trip lost the derived
	// flags, the browser would silently show a healthy workspace.
	var back Board
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Threads) != 1 || !back.Threads[0].Stalled {
		t.Error("stalled flag did not survive the wire")
	}
	if len(back.Attention) != len(b.Attention) {
		t.Errorf("attention count changed: %d -> %d", len(b.Attention), len(back.Attention))
	}
	if len(back.Attention) > 0 && back.Attention[0].Kind != b.Attention[0].Kind {
		t.Error("attention kind did not survive the wire")
	}
}

func TestSincePhrasing(t *testing.T) {
	// "just now ago" is broken English; the helper must return a standalone phrase.
	if got := since(10 * time.Second); got != "just now" {
		t.Errorf("since(10s) = %q", got)
	}
	if got := since(5 * time.Minute); got != "5m ago" {
		t.Errorf("since(5m) = %q", got)
	}
}
