// Package transport carries the core.Store contract over HTTP.
// The client speaks the wire protocol the server enforces: the Actor on the
// events routes arrives in X-Tearoom-Actor, idempotency in Idempotency-Key,
// and errors come back as the unified {"error":{...}} envelope.
package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/williamfzc/tearoom/internal/core"
)

// decode converts one wire error entry back into the store error it describes.
func (w wireError) decode() error {
	switch w.Code {
	case "unknown_actor":
		return fmt.Errorf("%w: %s", core.ErrUnknownActor, w.Message)
	case "conflict":
		return fmt.Errorf("%w: %s", core.ErrConflict, w.Message)
	case "no_room":
		return core.ErrNoRoom
	case "not_found":
		return fmt.Errorf("%w: %s", core.ErrNotFound, w.Message)
	default:
		return errors.New(w.Message)
	}
}

// Client implements core.Store against a remote Tearoom server. Local and
// remote callers therefore share exactly one domain contract. Token, when
// set, rides the Authorization header on every request: a write credential
// for agents, the owner token for owner operations.
type Client struct {
	Base  string
	HTTP  *http.Client
	Token string
}

// RequireSafeURL enforces the transport boundary: plaintext http is a
// loopback-only affair, because tokens ride the write path over https.
// The URL may carry a room prefix of the form /r/<id> and nothing else.
func RequireSafeURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("TEAROOM_SERVER must be an http(s) URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("TEAROOM_SERVER must be a bare server or room URL")
	}
	if parsed.Path != "" && parsed.Path != "/" && !roomPrefix(parsed.Path) {
		return fmt.Errorf("TEAROOM_SERVER may only carry a room prefix like /r/<id>, got %q", parsed.Path)
	}
	host := parsed.Hostname()
	if parsed.Scheme == "https" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	return fmt.Errorf("plaintext http targets loopback only, got %q", host)
}

func roomPrefix(path string) bool {
	rest, ok := strings.CutPrefix(path, "/r/")
	if !ok || rest == "" {
		return false
	}
	return !strings.Contains(rest, "/")
}

func NewClient(base, token string) *Client {
	return &Client{Base: base, HTTP: &http.Client{Timeout: 30 * time.Second}, Token: token}
}

func (c *Client) do(ctx context.Context, method, path string, headers map[string]string, body, out any) error {
	return c.doWith(c.HTTP, ctx, method, path, headers, body, out)
}

func (c *Client) doWith(httpClient *http.Client, ctx context.Context, method, path string, headers map[string]string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.Base+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		if value != "" {
			request.Header.Set(name, value)
		}
	}
	if c.Token != "" {
		request.Header.Set("Authorization", "Bearer "+c.Token)
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("tearoom server unreachable: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		var envelope errorEnvelope
		if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil || envelope.Error.Message == "" {
			return fmt.Errorf("server returned %s", response.Status)
		}
		return envelope.Error.decode()
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(response.Body).Decode(out)
}

func (c *Client) Room(ctx context.Context) (*core.Room, error) {
	var room core.Room
	return &room, c.do(ctx, http.MethodGet, "/api/room", nil, nil, &room)
}

// AdmitActor joins the named actor explicitly under loopback/OS trust.
func (c *Client) AdmitActor(ctx context.Context, actorID string) (*core.Actor, error) {
	var actor core.Actor
	return &actor, c.do(ctx, http.MethodPost, "/api/actors", nil, map[string]string{"id": actorID}, &actor)
}

func (c *Client) Actors(ctx context.Context) ([]core.Actor, error) {
	var actors []core.Actor
	return actors, c.do(ctx, http.MethodGet, "/api/actors", nil, nil, &actors)
}

// Read returns the events after the exclusive caller-owned boundary, read as
// the named actor. The read is pure; callers that need to wait for new
// history poll from their own boundary.
func (c *Client) Read(ctx context.Context, actor string, after uint64) ([]core.Event, error) {
	params := url.Values{}
	params.Set("after", strconv.FormatUint(after, 10))
	headers := map[string]string{actorHeader: actor}
	var events []core.Event
	return events, c.do(ctx, http.MethodGet, "/api/events?"+params.Encode(), headers, nil, &events)
}

// CreatedRoom is the answer of room creation on a hosted service. Server is
// the room URL that TEAROOM_SERVER points at from then on.
type CreatedRoom struct {
	ID      string    `json:"id"`
	Created time.Time `json:"created"`
	Server  string    `json:"server"`
}

// CreateRoom mints one Room on a hosted service and admits its creator. The
// bearer on this client must be the deployment's owner token.
func (c *Client) CreateRoom(ctx context.Context, creator string) (CreatedRoom, error) {
	var room CreatedRoom
	return room, c.do(ctx, http.MethodPost, "/api/rooms", nil, map[string]string{"actor": creator}, &room)
}

// IssueCredential mints one write credential for an admitted actor. The
// bearer on this client must be the deployment's owner token.
func (c *Client) IssueCredential(ctx context.Context, request IssueRequest) (IssuedCredential, error) {
	var issued IssuedCredential
	return issued, c.do(ctx, http.MethodPost, "/api/credentials", nil, request, &issued)
}

// RevokeCredential immediately invalidates one credential.
func (c *Client) RevokeCredential(ctx context.Context, credentialID string) error {
	return c.do(ctx, http.MethodPost, "/api/credentials/revoke", nil, map[string]string{"id": credentialID}, nil)
}

// IssueRequest names the actor and the lifetime of a new credential.
type IssueRequest struct {
	Actor      string `json:"actor"`
	TTLSeconds int    `json:"ttl_seconds"`
}

// IssuedCredential is the one-time view of a minted credential.
type IssuedCredential struct {
	ID        string    `json:"id"`
	Actor     string    `json:"actor"`
	ExpiresAt time.Time `json:"expires_at"`
	Token     string    `json:"token"`
}

// Append contributes one Event as the named actor. The request body carries
// only to and content; the idempotency key, when set, travels in the
// Idempotency-Key header.
func (c *Client) Append(ctx context.Context, actor string, request core.AppendRequest) (*core.Event, error) {
	var event core.Event
	headers := map[string]string{actorHeader: actor, idempotencyHeader: request.IdempotencyKey}
	return &event, c.do(ctx, http.MethodPost, "/api/events", headers, request, &event)
}

var _ core.Store = (*Client)(nil)
