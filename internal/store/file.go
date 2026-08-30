// Package store owns the .tearoom layout, selects the backend a room uses,
// and implements core.Store as one file-backed room. All file mutations run
// under flock; events are durable append-only JSON lines.
package store

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/williamfzc/tearoom/internal/core"
)

const Dir = ".tearoom"

// File implements core.Store on one .tearoom directory.
type File struct {
	root string
	now  func() time.Time
}

// record is the internal JSONL envelope: the Event plus the request
// idempotency metadata used to answer retries. It is never exposed outside
// the store; clients only ever see core.Event.
type record struct {
	Event       core.Event `json:"event"`
	Idempotency string     `json:"idempotency,omitempty"`
	Fingerprint []byte     `json:"fingerprint,omitempty"`
}

// OpenAt opens the file-backed room at an existing .tearoom root.
func OpenAt(root string) (*File, error) {
	m, err := readMarker(root)
	if err != nil {
		return nil, err
	}
	if m.Format != formatVersion || m.Backend != string(BackendFile) {
		return nil, fmt.Errorf("incompatible file store at %s: format %d backend %q", root, m.Format, m.Backend)
	}
	f := &File{root: root, now: time.Now}
	if _, err := f.Room(context.Background()); err != nil {
		return nil, err
	}
	return f, nil
}

func (f *File) Root() string { return f.root }

func (f *File) SetClock(now func() time.Time) { f.now = now }

func (f *File) path(name string) string { return filepath.Join(f.root, name) }

// Room returns the room identity this directory holds.
func (f *File) Room(context.Context) (*core.Room, error) {
	var room core.Room
	if err := f.readJSON("room.json", &room); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, core.ErrNoRoom
		}
		return nil, err
	}
	return &room, nil
}

// AdmitActor explicitly admits an actor; joining an already-admitted actor
// returns the existing one unchanged.
func (f *File) AdmitActor(_ context.Context, actorID string) (*core.Actor, error) {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, errors.New("actor id is required")
	}
	unlock, err := f.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()

	actors, err := f.loadActors()
	if err != nil {
		return nil, err
	}
	for _, current := range actors {
		if current.ID == actorID {
			copy := current
			return &copy, nil
		}
	}
	actor := &core.Actor{ID: actorID, Joined: f.now().UTC()}
	actors = append(actors, *actor)
	sort.Slice(actors, func(i, j int) bool { return actors[i].ID < actors[j].ID })
	if err := f.writeJSON("actors.json", actors); err != nil {
		return nil, err
	}
	return actor, nil
}

// Actors lists every admitted actor.
func (f *File) Actors(context.Context) ([]core.Actor, error) {
	unlock, err := f.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	return f.loadActors()
}

// Read returns the events after the caller-owned exclusive boundary for an
// admitted actor. Repeated reads are side-effect-free; no cursor is persisted.
func (f *File) Read(_ context.Context, actorID string, after uint64, limit int) ([]core.Event, error) {
	actorID = strings.TrimSpace(actorID)
	unlock, err := f.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := f.requireActor(actorID); err != nil {
		return nil, err
	}
	events := []core.Event{}
	err = f.scanRecords(func(rec record) (bool, error) {
		if rec.Event.Seq > after {
			events = append(events, rec.Event)
			if limit > 0 && len(events) >= limit {
				return false, nil
			}
		}
		return true, nil
	})
	return events, err
}

// Append accepts one contribution and assigns the next dense room sequence.
// The idempotency key, when present, is matched per (room, actor, key): an
// equal fingerprint returns the original event, a different one conflicts.
func (f *File) Append(_ context.Context, actorID string, request core.AppendRequest) (*core.Event, error) {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, errors.New("actor id is required")
	}
	content, err := core.CompactJSON(request.Content)
	if err != nil {
		return nil, fmt.Errorf("invalid content: %w", err)
	}
	to := core.NormalizeTo(request.To)
	key := strings.TrimSpace(request.IdempotencyKey)
	var fingerprint []byte
	if key != "" {
		fingerprint = core.Fingerprint(actorID, to, content)
	}

	unlock, err := f.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := f.requireActor(actorID); err != nil {
		return nil, err
	}
	for _, recipient := range to {
		if err := f.requireActor(recipient); err != nil {
			return nil, fmt.Errorf("recipient %q: %w", recipient, err)
		}
	}

	var head uint64
	var duplicate *record
	err = f.scanRecords(func(rec record) (bool, error) {
		head = rec.Event.Seq
		if key != "" && rec.Event.Actor == actorID && rec.Idempotency == key {
			if bytes.Equal(rec.Fingerprint, fingerprint) {
				duplicate = &rec
				return false, nil
			}
			return false, fmt.Errorf("%w: idempotency key %q already describes another event", core.ErrConflict, key)
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	if duplicate != nil {
		event := duplicate.Event
		return &event, nil
	}

	event := &core.Event{
		Seq:     head + 1,
		Actor:   actorID,
		At:      f.now().UTC(),
		To:      to,
		Content: content,
	}
	line, err := json.Marshal(record{Event: *event, Idempotency: key, Fingerprint: fingerprint})
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(f.path("events.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	if _, err = file.Write(append(line, '\n')); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return event, nil
}

func (f *File) requireActor(id string) error {
	actors, err := f.loadActors()
	if err != nil {
		return err
	}
	for _, actor := range actors {
		if actor.ID == id {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", core.ErrUnknownActor, id)
}

func (f *File) loadActors() ([]core.Actor, error) {
	var actors []core.Actor
	if err := f.readJSON("actors.json", &actors); err != nil {
		return nil, err
	}
	return actors, nil
}

func (f *File) scanRecords(fn func(record) (bool, error)) error {
	file, err := os.Open(f.path("events.jsonl"))
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var expectedSeq uint64
	for scanner.Scan() {
		var rec record
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			return fmt.Errorf("decode event log: %w", err)
		}
		if err := validateRecord(rec, expectedSeq+1); err != nil {
			return err
		}
		expectedSeq = rec.Event.Seq
		keep, err := fn(rec)
		if err != nil {
			return err
		}
		if !keep {
			return nil
		}
	}
	return scanner.Err()
}

func validateRecord(rec record, expectedSeq uint64) error {
	if rec.Event.Seq != expectedSeq {
		return fmt.Errorf("invalid event log sequence %d: want %d", rec.Event.Seq, expectedSeq)
	}
	if strings.TrimSpace(rec.Event.Actor) == "" || rec.Event.Actor != strings.TrimSpace(rec.Event.Actor) {
		return errors.New("invalid event log actor")
	}
	if rec.Event.At.IsZero() {
		return fmt.Errorf("invalid event log event %d: timestamp is required", rec.Event.Seq)
	}
	content, err := core.CompactJSON(rec.Event.Content)
	if err != nil || !bytes.Equal(content, rec.Event.Content) {
		return fmt.Errorf("invalid event log event %d: content is not compact valid JSON", rec.Event.Seq)
	}
	if normalized := core.NormalizeTo(rec.Event.To); !equalStrings(rec.Event.To, normalized) {
		return fmt.Errorf("invalid event log event %d: to is not normalized", rec.Event.Seq)
	}
	if rec.Idempotency == "" && len(rec.Fingerprint) != 0 {
		return fmt.Errorf("invalid event log event %d: fingerprint without idempotency key", rec.Event.Seq)
	}
	if rec.Idempotency != "" {
		want := core.Fingerprint(rec.Event.Actor, rec.Event.To, rec.Event.Content)
		if !bytes.Equal(rec.Fingerprint, want) {
			return fmt.Errorf("invalid event log event %d: idempotency fingerprint mismatch", rec.Event.Seq)
		}
	}
	return nil
}

func (f *File) readJSON(name string, out any) error {
	file, err := os.Open(f.path(name))
	if err != nil {
		return err
	}
	defer file.Close()
	return json.NewDecoder(file).Decode(out)
}

func (f *File) writeJSON(name string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return durableReplace(f.path(name), data, 0o644)
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

var _ core.Store = (*File)(nil)
