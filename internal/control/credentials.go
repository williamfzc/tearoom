// Credential lifecycle: issue, verify, revoke. A credential is an opaque
// token (tr_ + 192 random bits) bound to one Room, one Actor, a capability
// set, and an expiry. Only the sha256 hash is stored; the plaintext is
// shown once at issue. Hashing at rest means a database leak does not
// yield usable tokens; 192 random bits need no KDF.
package control

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// TokenPrefix is the visible marker of a hosted credential.
const TokenPrefix = "tr_"

// Capability names. "manage" is reserved for members who can issue and
// revoke credentials; Phase 1 exercises read and append.
const (
	CapRead   = "read"
	CapAppend = "append"
	CapManage = "manage"
)

var (
	ErrCredentialUnknown = errors.New("credential not found")
	ErrCredentialRevoked = errors.New("credential revoked")
	ErrCredentialExpired = errors.New("credential expired")
)

// ParseCaps turns a comma-joined capability list into its set, ignoring
// unknown tokens so a newer client never breaks an older server.
func ParseCaps(list string) map[string]bool {
	caps := make(map[string]bool)
	for _, part := range strings.Split(list, ",") {
		switch strings.TrimSpace(part) {
		case CapRead, CapAppend, CapManage:
			caps[strings.TrimSpace(part)] = true
		}
	}
	return caps
}

// HasCaps reports whether the comma-joined granted list contains every
// required capability.
func HasCaps(granted string, required ...string) bool {
	have := ParseCaps(granted)
	for _, want := range required {
		if !have[want] {
			return false
		}
	}
	return true
}

// Issue creates a new credential and returns the plaintext token, which is
// shown exactly once. ttl <= 0 means the credential never expires.
func Issue(ctx context.Context, db *gorm.DB, roomID, actorID, caps string, ttl time.Duration) (string, *Credential, error) {
	return issue(ctx, db, roomID, actorID, caps, ttl, time.Now().UTC())
}

func issue(ctx context.Context, db *gorm.DB, roomID, actorID, caps string, ttl time.Duration, now time.Time) (string, *Credential, error) {
	roomID = strings.TrimSpace(roomID)
	actorID = strings.TrimSpace(actorID)
	if roomID == "" || actorID == "" {
		return "", nil, errors.New("room and actor are required")
	}
	valid := map[string]bool{CapRead: true, CapAppend: true, CapManage: true}
	seen := map[string]bool{}
	kept := make([]string, 0, 3)
	for _, cap := range strings.Split(caps, ",") {
		cap = strings.TrimSpace(cap)
		if valid[cap] && !seen[cap] {
			seen[cap] = true
			kept = append(kept, cap)
		}
	}
	if len(kept) == 0 {
		return "", nil, errors.New("at least one capability is required")
	}

	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	plaintext := TokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	var expiresAt *time.Time
	if ttl > 0 {
		at := now.Add(ttl)
		expiresAt = &at
	}
	cred := &Credential{
		RoomID:       roomID,
		ActorID:      actorID,
		Capabilities: strings.Join(kept, ","),
		Hash:         HashToken(plaintext),
		ExpiresAt:    expiresAt,
		CreatedAt:    now,
	}
	if err := db.WithContext(ctx).Create(cred).Error; err != nil {
		return "", nil, err
	}
	return plaintext, cred, nil
}

// HashToken is the at-rest form of a credential: sha256 of the plaintext.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Verify resolves a token to its credential, rejecting unknown, revoked,
// and expired credentials.
func Verify(ctx context.Context, db *gorm.DB, token string) (*Credential, error) {
	var cred Credential
	if err := db.WithContext(ctx).Where("hash = ?", HashToken(token)).Limit(1).Find(&cred).Error; err != nil {
		return nil, err
	}
	if cred.ID == 0 {
		return nil, fmt.Errorf("%w: %s", ErrCredentialUnknown, token[:min(len(token), 8)])
	}
	if cred.RevokedAt != nil {
		return nil, fmt.Errorf("%w: %s", ErrCredentialRevoked, token[:min(len(token), 8)])
	}
	if cred.ExpiresAt != nil && !cred.ExpiresAt.After(time.Now()) {
		return nil, fmt.Errorf("%w: %s", ErrCredentialExpired, token[:min(len(token), 8)])
	}
	return &cred, nil
}

// Revoke permanently disables a credential within its Room. The lookup is
// scoped to the Room so an account cannot revoke in a room it does not
// manage through this endpoint.
func Revoke(ctx context.Context, db *gorm.DB, roomID string, id uint) error {
	res := db.WithContext(ctx).Where("room_id = ? AND id = ?", roomID, id).
		Updates(&Credential{RevokedAt: ptr(time.Now().UTC())})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("%w: credential %d in room %s", ErrCredentialUnknown, id, roomID)
	}
	return nil
}

// CredentialView is the management-facing shape of a credential. The hash
// is deliberately absent: the list must not carry even the at-rest token
// digest, which exists only to verify presented tokens.
type CredentialView struct {
	ID           uint       `json:"id"`
	RoomID       string     `json:"room"`
	ActorID      string     `json:"actor"`
	Capabilities string     `json:"capabilities"`
	ExpiresAt    *time.Time `json:"expires_at"`
	RevokedAt    *time.Time `json:"revoked_at"`
	CreatedAt    time.Time  `json:"created_at"`
}

// ListCredentials returns a Room's credentials, newest first, for the
// management UI: identity, capabilities, expiry, and revocation state —
// never the hash, which exists only at issue time.
func ListCredentials(ctx context.Context, db *gorm.DB, roomID string) ([]CredentialView, error) {
	var views []CredentialView
	err := db.WithContext(ctx).
		Table("ctl_credentials").
		Where("room_id = ?", roomID).
		Order("id DESC").
		Scan(&views).Error
	return views, err
}

func ptr(t time.Time) *time.Time { return &t }
