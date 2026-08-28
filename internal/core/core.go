// Package core defines Tearoom's complete domain vocabulary and storage contract.
// A room contains durable actors and one append-only sequence of events.
package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var (
	ErrNotFound     = errors.New("not found")
	ErrConflict     = errors.New("conflict")
	ErrNoRoom       = errors.New("no room: run 'tearoom room init' first")
	ErrUnknownActor = errors.New("unknown actor")
	ErrStaleWrite   = errors.New("stale write")
)

// StaleWriteError rejects an event based on an older room view.
type StaleWriteError struct {
	Expected uint64 `json:"expected"`
	Actual   uint64 `json:"actual"`
}

func (e *StaleWriteError) Error() string {
	return fmt.Sprintf("room changed: read again (expected cursor %d, current cursor %d)", e.Expected, e.Actual)
}

func (e *StaleWriteError) Is(target error) bool {
	return target == ErrStaleWrite || target == ErrConflict
}

// Room is the durable boundary in which actors exchange events.
type Room struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Created time.Time `json:"created"`
}

// Actor is a durable participant identity, independent of a process or session.
type Actor struct {
	ID       string    `json:"id"`
	Role     string    `json:"role,omitempty"`
	Joined   time.Time `json:"joined"`
	LastSeen time.Time `json:"last_seen"`
}

// Event is one immutable fact in a room's ordered log. An empty To list means
// every actor may read it; otherwise only the sender and named recipients may.
type Event struct {
	ID       string          `json:"id"`
	Room     string          `json:"room"`
	Seq      uint64          `json:"seq"`
	Actor    string          `json:"actor"`
	At       time.Time       `json:"at"`
	Type     string          `json:"type"`
	Body     string          `json:"body,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
	To       []string        `json:"to,omitempty"`
	CausedBy string          `json:"caused_by,omitempty"`
	Key      string          `json:"key"`
}

// VisibleTo reports whether an actor may read an event.
func (e Event) VisibleTo(actor string) bool {
	if len(e.To) == 0 || e.Actor == actor {
		return true
	}
	for _, id := range e.To {
		if id == actor {
			return true
		}
	}
	return false
}

// AppendInput carries every condition required for one atomic append.
type AppendInput struct {
	Actor    string          `json:"actor"`
	Expected uint64          `json:"expected"`
	Type     string          `json:"type"`
	Body     string          `json:"body,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
	To       []string        `json:"to,omitempty"`
	CausedBy string          `json:"caused_by,omitempty"`
	Key      string          `json:"key,omitempty"`
}

// Feed is a visible slice of a room log plus the last sequence inspected. A
// writer must pass Cursor back as Expected; the store rejects it if the room
// changed in between.
type Feed struct {
	Room   *Room   `json:"room"`
	Cursor uint64  `json:"cursor"`
	Events []Event `json:"events"`
}

// Store is the only persistence seam. Every append implementation must enforce
// ordering, stale-write rejection, visibility and idempotency atomically.
type Store interface {
	Room(ctx context.Context) (*Room, error)
	UpsertActor(ctx context.Context, id, role string) (*Actor, error)
	Actors(ctx context.Context) ([]*Actor, error)
	Read(ctx context.Context, actor string, after uint64, limit int) (*Feed, error)
	AppendEvent(ctx context.Context, input AppendInput) (*Event, error)
}
