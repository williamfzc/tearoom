//! The dashboard aggregate: one payload with every discovered workspace,
//! its rooms, roles, and latest activity — most recently active first.
//! Discovery primitives live in [`crate::workspace`].

use anyhow::Result;
use serde_json::{json, Value};
use std::path::{Path, PathBuf};
use std::time::{Duration, Instant};
use tearoom::store::Store;

use crate::workspace::{
    load_known, merge_roots, registry_roots, save_known, spotlight_roots, sweep_roots, ScanState,
    SWEEP_EVERY,
};

/// The dashboard payload: every workspace with its rooms, most recently
/// active first. The filesystem sweep runs on a detached thread and lands in
/// the next poll via the history file.
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

    let actor = ["TEAROOM_ACTOR", "USER", "LOGNAME"]
        .into_iter()
        .filter_map(|name| std::env::var(name).ok())
        .map(|value| value.trim().to_owned())
        .find(|value| !value.is_empty())
        .unwrap_or_else(|| "me".into());
    Ok(json!({
        "actor": actor,
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

    #[test]
    fn preview_text_covers_plain_message_and_data_shapes() {
        assert_eq!(preview_text(&serde_json::json!("plain")), "plain");
        assert_eq!(
            preview_text(&serde_json::json!({"text": "hello", "mentions": ["a"]})),
            "hello"
        );
        assert_eq!(preview_text(&serde_json::json!({"custom": 1})), "[data]");
    }

    #[test]
    fn nested_workspaces_are_named_after_their_project() {
        let dir = tempfile::tempdir().unwrap();
        let nested = dir.path().join("cartridge").join("tacticsfront");
        std::fs::create_dir_all(&nested).unwrap();
        let store = Store::init(&nested).unwrap();
        store.create_room("matches", "").unwrap();
        let value = workspace_json(&nested).unwrap();
        assert_eq!(value["name"], "cartridge/tacticsfront");
    }
}
