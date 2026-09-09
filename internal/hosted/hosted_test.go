// Hosted contract tests: one process serves many rooms of one database.
// Room creation is owner-only, room URLs scope the Room contract, a
// credential is bound to its room, and reads stay open throughout. Runs on a
// SQLite deployment database; PostgreSQL rides the same code path.
package hosted

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/williamfzc/tearoom/internal/core"
	"github.com/williamfzc/tearoom/internal/relational"
	"github.com/williamfzc/tearoom/internal/transport"
)

const ownerToken = "tr_owner_hosted_test"

func newHosted(t *testing.T) *transport.Client {
	t.Helper()
	db, err := relational.OpenDatabase(filepath.Join(t.TempDir(), "hosted.db"))
	if err != nil {
		t.Fatal(err)
	}
	server, err := New(db, ownerToken, nil)
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	return transport.NewClient(httpServer.URL, ownerToken)
}

// post sends one JSON body and returns the status with the response bytes.
func post(t *testing.T, client *transport.Client, path string, body any) (int, []byte) {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, client.Base+path, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if client.Token != "" {
		request.Header.Set("Authorization", "Bearer "+client.Token)
	}
	resp, err := client.HTTP.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, out
}

func TestHostedRoomLifecycle(t *testing.T) {
	client := newHosted(t)
	ctx := context.Background()

	// Room creation is owner-only and answers a room-scoped URL.
	if code, _ := post(t, transport.NewClient(client.Base, "tr_wrong"), "/api/rooms", map[string]string{}); code != 403 {
		t.Fatalf("non-owner room creation status = %d, want 403", code)
	}
	code, body := post(t, client, "/api/rooms", map[string]string{})
	if code != 200 {
		t.Fatalf("room creation status = %d, body %s", code, body)
	}
	var room struct {
		ID      string `json:"id"`
		Created string `json:"created"`
		Server  string `json:"server"`
	}
	if err := json.Unmarshal(body, &room); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(room.Server, client.Base+"/r/room_") {
		t.Fatalf("room server URL = %q", room.Server)
	}

	// The owner admits bob in that room and issues him a credential.
	roomC := transport.NewClient(room.Server, ownerToken)
	if _, err := roomC.AdmitActor(ctx, "bob"); err != nil {
		t.Fatalf("owner join = %v", err)
	}
	issued, err := roomC.IssueCredential(ctx, transport.IssueRequest{Actor: "bob", TTLSeconds: 3600})
	if err != nil {
		t.Fatalf("issue = %v", err)
	}

	// bob writes with his credential; reads stay open without any token.
	bob := transport.NewClient(room.Server, issued.Token)
	event, err := bob.Append(ctx, "bob", core.AppendRequest{Content: json.RawMessage(`"from bob"`)})
	if err != nil {
		t.Fatalf("credentialed append = %v", err)
	}
	if event.Seq != 1 {
		t.Fatalf("event = %+v", event)
	}
	open := transport.NewClient(room.Server, "")
	read, err := open.Read(ctx, "bob", 0)
	if err != nil || len(read) != 1 {
		t.Fatalf("open read = %+v, %v", read, err)
	}

	// The credential does not cross rooms, and revocation is immediate.
	_, otherBody := post(t, client, "/api/rooms", map[string]string{})
	var other struct {
		Server string `json:"server"`
	}
	if err := json.Unmarshal(otherBody, &other); err != nil {
		t.Fatal(err)
	}
	stranger := transport.NewClient(other.Server, issued.Token)
	if _, err := stranger.Append(ctx, "bob", core.AppendRequest{Content: json.RawMessage(`"cross"`)}); err == nil {
		t.Fatal("credential wrote across rooms")
	}
	if err := roomC.RevokeCredential(ctx, issued.ID); err != nil {
		t.Fatalf("revoke = %v", err)
	}
	if _, err := bob.Append(ctx, "bob", core.AppendRequest{Content: json.RawMessage(`"after revoke"`)}); err == nil {
		t.Fatal("revoked credential wrote")
	}

	// Unknown rooms are 404, not a leak.
	ghost := transport.NewClient(client.Base+"/r/room_missing", "")
	if _, err := ghost.Read(ctx, "bob", 0); err == nil {
		t.Fatal("unknown room read succeeded")
	}
}

func TestHostedRequiresOwnerToken(t *testing.T) {
	db, err := relational.OpenDatabase(filepath.Join(t.TempDir(), "hosted.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(db, "", nil); err == nil {
		t.Fatal("hosted without an owner token constructed")
	}
}
