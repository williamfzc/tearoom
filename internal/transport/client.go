// Package transport implements core.Store for a remote Tearoom server.
// Local and remote callers therefore share exactly one domain contract.
package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/williamfzc/tearoom/internal/core"
)

type wireError struct {
	Kind     string `json:"kind"`
	Message  string `json:"message"`
	Expected uint64 `json:"expected,omitempty"`
	Actual   uint64 `json:"actual,omitempty"`
}

func EncodeError(err error) (int, wireError) {
	var stale *core.StaleWriteError
	switch {
	case errors.As(err, &stale):
		return http.StatusConflict, wireError{Kind: "stale_write", Message: err.Error(), Expected: stale.Expected, Actual: stale.Actual}
	case errors.Is(err, core.ErrNotFound):
		return http.StatusNotFound, wireError{Kind: "not_found", Message: err.Error()}
	case errors.Is(err, core.ErrUnknownActor):
		return http.StatusForbidden, wireError{Kind: "unknown_actor", Message: err.Error()}
	case errors.Is(err, core.ErrNoRoom):
		return http.StatusServiceUnavailable, wireError{Kind: "no_room", Message: err.Error()}
	case errors.Is(err, core.ErrConflict):
		return http.StatusConflict, wireError{Kind: "conflict", Message: err.Error()}
	default:
		return http.StatusBadRequest, wireError{Kind: "invalid", Message: err.Error()}
	}
}

func (w wireError) decode() error {
	switch w.Kind {
	case "stale_write":
		return &core.StaleWriteError{Expected: w.Expected, Actual: w.Actual}
	case "not_found":
		return fmt.Errorf("%w: %s", core.ErrNotFound, w.Message)
	case "unknown_actor":
		return fmt.Errorf("%w: %s", core.ErrUnknownActor, w.Message)
	case "no_room":
		return core.ErrNoRoom
	case "conflict":
		return fmt.Errorf("%w: %s", core.ErrConflict, w.Message)
	default:
		return errors.New(w.Message)
	}
}

type Client struct {
	Base  string
	Token string
	HTTP  *http.Client
}

func NewClient(base, token string) *Client {
	return &Client{Base: base, Token: token, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
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
	if c.Token != "" {
		request.Header.Set("Authorization", "Bearer "+c.Token)
	}
	response, err := c.HTTP.Do(request)
	if err != nil {
		return fmt.Errorf("tearoom server unreachable: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		var wire wireError
		if err := json.NewDecoder(response.Body).Decode(&wire); err != nil || wire.Message == "" {
			return fmt.Errorf("server returned %s", response.Status)
		}
		return wire.decode()
	}
	return json.NewDecoder(response.Body).Decode(out)
}

func (c *Client) Room(ctx context.Context) (*core.Room, error) {
	var room core.Room
	return &room, c.do(ctx, http.MethodGet, "/api/room", nil, &room)
}

func (c *Client) UpsertActor(ctx context.Context, id, role string) (*core.Actor, error) {
	var actor core.Actor
	return &actor, c.do(ctx, http.MethodPost, "/api/actors", map[string]string{"id": id, "role": role}, &actor)
}

func (c *Client) Actors(ctx context.Context) ([]*core.Actor, error) {
	var actors []*core.Actor
	return actors, c.do(ctx, http.MethodGet, "/api/actors", nil, &actors)
}

func (c *Client) Read(ctx context.Context, actor string, after uint64, limit int) (*core.Feed, error) {
	var feed core.Feed
	path := "/api/events?actor=" + url.QueryEscape(actor) + "&after=" + strconv.FormatUint(after, 10) + "&limit=" + strconv.Itoa(limit)
	return &feed, c.do(ctx, http.MethodGet, path, nil, &feed)
}

func (c *Client) AppendEvent(ctx context.Context, input core.AppendInput) (*core.Event, error) {
	var event core.Event
	return &event, c.do(ctx, http.MethodPost, "/api/events", input, &event)
}

var _ core.Store = (*Client)(nil)
