//! Tearoom desktop app library: the builder, the IPC command surface, and
//! the panel-stream sessions. `src/main.rs` is a thin process entry over
//! [`run`]; integration tests drive [`handler`] through the mock runtime.

pub mod commands;
pub mod dashboard;
pub mod terminal;
pub mod workspace;

use commands::AppState;
use std::sync::Arc;
use tauri::Manager;

/// The IPC command surface, shared by the real app and the wiring tests.
pub fn handler<R: tauri::Runtime>() -> impl Fn(tauri::ipc::Invoke<R>) -> bool + Send + 'static {
    tauri::generate_handler![
        commands::workspaces,
        commands::workspace_pick,
        commands::room_records,
        commands::send_message,
        terminal::panel_info,
        terminal::attach_panel,
        terminal::terminal_write,
        terminal::terminal_resize,
        terminal::detach_panel,
    ]
}

fn setup(app: &mut tauri::App) -> Result<(), Box<dyn std::error::Error>> {
    let config = app.path().app_config_dir()?.join("workspace.json");
    // Both states are Arc-managed: the command signatures take
    // State<'_, Arc<_>> so relays can clone them off the main thread.
    app.manage(Arc::new(AppState::load(config)));
    app.manage(Arc::new(terminal::TerminalState::new()));
    Ok(())
}

/// Runs the desktop app (the real window).
pub fn run() {
    tauri::Builder::default()
        .setup(setup)
        .invoke_handler(handler())
        .run(tauri::generate_context!())
        .expect("tearoom app failed to run");
}
