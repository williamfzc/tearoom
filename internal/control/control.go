// Control owns credential policy for the rooms of one shared database: issue
// for an admitted actor, verify a bearer to its actor, revoke immediately.
// Credentials authorize writes and never change Event semantics; the token
// exists in plaintext only at issuance and is stored as a sha-256 hash.
package control

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/williamfzc/tearoom/internal/core"
	"github.com/williamfzc/tearoom/internal/relational"
)

// ErrUnauthorized marks a bearer that is missing, unknown, expired, or
// revoked. The wire maps it to 401; it never masquerades as an empty read.
var ErrUnauthorized = errors.New("credential is missing, unknown, expired, or revoked")

// Control issues and verifies write credentials over one shared database.
type Control struct {
	db  *gorm.DB
	now func() time.Time
}

// New builds a Control over the same database the relational stores share.
func New(db *gorm.DB, now func() time.Time) *Control {
	if now == nil {
		now = time.Now
	}
	return &Control{db: db, now: now}
}

// Issued is the one-time view of a minted credential. Token is the only place
// the plaintext ever appears; the caller relays it to its recipient once.
type Issued struct {
	ID        string    `json:"id"`
	Actor     string    `json:"actor"`
	ExpiresAt time.Time `json:"expires_at"`
	Token     string    `json:"token"`
}

// Issue mints one credential for an already-admitted Actor of the Room.
func (c *Control) Issue(ctx context.Context, roomID, actorID string, ttl time.Duration) (Issued, error) {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return Issued{}, core.ErrUnknownActor
	}
	if ttl <= 0 {
		return Issued{}, errors.New("credential expiry must be positive")
	}
	var actor relational.ActorRow
	err := c.db.WithContext(ctx).
		Where("room_id = ? AND id = ?", roomID, actorID).
		Limit(1).Find(&actor).Error
	if err != nil {
		return Issued{}, err
	}
	if actor.ID == "" {
		return Issued{}, core.ErrUnknownActor
	}
	token, err := newToken()
	if err != nil {
		return Issued{}, err
	}
	row := relational.CredentialRow{
		ID:        "cred_" + randomHex(4),
		RoomID:    roomID,
		Actor:     actorID,
		TokenHash: hashToken(token),
		ExpiresAt: c.now().UTC().Add(ttl),
		CreatedAt: c.now().UTC(),
	}
	if err := c.db.WithContext(ctx).Create(&row).Error; err != nil {
		return Issued{}, err
	}
	return Issued{ID: row.ID, Actor: row.Actor, ExpiresAt: row.ExpiresAt, Token: token}, nil
}

// Verify resolves a bearer token to its admitted actor inside one Room.
// Unknown, expired, and revoked tokens are the same refusal.
func (c *Control) Verify(ctx context.Context, roomID, bearer string) (string, error) {
	bearer = strings.TrimSpace(bearer)
	if bearer == "" {
		return "", ErrUnauthorized
	}
	var row relational.CredentialRow
	err := c.db.WithContext(ctx).
		Where("room_id = ? AND token_hash = ?", roomID, hashToken(bearer)).
		Limit(1).Find(&row).Error
	if err != nil {
		return "", err
	}
	if row.ID == "" || row.RevokedAt != nil || !c.now().UTC().Before(row.ExpiresAt) {
		return "", ErrUnauthorized
	}
	return row.Actor, nil
}

// Revoke immediately invalidates one credential. Revoking an already-revoked
// credential is idempotent; an unknown credential is not found.
func (c *Control) Revoke(ctx context.Context, roomID, credentialID string) error {
	result := c.db.WithContext(ctx).
		Model(&relational.CredentialRow{}).
		Where("room_id = ? AND id = ? AND revoked_at IS NULL", roomID, credentialID).
		Update("revoked_at", c.now().UTC())
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		var row relational.CredentialRow
		err := c.db.WithContext(ctx).
			Where("room_id = ? AND id = ?", roomID, credentialID).
			Limit(1).Find(&row).Error
		if err != nil {
			return err
		}
		if row.ID == "" {
			return core.ErrNotFound
		}
	}
	return nil
}

// newToken mints 192 bits of entropy as one printable bearer token.
func newToken() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "tr_" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func randomHex(bytes int) string {
	raw := make([]byte, bytes)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return hex.EncodeToString(raw)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
