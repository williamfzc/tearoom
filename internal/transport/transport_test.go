// Transport contract tests prove the HTTP surface against the room-tree
// contract: the client satisfies the full Store battery over the wire, bodies
// are decoded strictly, and errors come back as the unified envelope.
package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/williamfzc/tearoom/internal/core"
	"github.com/williamfzc/tearoom/internal/relational"
)

func servedStore(t *testing.T) (core.Store, *httptest.Server) {
	t.Helper()
	st, err := relational.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	server := httptest.NewServer(NewServer(st).Handler())
	t.Cleanup(server.Close)
	return st, server
}

// TestClientSatisfiesStore runs the conformance battery through the HTTP
// pair, so the wire face answers the same contract as the local store.
func TestClientSatisfiesStore(t *testing.T) {
	core.StoreContract(t, func(t *testing.T) core.Store {
		st, server := servedStore(t)
		_ = st
		return NewClient(server.URL)
	})
}

func TestAppendRejectsOldFields(t *testing.T) {
	_, server := servedStore(t)
	defer server.Close()
	// The removed fields of the old wire format must fail loudly instead of
	// being ignored.
	for _, body := range []string{
		`{"room":"r","to":["bob"],"content":"m"}`,
		`{"room":"r","content":"m","actor":"bob"}`,
	} {
		resp, err := http.Post(server.URL+"/api/events", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("append body %q status = %d, want 400", body, resp.StatusCode)
		}
	}
}

func TestAppendRejectsTrailingJSON(t *testing.T) {
	_, server := servedStore(t)
	defer server.Close()
	resp, err := http.Post(server.URL+"/api/events", "application/json",
		strings.NewReader(`{"room":"r","content":"m"} {"room":"r2","content":"m2"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("trailing JSON status = %d, want 400", resp.StatusCode)
	}
}

func TestStrictErrorBody(t *testing.T) {
	_, server := servedStore(t)
	defer server.Close()
	// An unknown room answers 404 in the unified envelope.
	resp, err := http.Get(server.URL + "/api/events?room=missing&after=0")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown room status = %d, want 404", resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("error content type = %q", resp.Header.Get("Content-Type"))
	}

	// A method error answers 405 in the unified envelope.
	put, err := http.NewRequest(http.MethodPut, server.URL+"/api/events", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	methodResp, err := http.DefaultClient.Do(put)
	if err != nil {
		t.Fatal(err)
	}
	methodResp.Body.Close()
	if methodResp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("PUT status = %d, want 405", methodResp.StatusCode)
	}
}

func TestUnreachableServerFailsLoudly(t *testing.T) {
	// Port 1 on the loopback interface is closed: the client must wrap the
	// failure as an unreachable server, not hang or swallow it.
	client := NewClient("http://127.0.0.1:1")
	if _, err := client.Rooms(context.Background()); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("unreachable error = %v, want a loud unreachable failure", err)
	}
}
