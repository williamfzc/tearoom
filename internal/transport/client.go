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
// remote callers therefore share exactly one domain contract.
type Client struct {
	Base string
	HTTP *http.Client
}

// RequireLoopbackURL enforces the unauthenticated P0 transport boundary.
// The served API has no credentials, so a client target must be loopback too.
func RequireLoopbackURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("TEAROOM_SERVER must be an http(s) loopback URL")
	}
	if parsed.User != nil || parsed.Path != "" && parsed.Path != "/" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("TEAROOM_SERVER must be a bare loopback server URL")
	}
	host := parsed.Hostname()
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	return fmt.Errorf("TEAROOM_SERVER must target loopback, got %q", host)
}

func NewClient(base string) *Client {
	return &Client{Base: base, HTTP: &http.Client{Timeout: 30 * time.Second}}
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

// Append contributes one Event as the named actor. The request body carries
// only to and content; the idempotency key, when set, travels in the
// Idempotency-Key header.
func (c *Client) Append(ctx context.Context, actor string, request core.AppendRequest) (*core.Event, error) {
	var event core.Event
	headers := map[string]string{actorHeader: actor, idempotencyHeader: request.IdempotencyKey}
	return &event, c.do(ctx, http.MethodPost, "/api/events", headers, request, &event)
}

var _ core.Store = (*Client)(nil)
