// workspace.go holds the Store type and the tree operations: create a room
// under its parent, and list the rooms. Every Room is one directory whose
// room.json carries its identity and its single parent link; the child list
// is derived by reading the listing, never cached.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/williamfzc/tearoom/internal/core"
)

// Store implements core.Store over one workspace of JSONL files.
type Store struct {
	root string
	now  func() time.Time
}

func nowFunc() func() time.Time { return time.Now }

var _ core.Store = (*Store)(nil)

// roomFile is the durable identity of one Room: its name, its single parent
// (omitted for a root), and its creation time. The parent link is written
// once, at creation; nothing ever writes into another Room afterwards.
type roomFile struct {
	Name    string    `json:"name"`
	Parent  string    `json:"parent,omitempty"`
	Created time.Time `json:"created"`
}

// ledgerFile is one retry-ledger entry: the fingerprint of the original
// request and the sequence of the record it created.
type ledgerFile struct {
	Author      string `json:"author"`
	Key         string `json:"key"`
	Fingerprint string `json:"fingerprint"`
	Seq         uint64 `json:"seq"`
}

// CreateRoom creates one node of the tree. The name must be a safe directory
// name and unused; the parent, when given, must already exist.
func (s *Store) CreateRoom(ctx context.Context, name, parent string) (*core.Room, error) {
	name = strings.TrimSpace(name)
	if err := validateRoomName(name); err != nil {
		return nil, err
	}
	parent = strings.TrimSpace(parent)

	unlock, err := s.lock(true)
	if err != nil {
		return nil, err
	}
	defer unlock()

	roomsPath := filepath.Join(s.root, roomsDir)
	if parent != "" {
		if _, err := os.Stat(filepath.Join(roomsPath, parent, "room.json")); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("parent room %q: %w", parent, core.ErrNotFound)
			}
			return nil, err
		}
	}
	if _, err := os.Stat(filepath.Join(roomsPath, name)); err == nil {
		return nil, fmt.Errorf("room %q already exists: %w", name, core.ErrConflict)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	created := s.now().UTC()
	file := roomFile{Name: name, Parent: parent, Created: created}
	tmp, err := os.MkdirTemp(roomsPath, ".tmp-*")
	if err != nil {
		return nil, err
	}
	if err := writeJSONFile(filepath.Join(tmp, "room.json"), file); err != nil {
		_ = os.RemoveAll(tmp)
		return nil, err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		_ = os.RemoveAll(tmp)
		return nil, err
	}
	if err := os.Rename(tmp, filepath.Join(roomsPath, name)); err != nil {
		_ = os.RemoveAll(tmp)
		return nil, err
	}
	if err := syncDirectory(roomsPath); err != nil {
		return nil, err
	}
	return &core.Room{Name: name, Parent: parent, Created: created}, nil
}

// Rooms lists every Room of the tree. The child list of any room is derived
// from this listing at read time and is never cached.
func (s *Store) Rooms(ctx context.Context) ([]core.Room, error) {
	unlock, err := s.lock(false)
	if err != nil {
		return nil, err
	}
	defer unlock()
	return s.roomsLocked()
}

func (s *Store) roomsLocked() ([]core.Room, error) {
	matches, err := filepath.Glob(filepath.Join(s.root, roomsDir, "*", "room.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	rooms := make([]core.Room, 0, len(matches))
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var file roomFile
		if err := json.Unmarshal(data, &file); err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		name := filepath.Base(filepath.Dir(path))
		if file.Name != name {
			return nil, fmt.Errorf("room directory %q declares name %q", name, file.Name)
		}
		rooms = append(rooms, core.Room{Name: file.Name, Parent: file.Parent, Created: file.Created})
	}
	return rooms, nil
}

// validateRoomName keeps room names usable as directory names.
func validateRoomName(name string) error {
	if name == "" {
		return errors.New("room name is required")
	}
	if name == "." || name == ".." || strings.HasPrefix(name, ".") {
		return fmt.Errorf("room name %q must not be a dotted name", name)
	}
	if strings.ContainsRune(name, '/') || strings.ContainsRune(name, '\\') {
		return fmt.Errorf("room name %q must not contain path separators", name)
	}
	return nil
}

// lock takes the workspace flock: exclusive for writers, shared for readers.
// The lock spans the whole read-modify-append of one operation, which is what
// makes concurrent processes receive one dense sequence.
func (s *Store) lock(exclusive bool) (func(), error) {
	f, err := os.Open(filepath.Join(s.root, lockName))
	if err != nil {
		return nil, err
	}
	how := syscall.LOCK_SH
	if exclusive {
		how = syscall.LOCK_EX
	}
	if err := syscall.Flock(int(f.Fd()), how); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

// writeJSONFile writes one JSON document with a trailing newline, durably.
func writeJSONFile(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	return nil
}
