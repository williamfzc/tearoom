//! Tearoom desktop app entry: one native window over the local workspace.
//! The window is a view over the Store, the CLI send command, and the hc
//! panel attach stream; it owns no product or storage invariant (see
//! docs/reference/app.md).

#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

fn main() {
    tearoom_app::run()
}
