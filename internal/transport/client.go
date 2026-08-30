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

const maxWaitRequest = 60 * time.Second

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
// the named actor.
func (c *Client) Read(ctx context.Context, actor string, after uint64, limit int) ([]core.Event, error) {
	return c.read(ctx, actor, after, limit, 0)
}

// ReadWait waits for the caller's full budget. The server caps each held
// request at 60 seconds, so a longer wait is retried without changing the
// caller-owned boundary.
func (c *Client) ReadWait(ctx context.Context, actor string, after uint64, limit int, wait time.Duration) ([]core.Event, error) {
	if wait <= 0 {
		return c.Read(ctx, actor, after, limit)
	}
	deadline := time.Now().Add(wait)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return []core.Event{}, nil
		}
		seconds := int64(remaining / time.Second)
		if seconds > int64(maxWaitRequest/time.Second) {
			seconds = int64(maxWaitRequest / time.Second)
		}
		if seconds == 0 {
			attemptCtx, cancel := context.WithTimeout(ctx, remaining)
			events, err := c.Read(attemptCtx, actor, after, limit)
			cancel()
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				if !time.Now().Before(deadline) && errors.Is(err, context.DeadlineExceeded) {
					return []core.Event{}, nil
				}
				return nil, err
			}
			if len(events) > 0 {
				return events, nil
			}
			remaining = time.Until(deadline)
			if remaining <= 0 {
				return []core.Event{}, nil
			}
			timer := time.NewTimer(remaining)
			select {
			case <-timer.C:
				return []core.Event{}, nil
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return nil, ctx.Err()
			}
		}
		requestWait := time.Duration(seconds) * time.Second
		attemptTimeout := requestWait + time.Second
		if remaining < attemptTimeout {
			attemptTimeout = remaining
		}
		attemptCtx, cancel := context.WithTimeout(ctx, attemptTimeout)
		events, err := c.read(attemptCtx, actor, after, limit, requestWait)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if time.Now().Before(deadline) && errors.Is(err, context.DeadlineExceeded) {
				continue
			}
			if !time.Now().Before(deadline) {
				return []core.Event{}, nil
			}
			return nil, err
		}
		if len(events) > 0 {
			return events, nil
		}
	}
}

func (c *Client) read(ctx context.Context, actor string, after uint64, limit int, wait time.Duration) ([]core.Event, error) {
	params := url.Values{}
	params.Set("after", strconv.FormatUint(after, 10))
	params.Set("limit", strconv.Itoa(limit))
	headers := map[string]string{actorHeader: actor}
	var events []core.Event
	if wait == 0 {
		return events, c.do(ctx, http.MethodGet, "/api/events?"+params.Encode(), headers, nil, &events)
	}
	params.Set("wait", strconv.FormatInt(int64((wait+time.Second-1)/time.Second), 10))
	waiter := *c.HTTP
	waiter.Timeout = wait + 10*time.Second
	return events, c.doWith(&waiter, ctx, http.MethodGet, "/api/events?"+params.Encode(), headers, nil, &events)
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
