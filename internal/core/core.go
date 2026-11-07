// Package core defines Tearoom's domain vocabulary and storage contract.
// A workspace holds a tree of rooms; every room owns one ordered, append-only
// record stream; appending emits a notification envelope that stores nothing.
package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
	ErrNoRoom   = errors.New("no workspace: run 'tearoom room init' first")
)

// Room is one span of context: a parent (empty when the Room is a root) and
// one ordered record stream. Rooms are identified by name within their
// workspace, and the name is the address other readers quote.
type Room struct {
	Name    string    `json:"name"`
	Parent  string    `json:"parent,omitempty"`
	Created time.Time `json:"created"`
}

// Record is one immutable entry in a Room's stream. The author is an
// unverified signature label; there is no recipient field and nothing filters
// by author.
type Record struct {
	Room    string          `json:"room"`
	Seq     uint64          `json:"seq"`
	Author  string          `json:"author"`
	At      time.Time       `json:"at"`
	Content json.RawMessage `json:"content"`
}

// Address returns the presentation address e<seq> of the record.
func (r Record) Address() string { return fmt.Sprintf("e%d", r.Seq) }

// Notification is the envelope emitted when a record lands: which room,
// which sequence, which signature, when. It carries no content, stores
// nothing, and requests nothing; deciding who is woken belongs outside.
type Notification struct {
	Room   string    `json:"room"`
	Seq    uint64    `json:"seq"`
	Author string    `json:"author"`
	At     time.Time `json:"at"`
}

// Notification derives the envelope of one record.
func (r Record) Notification() Notification {
	return Notification{Room: r.Room, Seq: r.Seq, Author: r.Author, At: r.At}
}

// AppendRequest carries the caller-provided parts of one append. Content is
// arbitrary JSON (plain text is encoded as a JSON string by the CLI);
// IdempotencyKey, when set, is matched per (room, author, key) and never
// becomes a Record field.
type AppendRequest struct {
	Content        json.RawMessage `json:"content"`
	IdempotencyKey string          `json:"-"`
}

// Store is the only persistence seam. Every implementation enforces the
// single-parent tree, dense per-room append, pure caller-owned reads, and
// per-request idempotency atomically. Identity is not part of the contract:
// reading and writing validate no signature and create no membership.
type Store interface {
	// CreateRoom creates a Room named name, recording parent as its parent.
	// An empty parent creates a root. The name must be unused and the parent,
	// when given, must exist.
	CreateRoom(ctx context.Context, name, parent string) (*Room, error)
	// Rooms lists every Room of the workspace; the child list of any Room is
	// derived from this listing at read time and is never cached.
	Rooms(ctx context.Context) ([]Room, error)
	// Read returns the records of one room after the caller-owned exclusive
	// boundary. Repeated reads are side-effect-free; no cursor is persisted.
	Read(ctx context.Context, room string, after uint64) ([]Record, error)
	// Append accepts one contribution to one room and assigns the next dense
	// sequence of that room. The author is stored as given, whitespace
	// trimmed; an empty author is a valid signature.
	Append(ctx context.Context, room, author string, request AppendRequest) (*Record, error)
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
// over the signature and the compacted content bytes, scoped by the ledger's
// (room, signature, key) row. Pass already-compacted content; the Store
// contract compacts before any fingerprint comparison.
func Fingerprint(author string, content json.RawMessage) []byte {
	h := sha256.New()
	h.Write([]byte(author))
	h.Write([]byte{0})
	h.Write(content)
	return h.Sum(nil)
}
