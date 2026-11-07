// Persistence records for the relational Store: the durable shape of the
// room tree, its per-room record streams, the per-request idempotency
// ledger, and the format marker. These are storage rows, not domain types;
// the canonical vocabulary remains core.Store.
package relational

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/williamfzc/tearoom/internal/core"
)

// Table names are the relational shape of record, not GORM's pluralized model
// names.
func (RoomRow) TableName() string        { return "rooms" }
func (EventRow) TableName() string       { return "events" }
func (IdempotencyRow) TableName() string { return "append_idempotency" }
func (MetaRow) TableName() string        { return "meta" }

// RoomRow is one node of the room tree: its name, its parent (null is a
// root), and the dense sequence head of its stream. The head is only ever
// advanced by an append transaction that holds the row lock.
type RoomRow struct {
	Name      string    `gorm:"primaryKey;size:255"`
	Parent    *string   `gorm:"size:255;index"`
	CreatedAt time.Time `gorm:"not null"`
	Head      uint64    `gorm:"not null"`
}

// EventRow is one immutable record of one room's stream. Author holds the
// signature verbatim; Content holds compacted JSON text.
type EventRow struct {
	RoomID  string    `gorm:"column:room_id;primaryKey;size:255"`
	Seq     uint64    `gorm:"primaryKey"`
	Author  string    `gorm:"not null"`
	At      time.Time `gorm:"not null"`
	Content string    `gorm:"not null"`
}

// IdempotencyRow records one (room, author, key) ledger entry: the
// fingerprint of the original request and the sequence of the record it
// created, which answers retries with the original record. It is never
// exposed as a Record field.
type IdempotencyRow struct {
	RoomID      string `gorm:"primaryKey;size:255"`
	Author      string `gorm:"primaryKey;size:255"`
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

// coreRecord converts a stored row back into the domain Record. Malformed
// content or a foreign room id is storage corruption and fails closed.
func (r EventRow) coreRecord(room string) (core.Record, error) {
	if r.Seq == 0 || r.RoomID != room || r.At.IsZero() {
		return core.Record{}, fmt.Errorf("invalid stored record %d of %q", r.Seq, room)
	}
	content, err := core.CompactJSON(json.RawMessage(r.Content))
	if err != nil || string(content) != r.Content {
		return core.Record{}, fmt.Errorf("stored record %d of %q content is not compact valid JSON", r.Seq, room)
	}
	return core.Record{
		Room:    room,
		Seq:     r.Seq,
		Author:  r.Author,
		At:      r.At,
		Content: content,
	}, nil
}
