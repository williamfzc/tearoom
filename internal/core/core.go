// Package core defines Tearoom's domain vocabulary and storage contract.
// A room contains admitted actors and one ordered, append-only event history.
package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	ErrNotFound     = errors.New("not found")
	ErrConflict     = errors.New("conflict")
	ErrNoRoom       = errors.New("no room: run 'tearoom room init' first")
	ErrUnknownActor = errors.New("unknown actor")
)

// Room is the durable boundary in which actors exchange events. One room owns
// one ordered event sequence; its identity and creation time are fixed at init.
type Room struct {
	ID      string    `json:"id"`
	Created time.Time `json:"created"`
}

// Actor is a durable participant identity, independent of any process or
// session. Admission is explicit: joining an already-admitted actor returns
// the existing actor.
type Actor struct {
	ID     string    `json:"id"`
	Joined time.Time `json:"joined"`
}

// Event is one immutable contribution in a room's dense sequence. Every member
// may read every event; To names whom the event addresses and is public
// addressing metadata, never a visibility or secrecy boundary.
type Event struct {
	Seq     uint64          `json:"seq"`
	Actor   string          `json:"actor"`
	At      time.Time       `json:"at"`
	To      []string        `json:"to,omitempty"`
	Content json.RawMessage `json:"content"`
}

// Address returns the presentation address e<seq> of the event.
func (e Event) Address() string { return fmt.Sprintf("e%d", e.Seq) }

// AppendRequest carries the caller-provided parts of one append. Content is
// arbitrary JSON (plain text is encoded as a JSON string by the CLI);
// IdempotencyKey, when set, is matched per (room, actor, key) and never
// becomes an Event field.
type AppendRequest struct {
	To             []string        `json:"to"`
	Content        json.RawMessage `json:"content"`
	IdempotencyKey string          `json:"-"`
}

// Store is the only persistence seam. Every implementation enforces explicit
// admission, dense ordered append, membership visibility, pure caller-owned
// reads, and per-request idempotency atomically.
type Store interface {
	Room(ctx context.Context) (*Room, error)
	AdmitActor(ctx context.Context, actorID string) (*Actor, error)
	Actors(ctx context.Context) ([]Actor, error)
	Read(ctx context.Context, actorID string, after uint64, limit int) ([]Event, error)
	Append(ctx context.Context, actorID string, request AppendRequest) (*Event, error)
}

// NormalizeTo returns the canonical form of the To addressing list: each value
// is trimmed, deduplicated, and sorted. A comma is part of an actor identity;
// callers that address multiple actors provide multiple values.
func NormalizeTo(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

// CompactJSON returns the canonical compacted form of valid JSON content,
// without insignificant whitespace. The empty message is rejected: content is
// required, though null is valid content.
func CompactJSON(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, errors.New("content is required")
	}
	if !json.Valid(raw) {
		return nil, errors.New("content must be valid JSON")
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return nil, err
	}
	return json.RawMessage(buf.Bytes()), nil
}

// Fingerprint returns the idempotency identity of one append request: SHA-256
// over the actor, the normalized To list, and the compacted content bytes.
// Pass already-compacted content and already-normalized To; the Store contract
// performs both normalizations before any fingerprint comparison.
func Fingerprint(actor string, to []string, content json.RawMessage) []byte {
	h := sha256.New()
	h.Write([]byte(actor))
	h.Write([]byte{0})
	for _, id := range to {
		h.Write([]byte(id))
		h.Write([]byte{0})
	}
	h.Write(content)
	return h.Sum(nil)
}
