package collab

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/williamfzc/tearoom/internal/core"
	"github.com/williamfzc/tearoom/internal/store"
)

type fakeHC struct {
	status  PanelStatus
	starts  int
	sends   int
	result  SubmitResult
	request PanelRequest
	task    string
}

func (f *fakeHC) PanelStatus(context.Context, string) (PanelStatus, error) { return f.status, nil }
func (f *fakeHC) EnsurePanel(_ context.Context, request PanelRequest) (SubmitResult, error) {
	f.starts++
	f.request = request
	return f.result, nil
}
func (f *fakeHC) Submit(_ context.Context, _ string, task string, _ PanelStatus) (SubmitResult, error) {
	f.sends++
	f.task = task
	return f.result, nil
}

func testDispatcher(t *testing.T, hc HCRuntime) (*Dispatcher, core.Store, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, store.Dir)
	if _, err := st.CreateRoom(context.Background(), "project", ""); err != nil {
		t.Fatal(err)
	}
	if err := Update(root, func(c *Config) error {
		c.Roles["reviewer"] = Role{Description: "review", AgentType: "test", Template: "reviewer"}
		c.Rooms["project"] = Room{Roles: []string{"reviewer"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return NewDispatcher(root, st, hc), st, root
}

func appendMessage(t *testing.T, st core.Store, author string, message Message) uint64 {
	t.Helper()
	content, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	record, err := st.Append(context.Background(), "project", author, core.AppendRequest{Content: content})
	if err != nil {
		t.Fatal(err)
	}
	return record.Seq
}

func mustPanelName(t *testing.T, root, room, role string) string {
	t.Helper()
	name, err := PanelName(root, room, role)
	if err != nil {
		t.Fatal(err)
	}
	return name
}

func TestPumpRoutesMentionOnceAndWaitsForRoomReply(t *testing.T) {
	started := true
	hc := &fakeHC{status: PanelMissing, result: SubmitResult{Accepted: true, Started: &started}}
	d, st, root := testDispatcher(t, hc)
	seq := appendMessage(t, st, "builder", Message{Text: "review this", Mentions: []string{"reviewer"}})
	if err := d.Pump(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hc.starts != 1 || hc.sends != 0 {
		t.Fatalf("hc calls = starts:%d sends:%d", hc.starts, hc.sends)
	}
	if !strings.Contains(hc.request.Task, "You are the reviewer role only") ||
		!strings.Contains(hc.request.Task, "do not review your own work") ||
		strings.Contains(hc.request.Task, "review this") {
		t.Fatalf("handoff prompt must preserve role and read from the Room: %q", hc.request.Task)
	}
	if err := d.Pump(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hc.starts != 1 {
		t.Fatalf("duplicate start: %d", hc.starts)
	}
	appendMessage(t, st, "reviewer", Message{Text: "looks good", ReplyTo: &RecordRef{Room: "project", Seq: seq}})
	d = NewDispatcher(root, st, hc)
	if err := d.Pump(context.Background()); err != nil {
		t.Fatal(err)
	}
	intents, err := d.Status()
	if err != nil {
		t.Fatal(err)
	}
	if len(intents) != 1 || intents[0].Deliveries[0].State != DeliveryCompleted {
		t.Fatalf("delivery not completed by correlated Room reply: %+v", intents)
	}
}

func TestReadyPanelReceivesRoleInstructions(t *testing.T) {
	for _, prestarted := range []bool{true, false} {
		t.Run(map[bool]string{true: "prestarted", false: "delayed-ready"}[prestarted], func(t *testing.T) {
			started := true
			hc := &fakeHC{status: PanelIdle, result: SubmitResult{Accepted: true, Started: &started}}
			if !prestarted {
				hc.status = PanelMissing
				hc.result = SubmitResult{Pending: true}
			}
			d, st, root := testDispatcher(t, hc)
			if err := Update(root, func(c *Config) error {
				role := c.Roles["reviewer"]
				role.Template = ""
				role.Command = []string{"agent"}
				role.Instructions = "Independently check the work."
				c.Roles["reviewer"] = role
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			appendMessage(t, st, "builder", Message{Text: "review this", Mentions: []string{"reviewer"}})
			if err := d.Pump(context.Background()); err != nil {
				t.Fatal(err)
			}
			if !prestarted {
				if hc.starts != 1 || hc.sends != 0 {
					t.Fatalf("first calls = starts:%d sends:%d", hc.starts, hc.sends)
				}
				hc.status = PanelIdle
				hc.result = SubmitResult{Accepted: true, Started: &started}
				if err := d.Pump(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			if hc.sends != 1 || !strings.Contains(hc.task, "Independently check the work.") || !strings.Contains(hc.task, "You are the reviewer role only") {
				t.Fatalf("role instructions missing from ready panel task: sends=%d task=%q", hc.sends, hc.task)
			}
		})
	}
}

func TestFirstScanReplyCannotCompleteIntentBeforeSubmission(t *testing.T) {
	started := true
	hc := &fakeHC{status: PanelMissing, result: SubmitResult{Accepted: true, Started: &started}}
	d, st, _ := testDispatcher(t, hc)
	seq := appendMessage(t, st, "builder", Message{Text: "review this", Mentions: []string{"reviewer"}})
	appendMessage(t, st, "reviewer", Message{Text: "already reviewed", ReplyTo: &RecordRef{Room: "project", Seq: seq}})

	if err := d.Pump(context.Background()); err != nil {
		t.Fatal(err)
	}
	intents, err := d.Status()
	if err != nil {
		t.Fatal(err)
	}
	if len(intents) != 1 || intents[0].Deliveries[0].State != DeliverySubmitted {
		t.Fatalf("pre-submission reply changed delivery state: %+v", intents)
	}
	if hc.starts != 1 || hc.sends != 0 {
		t.Fatalf("pre-submission reply must not suppress delivery: starts=%d sends=%d", hc.starts, hc.sends)
	}
}

func TestUnknownAgentStatusStaysPending(t *testing.T) {
	hc := &fakeHC{status: PanelUnknown}
	d, st, _ := testDispatcher(t, hc)
	appendMessage(t, st, "builder", Message{Text: "review this", Mentions: []string{"reviewer"}})
	if err := d.Pump(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hc.starts != 0 || hc.sends != 0 {
		t.Fatal("unknown status must not start or receive input")
	}
	intents, _ := d.Status()
	if intents[0].Deliveries[0].State != DeliveryPending {
		t.Fatalf("state = %s", intents[0].Deliveries[0].State)
	}
}

func TestRelativeRoleDirectoryUsesWorkspaceProjectRoot(t *testing.T) {
	started := true
	hc := &fakeHC{status: PanelMissing, result: SubmitResult{Accepted: true, Started: &started}}
	d, st, root := testDispatcher(t, hc)
	if err := Update(root, func(config *Config) error {
		role := config.Roles["reviewer"]
		role.Directory = filepath.Join("agents", "reviewer")
		config.Roles["reviewer"] = role
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	appendMessage(t, st, "builder", Message{Text: "review this", Mentions: []string{"reviewer"}})
	if err := d.Pump(context.Background()); err != nil {
		t.Fatal(err)
	}
	workspace, err := canonicalWorkspaceRoot(filepath.Dir(root))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(workspace, "agents", "reviewer"); hc.request.Directory != want {
		t.Fatalf("role directory = %q, want %q", hc.request.Directory, want)
	}
}

func TestRoleQueueWaitsForRoomReceipt(t *testing.T) {
	started := true
	hc := &fakeHC{status: PanelMissing, result: SubmitResult{Accepted: true, Started: &started}}
	d, st, _ := testDispatcher(t, hc)
	first := appendMessage(t, st, "builder", Message{Text: "first", Mentions: []string{"reviewer"}})
	if err := d.Pump(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hc.starts != 1 {
		t.Fatal("initial panel was not started")
	}
	appendMessage(t, st, "builder", Message{Text: "second", Mentions: []string{"reviewer"}})
	hc.status = PanelWorking
	if err := d.Pump(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hc.sends != 0 {
		t.Fatal("busy role received queued work")
	}
	appendMessage(t, st, "reviewer", Message{Text: "first done", ReplyTo: &RecordRef{Room: "project", Seq: first}})
	hc.status = PanelIdle
	if err := d.Pump(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hc.sends != 1 {
		t.Fatalf("submissions = %d, want one after first receipt", hc.sends)
	}
	intents, err := d.Status()
	if err != nil {
		t.Fatal(err)
	}
	if intents[0].Deliveries[0].State != DeliveryCompleted || intents[1].Deliveries[0].State != DeliverySubmitted {
		t.Fatalf("queue states = %s, %s", intents[0].Deliveries[0].State, intents[1].Deliveries[0].State)
	}
}

func TestSubmittingAfterCrashBecomesUnknownAndIsNotRetried(t *testing.T) {
	started := true
	hc := &fakeHC{status: PanelMissing, result: SubmitResult{Accepted: true, Started: &started}}
	d, st, root := testDispatcher(t, hc)
	seq := appendMessage(t, st, "builder", Message{Text: "review this", Mentions: []string{"reviewer"}})
	state := dispatchState{Version: 1, Cursor: map[string]uint64{"project": seq}, Intents: []Intent{{
		Room: "project", Seq: seq, Text: "review this", Deliveries: []Delivery{{
			Role: "reviewer", Panel: mustPanelName(t, root, "project", "reviewer"), State: DeliverySubmitting,
		}},
	}}}
	if err := d.writeState(state); err != nil {
		t.Fatal(err)
	}
	if err := d.Pump(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hc.starts != 0 {
		t.Fatal("uncertain prior submission was retried")
	}
	intents, err := d.Status()
	if err != nil {
		t.Fatal(err)
	}
	if intents[0].Deliveries[0].State != DeliveryUnknown {
		t.Fatalf("state = %s", intents[0].Deliveries[0].State)
	}
	_ = os.Remove(filepath.Join(root, dispatchStateFile))
}

func TestWorkspaceLockRejectsSecondDispatcher(t *testing.T) {
	d, _, _ := testDispatcher(t, &fakeHC{})
	unlock, err := d.lock()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if _, err := d.lock(); !errors.Is(err, ErrDispatcherRunning) {
		t.Fatalf("second lock error = %v", err)
	}
}

func TestPanelNameScopesRoomAndRoleToCanonicalWorkspace(t *testing.T) {
	workspaceA := t.TempDir()
	workspaceB := t.TempDir()
	nameA := mustPanelName(t, workspaceA, "project", "builder")
	nameB := mustPanelName(t, workspaceB, "project", "builder")
	if nameA == nameB {
		t.Fatalf("different workspaces share panel name %q", nameA)
	}
	alias := filepath.Join(t.TempDir(), "workspace-link")
	if err := os.Symlink(workspaceA, alias); err != nil {
		t.Skipf("cannot create symlink in test environment: %v", err)
	}
	if got := mustPanelName(t, alias, "project", "builder"); got != nameA {
		t.Fatalf("symlink alias panel = %q, want canonical panel %q", got, nameA)
	}
}

func TestPanelNameFailsClosedWhenWorkspaceCannotBeResolved(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := PanelName(missing, "project", "builder"); err == nil {
		t.Fatal("panel name accepted a workspace root that does not exist")
	}
}

func TestLegacyUnscopedPanelRequiresExplicitResolve(t *testing.T) {
	started := true
	hc := &fakeHC{status: PanelMissing, result: SubmitResult{Accepted: true, Started: &started}}
	d, st, root := testDispatcher(t, hc)
	seq := appendMessage(t, st, "builder", Message{Text: "review this", Mentions: []string{"reviewer"}})
	state := dispatchState{Version: 1, Cursor: map[string]uint64{"project": seq}, Intents: []Intent{{
		Room: "project", Seq: seq, Text: "review this", Deliveries: []Delivery{{
			Role: "reviewer", Panel: "tr-legacy-unscoped", State: DeliveryPending,
		}},
	}}}
	if err := d.writeState(state); err != nil {
		t.Fatal(err)
	}
	if err := d.Pump(context.Background()); err != nil {
		t.Fatal(err)
	}
	intents, err := d.Status()
	if err != nil {
		t.Fatal(err)
	}
	if intents[0].Deliveries[0].State != DeliveryUnknown || !strings.Contains(intents[0].Deliveries[0].Error, "workspace-scoped") {
		t.Fatalf("legacy delivery was not held for inspection: %+v", intents[0].Deliveries[0])
	}
	if hc.starts != 0 || hc.sends != 0 {
		t.Fatalf("legacy panel was reused automatically: starts=%d sends=%d", hc.starts, hc.sends)
	}
	if err := d.Resolve("project", seq, "reviewer", "retry", true); err != nil {
		t.Fatal(err)
	}
	intents, err = d.Status()
	if err != nil {
		t.Fatal(err)
	}
	if intents[0].Deliveries[0].Panel != mustPanelName(t, root, "project", "reviewer") || intents[0].Deliveries[0].State != DeliveryPending {
		t.Fatalf("explicit retry did not move to scoped panel: %+v", intents[0].Deliveries[0])
	}
}

func TestReplyDoesNotResolveUnknownDeliveryAfterCrash(t *testing.T) {
	started := true
	hc := &fakeHC{status: PanelMissing, result: SubmitResult{Accepted: true, Started: &started}}
	d, st, root := testDispatcher(t, hc)
	seq := appendMessage(t, st, "builder", Message{Text: "review this", Mentions: []string{"reviewer"}})
	state := dispatchState{Version: 1, Cursor: map[string]uint64{"project": seq}, Intents: []Intent{{
		Room: "project", Seq: seq, Text: "review this", Deliveries: []Delivery{{
			Role: "reviewer", Panel: mustPanelName(t, root, "project", "reviewer"), State: DeliverySubmitting,
		}},
	}}}
	if err := d.writeState(state); err != nil {
		t.Fatal(err)
	}
	appendMessage(t, st, "reviewer", Message{Text: "done", ReplyTo: &RecordRef{Room: "project", Seq: seq}})
	if err := d.Pump(context.Background()); err != nil {
		t.Fatal(err)
	}
	intents, err := d.Status()
	if err != nil {
		t.Fatal(err)
	}
	if intents[0].Deliveries[0].State != DeliveryUnknown {
		t.Fatalf("state = %s", intents[0].Deliveries[0].State)
	}
	if hc.starts != 0 {
		t.Fatal("ambiguous submission must not be retried automatically")
	}
}

func TestResolveRequiresConfirmationAndSerializesRetry(t *testing.T) {
	d, _, root := testDispatcher(t, &fakeHC{})
	state := dispatchState{Version: 1, Cursor: map[string]uint64{}, Intents: []Intent{{
		Room: "project", Seq: 7, Deliveries: []Delivery{{Role: "reviewer", Panel: "review", State: DeliveryUnknown, Error: "timeout"}},
	}}}
	if err := d.writeState(state); err != nil {
		t.Fatal(err)
	}
	if err := d.Resolve("project", 7, "reviewer", "retry", false); err == nil {
		t.Fatal("retry must require explicit confirmation")
	}
	if err := d.Resolve("project", 7, "reviewer", "retry", true); err != nil {
		t.Fatal(err)
	}
	intents, err := d.Status()
	if err != nil {
		t.Fatal(err)
	}
	if intents[0].Deliveries[0].State != DeliveryPending {
		t.Fatalf("state = %s", intents[0].Deliveries[0].State)
	}
	_ = os.Remove(filepath.Join(root, dispatchStateFile))
}
