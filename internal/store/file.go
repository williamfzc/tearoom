// Package store implements core.Store over the local filesystem.
//
// The correctness requirement that shapes this whole file: two agents in separate
// processes calling Claim at the same instant must not both succeed. Without
// cross-process locking both read "free", both write "held", and the product has
// failed at the one job it exists to do. So every mutation runs inside an
// exclusive flock, and reads of contended state happen inside that same lock.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/williamfzc/tearoom/internal/core"
)

// Dir is the workspace metadata directory name.
const Dir = ".tearoom"

type File struct {
	root string // absolute path to the .tearoom directory
	now  func() time.Time
}

// Find walks up from start looking for a workspace, the way git finds .git. An
// agent invoked from a subdirectory should still find the workspace.
func Find(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, Dir)
		if fi, err := os.Stat(candidate); err == nil && fi.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", core.ErrNoWorkspace
		}
		dir = parent
	}
}

// Open attaches to an existing workspace found from start.
func Open(start string) (*File, error) {
	root, err := Find(start)
	if err != nil {
		return nil, err
	}
	return &File{root: root, now: time.Now}, nil
}

// OpenAt attaches to an explicit .tearoom directory, for the server which is
// given a path rather than discovering one.
func OpenAt(root string) (*File, error) {
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		return nil, core.ErrNoWorkspace
	}
	return &File{root: root, now: time.Now}, nil
}

// Init creates a workspace. Refuses to clobber an existing one: silently
// resetting somebody's coordination state would be unforgivable.
func Init(dir, name string) (*File, error) {
	root := filepath.Join(dir, Dir)
	if _, err := os.Stat(root); err == nil {
		return nil, fmt.Errorf("workspace already exists at %s", root)
	}
	for _, sub := range []string{"agents", "threads", "leases", "gates"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			return nil, err
		}
	}
	f := &File{root: root, now: time.Now}
	ws := &core.Workspace{
		ID:      newID("ws"),
		Name:    name,
		Created: f.now().UTC(),
	}
	if err := writeJSON(filepath.Join(root, "config.json"), ws); err != nil {
		return nil, err
	}
	return f, nil
}

func (f *File) Root() string { return f.root }

// SetClock is for tests that need deterministic staleness and expiry.
func (f *File) SetClock(fn func() time.Time) { f.now = fn }

func (f *File) path(parts ...string) string {
	return filepath.Join(append([]string{f.root}, parts...)...)
}

func (f *File) Workspace(context.Context) (*core.Workspace, error) {
	var ws core.Workspace
	if err := readJSON(f.path("config.json"), &ws); err != nil {
		return nil, core.ErrNoWorkspace
	}
	return &ws, nil
}

// --- agents ---

func (f *File) UpsertAgent(ctx context.Context, id, role string) (*core.Agent, error) {
	unlock, err := f.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()

	now := f.now().UTC()
	p := f.path("agents", safe(id)+".json")
	var a core.Agent
	if err := readJSON(p, &a); err != nil {
		a = core.Agent{ID: id, Role: role, Joined: now}
	} else if role != "" {
		a.Role = role
	}
	a.LastSeen = now
	if err := writeJSON(p, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

func (f *File) Agents(context.Context) ([]*core.Agent, error) {
	var out []*core.Agent
	err := eachJSON(f.path("agents"), func(b []byte) error {
		var a core.Agent
		if err := json.Unmarshal(b, &a); err != nil {
			return err
		}
		out = append(out, &a)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, err
}

// --- threads ---

func (f *File) CreateThread(ctx context.Context, agent, title, body string) (*core.Thread, error) {
	if strings.TrimSpace(title) == "" {
		return nil, errors.New("thread title is required")
	}
	unlock, err := f.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()

	now := f.now().UTC()
	id, err := f.nextThreadID()
	if err != nil {
		return nil, err
	}
	t := &core.Thread{
		ID:      id,
		Title:   title,
		Status:  core.StatusOpen,
		Created: now,
		Updated: now,
	}
	if body != "" {
		t.Events = append(t.Events, core.Event{Seq: 1, Agent: agent, At: now, Body: body})
	}
	if err := writeJSON(f.path("threads", id+".json"), t); err != nil {
		return nil, err
	}
	f.appendLocked(core.LedgerEntry{At: now, Agent: agent, Action: "thread.create", Target: id,
		Meta: map[string]string{"title": title}})
	return t, nil
}

// nextThreadID allocates short sequential ids. Agents quote these in prose and
// humans read them aloud, so t1 beats a UUID; uniqueness only has to hold inside
// one workspace.
func (f *File) nextThreadID() (string, error) {
	entries, err := os.ReadDir(f.path("threads"))
	if err != nil {
		return "", err
	}
	max := 0
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".json")
		if !strings.HasPrefix(name, "t") {
			continue
		}
		if n, err := strconv.Atoi(name[1:]); err == nil && n > max {
			max = n
		}
	}
	return "t" + strconv.Itoa(max+1), nil
}

func (f *File) Thread(ctx context.Context, id string) (*core.Thread, error) {
	var t core.Thread
	if err := readJSON(f.path("threads", safe(id)+".json"), &t); err != nil {
		return nil, core.ErrNotFound
	}
	return &t, nil
}

func (f *File) Threads(context.Context) ([]*core.Thread, error) {
	var out []*core.Thread
	err := eachJSON(f.path("threads"), func(b []byte) error {
		var t core.Thread
		if err := json.Unmarshal(b, &t); err != nil {
			return err
		}
		out = append(out, &t)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out, err
}

func (f *File) AppendEvent(ctx context.Context, threadID, agent, body string, status core.ThreadStatus, evidence []string) (*core.Thread, error) {
	if status != "" && !status.Valid() {
		return nil, fmt.Errorf("invalid status %q", status)
	}
	unlock, err := f.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()

	p := f.path("threads", safe(threadID)+".json")
	var t core.Thread
	if err := readJSON(p, &t); err != nil {
		return nil, core.ErrNotFound
	}

	now := f.now().UTC()
	ev := core.Event{
		Seq:      len(t.Events) + 1,
		Agent:    agent,
		At:       now,
		Body:     body,
		Status:   status,
		Evidence: evidence,
	}

	// Refuse to resolve without evidence anywhere on the thread. This is the one
	// place the tool is opinionated about process, and it earns it: "done" with no
	// proof is the most common way a multi-agent run goes quietly wrong.
	if status == core.StatusResolved {
		probe := t
		probe.Events = append(probe.Events, ev)
		if !probe.HasEvidence() {
			return nil, core.ErrNeedEvidence
		}
	}

	t.Events = append(t.Events, ev)
	t.Updated = now
	if status != "" {
		t.Status = status
		switch status {
		case core.StatusClaimed:
			t.Owner = agent
		case core.StatusResolved:
			t.Owner = ""
		}
	}
	if err := writeJSON(p, &t); err != nil {
		return nil, err
	}
	meta := map[string]string{}
	if status != "" {
		meta["status"] = string(status)
	}
	f.appendLocked(core.LedgerEntry{At: now, Agent: agent, Action: "thread.reply", Target: t.ID, Meta: meta})
	return &t, nil
}

// --- leases ---

func (f *File) Claim(ctx context.Context, agent, resource, threadID string, ttl time.Duration) (*core.Lease, error) {
	if strings.TrimSpace(resource) == "" {
		return nil, errors.New("resource pattern is required")
	}
	if ttl <= 0 {
		ttl = core.DefaultLeaseTTL
	}
	unlock, err := f.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()

	now := f.now().UTC()
	existing, err := f.leasesLocked()
	if err != nil {
		return nil, err
	}

	agents := map[string]*core.Agent{}
	if list, err := f.Agents(ctx); err == nil {
		for _, a := range list {
			agents[a.ID] = a
		}
	}

	for _, l := range existing {
		if l.Expired(now) {
			os.Remove(f.path("leases", l.ID+".json"))
			continue
		}
		if l.Holder == agent && l.Resource == resource {
			// Re-claiming your own lease extends it rather than failing. An agent
			// that loops should not have to special-case its own hold.
			l.Expires = now.Add(ttl)
			if err := writeJSON(f.path("leases", l.ID+".json"), l); err != nil {
				return nil, err
			}
			return l, nil
		}
		if !overlaps(l.Resource, resource) {
			continue
		}
		// A lease whose holder is dead is preemptable. Progress beats deadlock,
		// but the takeover goes in the ledger so it is reviewable.
		if a, ok := agents[l.Holder]; ok && a.Stale(now) {
			os.Remove(f.path("leases", l.ID+".json"))
			f.appendLocked(core.LedgerEntry{At: now, Agent: agent, Action: "lease.preempt",
				Target: l.Resource, Meta: map[string]string{"from": l.Holder, "reason": "holder stale"}})
			continue
		}
		return nil, &core.ConflictError{Resource: l.Resource, Holder: l.Holder, Expires: l.Expires}
	}

	lease := &core.Lease{
		ID:       newID("l"),
		Resource: resource,
		Holder:   agent,
		Thread:   threadID,
		Acquired: now,
		Expires:  now.Add(ttl),
	}
	if err := writeJSON(f.path("leases", lease.ID+".json"), lease); err != nil {
		return nil, err
	}
	f.appendLocked(core.LedgerEntry{At: now, Agent: agent, Action: "lease.claim", Target: resource,
		Meta: map[string]string{"ttl": ttl.String(), "thread": threadID}})
	return lease, nil
}

func (f *File) Release(ctx context.Context, agent, resource string) error {
	unlock, err := f.lock()
	if err != nil {
		return err
	}
	defer unlock()

	now := f.now().UTC()
	list, err := f.leasesLocked()
	if err != nil {
		return err
	}
	for _, l := range list {
		if l.Resource != resource {
			continue
		}
		if l.Holder != agent {
			return fmt.Errorf("%w: %q is held by %s", core.ErrNotHolder, resource, l.Holder)
		}
		if err := os.Remove(f.path("leases", l.ID+".json")); err != nil {
			return err
		}
		f.appendLocked(core.LedgerEntry{At: now, Agent: agent, Action: "lease.release", Target: resource})
		return nil
	}
	return fmt.Errorf("%w: no lease on %q", core.ErrNotFound, resource)
}

func (f *File) Leases(context.Context) ([]*core.Lease, error) { return f.leasesLocked() }

func (f *File) leasesLocked() ([]*core.Lease, error) {
	var out []*core.Lease
	err := eachJSON(f.path("leases"), func(b []byte) error {
		var l core.Lease
		if err := json.Unmarshal(b, &l); err != nil {
			return err
		}
		out = append(out, &l)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Acquired.Before(out[j].Acquired) })
	return out, err
}

// overlaps decides whether two resource patterns contend.
//
// Exact-string comparison would miss the common case entirely: one agent claims
// "src/**" while another claims "src/auth/a.ts", and both would "succeed". So a
// glob is expanded against the other side as a prefix match too.
func overlaps(a, b string) bool {
	if a == b {
		return true
	}
	return globHit(a, b) || globHit(b, a)
}

func globHit(pattern, other string) bool {
	if ok, _ := filepath.Match(pattern, other); ok {
		return true
	}
	// filepath.Match does not cross separators, so handle the ** prefix form that
	// agents actually write.
	if i := strings.Index(pattern, "**"); i >= 0 {
		prefix := pattern[:i]
		if strings.HasPrefix(other, prefix) {
			return true
		}
		// "src/**" should also contend with the bare directory "src".
		if trimmed := strings.TrimSuffix(prefix, "/"); trimmed != "" && other == trimmed {
			return true
		}
	}
	// A directory-ish claim contends with anything beneath it.
	if strings.HasSuffix(pattern, "/") && strings.HasPrefix(other, pattern) {
		return true
	}
	return false
}

// --- gates ---

// CreateGate opens a gate and blocks its thread in the same locked section.
//
// The thread transition lives here rather than in the caller because it is an
// invariant, not a convenience: a gate is by definition something the thread is
// waiting on. When this coupling lived in the CLI, approving from the web console
// left threads blocked forever — the same rule has to hold for every client, so it
// belongs below the Store interface.
func (f *File) CreateGate(ctx context.Context, agent, reason, threadID string) (*core.Gate, error) {
	if strings.TrimSpace(reason) == "" {
		return nil, errors.New("gate reason is required")
	}
	unlock, err := f.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()

	now := f.now().UTC()
	g := &core.Gate{
		ID:      newID("g"),
		Reason:  reason,
		Thread:  threadID,
		State:   core.GatePending,
		AskedBy: agent,
		Asked:   now,
	}
	if err := writeJSON(f.path("gates", g.ID+".json"), g); err != nil {
		return nil, err
	}
	if threadID != "" {
		if err := f.transitionLocked(threadID, agent,
			"waiting for human approval: "+reason, core.StatusBlocked); err != nil && !errors.Is(err, core.ErrNotFound) {
			return nil, err
		}
	}
	f.appendLocked(core.LedgerEntry{At: now, Agent: agent, Action: "gate.open", Target: g.ID,
		Meta: map[string]string{"reason": reason, "thread": threadID}})
	return g, nil
}

// DecideGate settles a gate and moves its thread out of blocked.
//
// Approval resumes the thread as claimed; rejection returns it to open so somebody
// can pick a different approach. Doing this here means the console's Approve button
// and the CLI produce identical state.
func (f *File) DecideGate(ctx context.Context, id string, state core.GateState, by, note string) (*core.Gate, error) {
	if state != core.GateApproved && state != core.GateRejected {
		return nil, fmt.Errorf("invalid decision %q", state)
	}
	unlock, err := f.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()

	p := f.path("gates", safe(id)+".json")
	var g core.Gate
	if err := readJSON(p, &g); err != nil {
		return nil, core.ErrNotFound
	}
	// Deciding twice would let a rejected action be quietly re-approved.
	if g.State != core.GatePending {
		return nil, fmt.Errorf("gate %s already %s", id, g.State)
	}
	now := f.now().UTC()
	g.State = state
	g.Decided = now
	g.DecidedBy = by
	g.Note = note
	if err := writeJSON(p, &g); err != nil {
		return nil, err
	}

	if g.Thread != "" {
		next := core.StatusClaimed
		if state == core.GateRejected {
			next = core.StatusOpen
		}
		// Resume as the agent that asked, not as the human who approved. The human
		// pressed a button; they are not the one doing the work, and recording them
		// as owner would make the board claim a person is implementing something.
		resumeAs := g.AskedBy
		if resumeAs == "" {
			resumeAs = by
		}
		if err := f.transitionLocked(g.Thread, resumeAs,
			"gate "+string(state)+" by "+by+": "+g.Reason, next); err != nil && !errors.Is(err, core.ErrNotFound) {
			return nil, err
		}
	}
	f.appendLocked(core.LedgerEntry{At: now, Agent: by, Action: "gate." + string(state), Target: g.ID,
		Meta: map[string]string{"note": note}})
	return &g, nil
}

// transitionLocked appends a status-changing event. Caller must hold the lock; it
// exists so gate operations can move a thread inside their own critical section
// rather than issuing a second, racy call.
func (f *File) transitionLocked(threadID, agent, body string, status core.ThreadStatus) error {
	p := f.path("threads", safe(threadID)+".json")
	var t core.Thread
	if err := readJSON(p, &t); err != nil {
		return core.ErrNotFound
	}
	now := f.now().UTC()
	t.Events = append(t.Events, core.Event{
		Seq: len(t.Events) + 1, Agent: agent, At: now, Body: body, Status: status,
	})
	t.Updated = now
	t.Status = status
	switch status {
	case core.StatusClaimed:
		// Resuming after approval hands the thread to whoever asked, not to the
		// human who approved it; the human is not doing the work.
		if t.Owner == "" {
			t.Owner = agent
		}
	case core.StatusOpen:
		t.Owner = ""
	}
	return writeJSON(p, &t)
}

func (f *File) Gate(ctx context.Context, id string) (*core.Gate, error) {
	var g core.Gate
	if err := readJSON(f.path("gates", safe(id)+".json"), &g); err != nil {
		return nil, core.ErrNotFound
	}
	return &g, nil
}

func (f *File) Gates(context.Context) ([]*core.Gate, error) {
	var out []*core.Gate
	err := eachJSON(f.path("gates"), func(b []byte) error {
		var g core.Gate
		if err := json.Unmarshal(b, &g); err != nil {
			return err
		}
		out = append(out, &g)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Asked.Before(out[j].Asked) })
	return out, err
}

// --- ledger ---

func (f *File) Append(ctx context.Context, e core.LedgerEntry) error {
	unlock, err := f.lock()
	if err != nil {
		return err
	}
	defer unlock()
	return f.appendLocked(e)
}

// appendLocked assumes the caller holds the lock. Ledger writes ride along with
// the mutation they describe, so a mutation and its audit record cannot diverge.
func (f *File) appendLocked(e core.LedgerEntry) error {
	if e.At.IsZero() {
		e.At = f.now().UTC()
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	fh, err := os.OpenFile(f.path("ledger.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer fh.Close()
	_, err = fh.Write(append(b, '\n'))
	return err
}

func (f *File) Ledger(ctx context.Context, tail int) ([]core.LedgerEntry, error) {
	b, err := os.ReadFile(f.path("ledger.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	var out []core.LedgerEntry
	for _, ln := range lines {
		if ln == "" {
			continue
		}
		var e core.LedgerEntry
		if err := json.Unmarshal([]byte(ln), &e); err != nil {
			continue // a corrupt line must not hide the rest of the trail
		}
		out = append(out, e)
	}
	if tail > 0 && len(out) > tail {
		out = out[len(out)-tail:]
	}
	return out, nil
}

// --- helpers ---

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// writeJSON writes atomically via rename, so a crash mid-write cannot leave a
// truncated file that fails to parse forever after.
func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func eachJSON(dir string, fn func([]byte) error) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		if err := fn(b); err != nil {
			return err
		}
	}
	return nil
}

// safe stops an id from escaping the workspace directory via path traversal.
func safe(id string) string {
	id = strings.ReplaceAll(id, string(filepath.Separator), "_")
	id = strings.ReplaceAll(id, "..", "_")
	return id
}

var idCounter int

// newID is monotonic within a process and includes a nanosecond stamp, which is
// enough because ids are only unique per workspace.
func newID(prefix string) string {
	idCounter++
	return fmt.Sprintf("%s%d%02d", prefix, time.Now().UnixNano()%1e9, idCounter%100)
}

// mkdirAll is a small helper used by tests that need nested directories.
func mkdirAll(p string) error { return os.MkdirAll(p, 0o755) }
