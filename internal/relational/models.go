// GORM models for the relational Store. These are persistence records, not
// product entities: nothing outside this package may treat them as domain
// types, and the canonical vocabulary remains core.Store.
package relational

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/williamfzc/tearoom/internal/core"
)

// RoomID is the single Room each Store owns, matching the file backend's
// room id so both backends answer the same identity for one .tearoom.
const RoomID = "room"

// Table names are the relational shape of record, not GORM's pluralized
// model names.
func (RoomRow) TableName() string  { return "rooms" }
func (ActorRow) TableName() string { return "actors" }
func (EventRow) TableName() string { return "events" }

// RoomRow is the durable Room identity and its sequence head.
type RoomRow struct {
	ID        string    `gorm:"primaryKey;size:64"`
	Name      string    `gorm:"not null"`
	CreatedAt time.Time `gorm:"not null"`
	Head      uint64    `gorm:"not null"`
}

// ActorRow is one stable participant of a Room.
type ActorRow struct {
	RoomID     string    `gorm:"primaryKey;size:64"`
	ID         string    `gorm:"primaryKey;size:255"`
	Role       string    `gorm:"not null"`
	JoinedAt   time.Time `gorm:"not null"`
	LastSeenAt time.Time `gorm:"not null"`
}

// EventRow is one immutable fact. Recipients holds the canonical sorted
// comma-joined form (core.NormalizeRecipients); Data holds JSON text or "".
type EventRow struct {
	RoomID     string    `gorm:"primaryKey;size:64;uniqueIndex:idx_room_key"`
	Seq        uint64    `gorm:"primaryKey"`
	ID         string    `gorm:"not null;size:64"`
	Actor      string    `gorm:"not null"`
	At         time.Time `gorm:"not null"`
	Type       string    `gorm:"not null"`
	Body       string    `gorm:"not null"`
	Data       string    `gorm:"not null"`
	Recipients string    `gorm:"not null"`
	CausedBy   string    `gorm:"not null"`
	Key        string    `gorm:"not null;size:255;uniqueIndex:idx_room_key"`
}

func (r EventRow) coreEvent() core.Event {
	var data json.RawMessage
	if r.Data != "" {
		data = json.RawMessage(r.Data)
	}
	var to []string
	if r.Recipients != "" {
		to = strings.Split(r.Recipients, ",")
	}
	return core.Event{
		ID:       r.ID,
		Room:     r.RoomID,
		Seq:      r.Seq,
		Actor:    r.Actor,
		At:       r.At,
		Type:     r.Type,
		Body:     r.Body,
		Data:     data,
		To:       to,
		CausedBy: r.CausedBy,
		Key:      r.Key,
	}
}
