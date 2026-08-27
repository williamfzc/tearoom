//go:build js && wasm

package main

import (
	"testing"
	"time"

	"github.com/williamfzc/tearoom/internal/core"
	"github.com/williamfzc/tearoom/internal/view"
)

// The console polls every few seconds. Rebuilding the DOM on every poll destroyed
// text selection, invalidated element handles and leaked a callback per button, so
// the fingerprint must ignore time-only movement and notice real change.

func board(threads []*view.ThreadState, att []view.Attention, at time.Time) *view.Board {
	return &view.Board{
		Workspace: &core.Workspace{Name: "w"},
		At:        at,
		Threads:   threads,
		Attention: att,
	}
}

func thread(id string, status core.ThreadStatus, owner string) *view.ThreadState {
	return &view.ThreadState{Thread: &core.Thread{ID: id, Status: status, Owner: owner, Title: "t"}}
}

func TestFingerprintIgnoresClockMovement(t *testing.T) {
	t0 := time.Now()
	a := board([]*view.ThreadState{thread("t1", core.StatusOpen, "")}, nil, t0)
	b := board([]*view.ThreadState{thread("t1", core.StatusOpen, "")}, nil, t0.Add(time.Hour))
	if fingerprint(a) != fingerprint(b) {
		t.Error("clock movement alone triggered a repaint; the DOM would churn under the reader")
	}
}

func TestFingerprintNoticesStatusChange(t *testing.T) {
	t0 := time.Now()
	a := board([]*view.ThreadState{thread("t1", core.StatusBlocked, "impl")}, nil, t0)
	b := board([]*view.ThreadState{thread("t1", core.StatusClaimed, "impl")}, nil, t0)
	if fingerprint(a) == fingerprint(b) {
		t.Error("a status change did not repaint; the console would show stale state")
	}
}

func TestFingerprintNoticesNewGate(t *testing.T) {
	t0 := time.Now()
	a := board(nil, nil, t0)
	b := board(nil, []view.Attention{{Kind: view.NeedsApproval, Label: "sign off", Gate: "g1"}}, t0)
	if fingerprint(a) == fingerprint(b) {
		t.Error("a new gate did not repaint; a human would never see the request")
	}
}

func TestFingerprintNoticesOwnerHandoff(t *testing.T) {
	t0 := time.Now()
	a := board([]*view.ThreadState{thread("t1", core.StatusClaimed, "impl")}, nil, t0)
	b := board([]*view.ThreadState{thread("t1", core.StatusClaimed, "verify")}, nil, t0)
	if fingerprint(a) == fingerprint(b) {
		t.Error("a handoff between agents did not repaint")
	}
}

func TestFingerprintNoticesResolvedGate(t *testing.T) {
	t0 := time.Now()
	withGate := board(nil, []view.Attention{{Kind: view.NeedsApproval, Label: "x", Gate: "g1"}}, t0)
	cleared := board(nil, nil, t0)
	if fingerprint(withGate) == fingerprint(cleared) {
		t.Error("clearing the last gate did not repaint; the alert would stay on screen forever")
	}
}
