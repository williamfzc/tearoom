// dispatch_state.go owns the durable delivery ledger and singleton dispatcher lock.
package collab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func (d *Dispatcher) markSubmitting(current *Intent, delivery *Delivery) error {
	state, err := d.readState()
	if err != nil {
		return err
	}
	for i := range state.Intents {
		if state.Intents[i].Room != current.Room || state.Intents[i].Seq != current.Seq {
			continue
		}
		for j := range state.Intents[i].Deliveries {
			if state.Intents[i].Deliveries[j].Role == delivery.Role {
				state.Intents[i].Deliveries[j] = *delivery
				return d.writeState(state)
			}
		}
	}
	return errors.New("delivery intent disappeared while dispatch lock was held")
}

func (d *Dispatcher) markUnknown(delivery *Delivery, err error) error {
	delivery.State = DeliveryUnknown
	delivery.Error = err.Error()
	return nil // A possibly delivered prompt is never retried blindly.
}

func (d *Dispatcher) readState() (dispatchState, error) {
	data, err := os.ReadFile(filepath.Join(d.root, dispatchStateFile))
	if errors.Is(err, os.ErrNotExist) {
		return dispatchState{Version: 1, Cursor: map[string]uint64{}}, nil
	}
	if err != nil {
		return dispatchState{}, err
	}
	var state dispatchState
	if err := json.Unmarshal(data, &state); err != nil {
		return dispatchState{}, fmt.Errorf("read collaboration dispatch state: %w", err)
	}
	if state.Version != 1 {
		return dispatchState{}, fmt.Errorf("unsupported collaboration dispatch state version %d", state.Version)
	}
	return state, nil
}

// Status returns the durable delivery ledger for CLI inspection.
func (d *Dispatcher) Status() ([]Intent, error) {
	state, err := d.readState()
	if err != nil {
		return nil, err
	}
	if state.Intents == nil {
		return []Intent{}, nil
	}
	return state.Intents, nil
}

// Resolve moves an ambiguous delivery only after an explicit operator choice.
// retry may duplicate an action that hc accepted; callers must confirm it.
func (d *Dispatcher) Resolve(room string, seq uint64, role, outcome string, confirm bool) error {
	unlock, err := d.lock()
	if err != nil {
		return err
	}
	defer unlock()
	state, err := d.readState()
	if err != nil {
		return err
	}
	for i := range state.Intents {
		intent := &state.Intents[i]
		if intent.Room != room || intent.Seq != seq {
			continue
		}
		for j := range intent.Deliveries {
			delivery := &intent.Deliveries[j]
			if delivery.Role != role {
				continue
			}
			if delivery.State != DeliveryUnknown {
				return fmt.Errorf("delivery is %s; only unknown deliveries can be resolved", delivery.State)
			}
			switch outcome {
			case "delivered":
				delivery.State = DeliverySubmitted
			case "skip":
				delivery.State = DeliverySkipped
			case "retry":
				if !confirm {
					return errors.New("retry may duplicate a delivered action; pass --confirm after inspecting the panel and Room")
				}
				panel, err := PanelName(d.root, room, role)
				if err != nil {
					return err
				}
				delivery.Panel = panel
				delivery.State = DeliveryPending
			default:
				return fmt.Errorf("unknown resolution %q (expected delivered, skip, or retry)", outcome)
			}
			delivery.Error = ""
			return d.writeState(state)
		}
		return fmt.Errorf("role %q has no delivery for %s#%d", role, room, seq)
	}
	return fmt.Errorf("no delivery for %s#%d", room, seq)
}

// Run polls for new Room records and retries only work known not to have been
// submitted. Unknown outcomes stay visible in Status and are never replayed.
func (d *Dispatcher) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := d.Pump(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (d *Dispatcher) writeState(state dispatchState) error {
	state.Version = 1
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := filepath.Join(d.root, dispatchStateFile)
	tmp, err := os.CreateTemp(d.root, ".dispatch-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	dir, err := os.Open(d.root)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func (d *Dispatcher) lock() (func(), error) {
	path := filepath.Join(d.root, ".dispatch.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrDispatcherRunning
		}
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
