// The store package owns the .tearoom workspace layout. SQLite is the only
// room backend; one workspace database holds the whole room tree, and the
// marker file makes the layout format explicit instead of guessing from what
// files exist.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/williamfzc/tearoom/internal/core"
	"github.com/williamfzc/tearoom/internal/relational"
)

// Dir is the workspace directory name discovered upward from the working
// directory.
const Dir = ".tearoom"

// formatVersion is the store.json layout version this release writes. A
// format-2 workspace (the v1 single-room model) still opens: it upgrades
// once to format 3 on first touch, and the marker file is rewritten. Any
// other layout — markerless, version 1, or a retired backend — is rejected as
// incompatible; there is no other import or automatic upgrade.
const formatVersion = 3

// FormatVersion reports the store layout version written by this release.
// It is exposed for diagnostics so callers do not duplicate this invariant.
func FormatVersion() int { return formatVersion }

const markerName = "store.json"

// marker declares the layout format of a workspace. The backend field is
// historical: "sqlite" is the only value this release accepts.
type marker struct {
	Format  int    `json:"format"`
	Backend string `json:"backend"`
}

// Init creates a workspace: one .tearoom directory holding an empty room
// tree. The workspace is assembled in a private sibling directory and exposed
// only after the schema and marker are durable.
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

	st, err := relational.Init(tmp)
	if err != nil {
		return nil, err
	}
	if err := writeMarker(tmp, marker{Format: formatVersion, Backend: "sqlite"}); err != nil {
		_ = st.Close()
		return nil, err
	}
	if err := st.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, root); err != nil {
		return nil, err
	}
	if err := syncDirectory(dir); err != nil {
		return nil, err
	}
	return relational.Open(root)
}

// Open finds the nearest workspace and opens it. Format-3 sqlite markers are
// the current layout; a format-2 marker is the v1 layout and is upgraded once
// (its single room becomes a root, signatures are kept) with the marker file
// rewritten. Any other layout is refused with a clear incompatible error.
func Open(start string) (core.Store, error) {
	root, err := Find(start)
	if err != nil {
		return nil, err
	}
	m, err := readMarker(root)
	if err != nil {
		return nil, err
	}
	if m.Backend != "sqlite" {
		return nil, fmt.Errorf("incompatible store at %s: backend %q is retired; sqlite required, no automatic upgrade", root, m.Backend)
	}
	switch m.Format {
	case formatVersion:
	case 2:
		// The relational layer performs the one-time upgrade; after it
		// succeeds the marker file is rewritten so the next open sees the
		// current format directly.
	default:
		return nil, fmt.Errorf("incompatible store format %d at %s: format %d required, no automatic upgrade", m.Format, root, formatVersion)
	}
	st, err := relational.Open(root)
	if err != nil {
		return nil, err
	}
	if m.Format == 2 {
		if err := writeMarker(root, marker{Format: formatVersion, Backend: "sqlite"}); err != nil {
			_ = st.Close()
			return nil, err
		}
	}
	return st, nil
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
