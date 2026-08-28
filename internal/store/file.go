// Package store implements core.Store as one file-backed room.
// All mutations run under flock; events are durable append-only JSON lines.
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

type File struct {
	root string
	now  func() time.Time
}

func Find(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, Dir)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", core.ErrNoRoom
		}
		dir = parent
	}
}

func Open(start string) (*File, error) {
	root, err := Find(start)
	if err != nil {
		return nil, err
	}
	return OpenAt(root)
}

func OpenAt(root string) (*File, error) {
	f := &File{root: root, now: time.Now}
	if _, err := f.Room(context.Background()); err != nil {
		return nil, err
	}
	return f, nil
}

func Init(dir, name string) (*File, error) {
	root := filepath.Join(dir, Dir)
	if _, err := os.Stat(root); err == nil {
		return nil, fmt.Errorf("room already exists at %s", root)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if strings.TrimSpace(name) == "" {
		name = filepath.Base(dir)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	f := &File{root: root, now: time.Now}
	room := core.Room{ID: "room", Name: name, Created: f.now().UTC()}
	if err := f.writeJSON("room.json", room); err != nil {
		return nil, err
	}
	if err := f.writeJSON("actors.json", []*core.Actor{}); err != nil {
		return nil, err
	}
	if err := os.WriteFile(f.path("events.jsonl"), nil, 0o644); err != nil {
		return nil, err
	}
	return f, nil
}

func (f *File) Root() string { return f.root }

func (f *File) SetClock(now func() time.Time) { f.now = now }

func (f *File) path(name string) string { return filepath.Join(f.root, name) }

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

func (f *File) UpsertActor(_ context.Context, id, role string) (*core.Actor, error) {
	id = strings.TrimSpace(id)
	if id == "" {
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
	now := f.now().UTC()
	var actor *core.Actor
	for _, current := range actors {
		if current.ID == id {
			actor = current
			break
		}
	}
	if actor == nil {
		actor = &core.Actor{ID: id, Joined: now}
		actors = append(actors, actor)
	}
	if role != "" {
		actor.Role = role
	}
	actor.LastSeen = now
	sort.Slice(actors, func(i, j int) bool { return actors[i].ID < actors[j].ID })
	if err := f.writeJSON("actors.json", actors); err != nil {
		return nil, err
	}
	copy := *actor
	return &copy, nil
}

func (f *File) Actors(context.Context) ([]*core.Actor, error) {
	unlock, err := f.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	return f.loadActors()
}

func (f *File) Read(_ context.Context, actor string, after uint64, limit int) (*core.Feed, error) {
	unlock, err := f.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := f.requireActor(actor); err != nil {
		return nil, err
	}
	room, err := f.Room(context.Background())
	if err != nil {
		return nil, err
	}
	feed := &core.Feed{Room: room, Cursor: after, Events: []core.Event{}}
	return feed, f.scanEvents(func(event core.Event) (bool, error) {
		if event.Seq <= after {
			feed.Cursor = event.Seq
			return true, nil
		}
		feed.Cursor = event.Seq
		if event.VisibleTo(actor) {
			feed.Events = append(feed.Events, event)
			if limit > 0 && len(feed.Events) >= limit {
				return false, nil
			}
		}
		return true, nil
	})
}

func (f *File) AppendEvent(_ context.Context, input core.AppendInput) (*core.Event, error) {
	input.Actor = strings.TrimSpace(input.Actor)
	input.Type = strings.TrimSpace(input.Type)
	input.CausedBy = strings.TrimSpace(input.CausedBy)
	input.To = normalizeRecipients(input.To)
	if input.Actor == "" || input.Type == "" || strings.TrimSpace(input.Key) == "" {
		return nil, errors.New("actor, event type, and idempotency key are required")
	}
	if len(input.Data) > 0 && !json.Valid(input.Data) {
		return nil, errors.New("event data must be valid JSON")
	}
	unlock, err := f.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := f.requireActor(input.Actor); err != nil {
		return nil, err
	}
	for _, recipient := range input.To {
		if err := f.requireActor(recipient); err != nil {
			return nil, fmt.Errorf("recipient %q: %w", recipient, err)
		}
	}

	var head uint64
	var duplicate *core.Event
	err = f.scanEvents(func(event core.Event) (bool, error) {
		head = event.Seq
		if event.Key == input.Key {
			if sameEvent(event, input) {
				copy := event
				duplicate = &copy
				return false, nil
			}
			return false, fmt.Errorf("%w: idempotency key %q already describes another event", core.ErrConflict, input.Key)
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	if duplicate != nil {
		return duplicate, nil
	}
	if input.Expected != head {
		return nil, &core.StaleWriteError{Expected: input.Expected, Actual: head}
	}
	room, err := f.Room(context.Background())
	if err != nil {
		return nil, err
	}
	event := &core.Event{
		ID:       fmt.Sprintf("e%d", head+1),
		Room:     room.ID,
		Seq:      head + 1,
		Actor:    input.Actor,
		At:       f.now().UTC(),
		Type:     input.Type,
		Body:     input.Body,
		Data:     input.Data,
		To:       input.To,
		CausedBy: input.CausedBy,
		Key:      input.Key,
	}
	line, err := json.Marshal(event)
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

func (f *File) loadActors() ([]*core.Actor, error) {
	var actors []*core.Actor
	if err := f.readJSON("actors.json", &actors); err != nil {
		return nil, err
	}
	return actors, nil
}

func (f *File) scanEvents(fn func(core.Event) (bool, error)) error {
	file, err := os.Open(f.path("events.jsonl"))
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var event core.Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return fmt.Errorf("decode event log: %w", err)
		}
		keep, err := fn(event)
		if err != nil {
			return err
		}
		if !keep {
			return nil
		}
	}
	return scanner.Err()
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
	tmp := f.path(name + ".tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, f.path(name))
}

func normalizeRecipients(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part != "" && !seen[part] {
				seen[part] = true
				out = append(out, part)
			}
		}
	}
	sort.Strings(out)
	return out
}

func sameEvent(event core.Event, input core.AppendInput) bool {
	return event.Actor == input.Actor && event.Type == input.Type && event.Body == input.Body &&
		event.CausedBy == input.CausedBy && event.Key == input.Key &&
		bytes.Equal(event.Data, input.Data) && equalStrings(event.To, input.To)
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
