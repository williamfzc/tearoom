// Package control implements the hosted control plane: accounts, room
// memberships, and agent credentials. It lives outside the coordination
// schema on purpose — nothing in the Room/Actor/Event kernel knows it
// exists, and the ctl_ tables never mix with the coordination tables.
package control

import "time"

// Table names are the relational shape of record, not GORM's pluralized
// model names; the ctl_ prefix keeps the control plane visibly separate
// from the coordination schema in a shared database.
func (Account) TableName() string    { return "ctl_accounts" }
func (Membership) TableName() string { return "ctl_memberships" }
func (Credential) TableName() string { return "ctl_credentials" }

// Account is one human owner of hosted Rooms. Phase 1 creates it through
// the dev admin flow; the GitHub OAuth exchange (Phase 2) will own the
// GitHubID column.
type Account struct {
	ID        uint      `gorm:"primaryKey"`
	Login     string    `gorm:"not null;uniqueIndex"`
	Name      string    `gorm:"not null"`
	GitHubID  string    `gorm:"uniqueIndex"`
	CreatedAt time.Time `gorm:"not null"`
}

// Membership binds one Account to one hosted Room. The coordination Room
// row itself lives in the relational schema (rooms), never here.
type Membership struct {
	ID        uint      `gorm:"primaryKey"`
	AccountID uint      `gorm:"not null;uniqueIndex:idx_membership_room"`
	RoomID    string    `gorm:"not null;size:64;uniqueIndex:idx_membership_room"`
	Role      string    `gorm:"not null"` // RoleOwner or RoleMember
	CreatedAt time.Time `gorm:"not null"`
}

// Credential is one agent's access to one Room: an opaque token bound to
// (room, actor, capabilities, expiry). Only its sha256 hash is stored; the
// plaintext is shown once at issue and cannot be recovered.
type Credential struct {
	ID           uint       `gorm:"primaryKey"`
	RoomID       string     `gorm:"not null;size:64;index:idx_credentials_room"`
	ActorID      string     `gorm:"not null"`
	Capabilities string     `gorm:"not null"` // comma-joined subset of read,append,manage
	Hash         string     `gorm:"not null;size:64;uniqueIndex"`
	ExpiresAt    *time.Time `gorm:"index"`
	RevokedAt    *time.Time
	CreatedAt    time.Time `gorm:"not null"`
}
