//! Tearoom desktop app entry: one native window over the local workspace.
//! The window is a view over the Store and the CLI send command; it owns no
//! product or storage invariant (see docs/reference/app.md).

#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod commands;
mod workspace;

use commands::AppState;
use tauri::Manager;

fn main() {
    tauri::Builder::default()
        .setup(|app| {
            let config = app.path().app_config_dir()?.join("workspace.json");
            app.manage(AppState::load(config));
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            commands::workspace_current,
            commands::workspace_pick,
            commands::room_state,
            commands::room_records,
            commands::send_message,
        ])
        .run(tauri::generate_context!())
        .expect("tearoom app failed to run");
}
