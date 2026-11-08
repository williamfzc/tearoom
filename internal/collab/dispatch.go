// dispatch.go routes addressed Room records to Room-scoped hc terminals.
// It lives outside the kernel because delivery and process state are runtime policy.
package collab

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/williamfzc/tearoom/internal/core"
)

const dispatchStateFile = "dispatch.json"

var ErrDispatcherRunning = errors.New("another Room dispatcher holds the workspace lock")

type DeliveryState string

const (
	DeliveryPending    DeliveryState = "pending"
	DeliverySubmitting DeliveryState = "submitting"
	DeliverySubmitted  DeliveryState = "submitted"
	DeliveryCompleted  DeliveryState = "completed"
	DeliverySkipped    DeliveryState = "skipped"
	DeliveryUnknown    DeliveryState = "unknown"
)

// Delivery is durable work for one target role. Unknown work is never retried
// automatically because hc may have accepted the input before a timeout.
type Delivery struct {
	Role  string        `json:"role"`
	Panel string        `json:"panel"`
	State DeliveryState `json:"state"`
	Error string        `json:"error,omitempty"`
}

// Intent identifies one routed message by its immutable Room record address.
type Intent struct {
	Room       string     `json:"room"`
	Seq        uint64     `json:"seq"`
	Text       string     `json:"text"`
	Deliveries []Delivery `json:"deliveries"`
}

type dispatchState struct {
	Version int               `json:"version"`
	Cursor  map[string]uint64 `json:"cursor"`
	Intents []Intent          `json:"intents"`
}

type PanelStatus string

const (
	PanelMissing PanelStatus = "missing"
	PanelIdle    PanelStatus = "idle"
	PanelDone    PanelStatus = "done"
	PanelWorking PanelStatus = "working"
	PanelBlocked PanelStatus = "blocked"
	PanelUnknown PanelStatus = "unknown"
)

type PanelRequest struct {
	Name         string
	Room         string
	Role         string
	AgentType    string
	Template     string
	Command      []string
	Directory    string
	Instructions string
	Task         string
}

type SubmitResult struct {
	Accepted bool
	Started  *bool
	Pending  bool
}

// HCRuntime is the narrow boundary required from herdr-core. EnsurePanel must
// find an existing stable panel or create one; it must never create duplicates
// after an ambiguous spawn. Submit must report uncertainty as an error.
type HCRuntime interface {
	PanelStatus(context.Context, string) (PanelStatus, error)
	EnsurePanel(context.Context, PanelRequest) (SubmitResult, error)
	Submit(context.Context, string, string, PanelStatus) (SubmitResult, error)
}

type Dispatcher struct {
	root  string // the .tearoom directory
	store core.Store
	hc    HCRuntime
}

func NewDispatcher(root string, st core.Store, hc HCRuntime) *Dispatcher {
	return &Dispatcher{root: root, store: st, hc: hc}
}

// PanelName scopes a stable hc terminal name to one canonical workspace root.
func PanelName(workspaceRoot, room, role string) (string, error) {
	canonical, err := canonicalWorkspaceRoot(workspaceRoot)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256([]byte(canonical + "\x00" + room + "\x00" + role))
	return "tr-" + hex.EncodeToString(h[:12]), nil
}

func canonicalWorkspaceRoot(workspaceRoot string) (string, error) {
	absolute, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root %s: %w", absolute, err)
	}
	return filepath.Clean(canonical), nil
}

// Pump takes the workspace singleton lock, records new addressed work durably,
// then makes one safe delivery attempt for each pending intent. Call it again
// after notifications or on startup to reconcile Room history.
func (d *Dispatcher) Pump(ctx context.Context) error {
	unlock, err := d.lock()
	if err != nil {
		return err
	}
	defer unlock()

	state, err := d.readState()
	if err != nil {
		return err
	}
	if state.Cursor == nil {
		state.Cursor = map[string]uint64{}
	}
	for i := range state.Intents {
		for j := range state.Intents[i].Deliveries {
			delivery := &state.Intents[i].Deliveries[j]
			if delivery.State == DeliverySubmitting {
				delivery.State = DeliveryUnknown
				delivery.Error = "dispatcher stopped while hc submission outcome was unknown"
			}
		}
	}
	if err := d.writeState(state); err != nil {
		return err
	}
	rooms, err := d.store.Rooms(ctx)
	if err != nil {
		return err
	}
	config, err := Load(d.root)
	if err != nil {
		return err
	}
	for _, room := range rooms {
		records, err := d.store.Read(ctx, room.Name, state.Cursor[room.Name])
		if err != nil {
			return err
		}
		for _, record := range records {
			d.markReply(&state, record)
			if err := d.recordIntent(&state, config, room.Name, record); err != nil {
				return err
			}
			state.Cursor[room.Name] = record.Seq
			if err := d.writeState(state); err != nil {
				return err
			}
		}
	}
	for i := range state.Intents {
		for j := range state.Intents[i].Deliveries {
			if !d.isHeadOfRoleQueue(state, i, j) {
				continue
			}
			if err := d.deliver(ctx, config, &state.Intents[i], &state.Intents[i].Deliveries[j]); err != nil {
				return err
			}
			if err := d.writeState(state); err != nil {
				return err
			}
		}
	}
	return nil
}
