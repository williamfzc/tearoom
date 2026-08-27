// Package view derives everything a human or agent needs to see from raw core
// state. It is pure: no I/O, no time.Now, no formatting decisions that depend on
// the output medium.
//
// This is the point of building the whole thing in Go. "Who is stuck", "who
// holds what", "is this thread waiting on a human" are judgements, and if the
// CLI and the web console each implement them separately they will drift and
// disagree. Here they are computed once and rendered twice.
package view

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/williamfzc/tearoom/internal/core"
)

// AgentState is a live agent plus the derived facts an observer actually wants.
type AgentState struct {
	Agent      *core.Agent   `json:"agent"`
	Stale      bool          `json:"stale"`
	Idle       time.Duration `json:"idle"`
	Holding    []*core.Lease `json:"holding"`
	OwnThreads []string      `json:"own_threads"`
}

// ThreadState annotates a thread with why it is or is not moving.
type ThreadState struct {
	Thread         *core.Thread  `json:"thread"`
	Leases         []*core.Lease `json:"leases"`
	Gate           *core.Gate    `json:"gate,omitempty"`
	Blocked        bool          `json:"blocked"`
	WaitingOnHuman bool          `json:"waiting_on_human"`
	// Stalled means claimed by an agent that has gone quiet. This is the single
	// most useful signal in the whole product: nobody is going to finish it, and
	// nothing will say so unless we compute it.
	Stalled   bool      `json:"stalled"`
	StalledBy string    `json:"stalled_by,omitempty"`
	LastAt    time.Time `json:"last_at"`
}

// Board is the whole workspace, resolved. Both `tea status` and the web console
// render exactly this.
type Board struct {
	Workspace *core.Workspace `json:"workspace"`
	At        time.Time       `json:"at"`
	Agents    []*AgentState   `json:"agents"`
	Threads   []*ThreadState  `json:"threads"`
	// Attention is the ordered list of things a human should look at, most urgent
	// first. Empty means genuinely nothing needs a human.
	Attention []Attention `json:"attention"`
	Deadlocks [][]string  `json:"deadlocks,omitempty"`
}

// AttentionKind is ordered by urgency; lower sorts first.
type AttentionKind int

const (
	NeedsApproval AttentionKind = iota // a human is the only thing that can unblock this
	Deadlock                           // agents waiting on each other, needs a tiebreaker
	Stalled                            // claimed by a dead agent, will never finish
	StaleLease                         // lease outlived its holder
	Blocked                            // blocked for a non-human reason
)

// MarshalJSON emits the name rather than the ordinal. The integer exists only to
// express urgency ordering; leaking it to clients would make the API depend on
// constant order, which is exactly the kind of thing that breaks on reorder.
func (k AttentionKind) MarshalJSON() ([]byte, error) {
	return []byte(`"` + k.String() + `"`), nil
}

// UnmarshalJSON accepts the name so a round-trip through the wire is lossless,
// which the browser console relies on.
func (k *AttentionKind) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	for _, c := range []AttentionKind{NeedsApproval, Deadlock, Stalled, StaleLease, Blocked} {
		if c.String() == s {
			*k = c
			return nil
		}
	}
	// Tolerate the numeric form so older payloads still parse.
	if n, err := strconv.Atoi(s); err == nil {
		*k = AttentionKind(n)
		return nil
	}
	return fmt.Errorf("unknown attention kind %q", s)
}

func (k AttentionKind) String() string {
	switch k {
	case NeedsApproval:
		return "needs-approval"
	case Deadlock:
		return "deadlock"
	case Stalled:
		return "stalled"
	case StaleLease:
		return "stale-lease"
	case Blocked:
		return "blocked"
	}
	return "unknown"
}

// Attention is one actionable item with enough context to act without a follow-up
// query. An alert that forces the reader to go look something up is a bad alert.
type Attention struct {
	Kind   AttentionKind `json:"kind"`
	Label  string        `json:"label"`
	Thread string        `json:"thread,omitempty"`
	Gate   string        `json:"gate,omitempty"`
	Agent  string        `json:"agent,omitempty"`
	Detail string        `json:"detail"`
}

// Input is the raw state Build works from. Passing it explicitly, rather than
// letting Build fetch, is what keeps this package pure and testable.
type Input struct {
	Workspace *core.Workspace
	Agents    []*core.Agent
	Threads   []*core.Thread
	Leases    []*core.Lease
	Gates     []*core.Gate
	Now       time.Time
}

// Build resolves raw state into a board. Pure function of its input.
func Build(in Input) *Board {
	now := in.Now
	b := &Board{Workspace: in.Workspace, At: now}

	agentByID := make(map[string]*core.Agent, len(in.Agents))
	for _, a := range in.Agents {
		agentByID[a.ID] = a
	}

	// Expired leases are not deleted eagerly on read, so filter them here. The
	// store reclaims them lazily on the next mutation.
	live := make([]*core.Lease, 0, len(in.Leases))
	for _, l := range in.Leases {
		if !l.Expired(now) {
			live = append(live, l)
		}
	}

	held := map[string][]*core.Lease{}
	byThread := map[string][]*core.Lease{}
	for _, l := range live {
		held[l.Holder] = append(held[l.Holder], l)
		if l.Thread != "" {
			byThread[l.Thread] = append(byThread[l.Thread], l)
		}
	}

	pendingGateFor := map[string]*core.Gate{}
	for _, g := range in.Gates {
		if g.State == core.GatePending && g.Thread != "" {
			pendingGateFor[g.Thread] = g
		}
	}

	owned := map[string][]string{}
	for _, t := range in.Threads {
		if t.Owner != "" && t.Status != core.StatusResolved {
			owned[t.Owner] = append(owned[t.Owner], t.ID)
		}
	}

	for _, a := range in.Agents {
		b.Agents = append(b.Agents, &AgentState{
			Agent:      a,
			Stale:      a.Stale(now),
			Idle:       now.Sub(a.LastSeen).Round(time.Second),
			Holding:    held[a.ID],
			OwnThreads: owned[a.ID],
		})
	}
	sort.Slice(b.Agents, func(i, j int) bool {
		// Stale agents first: they are the ones something is wrong with.
		if b.Agents[i].Stale != b.Agents[j].Stale {
			return b.Agents[i].Stale
		}
		return b.Agents[i].Agent.ID < b.Agents[j].Agent.ID
	})

	for _, t := range in.Threads {
		ts := &ThreadState{
			Thread:  t,
			Leases:  byThread[t.ID],
			Blocked: t.Status == core.StatusBlocked,
			LastAt:  t.Updated,
		}
		if g, ok := pendingGateFor[t.ID]; ok {
			ts.Gate = g
			ts.WaitingOnHuman = true
		}
		// A thread claimed by an agent that has gone quiet will never finish on
		// its own, and no single record says so — it only falls out of joining
		// thread ownership against agent liveness.
		if t.Status == core.StatusClaimed && t.Owner != "" {
			if a, ok := agentByID[t.Owner]; ok && a.Stale(now) {
				ts.Stalled = true
				ts.StalledBy = t.Owner
			}
		}
		b.Threads = append(b.Threads, ts)
	}
	sort.Slice(b.Threads, func(i, j int) bool {
		return b.Threads[i].LastAt.After(b.Threads[j].LastAt)
	})

	b.Deadlocks = findDeadlocks(in.Threads, live, now)
	b.Attention = buildAttention(b, agentByID, live, in.Gates, now)
	return b
}

// findDeadlocks looks for cycles in "thread A's owner is blocked on a resource
// held by thread B's owner". We report rather than resolve: auto-breaking a cycle
// means picking a victim, and picking wrong loses work.
func findDeadlocks(threads []*core.Thread, leases []*core.Lease, now time.Time) [][]string {
	holderOf := map[string]string{}
	for _, l := range leases {
		holderOf[l.Resource] = l.Holder
	}

	// waits maps agent -> agent it is waiting on, derived from blocked threads
	// that named the resource they are stuck on.
	waits := map[string]string{}
	for _, t := range threads {
		if t.Status != core.StatusBlocked || t.Owner == "" {
			continue
		}
		for _, e := range t.Events {
			res := parseWaitingOn(e.Body)
			if res == "" {
				continue
			}
			if h, ok := holderOf[res]; ok && h != t.Owner {
				waits[t.Owner] = h
			}
		}
	}

	var cycles [][]string
	seen := map[string]bool{}
	for start := range waits {
		if seen[start] {
			continue
		}
		path := []string{}
		pos := map[string]int{}
		cur := start
		for {
			if i, ok := pos[cur]; ok {
				cycle := append([]string{}, path[i:]...)
				if len(cycle) > 1 {
					cycles = append(cycles, cycle)
				}
				break
			}
			next, ok := waits[cur]
			if !ok {
				break
			}
			pos[cur] = len(path)
			path = append(path, cur)
			seen[cur] = true
			cur = next
		}
	}
	sort.Slice(cycles, func(i, j int) bool {
		return strings.Join(cycles[i], ",") < strings.Join(cycles[j], ",")
	})
	return cycles
}

// waitingOnMarker is the convention an agent uses to declare what it is stuck on.
// Structured enough to detect cycles, loose enough that an agent can write it in
// a normal sentence.
const waitingOnMarker = "waiting-on:"

func parseWaitingOn(body string) string {
	i := strings.Index(body, waitingOnMarker)
	if i < 0 {
		return ""
	}
	rest := strings.TrimSpace(body[i+len(waitingOnMarker):])
	if rest == "" {
		return ""
	}
	return strings.Fields(rest)[0]
}

func buildAttention(b *Board, agents map[string]*core.Agent, leases []*core.Lease, gates []*core.Gate, now time.Time) []Attention {
	var out []Attention

	for _, g := range gates {
		if g.State != core.GatePending {
			continue
		}
		out = append(out, Attention{
			Kind:   NeedsApproval,
			Label:  g.Reason,
			Thread: g.Thread,
			Gate:   g.ID,
			Agent:  g.AskedBy,
			Detail: "asked by " + g.AskedBy + " " + since(now.Sub(g.Asked)) + "; only a human can clear this",
		})
	}

	for _, c := range b.Deadlocks {
		out = append(out, Attention{
			Kind:   Deadlock,
			Label:  "circular wait: " + strings.Join(c, " → "),
			Detail: "agents are waiting on each other; pick one to release its lease",
		})
	}

	for _, ts := range b.Threads {
		if ts.Stalled {
			out = append(out, Attention{
				Kind:   Stalled,
				Label:  ts.Thread.Title,
				Thread: ts.Thread.ID,
				Agent:  ts.StalledBy,
				Detail: "claimed by " + ts.StalledBy + ", quiet for " + humanAge(now.Sub(agents[ts.StalledBy].LastSeen)) + "; nobody will finish this",
			})
		}
	}

	for _, l := range leases {
		if a, ok := agents[l.Holder]; ok && a.Stale(now) {
			out = append(out, Attention{
				Kind:   StaleLease,
				Label:  l.Resource,
				Agent:  l.Holder,
				Thread: l.Thread,
				Detail: "held by " + l.Holder + " which is stale; preemptable now",
			})
		}
	}

	for _, ts := range b.Threads {
		// Skip anything already reported above — a thread blocked on a gate is
		// one problem, not two.
		if ts.Blocked && !ts.WaitingOnHuman && !ts.Stalled {
			out = append(out, Attention{
				Kind:   Blocked,
				Label:  ts.Thread.Title,
				Thread: ts.Thread.ID,
				Detail: "blocked with no pending gate; check the last event for what it needs",
			})
		}
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out
}

// since renders an elapsed duration as a phrase that reads correctly on its own,
// so callers do not append "ago" and produce "just now ago".
func since(d time.Duration) string {
	if d < time.Minute {
		return "just now"
	}
	return humanAge(d) + " ago"
}

func humanAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return itoa(int(d.Minutes())) + "m"
	case d < 24*time.Hour:
		return itoa(int(d.Hours())) + "h"
	default:
		return itoa(int(d.Hours()/24)) + "d"
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// Compact reduces a thread to what an agent needs to resume work, instead of its
// entire history. This is the token-cost lever: naive re-reading grows linearly
// with conversation length, so the default path has to be the cheap one and full
// history has to be opt-in.
type Compact struct {
	Thread  *core.Thread  `json:"thread"`
	Summary string        `json:"summary"`
	Recent  []core.Event  `json:"recent"`
	Leases  []*core.Lease `json:"leases"`
	Gate    *core.Gate    `json:"gate,omitempty"`
	Omitted int           `json:"omitted"`
}

// CompactThread keeps the last `keep` events verbatim and folds the rest into a
// one-line summary.
func CompactThread(t *core.Thread, leases []*core.Lease, gate *core.Gate, keep int) *Compact {
	if keep <= 0 {
		keep = 5
	}
	c := &Compact{Thread: t, Leases: leases, Gate: gate}
	if len(t.Events) <= keep {
		c.Recent = t.Events
		return c
	}
	cut := len(t.Events) - keep
	older := t.Events[:cut]
	c.Recent = t.Events[cut:]
	c.Omitted = cut

	actors := map[string]int{}
	var lastStatus core.ThreadStatus
	evidence := 0
	for _, e := range older {
		actors[e.Agent]++
		if e.Status != "" {
			lastStatus = e.Status
		}
		evidence += len(e.Evidence)
	}
	names := make([]string, 0, len(actors))
	for a := range actors {
		names = append(names, a)
	}
	sort.Strings(names)

	var sb strings.Builder
	sb.WriteString(itoa(cut))
	sb.WriteString(" earlier events from ")
	sb.WriteString(strings.Join(names, ", "))
	if lastStatus != "" {
		sb.WriteString("; last status change → ")
		sb.WriteString(string(lastStatus))
	}
	if evidence > 0 {
		sb.WriteString("; ")
		sb.WriteString(itoa(evidence))
		sb.WriteString(" evidence attachment(s)")
	}
	c.Summary = sb.String()
	return c
}
