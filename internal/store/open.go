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
	"time"

	"github.com/williamfzc/tearoom/internal/core"
)

// Backend names a storage backend for one .tearoom directory.
type Backend string

// BackendFile is the flock-protected JSON layout in this package.
const BackendFile Backend = "file"

// formatVersion is the only store.json layout version accepted. Any other
// layout — markerless, version 1, or another backend — is rejected as
// incompatible; there is no import or automatic upgrade.
const formatVersion = 2

const markerName = "store.json"

// marker declares the layout format and backend of a room.
type marker struct {
	Format  int    `json:"format"`
	Backend string `json:"backend"`
}

// Init creates a Room under dir with the named backend.
func Init(dir string, backend Backend) (core.Store, error) {
	root := filepath.Join(dir, Dir)
	if _, err := os.Stat(root); err == nil {
		return nil, fmt.Errorf("room already exists at %s", root)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	switch backend {
	case BackendFile:
		if err := writeMarker(root, marker{Format: formatVersion, Backend: string(BackendFile)}); err != nil {
			return nil, err
		}
		return initFile(root)
	default:
		return nil, fmt.Errorf("unknown backend %q", backend)
	}
}

// Open finds the nearest room and opens the backend its marker declares.
// Only format 2 markers are accepted; any older layout is rejected with a
// clear incompatible error.
func Open(start string) (core.Store, error) {
	root, err := Find(start)
	if err != nil {
		return nil, err
	}
	m, err := readMarker(root)
	if err != nil {
		return nil, err
	}
	if m.Format != formatVersion {
		return nil, fmt.Errorf("incompatible store format %d at %s: format %d required, no automatic upgrade", m.Format, root, formatVersion)
	}
	switch Backend(m.Backend) {
	case BackendFile:
		return OpenAt(root)
	default:
		return nil, fmt.Errorf("unsupported store backend %q at %s", m.Backend, root)
	}
}

// Find walks upward from start and returns the nearest .tearoom directory.
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

func readMarker(root string) (marker, error) {
	data, err := os.ReadFile(filepath.Join(root, markerName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return marker{}, fmt.Errorf("incompatible store at %s: missing %s (old layout); format %d required, no automatic upgrade", root, markerName, formatVersion)
		}
		return marker{}, err
	}
	var m marker
	if err := json.Unmarshal(data, &m); err != nil {
		return marker{}, fmt.Errorf("read %s: %w", markerName, err)
	}
	return m, nil
}

func writeMarker(root string, m marker) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(root, markerName), data, 0o644)
}

// initFile lays out a file-backed Room under an existing, empty root. The room
// identity is the name of the directory that owns .tearoom.
func initFile(root string) (*File, error) {
	f := &File{root: root, now: time.Now}
	room := core.Room{ID: filepath.Base(filepath.Dir(root)), Created: f.now().UTC()}
	if err := f.writeJSON("room.json", room); err != nil {
		return nil, err
	}
	if err := f.writeJSON("actors.json", []core.Actor{}); err != nil {
		return nil, err
	}
	if err := os.WriteFile(f.path("events.jsonl"), nil, 0o644); err != nil {
		return nil, err
	}
	return f, nil
}
