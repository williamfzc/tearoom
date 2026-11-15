//! Command-wiring regression tests: the real handlers run through the mock
//! runtime with the same state management as main.rs. This is the layer the
//! JS harness cannot see — a state that is managed with the wrong type shows
//! up here as an invoke failure instead of a blank window.
#![cfg(unix)]

use serde_json::json;
use tauri::Manager;
use tearoom_app::commands::AppState;
use tearoom_app::terminal::TerminalState;

fn stub_cli(dir: &std::path::Path) -> std::path::PathBuf {
    let path = dir.join("tearoom-stub");
    std::fs::write(
        &path,
        "#!/bin/sh\necho '{\"running\":false,\"stopping\":false,\"workspaces\":[]}'\n",
    )
    .unwrap();
    use std::os::unix::fs::PermissionsExt;
    std::fs::set_permissions(&path, std::fs::Permissions::from_mode(0o755)).unwrap();
    path
}

fn build_app(config: &std::path::Path) -> tauri::App<tauri::test::MockRuntime> {
    // MockRuntime's build() does not run setup closures, so the states are
    // managed here — the same types main.rs manages, in the same order.
    let app = tauri::test::mock_builder()
        .invoke_handler(tearoom_app::handler())
        .build(tauri::test::mock_context(tauri::test::noop_assets()))
        .unwrap();
    app.manage(std::sync::Arc::new(AppState::load(config.to_path_buf())));
    app.manage(std::sync::Arc::new(TerminalState::new()));
    app
}

fn invoke(
    webview: &tauri::WebviewWindow<tauri::test::MockRuntime>,
    command: &str,
    args: serde_json::Value,
) -> Result<tauri::ipc::InvokeResponseBody, serde_json::Value> {
    tauri::test::get_ipc_response(
        webview,
        tauri::webview::InvokeRequest {
            cmd: command.into(),
            callback: tauri::ipc::CallbackFn(0),
            error: tauri::ipc::CallbackFn(1),
            url: "tauri://localhost".parse().unwrap(),
            body: tauri::ipc::InvokeBody::Json(args),
            headers: Default::default(),
            invoke_key: tauri::test::INVOKE_KEY.to_string(),
        },
    )
}

#[test]
fn commands_resolve_their_managed_state() {
    let dir = tempfile::tempdir().unwrap();
    std::env::set_var("TEAROOM_CLI", stub_cli(dir.path()));
    let app = build_app(&dir.path().join("workspace.json"));
    let webview = tauri::WebviewWindowBuilder::new(&app, "main", Default::default())
        .build()
        .unwrap();

    // The dashboard command: with a stub registry and an empty history it
    // still succeeds with a list — and it proves `Arc<AppState>` is managed
    // (the previous build failed right here with "state not managed").
    let body = invoke(&webview, "workspaces", json!({})).expect("workspaces must resolve");
    let body: serde_json::Value = body.deserialize().unwrap();
    assert!(body["workspaces"].is_array(), "{body}");

    // The panel-stream state is managed too: a bogus session errors
    // gracefully instead of panicking on missing state.
    let error = invoke(
        &webview,
        "terminal_resize",
        json!({ "session": 999, "cols": 80, "rows": 24 }),
    )
    .expect_err("a closed session must error");
    assert!(
        error.to_string().contains("closed"),
        "unexpected error: {error}"
    );
    std::env::remove_var("TEAROOM_CLI");
}

#[test]
fn room_records_reads_a_real_workspace() {
    let dir = tempfile::tempdir().unwrap();
    let store = tearoom::store::Store::init(dir.path()).unwrap();
    let room = store.create_room("lobby", "").unwrap();
    store
        .append(
            &room.id,
            "tester",
            tearoom::core::AppendRequest {
                content: "\"hello from the records command\"",
                idempotency_key: "",
            },
        )
        .unwrap();
    let app = build_app(&dir.path().join("workspace.json"));
    let webview = tauri::WebviewWindowBuilder::new(&app, "main", Default::default())
        .build()
        .unwrap();

    let body = invoke(
        &webview,
        "room_records",
        json!({ "root": dir.path(), "room": "lobby", "after": 0 }),
    )
    .expect("room_records must resolve");
    let body: serde_json::Value = body.deserialize().unwrap();
    let records = body["records"].as_array().expect("records array");
    assert_eq!(records.len(), 1);
    assert_eq!(records[0]["author"], "tester");
}
