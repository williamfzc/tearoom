// Package transport carries the core.Store contract over HTTP.
// The client speaks the wire protocol the server enforces: the signature on
// the append route arrives in X-Tearoom-Author, idempotency in
// Idempotency-Key, and errors come back as the unified {"error":{...}}
// envelope.
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
	case "not_found":
		return fmt.Errorf("%w: %s", core.ErrNotFound, w.Message)
	case "no_room":
		return core.ErrNoRoom
	case "conflict":
		return fmt.Errorf("%w: %s", core.ErrConflict, w.Message)
	default:
		return errors.New(w.Message)
	}
}

// Client implements core.Store against a local Tearoom server. Local and
// HTTP callers therefore share exactly one domain contract.
type Client struct {
	Base string
	HTTP *http.Client
}

// RequireSafeURL enforces the transport boundary: plaintext http is a
// loopback-only affair, and the URL is a bare server URL — no paths, no
// query, no credentials.
func RequireSafeURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("TEAROOM_SERVER must be an http(s) URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("TEAROOM_SERVER must be a bare server URL")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return fmt.Errorf("TEAROOM_SERVER must be a bare server URL, got path %q", parsed.Path)
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

func NewClient(base string) *Client {
	return &Client{Base: strings.TrimRight(base, "/"), HTTP: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) do(ctx context.Context, method, path string, headers map[string]string, body, out any) error {
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
	response, err := c.HTTP.Do(request)
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

// CreateRoom creates one room in the served tree.
func (c *Client) CreateRoom(ctx context.Context, name, parent string) (*core.Room, error) {
	var room core.Room
	return &room, c.do(ctx, http.MethodPost, "/api/rooms", nil,
		map[string]string{"name": name, "parent": parent}, &room)
}

// Rooms lists the rooms of the served tree.
func (c *Client) Rooms(ctx context.Context) ([]core.Room, error) {
	var rooms []core.Room
	return rooms, c.do(ctx, http.MethodGet, "/api/rooms", nil, nil, &rooms)
}

// Read returns the records after the exclusive caller-owned boundary. The
// read is pure; callers that need to wait for new records poll from their
// own boundary.
func (c *Client) Read(ctx context.Context, room string, after uint64) ([]core.Record, error) {
	params := url.Values{}
	params.Set("room", room)
	params.Set("after", strconv.FormatUint(after, 10))
	var records []core.Record
	return records, c.do(ctx, http.MethodGet, "/api/events?"+params.Encode(), nil, nil, &records)
}

// Append contributes one record with the given signature. The request body
// carries only room and content; the idempotency key, when set, travels in
// the Idempotency-Key header.
func (c *Client) Append(ctx context.Context, room, author string, request core.AppendRequest) (*core.Record, error) {
	var record core.Record
	headers := map[string]string{authorHeader: author, idempotencyHeader: request.IdempotencyKey}
	return &record, c.do(ctx, http.MethodPost, "/api/events", headers,
		map[string]any{"room": room, "content": request.Content}, &record)
}

var _ core.Store = (*Client)(nil)
