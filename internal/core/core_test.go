// Unit tests for the domain vocabulary: normalization, compaction,
// fingerprinting, and the presentation address.
package core

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestNormalizeTo(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{name: "sorts and deduplicates", in: []string{"bob", "carol", "bob", "alice"}, want: []string{"alice", "bob", "carol"}},
		{name: "trims each value", in: []string{" bob ", "carol", "alice"}, want: []string{"alice", "bob", "carol"}},
		{name: "keeps commas in an identity", in: []string{"a,b", " a,b ", "c"}, want: []string{"a,b", "c"}},
		{name: "drops empties", in: []string{"", "  ", "alice"}, want: []string{"alice"}},
		{name: "nil stays nil", in: nil, want: nil},
		{name: "empty stays empty", in: []string{}, want: []string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := NormalizeTo(c.in)
			if !equalStrings(got, c.want) {
				t.Fatalf("NormalizeTo(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

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
	base := Fingerprint("alice", []string{"bob", "carol"}, []byte(`{"x":1}`))
	compacted, err := CompactJSON(json.RawMessage(`{ "x" : 1 }`))
	if err != nil {
		t.Fatal(err)
	}
	whitespace := Fingerprint("alice", []string{"bob", "carol"}, compacted)
	if !bytes.Equal(base, whitespace) {
		t.Fatal("fingerprint changed after content compaction")
	}
	// The store normalizes To before fingerprinting, so any ordering of the
	// same members must produce the same fingerprint once normalized.
	reorderedTo := Fingerprint("alice", NormalizeTo([]string{"carol", "bob"}), []byte(`{"x":1}`))
	if !bytes.Equal(base, reorderedTo) {
		t.Fatal("fingerprint changed after To normalization")
	}
	differentContent := Fingerprint("alice", []string{"bob", "carol"}, []byte(`{"x":2}`))
	if bytes.Equal(base, differentContent) {
		t.Fatal("fingerprint ignored content")
	}
	differentActor := Fingerprint("bob", []string{"bob", "carol"}, []byte(`{"x":1}`))
	if bytes.Equal(base, differentActor) {
		t.Fatal("fingerprint ignored the actor")
	}
}

func TestEventAddress(t *testing.T) {
	if got := (Event{Seq: 42}).Address(); got != "e42" {
		t.Fatalf("Address() = %q, want e42", got)
	}
	if got := (Event{Seq: 0}).Address(); got != "e0" {
		t.Fatalf("Address() = %q, want e0", got)
	}
}
