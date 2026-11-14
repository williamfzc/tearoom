//! IPC commands: the app's whole contract with the webview.
//! Reads go through the Tearoom Store; sends go through the CLI `send`
//! command so message composition and delivery activation keep one owner.

use crate::workspace;
use anyhow::{anyhow, bail, Context, Result};
use serde_json::{json, Value};
use std::path::{Path, PathBuf};
use std::process::Command;
use std::sync::Mutex;
use tauri::State;
use tearoom::collab::Config;
use tearoom::store::Store;

pub struct AppState {
    config_path: PathBuf,
    root: Mutex<Option<PathBuf>>,
}

impl AppState {
    pub fn load(config_path: PathBuf) -> Self {
        Self {
            root: Mutex::new(workspace::load(&config_path)),
            config_path,
        }
    }

    pub fn current(&self) -> Result<PathBuf> {
        self.root
            .lock()
            .unwrap()
            .clone()
            .ok_or_else(|| anyhow!("no workspace selected"))
    }

    pub fn choose(&self, start: &Path) -> Result<PathBuf> {
        let root = workspace::validate(start)?;
        workspace::save(&self.config_path, &root)?;
        *self.root.lock().unwrap() = Some(root.clone());
        Ok(root)
    }
}

#[tauri::command]
pub fn workspace_current(state: State<AppState>) -> Value {
    json!({ "root": state.current().ok().map(|p| p.display().to_string()) })
}

#[tauri::command]
pub fn workspace_pick(state: State<AppState>) -> Result<Value, String> {
    let Some(dir) = rfd::FileDialog::new()
        .set_title("Choose a Tearoom workspace folder")
        .pick_folder()
    else {
        return Ok(json!({ "root": null }));
    };
    let root = state.choose(&dir).map_err(|e| format!("{e:#}"))?;
    Ok(json!({ "root": root.display().to_string() }))
}

#[tauri::command]
pub fn room_state(state: State<AppState>) -> Result<Value, String> {
    let root = state.current().map_err(|e| e.to_string())?;
    state_json(&root).map_err(|e| format!("{e:#}"))
}

#[tauri::command]
pub fn room_records(state: State<AppState>, room: String, after: u64) -> Result<Value, String> {
    let root = state.current().map_err(|e| e.to_string())?;
    records_json(&root, &room, after).map_err(|e| format!("{e:#}"))
}

/// Sends run off the main thread: service activation inside the CLI may
/// block for seconds while the runtime starts.
#[tauri::command]
pub async fn send_message(
    state: State<'_, AppState>,
    room: String,
    text: String,
    to: Vec<String>,
    author: String,
) -> Result<Value, String> {
    let root = state.current().map_err(|e| e.to_string())?;
    let cli = cli_path().map_err(|e| e.to_string())?;
    tauri::async_runtime::spawn_blocking(move || send_json(&root, &cli, &room, &text, &to, &author))
        .await
        .map_err(|e| format!("send task failed: {e}"))?
        .map_err(|e| format!("{e:#}"))
}

fn state_json(root: &Path) -> Result<Value> {
    let store = Store::open(root)?;
    let config = Config::load(store.root())?;
    let rooms: Vec<Value> = store
        .rooms()?
        .into_iter()
        .map(|room| {
            let roles = config
                .rooms
                .get(&room.id)
                .map(|binding| binding.roles.clone())
                .unwrap_or_default();
            // Conversation-list preview: the newest record, read through the
            // same Store seam; a failed read leaves the preview empty rather
            // than failing the whole state.
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
    Ok(json!({
        "workspace": root.display().to_string(),
        "actor": std::env::var("TEAROOM_ACTOR").unwrap_or_default(),
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

fn records_json(root: &Path, room: &str, after: u64) -> Result<Value> {
    let store = Store::open(root)?;
    let id = store.resolve_room(room)?;
    Ok(json!({ "records": store.read(&id, after)? }))
}

fn send_json(
    root: &Path,
    cli: &Path,
    room: &str,
    text: &str,
    to: &[String],
    author: &str,
) -> Result<Value> {
    if text.trim().is_empty() {
        bail!("message text is required");
    }
    let mut command = Command::new(cli);
    command
        .current_dir(root)
        .env("PATH", child_path())
        .args(["send", "--room", room, "--text", text]);
    let author = author.trim();
    if !author.is_empty() {
        command.args(["--as", author]);
    }
    let targets: Vec<String> = to
        .iter()
        .filter_map(|target| {
            let name = target.trim().trim_start_matches('@');
            (!name.is_empty()).then(|| format!("@{name}"))
        })
        .collect();
    if !targets.is_empty() {
        command.arg("--to").arg(targets.join(","));
    }
    let output = command.output().context("cannot run the tearoom CLI")?;
    let stdout = String::from_utf8_lossy(&output.stdout);
    let stderr = String::from_utf8_lossy(&output.stderr);
    let record: Option<Value> = stdout
        .lines()
        .rev()
        .find_map(|line| serde_json::from_str(line).ok());
    if output.status.success() {
        let record = record.ok_or_else(|| anyhow!("the tearoom CLI did not return a record"))?;
        return Ok(json!({ "record": record, "warning": Value::Null }));
    }
    if stderr.contains("record saved") {
        let record = record.unwrap_or(Value::Null);
        return Ok(json!({ "record": record, "warning": stderr.trim() }));
    }
    bail!(
        "{}",
        stderr
            .lines()
            .rev()
            .find(|line| !line.trim().is_empty())
            .unwrap_or("the tearoom CLI refused the message")
    );
}

/// The tearoom CLI binary, found the way a person's shell would find it even
/// when the app is launched from Finder with a minimal PATH.
fn cli_path() -> Result<PathBuf> {
    if let Some(path) = std::env::var_os("TEAROOM_CLI").map(PathBuf::from) {
        if executable(&path) {
            return Ok(path);
        }
    }
    for dir in search_dirs() {
        let candidate = dir.join("tearoom");
        if executable(&candidate) {
            return Ok(candidate);
        }
    }
    bail!("cannot find the tearoom CLI; install it or set TEAROOM_CLI")
}

fn search_dirs() -> Vec<PathBuf> {
    let mut dirs: Vec<PathBuf> = std::env::var_os("PATH")
        .map(|path| std::env::split_paths(&path).collect())
        .unwrap_or_default();
    if let Some(home) = std::env::var_os("HOME").map(PathBuf::from) {
        dirs.push(home.join(".cargo/bin"));
        dirs.push(home.join(".local/bin"));
    }
    dirs.push(PathBuf::from("/opt/homebrew/bin"));
    dirs.push(PathBuf::from("/usr/local/bin"));
    dirs
}

/// The PATH handed to the CLI so the service and Agent terminals it spawns
/// later resolve tools the way a person's shell would.
fn child_path() -> std::ffi::OsString {
    std::env::join_paths(search_dirs()).unwrap_or_default()
}

fn executable(path: &Path) -> bool {
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        path.is_file()
            && std::fs::metadata(path)
                .map(|meta| meta.permissions().mode() & 0o111 != 0)
                .unwrap_or(false)
    }
    #[cfg(not(unix))]
    path.is_file()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn workspace() -> (tempfile::TempDir, PathBuf) {
        let dir = tempfile::tempdir().unwrap();
        let store = Store::init(dir.path()).unwrap();
        let project = store.create_room("project", "").unwrap();
        let review = store.create_room("review", &project.id).unwrap();
        Config::update(store.root(), |config| {
            config.roles.insert(
                "reviewer".into(),
                tearoom::collab::Role {
                    agent_type: "probe".into(),
                    command: vec!["true".into()],
                    ..Default::default()
                },
            );
            config.rooms.insert(
                project.id.clone(),
                tearoom::collab::RoomRoles {
                    roles: vec!["reviewer".into()],
                },
            );
            Ok(())
        })
        .unwrap();
        let _ = review;
        let root = dir.path().to_path_buf();
        (dir, root)
    }

    #[test]
    fn state_reports_rooms_roles_and_actor() {
        let (_dir, root) = workspace();
        std::env::set_var("TEAROOM_ACTOR", "tester");
        let store = Store::open(&root).unwrap();
        let id = store.resolve_room("project").unwrap();
        store
            .append(
                &id,
                "tester",
                tearoom::core::AppendRequest {
                    content: "{\"text\":\"hello there\",\"mentions\":[\"reviewer\"]}",
                    idempotency_key: "",
                },
            )
            .unwrap();
        let value = state_json(&root).unwrap();
        assert_eq!(value["actor"], "tester");
        let rooms = value["rooms"].as_array().unwrap();
        assert_eq!(rooms.len(), 2);
        let project = rooms.iter().find(|r| r["name"] == "project").unwrap();
        assert_eq!(project["roles"], json!(["reviewer"]));
        assert_eq!(project["last"]["author"], "tester");
        assert_eq!(project["last"]["text"], "hello there");
        let review = rooms.iter().find(|r| r["name"] == "review").unwrap();
        assert_eq!(review["parent"], project["id"]);
        assert!(review["last"].is_null(), "an empty room has no preview");
        std::env::remove_var("TEAROOM_ACTOR");
    }

    #[test]
    fn records_read_from_an_explicit_boundary() {
        let (_dir, root) = workspace();
        let store = Store::open(&root).unwrap();
        let id = store.resolve_room("project").unwrap();
        store
            .append(
                &id,
                "tester",
                tearoom::core::AppendRequest {
                    content: "\"hello\"",
                    idempotency_key: "",
                },
            )
            .unwrap();
        let all = records_json(&root, "project", 0).unwrap();
        assert_eq!(all["records"].as_array().unwrap().len(), 1);
        let after = records_json(&root, "project", 1).unwrap();
        assert_eq!(after["records"].as_array().unwrap().len(), 0);
        assert!(records_json(&root, "missing", 0).is_err());
    }

    fn stub_cli(dir: &Path, name: &str, body: &str) -> PathBuf {
        let path = dir.join(name);
        std::fs::write(&path, format!("#!/bin/sh\n{body}\n")).unwrap();
        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(&path, std::fs::Permissions::from_mode(0o755)).unwrap();
        path
    }

    #[test]
    fn send_builds_cli_arguments_and_returns_the_record() {
        let (_dir, root) = workspace();
        let stub_dir = tempfile::tempdir().unwrap();
        let cli = stub_cli(
            stub_dir.path(),
            "tearoom-ok",
            "printf '%s\\n' \"$*\" > args.txt\necho '{\"room\":\"project\",\"seq\":2,\"author\":\"t\",\"at\":\"now\",\"content\":\"hi\"}'",
        );
        let value = send_json(
            &root,
            &cli,
            "project",
            "please review",
            &["reviewer".into()],
            "tester",
        )
        .unwrap();
        assert_eq!(value["record"]["seq"], 2);
        assert!(value["warning"].is_null());
        // The stub runs with the workspace as its current directory.
        let args = std::fs::read_to_string(root.join("args.txt")).unwrap();
        assert!(args.contains("--room project"));
        assert!(args.contains("--text please review"));
        assert!(args.contains("--as tester"));
        assert!(args.contains("--to @reviewer"));
    }

    #[test]
    fn send_surfaces_a_saved_record_when_activation_fails() {
        let (_dir, root) = workspace();
        let stub_dir = tempfile::tempdir().unwrap();
        let cli = stub_cli(
            stub_dir.path(),
            "tearoom-soft",
            "echo '{\"room\":\"project\",\"seq\":2}'\necho 'record saved, but background collaboration did not start: boom' >&2\nexit 1",
        );
        let value = send_json(&root, &cli, "project", "hi", &[], "").unwrap();
        assert!(!value["record"].is_null());
        assert!(value["warning"].as_str().unwrap().contains("record saved"));
    }

    #[test]
    fn send_reports_cli_failures_and_refuses_empty_text() {
        let (_dir, root) = workspace();
        let stub_dir = tempfile::tempdir().unwrap();
        let cli = stub_cli(
            stub_dir.path(),
            "tearoom-bad",
            "echo 'room \"x\": not found' >&2\nexit 1",
        );
        let error = send_json(&root, &cli, "missing", "hi", &[], "").unwrap_err();
        assert!(format!("{error:#}").contains("not found"));
        assert!(send_json(&root, &cli, "project", "  ", &[], "").is_err());
    }

    #[test]
    fn cli_path_prefers_the_explicit_override() {
        let stub_dir = tempfile::tempdir().unwrap();
        let cli = stub_cli(stub_dir.path(), "tearoom", "exit 0");
        std::env::set_var("TEAROOM_CLI", &cli);
        assert_eq!(cli_path().unwrap(), cli);
        std::env::remove_var("TEAROOM_CLI");
    }
}
