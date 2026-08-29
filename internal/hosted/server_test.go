// The hosted server over httptest: management routes, credential-issued
// event API, and the 401/403 matrix. The coordination invariants behind the
// event API are the relational suite's job; here only the gate is tested.
package hosted

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/williamfzc/tearoom/internal/control"
	"github.com/williamfzc/tearoom/internal/relational"
)

const adminToken = "dev-admin-token"

type testHosted struct {
	t    *testing.T
	base string
}

func newTestHosted(t *testing.T) *testHosted {
	t.Helper()
	db, err := relational.OpenURL(filepath.Join(t.TempDir(), "hosted.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := relational.Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := control.Migrate(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Migrator().DropTable("ctl_credentials", "ctl_memberships", "ctl_accounts",
			"events", "actors", "rooms", "schema_versions", "ctl_schema_versions")
	})
	srv := httptest.NewServer(New(db, adminToken).Handler())
	t.Cleanup(srv.Close)
	return &testHosted{t: t, base: srv.URL}
}

func (h *testHosted) request(method, path, token string, body any) (int, map[string]any) {
	h.t.Helper()
	var reader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, h.base+path, reader)
	if err != nil {
		h.t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer response.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(response.Body).Decode(&out)
	return response.StatusCode, out
}

func (h *testHosted) createRoom(name string) string {
	h.t.Helper()
	status, out := h.request(http.MethodPost, "/api/hosted/rooms", adminToken, map[string]any{"name": name})
	if status != http.StatusCreated {
		h.t.Fatalf("create room: %d %v", status, out)
	}
	return out["id"].(string)
}

func (h *testHosted) issue(roomID, actor, caps string) (uint, string) {
	h.t.Helper()
	status, out := h.request(http.MethodPost,
		fmt.Sprintf("/api/hosted/rooms/%s/credentials", roomID), adminToken,
		map[string]any{"actor": actor, "capabilities": caps})
	if status != http.StatusCreated {
		h.t.Fatalf("issue credential: %d %v", status, out)
	}
	return uint(out["id"].(float64)), out["token"].(string)
}

// addActor registers the actor through the same endpoint the CLI's prepare
// step uses; a credential can only act as an actor that exists in the Room.
func (h *testHosted) addActor(roomID, token, actor string) {
	h.t.Helper()
	status, out := h.request(http.MethodPost, "/r/"+roomID+"/api/actors", token, map[string]any{"id": actor})
	if status != http.StatusOK {
		h.t.Fatalf("register %s: %d %v", actor, status, out)
	}
}

func (h *testHosted) roomStatus(method, roomID, token, actor string) int {
	h.t.Helper()
	var body any
	if method == http.MethodPost {
		body = map[string]any{"actor": actor, "type": "message", "body": "x", "expected": 0, "key": "k"}
	}
	status, _ := h.request(method, "/r/"+roomID+"/api/events?actor="+actor, token, body)
	return status
}

func TestManagementRoutesRequireAdmin(t *testing.T) {
	h := newTestHosted(t)
	status, _ := h.request(http.MethodPost, "/api/hosted/rooms", "", map[string]any{"name": "x"})
	if status != http.StatusUnauthorized {
		t.Fatalf("no-token create = %d, want 401", status)
	}
	status, _ = h.request(http.MethodPost, "/api/hosted/rooms", "wrong-token", map[string]any{"name": "x"})
	if status != http.StatusUnauthorized {
		t.Fatalf("wrong-token create = %d, want 401", status)
	}
}

func TestCredentialGatesTheEventAPI(t *testing.T) {
	h := newTestHosted(t)
	roomA := h.createRoom("a")
	roomB := h.createRoom("b")
	_, alice := h.issue(roomA, "alice", control.CapRead+","+control.CapAppend)
	_, carol := h.issue(roomA, "carol", control.CapRead)
	bobID, bob := h.issue(roomA, "bob", control.CapRead+","+control.CapAppend)
	for _, actor := range []struct {
		token, id string
	}{{alice, "alice"}, {carol, "carol"}, {bob, "bob"}} {
		h.addActor(roomA, actor.token, actor.id)
	}

	// Happy path: alice appends and reads her own room.
	if status := h.roomStatus(http.MethodGet, roomA, alice, "alice"); status != http.StatusOK {
		t.Fatalf("alice read = %d", status)
	}
	if status := h.roomStatus(http.MethodPost, roomA, alice, "alice"); status != http.StatusOK {
		t.Fatalf("alice append = %d", status)
	}

	// No token: 401.
	if status := h.roomStatus(http.MethodGet, roomA, "", "alice"); status != http.StatusUnauthorized {
		t.Fatalf("no-token = %d, want 401", status)
	}

	// A token bound to another room: 401, not 403 — a token must not probe
	// which rooms exist.
	if status := h.roomStatus(http.MethodGet, roomB, alice, "alice"); status != http.StatusUnauthorized {
		t.Fatalf("wrong-room = %d, want 401", status)
	}

	// A credential is one actor: using bob's token as alice is 403.
	if status := h.roomStatus(http.MethodGet, roomA, bob, "alice"); status != http.StatusForbidden {
		t.Fatalf("actor mismatch = %d, want 403", status)
	}
	if status := h.roomStatus(http.MethodPost, roomA, bob, "alice"); status != http.StatusForbidden {
		t.Fatalf("actor mismatch write = %d, want 403", status)
	}

	// Read-only credentials cannot append, and still read.
	if status := h.roomStatus(http.MethodPost, roomA, carol, "carol"); status != http.StatusForbidden {
		t.Fatalf("read-only append = %d, want 403", status)
	}
	if status := h.roomStatus(http.MethodGet, roomA, carol, "carol"); status != http.StatusOK {
		t.Fatalf("read-only read = %d", status)
	}

	// Revoking kills bob; alice keeps working.
	status, _ := h.request(http.MethodDelete,
		fmt.Sprintf("/api/hosted/rooms/%s/credentials/%d", roomA, bobID), adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("revoke = %d", status)
	}
	if status := h.roomStatus(http.MethodGet, roomA, bob, "bob"); status != http.StatusUnauthorized {
		t.Fatalf("revoked = %d, want 401", status)
	}
	if status := h.roomStatus(http.MethodPost, roomA, alice, "alice"); status != http.StatusOK {
		t.Fatalf("alice after revocation = %d", status)
	}
}

func TestDevLoginAnswersTheAdminToken(t *testing.T) {
	h := newTestHosted(t)
	status, out := h.request(http.MethodPost, "/api/hosted/login", adminToken, map[string]any{"provider": "dev"})
	if status != http.StatusOK || out["login"] != "dev" {
		t.Fatalf("login = %d %v", status, out)
	}
}

// An unknown room must not be distinguishable from a wrong credential: the
// admin token is not a room credential, so this answers 401, never 404.
func TestUnknownRoomDoesNotLeak(t *testing.T) {
	h := newTestHosted(t)
	status, _ := h.request(http.MethodGet, "/r/deadbeef/api/events?actor=x", adminToken, nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("unknown room = %d, want 401", status)
	}
}

func TestTwoRoomsStayIsolated(t *testing.T) {
	h := newTestHosted(t)
	roomA := h.createRoom("a")
	roomB := h.createRoom("b")
	_, alice := h.issue(roomA, "alice", control.CapRead+","+control.CapAppend)
	_, bob := h.issue(roomB, "bob", control.CapRead+","+control.CapAppend)
	h.addActor(roomA, alice, "alice")
	h.addActor(roomB, bob, "bob")

	appendBody := func(actor, key string) map[string]any {
		return map[string]any{"actor": actor, "type": "message", "body": actor, "expected": 0, "key": key}
	}
	status, _ := h.request(http.MethodPost, "/r/"+roomA+"/api/events", alice, appendBody("alice", "a1"))
	if status != http.StatusOK {
		t.Fatalf("room A append = %d", status)
	}
	// Bob's credential is for room B; same path but foreign token.
	status, _ = h.request(http.MethodPost, "/r/"+roomA+"/api/events", bob, appendBody("bob", "b1"))
	if status != http.StatusUnauthorized {
		t.Fatalf("cross-room append = %d, want 401", status)
	}
	// Room B is empty for its own credential.
	status, out := h.request(http.MethodGet, "/r/"+roomB+"/api/events?actor=bob&after=0", bob, nil)
	if status != http.StatusOK {
		t.Fatalf("room B read = %d", status)
	}
	if events, ok := out["events"].([]any); !ok || len(events) != 0 {
		t.Fatalf("room B feed = %v", out)
	}
}
