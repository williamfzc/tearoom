//! Black-box contracts for Herdr sessions created from Tearoom Rooms.
use serde_json::{json, Value};
use std::fs;
use std::os::unix::fs::PermissionsExt;
use std::path::PathBuf;
use std::process::{Command, Output};

const FAKE_HERDR: &str = r##"#!/usr/bin/env python3
import json, os, sys, time

state_path = os.environ["FAKE_HERDR_STATE"]
log_path = os.environ["FAKE_HERDR_LOG"]
args = sys.argv[1:]
session = args[1] if len(args) > 1 and args[0] == "--session" else "default"
command = args[2:] if len(args) > 1 and args[0] == "--session" else args
with open(log_path, "a") as log:
    log.write(json.dumps(args) + "\n")
try:
    with open(state_path) as source:
        state = json.load(source)
except FileNotFoundError:
    state = {"sessions": {"default": {"ready": True, "workspaces": []}}, "next": 1}
sessions = state.setdefault("sessions", {})
entry = sessions.setdefault(session, {"ready": False, "workspaces": []})
if command == ["server"]:
    entry["ready"] = True
    with open(state_path, "w") as target:
        json.dump(state, target)
    time.sleep(2)
    raise SystemExit(0)
if command[:2] == ["workspace", "list"]:
    if not entry["ready"]:
        print('{"error":{"code":"server_not_running"}}', file=sys.stderr)
        raise SystemExit(1)
    print(json.dumps({"workspaces": entry["workspaces"]}))
elif command[:2] == ["workspace", "create"]:
    def option(name):
        return command[command.index(name) + 1]
    wid, pane = "ws-" + str(state["next"]), "pane-" + str(state["next"])
    state["next"] += 1
    workspace = {"workspace_id": wid, "label": option("--label"), "panes": [{"pane_id": pane, "terminal_id": "term-" + pane}]}
    entry["workspaces"].append(workspace)
    with open(state_path, "w") as target:
        json.dump(state, target)
    print(json.dumps({"result": {"workspace": {"workspace_id": wid}, "root_pane": {"pane_id": pane}}}))
elif command[:2] == ["pane", "list"]:
    wid = command[command.index("--workspace") + 1]
    workspace = next((workspace for workspace in entry["workspaces"] if workspace["workspace_id"] == wid), None)
    print(json.dumps({"panes": workspace["panes"] if workspace else []}))
else:
    print(json.dumps({"ok": True}))
"##;

struct Fixture {
    dir: tempfile::TempDir,
    log: PathBuf,
    state: PathBuf,
}

impl Fixture {
    fn new() -> Self {
        let dir = tempfile::tempdir().unwrap();
        let fake = dir.path().join("herdr");
        fs::write(&fake, FAKE_HERDR).unwrap();
        fs::set_permissions(&fake, fs::Permissions::from_mode(0o755)).unwrap();
        let log = dir.path().join("herdr.log");
        let state = dir.path().join("herdr-state.json");
        fs::write(
            &state,
            json!({"sessions":{"default":{"ready":true,"workspaces":[]}},"next":1}).to_string(),
        )
        .unwrap();
        Self { dir, log, state }
    }

    fn command(&self) -> Command {
        let mut command = Command::new(env!("CARGO_BIN_EXE_tearoom"));
        command
            .current_dir(self.dir.path())
            .env("TEAROOM_HERDR_BIN", self.dir.path().join("herdr"))
            .env("FAKE_HERDR_STATE", &self.state)
            .env("FAKE_HERDR_LOG", &self.log);
        command
    }

    fn run(&self, args: &[&str]) -> Output {
        self.command().args(args).output().unwrap()
    }

    fn success(&self, args: &[&str]) -> Output {
        let output = self.run(args);
        assert!(
            output.status.success(),
            "{} failed:\n{}",
            args.join(" "),
            String::from_utf8_lossy(&output.stderr)
        );
        output
    }

    fn herdr_calls(&self) -> Vec<Vec<String>> {
        fs::read_to_string(&self.log)
            .unwrap_or_default()
            .lines()
            .map(|line| serde_json::from_str(line).unwrap())
            .collect()
    }

    fn state(&self) -> Value {
        serde_json::from_slice(&fs::read(&self.state).unwrap()).unwrap()
    }

    fn workspace_binding(&self, room: &str) -> Value {
        let bindings: Value =
            serde_json::from_slice(&fs::read(self.dir.path().join(".tearoom/herdr.json")).unwrap())
                .unwrap();
        bindings["rooms"][room].clone()
    }
}

fn session_arg(call: &[String]) -> &str {
    assert_eq!(call.first().map(String::as_str), Some("--session"));
    &call[1]
}

#[test]
fn each_room_gets_a_distinct_named_session_and_repeated_ensure_reuses_its_workspace() {
    let fixture = Fixture::new();
    fixture.success(&["room", "init", "--name", "alpha"]);
    fixture.success(&["room", "init", "--name", "beta"]);
    let first = fixture.workspace_binding("alpha");
    let second = fixture.workspace_binding("beta");
    assert_ne!(first["session"], second["session"]);
    assert_ne!(first["workspace_id"], second["workspace_id"]);

    let before = fixture.state();
    let prior_count = before["sessions"][first["session"].as_str().unwrap()]["workspaces"]
        .as_array()
        .unwrap()
        .len();
    let output = fixture.success(&["room", "herdr", "ensure", "--room", "alpha"]);
    let ensured: Value = serde_json::from_slice(&output.stdout).unwrap();
    assert_eq!(ensured["herdr"]["workspace_id"], first["workspace_id"]);
    assert_eq!(ensured["herdr"]["reused"], true);
    let after = fixture.state();
    let next_count = after["sessions"][first["session"].as_str().unwrap()]["workspaces"]
        .as_array()
        .unwrap()
        .len();
    assert_eq!(prior_count, next_count);
}

#[test]
fn legacy_default_session_binding_is_preserved_without_calling_default_session() {
    let fixture = Fixture::new();
    fixture.success(&["room", "init", "--name", "legacy"]);
    let binding = fixture.workspace_binding("legacy");
    let label = binding["label"].as_str().unwrap();
    let state_path = fixture.dir.path().join(".tearoom/herdr.json");
    let mut bindings: Value = serde_json::from_slice(&fs::read(&state_path).unwrap()).unwrap();
    bindings["rooms"]["legacy"] = json!({
        "session":"default",
        "label":label,
        "workspace_id":"legacy-ws",
        "root_pane_id":"legacy-pane",
        "panes":{},
        "pending_split":null
    });
    fs::write(&state_path, serde_json::to_vec(&bindings).unwrap()).unwrap();
    fs::write(&fixture.log, "").unwrap();
    let mut herdr_state = fixture.state();
    herdr_state["sessions"]["default"]["workspaces"] = json!([{
        "workspace_id":"legacy-ws",
        "label":label,
        "panes":[{"pane_id":"legacy-pane","terminal_id":"legacy-term"}]
    }]);
    fs::write(&fixture.state, serde_json::to_vec(&herdr_state).unwrap()).unwrap();

    fixture.success(&["room", "herdr", "ensure", "--room", "legacy"]);
    let calls = fixture.herdr_calls();
    assert!(!calls.is_empty());
    assert!(calls.iter().all(|call| session_arg(call) != "default"));
    let migrated = fixture.workspace_binding("legacy");
    assert_eq!(migrated["session"], binding["session"]);
    assert_eq!(migrated["previous"]["session"], "default");
    assert_eq!(migrated["previous"]["workspace_id"], "legacy-ws");
    let default_workspaces = fixture.state()["sessions"]["default"]["workspaces"].clone();
    assert_eq!(default_workspaces[0]["workspace_id"], "legacy-ws");
}

#[test]
fn room_open_help_does_not_require_a_herdr_session() {
    let fixture = Fixture::new();
    let output = fixture.success(&["room", "open", "--help"]);
    assert!(String::from_utf8_lossy(&output.stdout).contains("tearoom room open --room <name>"));
    assert!(fixture.herdr_calls().is_empty());
}
