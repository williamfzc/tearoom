// auth.go enforces the exposed profile: reads stay open, writes require a
// bearer credential bound to its actor, and joins plus credential management
// are owner operations. The local loopback profile keeps self-asserted
// actors and never constructs Auth.
package transport

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/williamfzc/tearoom/internal/control"
)

// ErrUnauthorized and ErrForbidden are the exposed-profile refusal classes;
// they map to 401 and 403 and never masquerade as an empty read.
var (
	ErrUnauthorized = errors.New("authentication required")
	ErrForbidden    = errors.New("not allowed for this credential")
)

// Auth configures the exposed profile. Control verifies write credentials;
// OwnerTokenHash is the sha-256 hex of the deployment-injected owner token.
type Auth struct {
	Control        *control.Control
	OwnerTokenHash string
}

func (a *Auth) isOwner(bearer string) bool {
	if bearer == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(control.HashToken(bearer)), []byte(a.OwnerTokenHash)) == 1
}

// bearer extracts the "Authorization: Bearer <token>" value.
func bearer(r *http.Request) string {
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	if rest, ok := strings.CutPrefix(value, "Bearer "); ok {
		return strings.TrimSpace(rest)
	}
	return ""
}

// requireOwner gates the owner operations of the exposed profile.
func (s *Server) requireOwner(r *http.Request) error {
	b := bearer(r)
	if b == "" {
		return ErrUnauthorized
	}
	if !s.Auth.isOwner(b) {
		return ErrForbidden
	}
	return nil
}

// actorForWrite resolves the writing actor: the credential is authoritative,
// and a present X-Tearoom-Actor that disagrees is a mismatch.
func (s *Server) actorForWrite(r *http.Request) (string, error) {
	b := bearer(r)
	if b == "" {
		return "", ErrUnauthorized
	}
	room, err := s.Store.Room(r.Context())
	if err != nil {
		return "", err
	}
	actor, err := s.Auth.Control.Verify(r.Context(), room.ID, b)
	if err != nil {
		return "", err
	}
	if header := strings.TrimSpace(r.Header.Get(actorHeader)); header != "" && header != actor {
		return "", fmt.Errorf("%w: actor %q does not match the credential", ErrForbidden, header)
	}
	return actor, nil
}

// issueCredential serves POST /api/credentials for the owner.
func (s *Server) issueCredential(r *http.Request) (any, error) {
	if err := s.requireOwner(r); err != nil {
		return nil, err
	}
	var input struct {
		Actor      string `json:"actor"`
		TTLSeconds int    `json:"ttl_seconds"`
	}
	if err := DecodeStrict(r, &input); err != nil {
		return nil, fmt.Errorf("invalid issue body: %w", err)
	}
	room, err := s.Store.Room(r.Context())
	if err != nil {
		return nil, err
	}
	return s.Auth.Control.Issue(r.Context(), room.ID, input.Actor, time.Duration(input.TTLSeconds)*time.Second)
}

// revokeCredential serves POST /api/credentials/revoke for the owner.
func (s *Server) revokeCredential(r *http.Request) (any, error) {
	if err := s.requireOwner(r); err != nil {
		return nil, err
	}
	var input struct {
		ID string `json:"id"`
	}
	if err := DecodeStrict(r, &input); err != nil {
		return nil, fmt.Errorf("invalid revoke body: %w", err)
	}
	room, err := s.Store.Room(r.Context())
	if err != nil {
		return nil, err
	}
	if err := s.Auth.Control.Revoke(r.Context(), room.ID, strings.TrimSpace(input.ID)); err != nil {
		return nil, err
	}
	return map[string]bool{"revoked": true}, nil
}
