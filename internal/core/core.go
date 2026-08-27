// Package core defines Tearoom's five concepts and the Store contract.
//
// Deliberately small: Workspace, Agent, Thread, Lease, Gate. A change that needs
// a sixth concept should be rejected unless it can show the existing five cannot
// express it.
package core

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ThreadStatus is a fixed enum. Projects do not get custom states — the point of
// a fixed machine is that a human reading the console never has to learn
// per-project vocabulary.
type ThreadStatus string

const (
	StatusOpen     ThreadStatus = "open"
	StatusClaimed  ThreadStatus = "claimed"
	StatusBlocked  ThreadStatus = "blocked"
	StatusResolved ThreadStatus = "resolved"
)

func (s ThreadStatus) Valid() bool {
	switch s {
	case StatusOpen, StatusClaimed, StatusBlocked, StatusResolved:
		return true
	}
	return false
}

// Sentinel errors. Callers branch on these; the CLI maps them to exit codes, so
// agents can react without parsing message text.
var (
	ErrNotFound     = errors.New("not found")
	ErrConflict     = errors.New("conflict")
	ErrNotHolder    = errors.New("not the lease holder")
	ErrNoWorkspace  = errors.New("no workspace: run 'tea init' first")
	ErrNeedEvidence = errors.New("resolving requires at least one event with evidence")
)

// ConflictError reports who holds a contested resource and when it frees up.
// A bare "conflict" is useless to an agent: it cannot decide between waiting and
// working on something else without knowing the expiry.
type ConflictError struct {
	Resource string
	Holder   string
	Expires  time.Time
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%q is held by %s until %s (%s remaining)",
		e.Resource, e.Holder, e.Expires.Format(time.RFC3339),
		e.Expires.Sub(time.Now()).Round(time.Second))
}

func (e *ConflictError) Is(target error) bool { return target == ErrConflict }

// Workspace is the isolation unit, one per project directory.
type Workspace struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Created time.Time `json:"created"`
}

// Agent identity is derived from the supplied name, never from the process. A
// crashed and restarted agent is the same agent, which is what makes lease
// reclamation and staleness meaningful.
type Agent struct {
	ID       string    `json:"id"`
	Role     string    `json:"role"`
	Joined   time.Time `json:"joined"`
	LastSeen time.Time `json:"last_seen"`
}

// StaleAfter is how long without a heartbeat before an agent is presumed dead
// and its leases become preemptable.
const StaleAfter = 10 * time.Minute

func (a *Agent) Stale(now time.Time) bool {
	return now.Sub(a.LastSeen) > StaleAfter
}

// Event is one immutable entry in a thread's log.
type Event struct {
	Seq      int          `json:"seq"`
	Agent    string       `json:"agent"`
	At       time.Time    `json:"at"`
	Body     string       `json:"body"`
	Status   ThreadStatus `json:"status,omitempty"` // set when this event changed status
	Evidence []string     `json:"evidence,omitempty"`
}

// Thread is one goal plus its append-only event log.
type Thread struct {
	ID      string       `json:"id"`
	Title   string       `json:"title"`
	Status  ThreadStatus `json:"status"`
	Owner   string       `json:"owner,omitempty"`
	Created time.Time    `json:"created"`
	Updated time.Time    `json:"updated"`
	Events  []Event      `json:"events"`
}

// HasEvidence reports whether any event carries evidence. Gating `resolved` on
// this is what stops an agent from declaring success with no proof — the most
// common way a multi-agent run silently goes wrong.
func (t *Thread) HasEvidence() bool {
	for _, e := range t.Events {
		if len(e.Evidence) > 0 {
			return true
		}
	}
	return false
}

// Lease is an advisory exclusive hold with a mandatory TTL.
//
// Advisory because enforcing it would need a kernel hook or FUSE mount — a much
// heavier install than one binary, and still bypassable. The git pre-commit
// guard is the second line of defence.
type Lease struct {
	ID       string    `json:"id"`
	Resource string    `json:"resource"`
	Holder   string    `json:"holder"`
	Thread   string    `json:"thread,omitempty"`
	Acquired time.Time `json:"acquired"`
	Expires  time.Time `json:"expires"`
}

func (l *Lease) Expired(now time.Time) bool { return now.After(l.Expires) }

// DefaultLeaseTTL is provisional. Too short interrupts long edits, too long
// slows crash recovery; needs measuring against real agent edit durations.
const DefaultLeaseTTL = 15 * time.Minute

// GateState tracks a human decision.
type GateState string

const (
	GatePending  GateState = "pending"
	GateApproved GateState = "approved"
	GateRejected GateState = "rejected"
)

// Gate is a checkpoint requiring human approval.
//
// There is no auto-approve-on-timeout, by design: auto-approval would remove the
// only reason a gate exists.
type Gate struct {
	ID        string    `json:"id"`
	Reason    string    `json:"reason"`
	Thread    string    `json:"thread,omitempty"`
	State     GateState `json:"state"`
	AskedBy   string    `json:"asked_by"`
	Asked     time.Time `json:"asked"`
	Decided   time.Time `json:"decided,omitempty"`
	DecidedBy string    `json:"decided_by,omitempty"`
	Note      string    `json:"note,omitempty"`
}

// LedgerEntry is one line of the append-only audit trail. There is no delete
// path; this is what makes a run reviewable after the fact.
type LedgerEntry struct {
	At     time.Time         `json:"at"`
	Agent  string            `json:"agent"`
	Action string            `json:"action"`
	Target string            `json:"target,omitempty"`
	Meta   map[string]string `json:"meta,omitempty"`
}

// Store is the single seam between callers and persistence, and the reason the
// same code runs in three places. The CLI talks to a file store directly, the
// server wraps that same file store, and the browser build talks to an HTTP
// client implementing this identical interface. Nothing above this line knows
// which one it holds.
//
// Every mutating method must be atomic against concurrent callers: two agents
// polling at the same instant must not both observe a resource as free.
//
// Methods take a context so the HTTP-backed implementation can honour
// cancellation and deadlines. The file-backed one ignores it, which is fine —
// the interface is written for the stricter caller.
type Store interface {
	Workspace(ctx context.Context) (*Workspace, error)

	UpsertAgent(ctx context.Context, id, role string) (*Agent, error)
	Agents(ctx context.Context) ([]*Agent, error)

	CreateThread(ctx context.Context, agent, title, body string) (*Thread, error)
	Thread(ctx context.Context, id string) (*Thread, error)
	Threads(ctx context.Context) ([]*Thread, error)
	AppendEvent(ctx context.Context, threadID, agent, body string, status ThreadStatus, evidence []string) (*Thread, error)

	// Claim returns *ConflictError when an overlapping live lease exists.
	Claim(ctx context.Context, agent, resource, threadID string, ttl time.Duration) (*Lease, error)
	Release(ctx context.Context, agent, resource string) error
	Leases(ctx context.Context) ([]*Lease, error)

	CreateGate(ctx context.Context, agent, reason, threadID string) (*Gate, error)
	DecideGate(ctx context.Context, id string, state GateState, by, note string) (*Gate, error)
	Gate(ctx context.Context, id string) (*Gate, error)
	Gates(ctx context.Context) ([]*Gate, error)

	Append(ctx context.Context, entry LedgerEntry) error
	Ledger(ctx context.Context, tail int) ([]LedgerEntry, error)
}
