// Package transport carries core.Store over HTTP.
//
// Client implements core.Store, so code above the interface cannot tell whether it
// is talking to local files or a remote server. That is what makes the browser
// build possible: the web console compiles the same view package against a Client
// instead of a File, and no view logic is duplicated in JavaScript.
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

// wireError travels in both directions. Sentinel errors are reconstructed on the
// client so callers keep branching on errors.Is rather than inspecting strings —
// including ConflictError, whose holder and expiry are the whole reason a rejected
// claim is actionable.
type wireError struct {
	Kind     string    `json:"kind"`
	Message  string    `json:"message"`
	Resource string    `json:"resource,omitempty"`
	Holder   string    `json:"holder,omitempty"`
	Expires  time.Time `json:"expires,omitempty"`
}

// EncodeError maps a Go error onto the wire.
func EncodeError(err error) (int, wireError) {
	var ce *core.ConflictError
	switch {
	case errors.As(err, &ce):
		return http.StatusConflict, wireError{
			Kind: "conflict", Message: ce.Error(),
			Resource: ce.Resource, Holder: ce.Holder, Expires: ce.Expires,
		}
	case errors.Is(err, core.ErrNotFound):
		return http.StatusNotFound, wireError{Kind: "not_found", Message: err.Error()}
	case errors.Is(err, core.ErrNotHolder):
		return http.StatusForbidden, wireError{Kind: "not_holder", Message: err.Error()}
	case errors.Is(err, core.ErrNoWorkspace):
		return http.StatusServiceUnavailable, wireError{Kind: "no_workspace", Message: err.Error()}
	case errors.Is(err, core.ErrNeedEvidence):
		return http.StatusUnprocessableEntity, wireError{Kind: "need_evidence", Message: err.Error()}
	default:
		return http.StatusBadRequest, wireError{Kind: "invalid", Message: err.Error()}
	}
}

func (w wireError) decode() error {
	switch w.Kind {
	case "conflict":
		return &core.ConflictError{Resource: w.Resource, Holder: w.Holder, Expires: w.Expires}
	case "not_found":
		return fmt.Errorf("%w: %s", core.ErrNotFound, w.Message)
	case "not_holder":
		return fmt.Errorf("%w: %s", core.ErrNotHolder, w.Message)
	case "no_workspace":
		return core.ErrNoWorkspace
	case "need_evidence":
		return core.ErrNeedEvidence
	default:
		return errors.New(w.Message)
	}
}

// Client is a core.Store backed by a Tearoom server.
type Client struct {
	Base  string
	Token string
	HTTP  *http.Client
}

func NewClient(base, token string) *Client {
	return &Client{
		Base:  base,
		Token: token,
		HTTP:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		// A transport failure must never look like a successful no-op. An agent
		// that believes it holds a lease it does not hold is worse than a crash.
		return fmt.Errorf("tearoom server unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var we wireError
		if err := json.NewDecoder(resp.Body).Decode(&we); err != nil || we.Message == "" {
			return fmt.Errorf("server returned %s", resp.Status)
		}
		return we.decode()
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// GetJSON fetches an arbitrary read-only endpoint into v.
//
// Exists for /api/board, which is not part of core.Store: the board is a derived
// view, not stored state. The browser console needs it, and letting it reuse the
// same auth and error decoding as every other call is better than giving the web
// build its own HTTP path.
func (c *Client) GetJSON(ctx context.Context, path string, v any) error {
	return c.do(ctx, "GET", path, nil, v)
}

func (c *Client) Workspace(ctx context.Context) (*core.Workspace, error) {
	var ws core.Workspace
	return &ws, c.do(ctx, "GET", "/api/workspace", nil, &ws)
}

func (c *Client) UpsertAgent(ctx context.Context, id, role string) (*core.Agent, error) {
	var a core.Agent
	return &a, c.do(ctx, "POST", "/api/agents", map[string]string{"id": id, "role": role}, &a)
}

func (c *Client) Agents(ctx context.Context) ([]*core.Agent, error) {
	var out []*core.Agent
	return out, c.do(ctx, "GET", "/api/agents", nil, &out)
}

func (c *Client) CreateThread(ctx context.Context, agent, title, body string) (*core.Thread, error) {
	var t core.Thread
	return &t, c.do(ctx, "POST", "/api/threads",
		map[string]string{"agent": agent, "title": title, "body": body}, &t)
}

func (c *Client) Thread(ctx context.Context, id string) (*core.Thread, error) {
	var t core.Thread
	return &t, c.do(ctx, "GET", "/api/threads/"+url.PathEscape(id), nil, &t)
}

func (c *Client) Threads(ctx context.Context) ([]*core.Thread, error) {
	var out []*core.Thread
	return out, c.do(ctx, "GET", "/api/threads", nil, &out)
}

func (c *Client) AppendEvent(ctx context.Context, threadID, agent, body string, status core.ThreadStatus, evidence []string) (*core.Thread, error) {
	var t core.Thread
	return &t, c.do(ctx, "POST", "/api/threads/"+url.PathEscape(threadID)+"/events", map[string]any{
		"agent": agent, "body": body, "status": status, "evidence": evidence,
	}, &t)
}

func (c *Client) Claim(ctx context.Context, agent, resource, threadID string, ttl time.Duration) (*core.Lease, error) {
	var l core.Lease
	return &l, c.do(ctx, "POST", "/api/leases", map[string]any{
		"agent": agent, "resource": resource, "thread": threadID, "ttl": ttl.String(),
	}, &l)
}

func (c *Client) Release(ctx context.Context, agent, resource string) error {
	return c.do(ctx, "POST", "/api/leases/release",
		map[string]string{"agent": agent, "resource": resource}, nil)
}

func (c *Client) Leases(ctx context.Context) ([]*core.Lease, error) {
	var out []*core.Lease
	return out, c.do(ctx, "GET", "/api/leases", nil, &out)
}

func (c *Client) CreateGate(ctx context.Context, agent, reason, threadID string) (*core.Gate, error) {
	var g core.Gate
	return &g, c.do(ctx, "POST", "/api/gates",
		map[string]string{"agent": agent, "reason": reason, "thread": threadID}, &g)
}

func (c *Client) DecideGate(ctx context.Context, id string, state core.GateState, by, note string) (*core.Gate, error) {
	var g core.Gate
	return &g, c.do(ctx, "POST", "/api/gates/"+url.PathEscape(id)+"/decide",
		map[string]any{"state": state, "by": by, "note": note}, &g)
}

func (c *Client) Gate(ctx context.Context, id string) (*core.Gate, error) {
	var g core.Gate
	return &g, c.do(ctx, "GET", "/api/gates/"+url.PathEscape(id), nil, &g)
}

func (c *Client) Gates(ctx context.Context) ([]*core.Gate, error) {
	var out []*core.Gate
	return out, c.do(ctx, "GET", "/api/gates", nil, &out)
}

func (c *Client) Append(ctx context.Context, e core.LedgerEntry) error {
	return c.do(ctx, "POST", "/api/ledger", e, nil)
}

func (c *Client) Ledger(ctx context.Context, tail int) ([]core.LedgerEntry, error) {
	var out []core.LedgerEntry
	return out, c.do(ctx, "GET", "/api/ledger?tail="+strconv.Itoa(tail), nil, &out)
}

// compile-time proof that the remote client is substitutable for the local store
var _ core.Store = (*Client)(nil)
