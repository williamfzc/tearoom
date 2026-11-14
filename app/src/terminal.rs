//! The role-panel terminal drawer: tauri commands over the framed
//! `terminal.attach` client in `tearoom::hc::attach`. The PTY lives in
//! herdr-core; this app is just another viewer, as Herdr's panes are.

use crate::commands::AppState;
use anyhow::Result;
use base64::Engine as _;
use serde_json::{json, Value};
use std::collections::HashMap;
use std::path::Path;
use std::sync::{Arc, Mutex};
use tauri::ipc::Channel;
use tauri::State;
use tearoom::collab::panel_name;
use tearoom::hc;
use tearoom::hc::attach::{PanelEvent, PanelStream};
use tearoom::store::Store;

/// Live role-panel streams, one per open drawer. Managed as an `Arc` so the
/// attach relay can run off the main thread.
pub struct TerminalState {
    sessions: Mutex<HashMap<u32, Arc<PanelStream>>>,
}

impl TerminalState {
    pub fn new() -> Self {
        Self {
            sessions: Mutex::new(HashMap::new()),
        }
    }

    /// Keeps at most one live drawer: opening a new panel detaches the old.
    fn replace(&self, stream: Arc<PanelStream>) -> u32 {
        let mut sessions = self.sessions.lock().unwrap();
        let id = (sessions.keys().max().copied().unwrap_or(0) + 1).max(1);
        for old in sessions.values() {
            old.detach();
        }
        sessions.clear();
        sessions.insert(id, stream);
        id
    }

    fn take(&self, id: u32) -> Option<Arc<PanelStream>> {
        self.sessions.lock().unwrap().remove(&id)
    }
}

/// Whether a role has a live panel, and its health if so.
#[tauri::command]
pub fn panel_info(
    workspace: State<'_, AppState>,
    room: String,
    role: String,
) -> Result<Value, String> {
    let root = workspace.current().map_err(|e| e.to_string())?;
    panel_info_json(&root, &room, &role).map_err(|e| format!("{e:#}"))
}

fn panel_info_json(root: &Path, room: &str, role: &str) -> Result<Value> {
    let store = Store::open(root)?;
    let id = store.resolve_room(room)?;
    let panel = panel_name(store.root(), &id, role)?;
    let client = hc::HcClient::from_env()?;
    let listed = client.call(
        "terminal.list",
        json!({ "all": false }),
        std::time::Duration::from_secs(5),
    )?;
    let terminals = listed["terminals"].as_array().cloned().unwrap_or_default();
    let live = terminals
        .iter()
        .find(|t| t["name"] == panel && t["state"] == "live");
    Ok(match live {
        Some(terminal) => {
            // hc reports the health word at `status.state`; older shapes put
            // the bare word at `status`.
            let status = terminal["status"]["state"]
                .as_str()
                .or_else(|| terminal["status"].as_str())
                .unwrap_or("unknown");
            json!({
                "panel": panel,
                "live": true,
                "status": status,
                "title": terminal["title"],
            })
        }
        None => json!({ "panel": panel, "live": false }),
    })
}

/// Opens a viewer stream on a role's panel and relays frames to `on`.
#[tauri::command]
pub async fn attach_panel(
    state: State<'_, Arc<TerminalState>>,
    workspace: State<'_, AppState>,
    room: String,
    role: String,
    cols: u16,
    rows: u16,
    on: Channel<Value>,
) -> Result<Value, String> {
    let root = workspace.current().map_err(|e| e.to_string())?;
    let state = state.inner().clone();
    tauri::async_runtime::spawn_blocking(move || {
        attach_json(&state, &root, &room, &role, cols, rows, on)
    })
    .await
    .map_err(|e| format!("attach task failed: {e}"))?
    .map_err(|e| format!("{e:#}"))
}

fn attach_json(
    state: &TerminalState,
    root: &Path,
    room: &str,
    role: &str,
    cols: u16,
    rows: u16,
    on: Channel<Value>,
) -> Result<Value> {
    let store = Store::open(root)?;
    let id = store.resolve_room(room)?;
    let panel = panel_name(store.root(), &id, role)?;
    let client = hc::HcClient::from_env()?;
    let socket = client.socket().to_path_buf();

    let relay = move |event: PanelEvent| match event {
        PanelEvent::Data(bytes) => {
            let encoded = base64::engine::general_purpose::STANDARD.encode(&bytes);
            let _ = on.send(json!({ "t": "data", "d": encoded }));
        }
        PanelEvent::Closed(reason) => {
            let _ = on.send(json!({ "t": "close", "reason": reason }));
        }
    };
    let stream = Arc::new(hc::attach::attach(
        &socket, &panel, cols, rows, 1000, relay,
    )?);
    let session = state.replace(stream.clone());

    Ok(json!({ "session": session, "panel": panel }))
}

/// Types bytes into the panel (DATA frames), as a Herdr pane would.
#[tauri::command]
pub fn terminal_write(
    state: State<'_, Arc<TerminalState>>,
    session: u32,
    data: String,
) -> Result<(), String> {
    let bytes = base64::engine::general_purpose::STANDARD
        .decode(data)
        .map_err(|e| format!("bad input encoding: {e}"))?;
    stream(&state, session)?
        .write(&bytes)
        .map_err(|e| format!("{e:#}"))
}

/// Resizes this viewer, which the core forwards to the panel.
#[tauri::command]
pub fn terminal_resize(
    state: State<'_, Arc<TerminalState>>,
    session: u32,
    cols: u16,
    rows: u16,
) -> Result<(), String> {
    stream(&state, session)?
        .resize(cols, rows)
        .map_err(|e| format!("{e:#}"))
}

/// Closes the drawer's stream; the panel itself keeps running.
#[tauri::command]
pub fn detach_panel(state: State<'_, Arc<TerminalState>>, session: u32) -> Result<(), String> {
    if let Some(stream) = state.take(session) {
        stream.detach();
    }
    Ok(())
}

fn stream(state: &TerminalState, session: u32) -> Result<Arc<PanelStream>, String> {
    state
        .sessions
        .lock()
        .unwrap()
        .get(&session)
        .cloned()
        .ok_or_else(|| "this panel stream is already closed".to_owned())
}
