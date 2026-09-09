// Exposed-profile contract tests: on a server constructed with credentials,
// reads stay open, writes require a valid bearer bound to its actor, joins
// and credential management are owner operations, and revocation lands
// immediately. The local profile keeps self-asserted actors.
package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/williamfzc/tearoom/internal/control"
	"github.com/williamfzc/tearoom/internal/core"
	"github.com/williamfzc/tearoom/internal/relational"
)

const ownerToken = "tr_owner_test_token"

func newExposedPair(t *testing.T) (*Client, *control.Control) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/exposed.db"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := relational.Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&relational.RoomRow{ID: "exposed", CreatedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
	st, err := relational.New(db, "exposed")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AdmitActor(context.Background(), "planner"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AdmitActor(context.Background(), "bob"); err != nil {
		t.Fatal(err)
	}
	ctl := control.New(db, nil)
	server := NewServer(st)
	server.Auth = &Auth{Control: ctl, OwnerTokenHash: control.HashToken(ownerToken)}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	return NewClient(httpServer.URL, ""), ctl
}

func issueFor(t *testing.T, client *Client, actor string) IssuedCredential {
	t.Helper()
	owner := *client
	owner.Token = ownerToken
	issued, err := owner.IssueCredential(context.Background(), IssueRequest{Actor: actor, TTLSeconds: int(24 * time.Hour / time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	return issued
}

func TestReadsStayOpenWithoutCredentials(t *testing.T) {
	client, _ := newExposedPair(t) //lint:ignore
	_ = client
	events, err := client.Read(context.Background(), "planner", 0)
	if err != nil {
		t.Fatalf("unauthenticated read = %v, want an open read", err)
	}
	if len(events) != 0 {
		t.Fatalf("read = %+v, want empty", events)
	}
}

func TestWritesRequireCredentialAndMatchActor(t *testing.T) {
	client, _ := newExposedPair(t)
	ctx := context.Background()

	// No token at all is a refusal, not an empty success.
	if _, err := client.Append(ctx, "bob", core.AppendRequest{Content: json.RawMessage(`"hi"`)}); err == nil {
		t.Fatal("unauthenticated append succeeded")
	}

	issued := issueFor(t, client, "bob")
	bob := *client
	bob.Token = issued.Token

	// The credential's actor is authoritative: a different X-Tearoom-Actor is a mismatch.
	if _, err := bob.Append(ctx, "planner", core.AppendRequest{Content: json.RawMessage(`"hi"`)}); err == nil {
		t.Fatal("actor mismatch append succeeded")
	}
	event, err := bob.Append(ctx, "bob", core.AppendRequest{Content: json.RawMessage(`"hi"`)})
	if err != nil {
		t.Fatalf("credentialed append = %v", err)
	}
	if event.Seq != 1 || event.Actor != "bob" {
		t.Fatalf("appended = %+v", event)
	}

	// The owner token is not a write credential.
	owner := *client
	owner.Token = ownerToken
	if _, err := owner.Append(ctx, "planner", core.AppendRequest{Content: json.RawMessage(`"hi"`)}); err == nil {
		t.Fatal("owner token write succeeded")
	}
}

func TestExpiredTokenRefusedAtBoundary(t *testing.T) {
	client, _ := newExposedPair(t)
	ctx := context.Background()
	owner := *client
	owner.Token = ownerToken
	issued, err := owner.IssueCredential(ctx, IssueRequest{Actor: "bob", TTLSeconds: 1})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond)

	// The expired token is refused at the HTTP boundary with 401.
	bob := NewClient(client.Base, issued.Token)
	request, _ := http.NewRequest(http.MethodPost, client.Base+"/api/events", strings.NewReader(`{"content":"late"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Tearoom-Actor", "bob")
	request.Header.Set("Authorization", "Bearer "+issued.Token)
	resp, err := client.HTTP.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expired write status = %d, want 401", resp.StatusCode)
	}

	// The refusal never masquerades as a successful append.
	if _, err := bob.Append(ctx, "bob", core.AppendRequest{Content: json.RawMessage(`"late"`)}); err == nil {
		t.Fatal("expired credential write succeeded")
	}

	// Reads stay open throughout.
	events, err := client.Read(ctx, "planner", 0)
	if err != nil || len(events) != 0 {
		t.Fatalf("open read after expiry = %+v, %v", events, err)
	}
}

func TestOwnerOperationsAndRevocation(t *testing.T) {
	client, _ := newExposedPair(t)
	ctx := context.Background()
	owner := *client
	owner.Token = ownerToken

	// Joins and issues are owner-only.
	joiner := *client
	joiner.Token = issueFor(t, client, "bob").Token
	if _, err := joiner.AdmitActor(ctx, "carol"); err == nil {
		t.Fatal("non-owner join succeeded")
	}
	stranger := *client
	stranger.Token = "tr_not_the_owner"
	if _, err := stranger.IssueCredential(ctx, IssueRequest{Actor: "bob", TTLSeconds: 60}); err == nil {
		t.Fatal("non-owner issue succeeded")
	}

	issued := issueFor(t, client, "bob")
	bob := *client
	bob.Token = issued.Token
	if _, err := bob.Append(ctx, "bob", core.AppendRequest{Content: json.RawMessage(`"first"`)}); err != nil {
		t.Fatalf("append before revocation = %v", err)
	}

	// Revocation lands immediately and leaves the history untouched.
	if err := owner.RevokeCredential(ctx, issued.ID); err != nil {
		t.Fatalf("revoke = %v", err)
	}
	if _, err := bob.Append(ctx, "bob", core.AppendRequest{Content: json.RawMessage(`"second"`)}); err == nil {
		t.Fatal("revoked credential write succeeded")
	}
	events, err := client.Read(ctx, "planner", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Seq != 1 {
		t.Fatalf("history after revocation = %+v, want only the first event", events)
	}
	if err := owner.RevokeCredential(ctx, "cred_missing"); err == nil {
		t.Fatal("revoking an unknown credential succeeded")
	}
}

func TestIssueShapeCarriesTokenOnce(t *testing.T) {
	client, _ := newExposedPair(t)
	issued := issueFor(t, client, "bob")
	if !strings.HasPrefix(issued.Token, "tr_") || issued.Actor != "bob" || issued.ID == "" || issued.ExpiresAt.IsZero() {
		t.Fatalf("issued = %+v", issued)
	}
}

func TestPlainLoopbackProfileKeepsSelfAssertedActors(t *testing.T) {
	_, st := newPair(t)
	server := httptest.NewServer(NewServer(st).Handler())
	defer server.Close()
	client := NewClient(server.URL, "")
	if _, err := client.Append(context.Background(), "planner", core.AppendRequest{Content: json.RawMessage(`"hi"`)}); err != nil {
		t.Fatalf("local append = %v", err)
	}
}

func TestRequireSafeURL(t *testing.T) {
	cases := []struct {
		raw    string
		wantOK bool
	}{
		{"http://127.0.0.1:8787", true},
		{"http://localhost:8787", true},
		{"https://tearoom.example", true},
		{"https://tearoom.example/r/room-id", true},
		{"http://192.0.2.1:8787", false},
		{"https://tearoom.example/r/room-id/api", false},
		{"https://tearoom.example?x=1", false},
		{"https://bob@tearoom.example", false},
		{"ftp://tearoom.example", false},
	}
	for _, c := range cases {
		err := RequireSafeURL(c.raw)
		if c.wantOK && err != nil {
			t.Fatalf("%s = %v, want ok", c.raw, err)
		}
		if !c.wantOK && err == nil {
			t.Fatalf("%s accepted, want refusal", c.raw)
		}
	}
}
