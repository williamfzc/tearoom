//! Tearoom app library. The desktop face carries the IPC command surface and
//! the panel-stream sessions; the iOS face is a bare webview whose UI talks
//! to the paired Mac over the LAN face (app/ui/transport.js) and needs no
//! workspace code. `src/main.rs` is a thin process entry over [`run`];
//! integration tests drive [`handler`] through the mock runtime.

#[cfg(not(target_os = "ios"))]
pub mod commands;
#[cfg(not(target_os = "ios"))]
pub mod dashboard;
#[cfg(not(target_os = "ios"))]
pub mod terminal;
#[cfg(not(target_os = "ios"))]
pub mod workspace;

// The wiring tests drive the desktop command surface through this re-export.
#[cfg(not(target_os = "ios"))]
pub use desktop::handler;

#[cfg(target_os = "ios")]
/// The iOS face: the whole data plane lives in the UI's transport, so the
/// Rust side is an empty window. The entry-point macro exports the symbol
/// the generated Xcode bootstrap links against.
#[tauri::mobile_entry_point]
pub fn run() {
    tauri::Builder::default()
        .run(tauri::generate_context!())
        .expect("tearoom app failed to run");
}

#[cfg(not(target_os = "ios"))]
mod desktop {
    use super::{commands, terminal};
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
            commands::delete_room,
            terminal::panel_info,
            terminal::attach_panel,
            terminal::terminal_write,
            terminal::terminal_resize,
            terminal::detach_panel,
        ]
    }

    pub fn setup(app: &mut tauri::App) -> Result<(), Box<dyn std::error::Error>> {
        let config = app.path().app_config_dir()?.join("workspace.json");
        // Both states are Arc-managed: the command signatures take
        // State<'_, Arc<_>> so relays can clone them off the main thread.
        app.manage(Arc::new(AppState::load(config)));
        app.manage(Arc::new(super::terminal::TerminalState::new()));
        Ok(())
    }
}

/// Runs the desktop app (the real window).
#[cfg(not(target_os = "ios"))]
pub fn run() {
    tauri::Builder::default()
        .setup(desktop::setup)
        .invoke_handler(desktop::handler())
        .run(tauri::generate_context!())
        .expect("tearoom app failed to run");
}
