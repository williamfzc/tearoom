// Unit tests for the domain vocabulary: compaction, fingerprinting, and the
// presentation address.
package core

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestCompactJSON(t *testing.T) {
	compacted, err := CompactJSON(json.RawMessage("{\n  \"a\" : 1,\n  \"b\": [ 1, 2 ]\n}"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(compacted, json.RawMessage(`{"a":1,"b":[1,2]}`)) {
		t.Fatalf("compacted = %s", compacted)
	}
	if _, err := CompactJSON(nil); err == nil {
		t.Fatal("empty content accepted")
	}
	if _, err := CompactJSON(json.RawMessage("{")); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	null, err := CompactJSON(json.RawMessage(" null "))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(null, json.RawMessage("null")) {
		t.Fatalf("null content = %s", null)
	}
}

func TestFingerprint(t *testing.T) {
	// Fingerprint compares compacted content bytes; the store compacts before
	// fingerprinting, so equal logical content in different whitespace forms
	// must produce the same fingerprint once compacted.
	base := Fingerprint("codex", []byte(`{"x":1}`))
	compacted, err := CompactJSON(json.RawMessage(`{ "x" : 1 }`))
	if err != nil {
		t.Fatal(err)
	}
	whitespace := Fingerprint("codex", compacted)
	if !bytes.Equal(base, whitespace) {
		t.Fatal("fingerprint changed after content compaction")
	}
	differentContent := Fingerprint("codex", []byte(`{"x":2}`))
	if bytes.Equal(base, differentContent) {
		t.Fatal("fingerprint ignored content")
	}
	differentAuthor := Fingerprint("other", []byte(`{"x":1}`))
	if bytes.Equal(base, differentAuthor) {
		t.Fatal("fingerprint ignored the signature")
	}
}

func TestRecordAddress(t *testing.T) {
	if got := (Record{Seq: 42}).Address(); got != "e42" {
		t.Fatalf("Address() = %q, want e42", got)
	}
	if got := (Record{Seq: 0}).Address(); got != "e0" {
		t.Fatalf("Address() = %q, want e0", got)
	}
}

func TestNotificationEnvelope(t *testing.T) {
	record := Record{Room: "implementation", Seq: 7, Author: "codex"}
	n := record.Notification()
	if n.Room != "implementation" || n.Seq != 7 || n.Author != "codex" {
		t.Fatalf("notification = %+v", n)
	}
}
