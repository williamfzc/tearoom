// Backend selection: the .tearoom layout is one owner's truth, so this
// package decides which Store implementation a room uses. The marker file
// makes the choice explicit instead of guessing from what files exist.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/williamfzc/tearoom/internal/core"
	"github.com/williamfzc/tearoom/internal/relational"
)

// Backend names a storage backend for one .tearoom directory.
type Backend string

const (
	BackendFile   Backend = "file"
	BackendSQLite Backend = "sqlite"
)

const markerName = "store.json"

type marker struct {
	Backend string `json:"backend"`
}

// Init creates a Room under dir with the named backend. BackendFile is the
// original flock layout; BackendSQLite uses a GORM-backed database.
func Init(dir, name string, backend Backend) (core.Store, error) {
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
	switch backend {
	case BackendSQLite:
		if err := writeMarker(root, marker{Backend: string(BackendSQLite)}); err != nil {
			return nil, err
		}
		return relational.Init(root, name)
	case BackendFile:
		return initFile(root, name)
	default:
		return nil, fmt.Errorf("unknown backend %q", backend)
	}
}

// Open finds the nearest room and opens the backend its marker declares.
// A room without a marker is the original file backend.
func Open(start string) (core.Store, error) {
	root, err := Find(start)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(root, markerName))
	if err == nil {
		var m marker
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("read %s: %w", markerName, err)
		}
		switch Backend(m.Backend) {
		case BackendSQLite:
			return relational.OpenAt(root)
		default:
			return nil, fmt.Errorf("unknown backend %q", m.Backend)
		}
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return OpenAt(root)
}

func writeMarker(root string, m marker) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(root, markerName), data, 0o644)
}
