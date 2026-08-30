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
	"time"

	"github.com/williamfzc/tearoom/internal/core"
	"github.com/williamfzc/tearoom/internal/relational"
)

// Backend names a storage backend for one .tearoom directory.
type Backend string

// BackendFile is the flock-protected JSON layout in this package.
const BackendFile Backend = "file"

// BackendSQLite is the relational layout on one SQLite file.
const BackendSQLite Backend = "sqlite"

// formatVersion is the only store.json layout version accepted. Any other
// layout — markerless, version 1, or another backend — is rejected as
// incompatible; there is no import or automatic upgrade.
const formatVersion = 2

// FormatVersion reports the store layout version accepted by this release.
// It is exposed for diagnostics so callers do not duplicate this invariant.
func FormatVersion() int { return formatVersion }

const markerName = "store.json"

// marker declares the layout format and backend of a room.
type marker struct {
	Format  int    `json:"format"`
	Backend string `json:"backend"`
}

// Init creates a Room and explicitly admits creator as one atomic operation.
// The room is assembled in a private sibling directory and exposed only after
// all files, the backend schema, and the creator membership are durable.
func Init(dir string, backend Backend, creator string) (core.Store, error) {
	creator = strings.TrimSpace(creator)
	if creator == "" {
		return nil, errors.New("creator actor is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	root := filepath.Join(dir, Dir)
	if _, err := os.Stat(root); err == nil {
		return nil, fmt.Errorf("room already exists at %s", root)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	tmp, err := os.MkdirTemp(dir, ".tearoom-init-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if err := os.Chmod(tmp, 0o755); err != nil {
		return nil, err
	}

	var st core.Store
	switch backend {
	case BackendFile:
		if _, err := initFile(tmp, creator); err != nil {
			return nil, err
		}
		if err := writeMarker(tmp, marker{Format: formatVersion, Backend: string(BackendFile)}); err != nil {
			return nil, err
		}
		st, err = OpenAt(tmp)
	case BackendSQLite:
		st, err = relational.Init(tmp, creator)
		if err != nil {
			return nil, err
		}
		if err := writeMarker(tmp, marker{Format: formatVersion, Backend: string(BackendSQLite)}); err != nil {
			_ = st.(*relational.Store).Close()
			return nil, err
		}
		if err := st.(*relational.Store).Close(); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown backend %q", backend)
	}
	if err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, root); err != nil {
		if sqlite, ok := st.(*relational.Store); ok {
			_ = sqlite.Close()
		}
		return nil, err
	}
	if err := syncDirectory(dir); err != nil {
		return nil, err
	}
	switch backend {
	case BackendFile:
		return OpenAt(root)
	case BackendSQLite:
		return relational.Open(root)
	}
	return nil, fmt.Errorf("unsupported store backend %q at %s", backend, root)
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
	case BackendSQLite:
		// relational.Open applies the database-level format gate: a database
		// without the format-2 marker is refused, never read or upgraded.
		return relational.Open(root)
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
	return durableReplace(filepath.Join(root, markerName), data, 0o644)
}

// initFile lays out a file-backed Room under an existing, empty root. The room
// identity is the name of the directory that owns .tearoom.
func initFile(root, creator string) (*File, error) {
	f := &File{root: root, now: time.Now}
	room := core.Room{ID: filepath.Base(filepath.Dir(root)), Created: f.now().UTC()}
	if err := f.writeJSON("room.json", room); err != nil {
		return nil, err
	}
	if err := f.writeJSON("actors.json", []core.Actor{{ID: creator, Joined: f.now().UTC()}}); err != nil {
		return nil, err
	}
	if err := durableReplace(f.path("events.jsonl"), nil, 0o644); err != nil {
		return nil, err
	}
	return f, nil
}

func durableReplace(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	if err := dir.Sync(); err != nil {
		_ = dir.Close()
		return err
	}
	return dir.Close()
}
