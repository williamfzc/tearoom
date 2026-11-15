//! Workspace discovery for the desktop app. The dashboard shows every
//! Tearoom workspace on this machine side by side — there is no switching
//! ceremony. Three sources merge: the collaboration service's registry
//! (workspaces activated at least once), Spotlight (`mdfind -name
//! .tearoom`), and the app's own history. Every candidate is validated by
//! opening its Store before it reaches the dashboard.

use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::path::{Path, PathBuf};
use std::process::Command;
use std::time::{Duration, Instant};
use tearoom::store::Store;

#[derive(Serialize, Deserialize, Default)]
struct Saved {
    #[serde(default)]
    known: Vec<PathBuf>,
    /// Single-root format from earlier builds; adopted into `known` once.
    #[serde(default)]
    root: Option<PathBuf>,
}

/// The remembered workspace directories (app-side history).
pub fn load_known(config_path: &Path) -> Vec<PathBuf> {
    let Ok(data) = std::fs::read(config_path) else {
        return Vec::new();
    };
    let saved: Saved = serde_json::from_slice(&data).unwrap_or_default();
    let mut known = saved.known;
    if let Some(root) = saved.root {
        if !known.iter().any(|k| k == &root) {
            known.push(root);
        }
    }
    known
}

pub fn save_known(config_path: &Path, known: &[PathBuf]) -> Result<()> {
    if let Some(parent) = config_path.parent() {
        std::fs::create_dir_all(parent)
            .with_context(|| format!("cannot create {}", parent.display()))?;
    }
    let data = serde_json::to_vec(&Saved {
        known: known.to_vec(),
        root: None,
    })?;
    std::fs::write(config_path, data)
        .with_context(|| format!("cannot write {}", config_path.display()))
}

pub fn add_known(config_path: &Path, root: &Path) -> Result<Vec<PathBuf>> {
    let mut known = load_known(config_path);
    if !known.iter().any(|k| k == root) {
        known.push(root.to_path_buf());
    }
    save_known(config_path, &known)?;
    Ok(known)
}

/// Validate a manually picked folder by opening it; report the workspace
/// directory (the parent of the `.tearoom` marker) so a subfolder works.
pub fn validate_candidate(start: &Path) -> Result<PathBuf> {
    let store = Store::open(start)?;
    store
        .root()
        .parent()
        .map(Path::to_path_buf)
        .context("workspace marker has no parent directory")
}

/// Candidate roots from one discovery source, already resolved to
/// workspace directories (the parent of a `.tearoom` marker).
fn candidate_dirs(raw: &[PathBuf]) -> Vec<PathBuf> {
    raw.iter()
        .filter_map(|path| {
            if path.file_name()?.to_str()? == ".tearoom" {
                path.parent().map(Path::to_path_buf)
            } else {
                Some(path.clone())
            }
        })
        .collect()
}

/// Merge, validate and dedupe candidate roots. Validation is opening the
/// Store; the canonical path is the dedupe key. Order is the caller's.
pub fn merge_roots(registry: &[PathBuf], spotlight: &[PathBuf], known: &[PathBuf]) -> Vec<PathBuf> {
    let mut seen: Vec<PathBuf> = Vec::new();
    for candidate in candidate_dirs(registry)
        .into_iter()
        .chain(candidate_dirs(spotlight))
        .chain(candidate_dirs(known))
    {
        let Ok(store) = Store::open(&candidate) else {
            continue;
        };
        let Some(dir) = store.root().parent() else {
            continue;
        };
        if !seen.iter().any(|seen| seen == dir) {
            seen.push(dir.to_path_buf());
        }
    }
    seen
}

/// Workspaces the collaboration service has registered. The service may not
/// be running and the CLI may be absent — either way this source is empty,
/// never a dashboard failure.
pub(crate) fn registry_roots(cli: Option<&Path>) -> Vec<PathBuf> {
    let Some(cli) = cli else {
        return Vec::new();
    };
    let output = match Command::new(cli).args(["service", "status"]).output() {
        Ok(output) => output,
        Err(_) => return Vec::new(),
    };
    let Ok(value) = serde_json::from_slice::<Value>(&output.stdout) else {
        return Vec::new();
    };
    value["workspaces"]
        .as_array()
        .map(|list| {
            list.iter()
                .filter_map(|ws| ws["root"].as_str().map(PathBuf::from))
                .collect()
        })
        .unwrap_or_default()
}

/// Filesystem sweep for `.tearoom` markers under the home directory —
/// Spotlight does not index dot-directories, so this is the only source that
/// finds workspaces nobody has opened in this app before. Bounded by depth
/// and prune lists, run off-thread and throttled by the caller.
pub(crate) fn sweep_roots(home: &Path) -> Vec<PathBuf> {
    let output = Command::new("find")
        .arg(home)
        .args(["-maxdepth", "4", "-type", "d", "-name", ".tearoom"])
        .args(["!", "-path", "*/Library/*"])
        .args(["!", "-path", "*/node_modules/*"])
        .args(["!", "-path", "*/target/*"])
        .args(["!", "-path", "*/.git/*"])
        .args(["!", "-path", "*/.cargo/*"])
        .stderr(std::process::Stdio::null())
        .output();
    let Ok(output) = output else {
        return Vec::new();
    };
    String::from_utf8_lossy(&output.stdout)
        .lines()
        .map(PathBuf::from)
        .collect()
}

/// Spotlight sweep for `.tearoom` markers — instant when the index has them,
/// but Spotlight does not index dot-directories, so the filesystem sweep
/// below is the source that actually finds fresh workspaces.
pub(crate) fn spotlight_roots() -> Vec<PathBuf> {
    let Ok(output) = Command::new("mdfind")
        .arg("kMDItemFSName == '.tearoom'")
        .output()
    else {
        return Vec::new();
    };
    String::from_utf8_lossy(&output.stdout)
        .lines()
        .map(PathBuf::from)
        .collect()
}

pub(crate) const SWEEP_EVERY: Duration = Duration::from_secs(300);

/// Throttle state for the discovery scans (Spotlight + filesystem sweep).
#[derive(Default)]
pub struct ScanState {
    pub spotlight: Option<Instant>,
    pub sweep: Option<Instant>,
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_workspace(dir: &Path, name: &str) -> PathBuf {
        let store = Store::init(dir).unwrap();
        store.create_room(name, "").unwrap();
        dir.to_path_buf()
    }

    #[test]
    fn merge_validates_dedupes_and_takes_marker_parents() {
        let base = tempfile::tempdir().unwrap();
        let ws_a = make_workspace(&base.path().join("a"), "lobby");
        let ws_b = make_workspace(&base.path().join("b"), "notes");
        let marker_b = ws_b.join(".tearoom");
        let broken = base.path().join("gone");

        let merged = merge_roots(
            &[ws_a.clone(), broken, marker_b.clone()],
            &[ws_b.join(".tearoom")],
            &[],
        );
        // macOS hands back /private/var/... for /var/...: the dedupe key is
        // the canonical dir, so compare canonically too.
        let expected: Vec<PathBuf> = [&ws_a, &ws_b]
            .iter()
            .map(|p| std::fs::canonicalize(p).unwrap())
            .collect();
        assert_eq!(
            merged, expected,
            "broken root dropped, dedupe by canonical dir"
        );
        assert!(!merged.iter().any(|p| p.ends_with(".tearoom")));
    }

    #[test]
    fn known_history_adopts_the_old_single_root_format() {
        let dir = tempfile::tempdir().unwrap();
        let config = dir.path().join("workspace.json");
        std::fs::write(&config, br#"{"root":"/tmp/somewhere"}"#).unwrap();
        let known = load_known(&config);
        assert_eq!(known, vec![PathBuf::from("/tmp/somewhere")]);
    }
}
