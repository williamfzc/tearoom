//! Optional end-to-end CLI dispatch against an isolated hc core and shell agent.
#![cfg(unix)]
use std::fs;
use std::process::{Command, Stdio};
use std::time::{Duration, Instant};

#[test]
#[ignore = "starts an isolated hc core and terminal; requires hc in PATH"]
fn cli_delivers_a_room_mention_to_hc() {
    let dir = tempfile::tempdir().unwrap();
    let socket = format!("/tmp/tearoom-cli-hc-{}.sock", std::process::id());
    let session = format!("tearoom-cli-test-{}", std::process::id());
    let config = dir.path().join("config");
    let state = dir.path().join("state");
    let binary = env!("CARGO_BIN_EXE_tearoom");
    let script = dir.path().join("agent.sh");
    fs::write(&script, format!("#!/bin/sh\nprintf '\\033]7501;state=idle\\007'\nwhile IFS= read -r line; do\n  printf '\\033]7501;state=working\\007'\n  '{binary}' send --room project --as reviewer --reply-to project#1 --text reviewed\n  printf '\\033]7501;state=done\\007'\ndone\n")).unwrap();
    let build = |program: &str| {
        let mut command = Command::new(program);
        command
            .current_dir(dir.path())
            .env("HC_SESSION", &session)
            .env("HC_SOCKET", &socket)
            .env("XDG_CONFIG_HOME", &config)
            .env("XDG_STATE_HOME", &state)
            .env("TEAROOM_HERDR_BIN", dir.path().join("missing-herdr"));
        command
    };
    let run = |args: &[&str]| {
        let output = build(binary).args(args).output().unwrap();
        assert!(
            output.status.success(),
            "{}",
            String::from_utf8_lossy(&output.stderr)
        );
    };
    run(&["room", "init", "--name", "project"]);
    run(&[
        "role",
        "init",
        "--name",
        "reviewer",
        "--agent-type",
        "probe",
        "--command",
        "sh",
        "--arg",
        script.to_str().unwrap(),
    ]);
    run(&[
        "role",
        "init",
        "--name",
        "builder",
        "--agent-type",
        "probe",
        "--command",
        "sh",
        "--arg",
        script.to_str().unwrap(),
    ]);
    run(&[
        "room",
        "roles",
        "--room",
        "project",
        "--add",
        "builder,reviewer",
    ]);
    run(&[
        "send",
        "--room",
        "project",
        "--as",
        "human",
        "--to",
        "@reviewer",
        "--text",
        "Please review; @builder will follow up",
    ]);
    let mut dispatcher = build(binary)
        .args(["collab", "run"])
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .unwrap();
    let path = dir.path().join(".tearoom/rooms/project/records.jsonl");
    let deadline = Instant::now() + Duration::from_secs(30);
    let mut delivered = false;
    while Instant::now() < deadline {
        let records = fs::read_to_string(&path).unwrap_or_default();
        if records.contains("reviewed") {
            delivered = true;
            break;
        }
        std::thread::sleep(Duration::from_millis(100));
    }
    let _ = dispatcher.kill();
    let _ = dispatcher.wait();
    let _ = build("hc").args(["server", "stop", "--json"]).output();
    let _ = fs::remove_file(socket);
    assert!(delivered, "the reviewer did not record its reply");
    let state: serde_json::Value =
        serde_json::from_slice(&fs::read(dir.path().join(".tearoom/dispatch.json")).unwrap())
            .unwrap();
    assert_eq!(
        state["intents"][0]["deliveries"].as_array().unwrap().len(),
        1
    );
    assert_eq!(state["intents"][0]["deliveries"][0]["role"], "reviewer");
}
