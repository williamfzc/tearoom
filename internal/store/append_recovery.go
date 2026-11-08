// append_recovery.go owns durable intent and replay for keyed JSONL appends.
package store

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/williamfzc/tearoom/internal/core"
)

// pendingAppend is a durable intent for the two-file keyed append. It lets a
// later writer finish the record and retry ledger after an interrupted write.
type pendingAppend struct {
	Record core.Record `json:"record"`
	Ledger ledgerFile  `json:"ledger"`
}

func (s *Store) pendingPath(room string) string {
	return filepath.Join(s.roomDir(room), "append-pending.json")
}

func (s *Store) writePendingAppend(room string, pending pendingAppend) error {
	data, err := json.Marshal(pending)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return durableReplace(s.pendingPath(room), data, 0o644)
}

// recoverAppendLocked completes a keyed append whose intent survived a crash.
// The workspace lock is held by the caller. An incomplete tail is repaired
// only when it is a prefix of this intent's exact JSONL entry.
func (s *Store) recoverAppendLocked(room string) error {
	path := s.pendingPath(room)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var pending pendingAppend
	if err := json.Unmarshal(data, &pending); err != nil {
		return fmt.Errorf("read %s: corrupt pending append: %w", path, err)
	}
	content, contentErr := core.CompactJSON(pending.Record.Content)
	if pending.Record.Room != room || pending.Record.Seq == 0 || pending.Ledger.Seq != pending.Record.Seq || pending.Ledger.Key == "" ||
		pending.Ledger.Author != pending.Record.Author || contentErr != nil || !bytes.Equal(content, pending.Record.Content) ||
		pending.Ledger.Fingerprint != hex.EncodeToString(core.Fingerprint(pending.Record.Author, content)) {
		return fmt.Errorf("read %s: invalid pending append", path)
	}
	if err := validatePendingRecordStream(s.recordsPath(room), room, pending.Record); err != nil {
		return err
	}
	if err := validatePendingLedger(s.ledgerPath(room), pending.Ledger); err != nil {
		return err
	}
	if err := repairPendingJSONL(s.recordsPath(room), pending.Record, func(tail []byte) (bool, error) {
		var record core.Record
		if err := json.Unmarshal(tail, &record); err != nil {
			return false, nil
		}
		return sameRecord(record, pending.Record), nil
	}); err != nil {
		return err
	}
	if err := repairPendingJSONL(s.ledgerPath(room), pending.Ledger, func(tail []byte) (bool, error) {
		var entry ledgerFile
		if err := json.Unmarshal(tail, &entry); err != nil {
			return false, nil
		}
		return entry == pending.Ledger, nil
	}); err != nil {
		return err
	}
	records, err := s.readRecordsLocked(room)
	if err != nil {
		return err
	}
	if uint64(len(records)) == pending.Record.Seq {
		if !sameRecord(records[pending.Record.Seq-1], pending.Record) {
			return fmt.Errorf("read %s: pending append sequence %d conflicts with stream", s.recordsPath(room), pending.Record.Seq)
		}
	} else if uint64(len(records)) > pending.Record.Seq {
		return fmt.Errorf("read %s: pending append sequence %d is behind stream head", s.recordsPath(room), pending.Record.Seq)
	} else {
		last := uint64(0)
		if len(records) > 0 {
			last = records[len(records)-1].Seq
		}
		if pending.Record.Seq != last+1 {
			return fmt.Errorf("read %s: pending append sequence %d does not follow stream head %d", s.recordsPath(room), pending.Record.Seq, last)
		}
		if err := appendJSONLine(s.recordsPath(room), pending.Record); err != nil {
			return err
		}
	}
	ledger, err := s.readLedgerLocked(room)
	if err != nil {
		return err
	}
	found := false
	for _, entry := range ledger {
		if entry.Author != pending.Ledger.Author || entry.Key != pending.Ledger.Key {
			continue
		}
		if entry != pending.Ledger {
			return fmt.Errorf("read %s: pending append conflicts with retry ledger", s.ledgerPath(room))
		}
		found = true
		break
	}
	if !found {
		if err := appendJSONLine(s.ledgerPath(room), pending.Ledger); err != nil {
			return err
		}
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

// validatePendingRecordStream checks all committed records and the pending
// sequence relationship before recovery is allowed to truncate any tail.
func validatePendingRecordStream(path, room string, pending core.Record) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		data = nil
	} else if err != nil {
		return err
	}
	lastNewline := bytes.LastIndexByte(data, '\n')
	completeEnd := lastNewline + 1
	var records []core.Record
	for _, line := range bytes.Split(data[:completeEnd], []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var record core.Record
		if err := json.Unmarshal(line, &record); err != nil {
			return fmt.Errorf("read %s: corrupt record line: %w", path, err)
		}
		wantSeq := uint64(len(records) + 1)
		if record.Room != room || record.Seq != wantSeq {
			return fmt.Errorf("read %s: committed record sequence %d is invalid; want %d for room %q", path, record.Seq, wantSeq, room)
		}
		records = append(records, record)
	}
	if len(data) > completeEnd {
		tail := data[completeEnd:]
		matches := sameRecordJSON(tail, pending)
		if json.Valid(tail) && !matches {
			return fmt.Errorf("read %s: complete unterminated record does not match pending append", path)
		}
		expected, err := json.Marshal(pending)
		if err != nil {
			return err
		}
		if !matches && !bytes.HasPrefix(append(expected, '\n'), tail) {
			return fmt.Errorf("read %s: incomplete record tail is not a prefix of pending append", path)
		}
	}
	head := uint64(len(records))
	switch pending.Seq {
	case head:
		if len(data) != completeEnd || head == 0 || !sameRecord(records[head-1], pending) {
			return fmt.Errorf("read %s: pending record sequence %d does not match stream head %d", path, pending.Seq, head)
		}
	case head + 1:
	default:
		return fmt.Errorf("read %s: pending record sequence %d does not follow stream head %d", path, pending.Seq, head)
	}
	return nil
}

func sameRecordJSON(data []byte, expected core.Record) bool {
	var actual core.Record
	return json.Unmarshal(data, &actual) == nil && sameRecord(actual, expected)
}

// validatePendingLedger rejects malformed committed entries and conflicting
// uses of the pending key before any tail repair can mutate either file.
func validatePendingLedger(path string, pending ledgerFile) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	lastNewline := bytes.LastIndexByte(data, '\n')
	completeEnd := lastNewline + 1
	for _, line := range bytes.Split(data[:completeEnd], []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var entry ledgerFile
		if err := json.Unmarshal(line, &entry); err != nil {
			return fmt.Errorf("read %s: corrupt ledger line: %w", path, err)
		}
		if entry.Author == pending.Author && entry.Key == pending.Key && entry != pending {
			return fmt.Errorf("read %s: pending append conflicts with retry ledger", path)
		}
	}
	if len(data) > completeEnd {
		tail := data[completeEnd:]
		matches := false
		var entry ledgerFile
		if json.Unmarshal(tail, &entry) == nil {
			matches = entry == pending
			if !matches {
				return fmt.Errorf("read %s: complete unterminated ledger entry does not match pending append", path)
			}
		}
		expected, err := json.Marshal(pending)
		if err != nil {
			return err
		}
		if !matches && !bytes.HasPrefix(append(expected, '\n'), tail) {
			return fmt.Errorf("read %s: incomplete ledger tail is not a prefix of pending append", path)
		}
	}
	return nil
}

func sameRecord(a, b core.Record) bool {
	return a.Room == b.Room && a.Seq == b.Seq && a.Author == b.Author && a.At.Equal(b.At) && bytes.Equal(a.Content, b.Content)
}

func repairPendingJSONL(path string, expected any, matches func([]byte) (bool, error)) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) || err == nil && (len(data) == 0 || data[len(data)-1] == '\n') {
		return nil
	}
	if err != nil {
		return err
	}
	tail := data[bytes.LastIndexByte(data, '\n')+1:]
	expectedLine, err := json.Marshal(expected)
	if err != nil {
		return err
	}
	expectedLine = append(expectedLine, '\n')
	if json.Valid(tail) {
		matched, err := matches(tail)
		if err != nil {
			return err
		}
		if !matched {
			return fmt.Errorf("read %s: complete unterminated JSONL entry does not match pending append", path)
		}
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			return err
		}
		n, err := f.Write([]byte{'\n'})
		if err != nil {
			_ = f.Close()
			return err
		}
		if n != 1 {
			_ = f.Close()
			return fmt.Errorf("write %s: short newline write", path)
		}
		if err := f.Sync(); err != nil {
			_ = f.Close()
			return err
		}
		return f.Close()
	}
	if len(tail) == 0 || !bytes.HasPrefix(expectedLine, tail) {
		return fmt.Errorf("read %s: incomplete JSONL tail is not a prefix of the pending append", path)
	}
	lastLineEnd := int64(bytes.LastIndexByte(data, '\n') + 1)
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	if err := f.Truncate(lastLineEnd); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
