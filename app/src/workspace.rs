//! Workspace discovery for the desktop app. The dashboard shows every
//! Tearoom workspace on this machine side by side — there is no switching
//! ceremony. Three sources merge: the collaboration service's registry
//! (workspaces activated at least once), Spotlight (`mdfind -name
//! .tearoom`), and the app's own history. Every candidate is validated by
//! opening its Store before it reaches the dashboard.

use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
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
fn registry_roots(cli: Option<&Path>) -> Vec<PathBuf> {
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
fn sweep_roots(home: &Path) -> Vec<PathBuf> {
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
fn spotlight_roots() -> Vec<PathBuf> {
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

const SWEEP_EVERY: Duration = Duration::from_secs(300);

/// Throttle state for the discovery scans (Spotlight + filesystem sweep).
#[derive(Default)]
pub struct ScanState {
    pub spotlight: Option<Instant>,
    pub sweep: Option<Instant>,
}

/// The dashboard payload: every workspace with its rooms, most recently
/// active first. Discovery degrades gracefully: a missing CLI only removes
/// the service-registry source. The filesystem sweep runs on a detached
/// thread and lands in the next poll via the history file.
pub fn workspaces_json(
    cli: Option<&Path>,
    config_path: &Path,
    scan: &mut ScanState,
) -> Result<Value> {
    let registry = registry_roots(cli);
    let spotlight = match scan.spotlight {
        Some(at) if at.elapsed() < Duration::from_secs(30) => Vec::new(),
        _ => {
            scan.spotlight = Some(Instant::now());
            spotlight_roots()
        }
    };
    if scan.sweep.is_none_or(|at| at.elapsed() > SWEEP_EVERY) {
        scan.sweep = Some(Instant::now());
        if let Some(home) = std::env::var_os("HOME").map(PathBuf::from) {
            let config = config_path.to_path_buf();
            std::thread::spawn(move || {
                let found = sweep_roots(&home);
                let known = load_known(&config);
                let merged = merge_roots(&found, &[], &known);
                let _ = save_known(&config, &merged);
            });
        }
    }
    let known = load_known(config_path);
    let roots = merge_roots(&registry, &spotlight, &known);
    let _ = save_known(config_path, &roots);

    let mut workspaces: Vec<Value> = roots
        .iter()
        .filter_map(|root| workspace_json(root).ok())
        .collect();
    workspaces.sort_by_key(|workspace| std::cmp::Reverse(activity(workspace)));

    Ok(json!({
        "actor": std::env::var("TEAROOM_ACTOR").unwrap_or_default(),
        "workspaces": workspaces,
    }))
}

fn activity(workspace: &Value) -> String {
    workspace["rooms"]
        .as_array()
        .and_then(|rooms| {
            rooms
                .iter()
                .filter_map(|room| room["last"]["at"].as_str())
                .max()
                .map(str::to_owned)
        })
        .unwrap_or_default()
}

/// One workspace's dashboard entry: its rooms with roles and last activity.
pub fn workspace_json(root: &Path) -> Result<Value> {
    let store = Store::open(root)?;
    let config = tearoom::collab::Config::load(store.root())?;
    let rooms: Vec<Value> = store
        .rooms()?
        .into_iter()
        .map(|room| {
            let roles = config
                .rooms
                .get(&room.id)
                .map(|binding| binding.roles.clone())
                .unwrap_or_default();
            let last = match store.read(&room.id, 0) {
                Ok(records) => records.last().map_or(Value::Null, |record| {
                    json!({
                        "author": record.author,
                        "text": preview_text(&record.content),
                        "at": record.at,
                    })
                }),
                Err(_) => Value::Null,
            };
            json!({
                "id": room.id,
                "name": room.name,
                "parent": room.parent,
                "description": room.description,
                "created": room.created,
                "roles": roles,
                "last": last,
            })
        })
        .collect();
    // Nested workspaces read as their project: show the parent directory
    // too ("cartridge/tacticsfront"), unless it is the home directory.
    let name = match (root.parent(), root.file_name()) {
        (Some(parent), Some(base))
            if parent
                != std::env::var_os("HOME")
                    .map(PathBuf::from)
                    .as_deref()
                    .unwrap_or(Path::new("")) =>
        {
            format!(
                "{}/{}",
                parent
                    .file_name()
                    .map(|n| n.to_string_lossy().into_owned())
                    .unwrap_or_default(),
                base.to_string_lossy()
            )
        }
        (Some(_), Some(base)) => base.to_string_lossy().into_owned(),
        _ => root.display().to_string(),
    };
    Ok(json!({
        "root": root.display().to_string(),
        "name": name,
        "rooms": rooms,
    }))
}

/// The plain preview text of one record: a message's text, a plain JSON
/// string, or a bracketed placeholder for any other content shape.
fn preview_text(content: &impl serde::ser::Serialize) -> String {
    match serde_json::to_value(content) {
        Ok(Value::String(text)) => text,
        Ok(Value::Object(map)) => match map.get("text") {
            Some(Value::String(text)) => text.clone(),
            _ => "[data]".into(),
        },
        _ => "[data]".into(),
    }
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

    #[test]
    fn preview_text_covers_plain_message_and_data_shapes() {
        assert_eq!(preview_text(&serde_json::json!("plain")), "plain");
        assert_eq!(
            preview_text(&serde_json::json!({"text": "hello", "mentions": ["a"]})),
            "hello"
        );
        assert_eq!(preview_text(&serde_json::json!({"custom": 1})), "[data]");
    }
}
