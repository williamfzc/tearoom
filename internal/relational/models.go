// Persistence records for the relational Store: the durable shape of a Room,
// its Actors, its Events, the per-request idempotency ledger, and the format
// marker. These are storage rows, not domain types; the canonical vocabulary
// remains core.Store.
package relational

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/williamfzc/tearoom/internal/core"
)

// Table names are the relational shape of record, not GORM's pluralized model
// names.
func (RoomRow) TableName() string        { return "rooms" }
func (ActorRow) TableName() string       { return "actors" }
func (EventRow) TableName() string       { return "events" }
func (IdempotencyRow) TableName() string { return "append_idempotency" }
func (MetaRow) TableName() string        { return "meta" }

// RoomRow is the durable Room identity and its dense sequence head. The head
// is only ever advanced by an append transaction that holds the row lock.
type RoomRow struct {
	ID        string    `gorm:"primaryKey;size:64"`
	CreatedAt time.Time `gorm:"not null"`
	Head      uint64    `gorm:"not null"`
}

// ActorRow is one explicitly admitted participant of a Room.
type ActorRow struct {
	RoomID   string    `gorm:"primaryKey;size:64"`
	ID       string    `gorm:"primaryKey;size:255"`
	JoinedAt time.Time `gorm:"not null"`
}

// EventRow is one immutable contribution. To holds the canonical JSON array
// form of core.NormalizeTo; Content holds compacted JSON text.
type EventRow struct {
	RoomID  string    `gorm:"primaryKey;size:64"`
	Seq     uint64    `gorm:"primaryKey"`
	Actor   string    `gorm:"not null"`
	At      time.Time `gorm:"not null"`
	To      string    `gorm:"not null"`
	Content string    `gorm:"not null"`
}

// IdempotencyRow records one (room, actor, key) idempotency ledger entry: the
// fingerprint of the original request and the sequence of the Event it
// created, which answers retries with the original Event. It is never exposed
// as an Event field.
type IdempotencyRow struct {
	RoomID      string `gorm:"primaryKey;size:64"`
	Actor       string `gorm:"primaryKey;size:255"`
	Key         string `gorm:"primaryKey;size:255"`
	Fingerprint []byte `gorm:"not null"`
	EventSeq    uint64 `gorm:"not null"`
}

// MetaRow is the format marker: one key/value row records the database layout
// format. Databases without the marker are refused on open.
type MetaRow struct {
	Key   string `gorm:"primaryKey;size:64"`
	Value string `gorm:"not null"`
}

// coreEvent converts a stored row back into the domain Event. The To column
// is JSON we wrote ourselves, so malformed JSON or non-member identities are
// storage corruption and fail closed.
func (r EventRow) coreEvent(members map[string]struct{}) (core.Event, error) {
	if r.Seq == 0 || strings.TrimSpace(r.Actor) == "" || r.Actor != strings.TrimSpace(r.Actor) || r.At.IsZero() {
		return core.Event{}, fmt.Errorf("invalid stored event %d", r.Seq)
	}
	if !isMember(members, r.Actor) {
		return core.Event{}, fmt.Errorf("invalid stored event %d: actor %q is not an admitted member", r.Seq, r.Actor)
	}
	var to []string
	if err := json.Unmarshal([]byte(r.To), &to); err != nil {
		return core.Event{}, fmt.Errorf("decode stored event %d recipients: %w", r.Seq, err)
	}
	if normalized := core.NormalizeTo(to); !equalStrings(to, normalized) {
		return core.Event{}, fmt.Errorf("stored event %d recipients are not normalized", r.Seq)
	}
	for _, recipient := range to {
		if !isMember(members, recipient) {
			return core.Event{}, fmt.Errorf("invalid stored event %d: recipient %q is not an admitted member", r.Seq, recipient)
		}
	}
	content, err := core.CompactJSON(json.RawMessage(r.Content))
	if err != nil || string(content) != r.Content {
		return core.Event{}, fmt.Errorf("stored event %d content is not compact valid JSON", r.Seq)
	}
	return core.Event{
		Seq:     r.Seq,
		Actor:   r.Actor,
		At:      r.At,
		To:      to,
		Content: content,
	}, nil
}

func isMember(members map[string]struct{}, actorID string) bool {
	_, ok := members[actorID]
	return ok
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
