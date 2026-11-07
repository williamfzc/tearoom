// The store package owns the .tearoom layout and the JSONL Store. One
// workspace directory holds the whole room tree as plain files — one
// directory, one stream, one retry ledger per Room — behind a format marker,
// so the memory can be committed to git and travel with the checkout.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/williamfzc/tearoom/internal/core"
)

// Dir is the workspace directory name discovered upward from the working
// directory.
const Dir = ".tearoom"

// formatVersion is the store.json layout version this release writes: the
// JSONL room tree. Formats 2 and 3 — the SQLite eras — upgrade once on open;
// any other layout is rejected as incompatible, and there is no other import
// or automatic upgrade.
const formatVersion = 4

// FormatVersion reports the store layout version written by this release.
// It is exposed for diagnostics so callers do not duplicate this invariant.
func FormatVersion() int { return formatVersion }

const (
	markerName  = "store.json"
	roomsDir    = "rooms"
	lockName    = "lock"
	jsonlLayout = "jsonl"
)

// marker declares the layout format of a workspace. The backend field is
// historical: "jsonl" is the only value this release accepts going forward.
type marker struct {
	Format  int    `json:"format"`
	Backend string `json:"backend"`
}

// Init creates a workspace: one .tearoom directory with an empty room tree.
// The workspace is assembled in a private sibling directory and exposed only
// after the marker and the lock file are durable.
func Init(dir string) (core.Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	root := filepath.Join(dir, Dir)
	if _, err := os.Stat(root); err == nil {
		return nil, fmt.Errorf("workspace already exists at %s", root)
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
	if err := os.Mkdir(filepath.Join(tmp, roomsDir), 0o755); err != nil {
		return nil, err
	}
	if err := writeMarker(tmp, marker{Format: formatVersion, Backend: jsonlLayout}); err != nil {
		return nil, err
	}
	if err := touchLock(tmp); err != nil {
		return nil, err
	}
	if err := syncDirectory(tmp); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, root); err != nil {
		return nil, err
	}
	if err := syncDirectory(dir); err != nil {
		return nil, err
	}
	return Open(dir)
}

// Open finds the nearest workspace and opens it. Format 4 is the current
// layout; formats 2 and 3 are the retired SQLite eras and are migrated once,
// on open, into JSONL files with the marker rewritten. Any other layout is
// refused with a clear incompatible error.
func Open(start string) (core.Store, error) {
	root, err := Find(start)
	if err != nil {
		return nil, err
	}
	m, err := readMarker(root)
	if err != nil {
		return nil, err
	}
	switch {
	case m.Format == formatVersion && m.Backend == jsonlLayout:
	case (m.Format == 2 || m.Format == 3) && m.Backend == "sqlite":
		if err := migrateSQLWorkspace(root, m.Format); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("incompatible store format %d backend %q at %s: format %d required, no automatic upgrade",
			m.Format, m.Backend, root, formatVersion)
	}
	if err := touchLock(root); err != nil {
		return nil, err
	}
	return &Store{root: root, now: nowFunc()}, nil
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

// touchLock creates the flock target if it is missing. Open and Init are the
// only writers; every other operation expects it to exist.
func touchLock(root string) error {
	path := filepath.Join(root, lockName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func readMarker(root string) (marker, error) {
	data, err := os.ReadFile(filepath.Join(root, markerName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return marker{}, fmt.Errorf("incompatible store at %s: missing %s (pre-format-2 layout); format %d required, no automatic upgrade", root, markerName, formatVersion)
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
		return errors.Join(err, dir.Close())
	}
	return dir.Close()
}
