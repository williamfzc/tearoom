// records.go is the stream surface: read one Room's records from a caller
// boundary, and append one record under the workspace lock. records.jsonl is
// append-only — one JSON record per line, in sequence order.
package store

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/williamfzc/tearoom/internal/core"
)

// Read returns the records of one room after the caller-owned exclusive
// boundary. Repeated reads are side-effect-free; no cursor is persisted.
func (s *Store) Read(ctx context.Context, room string, after uint64) ([]core.Record, error) {
	room = strings.TrimSpace(room)
	unlock, err := s.lock(false)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := s.requireRoomLocked(room); err != nil {
		return nil, err
	}
	records, err := s.readRecordsLocked(room)
	if err != nil {
		return nil, err
	}
	out := make([]core.Record, 0, len(records))
	for _, record := range records {
		if record.Seq > after {
			out = append(out, record)
		}
	}
	return out, nil
}

// Append accepts one contribution and assigns the next dense sequence of the
// target room. The workspace lock is held across room verification,
// idempotency resolution, sequence assignment, and the append, so concurrent
// processes receive one dense sequence with no loss or duplication. The
// author is stored verbatim after whitespace trimming; an empty author is
// valid.
func (s *Store) Append(ctx context.Context, room, author string, request core.AppendRequest) (*core.Record, error) {
	room = strings.TrimSpace(room)
	author = strings.TrimSpace(author)
	content, err := core.CompactJSON(request.Content)
	if err != nil {
		return nil, fmt.Errorf("invalid content: %w", err)
	}
	key := strings.TrimSpace(request.IdempotencyKey)
	fingerprint := hex.EncodeToString(core.Fingerprint(author, content))

	unlock, err := s.lock(true)
	if err != nil {
		return nil, err
	}
	defer unlock()

	if err := s.requireRoomLocked(room); err != nil {
		return nil, err
	}
	if err := s.recoverAppendLocked(room); err != nil {
		return nil, err
	}

	// Resolve an existing idempotency key: an equal fingerprint returns the
	// original record, a different one conflicts.
	if key != "" {
		ledger, err := s.readLedgerLocked(room)
		if err != nil {
			return nil, err
		}
		for _, entry := range ledger {
			if entry.Author != author || entry.Key != key {
				continue
			}
			if entry.Fingerprint != fingerprint {
				return nil, fmt.Errorf("%w: idempotency key %q already describes another record", core.ErrConflict, key)
			}
			records, err := s.readRecordsLocked(room)
			if err != nil {
				return nil, err
			}
			for i := range records {
				if records[i].Seq == entry.Seq {
					return &records[i], nil
				}
			}
			return nil, fmt.Errorf("idempotency key %q lost its original record", key)
		}
	}

	// Assign the next dense sequence from the stream itself: the last line's
	// seq plus one. The lock makes this safe across processes.
	records, err := s.readRecordsLocked(room)
	if err != nil {
		return nil, err
	}
	seq := uint64(0)
	if len(records) > 0 {
		seq = records[len(records)-1].Seq
	}
	record := core.Record{
		Room:    room,
		Seq:     seq + 1,
		Author:  author,
		At:      s.now().UTC(),
		Content: content,
	}
	if key != "" {
		entry := ledgerFile{Author: author, Key: key, Fingerprint: fingerprint, Seq: record.Seq}
		if err := s.writePendingAppend(room, pendingAppend{Record: record, Ledger: entry}); err != nil {
			return nil, err
		}
		if err := s.recoverAppendLocked(room); err != nil {
			return nil, err
		}
		return &record, nil
	}
	if err := appendJSONLine(s.recordsPath(room), record); err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *Store) roomDir(room string) string {
	return filepath.Join(s.root, roomsDir, room)
}

func (s *Store) recordsPath(room string) string {
	return filepath.Join(s.roomDir(room), "records.jsonl")
}

func (s *Store) ledgerPath(room string) string {
	return filepath.Join(s.roomDir(room), "idempotency.jsonl")
}

// requireRoomLocked refuses an unknown room with the store's not-found error.
func (s *Store) requireRoomLocked(room string) error {
	if _, err := os.Stat(filepath.Join(s.roomDir(room), "room.json")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("room %q: %w", room, core.ErrNotFound)
		}
		return err
	}
	return nil
}

// readRecordsLocked decodes the stream. A line that fails to parse, or that
// names another room, is storage corruption and fails closed.
func (s *Store) readRecordsLocked(room string) ([]core.Record, error) {
	data, err := os.ReadFile(s.recordsPath(room))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	records := []core.Record{}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var record core.Record
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, fmt.Errorf("read %s: corrupt record line: %w", s.recordsPath(room), err)
		}
		if record.Room != room {
			return nil, fmt.Errorf("read %s: record %d names room %q", s.recordsPath(room), record.Seq, record.Room)
		}
		records = append(records, record)
	}
	return records, nil
}

// readLedgerLocked decodes the retry ledger.
func (s *Store) readLedgerLocked(room string) ([]ledgerFile, error) {
	data, err := os.ReadFile(s.ledgerPath(room))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	entries := []ledgerFile{}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry ledgerFile
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil, fmt.Errorf("read %s: corrupt ledger line: %w", s.ledgerPath(room), err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// appendJSONLine writes one JSON object as one line, durably: a single
// O_APPEND write of the complete line, then a file sync.
func appendJSONLine(path string, value any) error {
	data, err := marshalStoredJSON(value)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	n, err := f.Write(data)
	if err != nil {
		_ = f.Close()
		return err
	}
	if n != len(data) {
		_ = f.Close()
		return io.ErrShortWrite
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// marshalStoredJSON keeps raw content byte-identical to the fingerprint while
// retaining the existing encoding of record metadata and ledger entries.
func marshalStoredJSON(value any) ([]byte, error) {
	switch v := value.(type) {
	case pendingAppend:
		record, err := marshalStoredJSON(v.Record)
		if err != nil {
			return nil, err
		}
		ledger, err := json.Marshal(v.Ledger)
		if err != nil {
			return nil, err
		}
		data := append([]byte(`{"record":`), record...)
		data = append(data, []byte(`,"ledger":`)...)
		data = append(data, ledger...)
		return append(data, '}'), nil
	case core.Record:
		if !json.Valid(v.Content) {
			return nil, fmt.Errorf("invalid record content")
		}
		content := v.Content
		v.Content = nil
		data, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		const placeholder = `"content":null}`
		if !bytes.HasSuffix(data, []byte(placeholder)) {
			return nil, errors.New("record JSON is missing its content field")
		}
		data = data[:len(data)-len(placeholder)]
		data = append(data, []byte(`"content":`)...)
		data = append(data, content...)
		return append(data, '}'), nil
	default:
		return json.Marshal(value)
	}
}
