// records_test.go proves keyed append recovery at the filesystem boundary.
package store

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/williamfzc/tearoom/internal/core"
)

func TestAppendRetryRecoversAfterRecordWasWrittenButLedgerFailed(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateRoom(ctx, "main-line", ""); err != nil {
		t.Fatal(err)
	}
	store := st.(*Store)
	request := core.AppendRequest{Content: json.RawMessage(`{"work":true}`), IdempotencyKey: "retry-1"}
	content, err := core.CompactJSON(request.Content)
	if err != nil {
		t.Fatal(err)
	}
	record := core.Record{Room: "main-line", Seq: 1, Author: "agent", At: time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC), Content: content}
	entry := ledgerFile{Author: "agent", Key: request.IdempotencyKey, Fingerprint: hex.EncodeToString(core.Fingerprint("agent", content)), Seq: 1}
	if err := store.writePendingAppend("main-line", pendingAppend{Record: record, Ledger: entry}); err != nil {
		t.Fatal(err)
	}
	if err := appendJSONLine(store.recordsPath("main-line"), record); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(store.ledgerPath("main-line"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Append(ctx, "main-line", "agent", request); err == nil {
		t.Fatal("recovery succeeded while the ledger path was a directory")
	}
	if _, err := os.Stat(store.pendingPath("main-line")); err != nil {
		t.Fatalf("pending recovery intent missing: %v", err)
	}
	if err := os.Remove(store.ledgerPath("main-line")); err != nil {
		t.Fatal(err)
	}
	first, err := st.Append(ctx, "main-line", "agent", request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.Append(ctx, "main-line", "agent", request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Seq != 1 || second.Seq != 1 {
		t.Fatalf("retry sequences = %d, %d; want same original sequence 1", first.Seq, second.Seq)
	}
	records, err := st.Read(ctx, "main-line", 0)
	if err != nil || len(records) != 1 {
		t.Fatalf("records = %+v, %v; want exactly one", records, err)
	}
	if _, err := os.Stat(store.pendingPath("main-line")); !os.IsNotExist(err) {
		t.Fatalf("pending intent remains after recovery: %v", err)
	}
}

func TestAppendRecoveryRepairsOnlyIncompleteTail(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateRoom(ctx, "main-line", ""); err != nil {
		t.Fatal(err)
	}
	store := st.(*Store)
	content := json.RawMessage(`{"recovered":true}`)
	compacted, err := core.CompactJSON(content)
	if err != nil {
		t.Fatal(err)
	}
	record := core.Record{Room: "main-line", Seq: 1, Author: "agent", At: time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC), Content: compacted}
	entry := ledgerFile{Author: "agent", Key: "retry-2", Fingerprint: hex.EncodeToString(core.Fingerprint("agent", compacted)), Seq: 1}
	if err := store.writePendingAppend("main-line", pendingAppend{Record: record, Ledger: entry}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.recordsPath("main-line"), []byte(`{"room":"main-line"`), 0o644); err != nil {
		t.Fatal(err)
	}
	request := core.AppendRequest{Content: content, IdempotencyKey: entry.Key}
	got, err := st.Append(ctx, "main-line", "agent", request)
	if err != nil {
		t.Fatal(err)
	}
	if got.Seq != 1 {
		t.Fatalf("recovered seq = %d, want 1", got.Seq)
	}
	records, err := st.Read(ctx, "main-line", 0)
	if err != nil || len(records) != 1 || records[0].Seq != 1 {
		t.Fatalf("records = %+v, %v; want one recovered record", records, err)
	}
	data, err := os.ReadFile(filepath.Join(store.roomDir("main-line"), "records.jsonl"))
	if err != nil || len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatalf("record stream after recovery = %q, %v; want complete JSONL", data, err)
	}
}

func TestAppendRecoveryPreservesUnrelatedIncompleteTail(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateRoom(ctx, "main-line", ""); err != nil {
		t.Fatal(err)
	}
	store := st.(*Store)
	content := json.RawMessage(`{"intended":true}`)
	compacted, err := core.CompactJSON(content)
	if err != nil {
		t.Fatal(err)
	}
	record := core.Record{Room: "main-line", Seq: 1, Author: "agent", At: time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC), Content: compacted}
	entry := ledgerFile{Author: "agent", Key: "retry-3", Fingerprint: hex.EncodeToString(core.Fingerprint("agent", compacted)), Seq: 1}
	if err := store.writePendingAppend("main-line", pendingAppend{Record: record, Ledger: entry}); err != nil {
		t.Fatal(err)
	}
	tail := []byte(`{"unrelated":`)
	if err := os.WriteFile(store.recordsPath("main-line"), tail, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Append(ctx, "main-line", "agent", core.AppendRequest{Content: content, IdempotencyKey: entry.Key}); err == nil {
		t.Fatal("recovery accepted an incomplete tail unrelated to the pending append")
	}
	got, err := os.ReadFile(store.recordsPath("main-line"))
	if err != nil || !bytes.Equal(got, tail) {
		t.Fatalf("unrelated tail changed to %q, %v", got, err)
	}
}

func TestAppendRecoveryRejectsFutureSequenceBeforeRepair(t *testing.T) {
	ctx := context.Background()
	st, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateRoom(ctx, "project", ""); err != nil {
		t.Fatal(err)
	}
	s := st.(*Store)
	content := json.RawMessage(`"intended"`)
	record := core.Record{Room: "project", Seq: 99, Author: "builder", At: time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC), Content: content}
	entry := ledgerFile{Author: "builder", Key: "future", Fingerprint: hex.EncodeToString(core.Fingerprint("builder", content)), Seq: record.Seq}
	if err := s.writePendingAppend("project", pendingAppend{Record: record, Ledger: entry}); err != nil {
		t.Fatal(err)
	}
	for seq := uint64(1); seq <= 2; seq++ {
		committed := core.Record{Room: "project", Seq: seq, Author: "prior", At: time.Date(2026, 11, 7, 0, 0, int(seq), 0, time.UTC), Content: json.RawMessage(`"prior"`)}
		if err := appendJSONLine(s.recordsPath("project"), committed); err != nil {
			t.Fatal(err)
		}
	}
	line, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	committedBytes, err := os.ReadFile(s.recordsPath("project"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.recordsPath("project"), append(committedBytes, line[:18]...), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(s.recordsPath("project"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Append(ctx, "project", "builder", core.AppendRequest{Content: content, IdempotencyKey: entry.Key}); err == nil {
		t.Fatal("recovery accepted an intent whose sequence skips the stream head")
	}
	after, err := os.ReadFile(s.recordsPath("project"))
	if err != nil || !bytes.Equal(after, before) {
		t.Fatalf("stream changed before invalid sequence was rejected: %q, %v", after, err)
	}
}

func TestAppendRecoveryFinishesCompleteUnterminatedEntries(t *testing.T) {
	ctx := context.Background()
	st, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateRoom(ctx, "project", ""); err != nil {
		t.Fatal(err)
	}
	s := st.(*Store)
	content := json.RawMessage(`"work"`)
	record := core.Record{Room: "project", Seq: 1, Author: "builder", At: time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC), Content: content}
	entry := ledgerFile{Author: "builder", Key: "complete-tail", Fingerprint: hex.EncodeToString(core.Fingerprint("builder", content)), Seq: 1}
	if err := s.writePendingAppend("project", pendingAppend{Record: record, Ledger: entry}); err != nil {
		t.Fatal(err)
	}
	recordLine, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	ledgerLine, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.recordsPath("project"), recordLine, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.ledgerPath("project"), ledgerLine, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := st.Append(ctx, "project", "builder", core.AppendRequest{Content: content, IdempotencyKey: entry.Key})
	if err != nil || got.Seq != 1 {
		t.Fatalf("retry = %+v, %v; want recovered sequence 1", got, err)
	}
	for _, path := range []string{s.recordsPath("project"), s.ledgerPath("project")} {
		data, err := os.ReadFile(path)
		if err != nil || len(data) == 0 || data[len(data)-1] != '\n' {
			t.Fatalf("recovered file %s = %q, %v; want newline terminated", path, data, err)
		}
	}
}

func TestAppendRecoveryRejectsInconsistentIntent(t *testing.T) {
	for _, change := range []string{"author", "fingerprint"} {
		t.Run(change, func(t *testing.T) {
			ctx := context.Background()
			st, err := Init(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st.CreateRoom(ctx, "project", ""); err != nil {
				t.Fatal(err)
			}
			s := st.(*Store)
			content := json.RawMessage(`"work"`)
			intent := pendingAppend{
				Record: core.Record{Room: "project", Seq: 1, Author: "builder", At: time.Now().UTC(), Content: content},
				Ledger: ledgerFile{Author: "builder", Key: "k", Seq: 1, Fingerprint: hex.EncodeToString(core.Fingerprint("builder", content))},
			}
			if change == "author" {
				intent.Ledger.Author = "reviewer"
			} else {
				intent.Ledger.Fingerprint = "wrong"
			}
			if err := s.writePendingAppend("project", intent); err != nil {
				t.Fatal(err)
			}
			if _, err := st.Append(ctx, "project", "builder", core.AppendRequest{Content: content, IdempotencyKey: "k"}); err == nil {
				t.Fatal("inconsistent intent was accepted")
			}
			records, err := st.Read(ctx, "project", 0)
			if err != nil || len(records) != 0 {
				t.Fatalf("records = %v, %v; want none", records, err)
			}
		})
	}
}

func TestKeyedAppendDoesNotClaimExistingIncompleteRecord(t *testing.T) {
	ctx := context.Background()
	st, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateRoom(ctx, "project", ""); err != nil {
		t.Fatal(err)
	}
	s := st.(*Store)
	tail := []byte(`{"room":"project"`)
	if err := os.WriteFile(s.recordsPath("project"), tail, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Append(ctx, "project", "builder", core.AppendRequest{Content: json.RawMessage(`"work"`), IdempotencyKey: "k"}); err == nil {
		t.Fatal("append accepted a pre-existing partial record")
	}
	got, err := os.ReadFile(s.recordsPath("project"))
	if err != nil || !bytes.Equal(got, tail) {
		t.Fatalf("partial record changed: %q, %v", got, err)
	}
	if _, err := os.Stat(s.pendingPath("project")); !os.IsNotExist(err) {
		t.Fatalf("unexpected intent: %v", err)
	}
}
