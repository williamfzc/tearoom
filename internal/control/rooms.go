// Hosted Room lifecycle. Creating a hosted Room is one transaction: a
// coordination Room row (the same table the relational Store owns) plus an
// owner Membership in the control plane. The coordination kernel is never
// involved — this is pure record keeping around it.
package control

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/williamfzc/tearoom/internal/relational"
	"gorm.io/gorm"
)

// Membership roles.
const (
	RoleOwner  = "owner"
	RoleMember = "member"
)

var ErrNotMember = errors.New("account has no membership in this room")

// RoomView is one hosted Room as its owner sees it.
type RoomView struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateRoom creates a coordination Room row and binds owner as its owner
// in one transaction. The returned id is the tenant key: the URL path
// /r/<id>/api and every relational.Store over the shared database key on it.
func CreateRoom(ctx context.Context, db *gorm.DB, owner uint, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("room name is required")
	}
	roomID, err := newRoomID()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		room := relational.RoomRow{ID: roomID, Name: name, CreatedAt: now}
		if err := tx.Create(&room).Error; err != nil {
			return err
		}
		return tx.Create(&Membership{AccountID: owner, RoomID: roomID, Role: RoleOwner, CreatedAt: now}).Error
	})
	if err != nil {
		return "", err
	}
	return roomID, nil
}

// RoomsOf lists the hosted Rooms one account can see, newest first.
func RoomsOf(ctx context.Context, db *gorm.DB, accountID uint) ([]RoomView, error) {
	var rows []struct {
		Membership
		RoomName  string
		RoomID    string
		CreatedAt time.Time
	}
	err := db.WithContext(ctx).Table("ctl_memberships").
		Select("ctl_memberships.*, rooms.name AS room_name, rooms.id AS room_id, rooms.created_at AS created_at").
		Joins("JOIN rooms ON rooms.id = ctl_memberships.room_id").
		Where("ctl_memberships.account_id = ?", accountID).
		Order("rooms.created_at DESC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	rooms := make([]RoomView, 0, len(rows))
	for _, row := range rows {
		rooms = append(rooms, RoomView{ID: row.RoomID, Name: row.RoomName, Role: row.Role, CreatedAt: row.CreatedAt})
	}
	return rooms, nil
}

// MembershipOf returns the account's membership in a room, or ErrNotMember.
func MembershipOf(ctx context.Context, db *gorm.DB, accountID uint, roomID string) (*Membership, error) {
	var membership Membership
	if err := db.WithContext(ctx).
		Where("account_id = ? AND room_id = ?", accountID, roomID).
		Limit(1).Find(&membership).Error; err != nil {
		return nil, err
	}
	if membership.ID == 0 {
		return nil, ErrNotMember
	}
	return &membership, nil
}

func newRoomID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}
