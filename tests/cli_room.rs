//! Black-box contract for room and record commands.
use std::path::Path;
use std::process::Command;

#[test]
fn installed_skill_exposes_pi_extension_without_a_workspace() {
    let dir = tempfile::tempdir().unwrap();
    let output = skill_command(dir.path())
        .args(["skill", "--pi-extension"])
        .output()
        .unwrap();
    assert!(output.status.success());
    assert_eq!(output.stdout, include_bytes!("../scripts/pi-hc-status.ts"));
    assert_eq!(std::fs::read_dir(dir.path()).unwrap().count(), 0);
    let guide = skill_command(dir.path()).arg("skill").output().unwrap();
    assert!(guide.status.success());
    assert!(String::from_utf8(guide.stdout)
        .unwrap()
        .contains("tearoom agent guide"));
}

#[test]
fn dispatcher_survives_a_busy_delivery_lock_and_duplicate_registration() {
    use std::time::{Duration, Instant};

    let dir = tempfile::tempdir().unwrap();
    let _cleanup = ServiceCleanup::new(dir.path());
    assert!(command(dir.path())
        .args(["room", "init", "--name", "project"])
        .status()
        .unwrap()
        .success());
    let root = tearoom::store::Store::find(dir.path()).unwrap();
    let held = tearoom::collab::dispatch_lock(&root).unwrap();
    let first = command(dir.path())
        .args(["collab", "run"])
        .output()
        .unwrap();
    assert!(
        first.status.success(),
        "{}",
        String::from_utf8_lossy(&first.stderr)
    );
    let second = command(dir.path())
        .args(["collab", "run"])
        .output()
        .unwrap();
    assert!(
        second.status.success(),
        "{}",
        String::from_utf8_lossy(&second.stderr)
    );
    let status = command(dir.path())
        .args(["service", "status"])
        .output()
        .unwrap();
    assert!(status.status.success());
    let status: serde_json::Value = serde_json::from_slice(&status.stdout).unwrap();
    assert_eq!(status["running"], true);
    assert_eq!(status["workspaces"].as_array().unwrap().len(), 1);
    drop(held);
    let append = command(dir.path())
        .args(["send", "--room", "project", "hello"])
        .output()
        .unwrap();
    let deadline = Instant::now() + Duration::from_secs(5);
    let mut scanned = false;
    while Instant::now() < deadline {
        if let Ok(data) = std::fs::read_to_string(root.join("dispatch.json")) {
            let state: serde_json::Value = serde_json::from_str(&data).unwrap();
            if state["cursor"]["project"] == 1 {
                scanned = true;
                break;
            }
        }
        std::thread::sleep(Duration::from_millis(20));
    }
    assert!(append.status.success());
    assert!(
        scanned,
        "dispatcher did not process the record after the lock was released"
    );
}

/// A Tearoom command isolated from any Herdr session the developer may have open.
fn command(dir: &Path) -> Command {
    let mut command = Command::new(env!("CARGO_BIN_EXE_tearoom"));
    let runtime = dir.join(".test-runtime");
    std::fs::create_dir_all(&runtime).unwrap();
    use std::os::unix::fs::PermissionsExt;
    std::fs::set_permissions(&runtime, std::fs::Permissions::from_mode(0o700)).unwrap();
    let session = format!("tearoom-test-{}", std::process::id());
    command
        .current_dir(dir)
        .env("HC_SESSION", session)
        .env("HC_SOCKET", runtime.join("hc.sock"))
        .env("TEAROOM_SERVICE_DIR", dir.join(".test-service"))
        .env("XDG_RUNTIME_DIR", runtime)
        .env("XDG_CONFIG_HOME", dir.join(".test-config"))
        .env("XDG_STATE_HOME", dir.join(".test-state"))
        .env("HOME", dir.join(".test-home"))
        .env("PATH", "/usr/bin:/bin")
        .env("TEAROOM_HERDR_BIN", dir.join("missing-herdr"));
    command
}

fn skill_command(dir: &Path) -> Command {
    let mut command = Command::new(env!("CARGO_BIN_EXE_tearoom"));
    command
        .current_dir(dir)
        .env("TEAROOM_HERDR_BIN", dir.join("missing-herdr"));
    command
}

struct ServiceCleanup {
    workspace: std::path::PathBuf,
}

impl ServiceCleanup {
    fn new(workspace: &Path) -> Self {
        Self {
            workspace: workspace.to_path_buf(),
        }
    }
}

impl Drop for ServiceCleanup {
    fn drop(&mut self) {
        let _ = command(&self.workspace).args(["service", "stop"]).output();
        let _ = command(&self.workspace)
            .args(["hc", "server", "stop", "--json"])
            .output();
    }
}

#[test]
fn room_tree_append_and_tail() {
    let dir = tempfile::tempdir().unwrap();
    let run = |args: &[&str]| {
        let output = command(dir.path()).args(args).output().unwrap();
        assert!(
            output.status.success(),
            "{}",
            String::from_utf8_lossy(&output.stderr)
        );
        output.stdout
    };
    run(&["room", "init", "--name", "root"]);
    run(&["room", "init", "--name", "child", "--parent", "root"]);
    for text in ["first", "second"] {
        run(&["send", "--room", "child", "--text", text, "--as", "test"]);
    }
    let tree = String::from_utf8(run(&["tree"])).unwrap();
    assert_eq!(tree, "root\n└── child\n");
    assert!(run(&["read", "--room", "child", "--tail", "0"]).is_empty());
    let read = String::from_utf8(run(&["read", "--room", "child", "--tail", "1"])).unwrap();
    assert_eq!(read.lines().count(), 1);
    assert!(read.contains("second"));
    let subtree = String::from_utf8(run(&["read", "--room", "root", "--subtree"])).unwrap();
    assert_eq!(subtree.lines().count(), 2);
}

#[test]
fn room_move_changes_tree_without_changing_room_history() {
    let dir = tempfile::tempdir().unwrap();
    let run = |args: &[&str]| command(dir.path()).args(args).output().unwrap();
    assert!(run(&["room", "init", "--name", "root"]).status.success());
    assert!(
        run(&["room", "init", "--name", "child", "--parent", "root"])
            .status
            .success()
    );
    assert!(
        run(&["room", "init", "--name", "leaf", "--parent", "child"])
            .status
            .success()
    );
    assert!(
        run(&["send", "--room", "child", "--text", "kept", "--as", "agent"])
            .status
            .success()
    );
    assert!(run(&[
        "role",
        "init",
        "--name",
        "reviewer",
        "--agent-type",
        "pi",
        "--command",
        "pi"
    ])
    .status
    .success());
    assert!(
        run(&["room", "roles", "--room", "child", "--add", "reviewer"])
            .status
            .success()
    );

    let cycle = run(&["room", "move", "--room", "child", "--parent", "leaf"]);
    assert!(!cycle.status.success());
    let moved = run(&["room", "move", "--room", "child"]);
    assert!(
        moved.status.success(),
        "{}",
        String::from_utf8_lossy(&moved.stderr)
    );
    let room: serde_json::Value = serde_json::from_slice(&moved.stdout).unwrap();
    assert_eq!(room["name"], "child");
    assert!(room["parent"].is_null());
    let tree = String::from_utf8(run(&["tree"]).stdout).unwrap();
    assert!(tree.contains("root\n"));
    assert!(tree.contains("child\n└── leaf\n"));
    assert!(String::from_utf8(run(&["read", "--room", "child"]).stdout)
        .unwrap()
        .contains("kept"));
    assert!(
        String::from_utf8(run(&["room", "roles", "--room", "child"]).stdout)
            .unwrap()
            .contains("reviewer")
    );
}

#[test]
fn roles_and_room_mentions_are_discoverable() {
    let dir = tempfile::tempdir().unwrap();
    let _cleanup = ServiceCleanup::new(dir.path());
    let run = |args: &[&str]| {
        let output = command(dir.path()).args(args).output().unwrap();
        assert!(
            output.status.success(),
            "{}",
            String::from_utf8_lossy(&output.stderr)
        );
        output.stdout
    };
    run(&["room", "init", "--name", "project"]);
    run(&[
        "role",
        "init",
        "--name",
        "reviewer",
        "--agent-type",
        "pi",
        "--command",
        "pi",
        "--arg=one",
        "--arg=two",
        "--description",
        "reviews",
    ]);
    let role: serde_json::Value =
        serde_json::from_slice(&run(&["role", "show", "--name", "reviewer"])).unwrap();
    assert_eq!(
        role["role"]["command"],
        serde_json::json!(["pi", "one", "two"])
    );
    let duplicate = command(dir.path())
        .args([
            "role",
            "init",
            "--name",
            "reviewer",
            "--agent-type",
            "other",
            "--command",
            "other",
            "--description",
            "must not replace",
        ])
        .output()
        .unwrap();
    assert!(!duplicate.status.success());
    assert!(String::from_utf8_lossy(&duplicate.stderr).contains("use 'tearoom role update'"));
    let role: serde_json::Value =
        serde_json::from_slice(&run(&["role", "show", "--name", "reviewer"])).unwrap();
    assert_eq!(role["role"]["description"], "reviews");
    let updated: serde_json::Value = serde_json::from_slice(&run(&[
        "role",
        "update",
        "--name",
        "reviewer",
        "--instructions",
        "Check the whole change.",
    ]))
    .unwrap();
    assert_eq!(updated["role"]["description"], "reviews");
    assert_eq!(
        updated["role"]["command"],
        serde_json::json!(["pi", "one", "two"])
    );
    assert_eq!(updated["role"]["instructions"], "Check the whole change.");
    let launch_change = command(dir.path())
        .args([
            "role",
            "update",
            "--name",
            "reviewer",
            "--agent-type",
            "other",
            "--command",
            "pi-new",
            "--arg",
            "--model",
            "--arg",
            "other-model",
            "--cwd",
            "workdir",
        ])
        .output()
        .unwrap();
    assert!(
        launch_change.status.success(),
        "{}",
        String::from_utf8_lossy(&launch_change.stderr)
    );
    let warning = String::from_utf8_lossy(&launch_change.stderr);
    assert!(warning.contains("running role processes"));
    assert!(warning.contains("newly started processes"));
    let launch_change: serde_json::Value = serde_json::from_slice(&launch_change.stdout).unwrap();
    assert_eq!(launch_change["role"]["agent_type"], "other");
    assert_eq!(
        launch_change["role"]["command"],
        serde_json::json!(["pi-new", "--model", "other-model"])
    );
    assert_eq!(launch_change["role"]["directory"], "workdir");
    run(&["room", "roles", "--room", "project", "--add", "reviewer"]);
    run(&[
        "role",
        "init",
        "--name",
        "builder",
        "--agent-type",
        "codex",
        "--command",
        "codex",
        "--arg=--no-daemon",
        "--arg=--model",
        "--arg=example-model",
    ]);
    let builder: serde_json::Value =
        serde_json::from_slice(&run(&["role", "show", "--name", "builder"])).unwrap();
    assert_eq!(builder["role"]["agent_type"], "codex");
    assert_eq!(
        builder["role"]["command"],
        serde_json::json!(["codex", "--no-daemon", "--model", "example-model"])
    );
    run(&["room", "roles", "--room", "project", "--add", "builder"]);
    let roster = String::from_utf8(run(&["room", "roles", "--room", "project"])).unwrap();
    assert!(roster.contains("reviewer"));
    let record: serde_json::Value = serde_json::from_slice(&run(&[
        "send",
        "--room",
        "project",
        "--as",
        "builder",
        "--text",
        "Please review @reviewer",
    ]))
    .unwrap();
    assert_eq!(record["content"], "Please review @reviewer");
    let selected = serde_json::from_slice::<serde_json::Value>(&run(&[
        "send",
        "--room",
        "project",
        "--as",
        "codex",
        "--text",
        "@reviewer please inspect this",
        "--to",
        "reviewer",
    ]))
    .unwrap();
    assert_eq!(
        selected["content"]["mentions"],
        serde_json::json!(["reviewer"])
    );
    let selected = serde_json::from_slice::<serde_json::Value>(&run(&[
        "send",
        "--room",
        "project",
        "--as",
        "codex",
        "--text",
        "@reviewer please inspect this",
        "--to",
        "builder",
    ]))
    .unwrap();
    assert_eq!(
        selected["content"]["mentions"],
        serde_json::json!(["builder"])
    );
    for target in ["", "   "] {
        let invalid = command(dir.path())
            .args([
                "send",
                "--room",
                "project",
                "--text",
                "@reviewer please inspect this",
                "--to",
                target,
            ])
            .output()
            .unwrap();
        assert!(!invalid.status.success());
        assert!(
            String::from_utf8_lossy(&invalid.stderr).contains("--to requires at least one role")
        );
    }
    let addressed_json = command(dir.path())
        .args([
            "send",
            "--room",
            "project",
            "--json",
            r#"{"text":"raw handoff","mentions":["reviewer"]}"#,
        ])
        .output()
        .unwrap();
    assert!(!addressed_json.status.success());
    assert!(String::from_utf8_lossy(&addressed_json.stderr).contains("cannot address roles"));
    let history = String::from_utf8(run(&["read", "--room", "project"])).unwrap();
    assert_eq!(history.lines().count(), 3, "invalid --to must not append");
    let deadline = std::time::Instant::now() + std::time::Duration::from_secs(15);
    let deliveries = loop {
        let output = run(&["collab", "deliveries"]);
        let deliveries: serde_json::Value = serde_json::from_slice(&output).unwrap();
        if deliveries
            .as_array()
            .is_some_and(|intents| intents.len() == 2)
        {
            break deliveries;
        }
        assert!(
            std::time::Instant::now() < deadline,
            "addressed sends were not dispatched: {deliveries}"
        );
        std::thread::sleep(std::time::Duration::from_millis(50));
    };
    assert_eq!(deliveries[0]["seq"], 2);
    assert_eq!(deliveries[0]["deliveries"][0]["role"], "reviewer");
    assert_eq!(deliveries[1]["seq"], 3);
    assert_eq!(deliveries[1]["deliveries"][0]["role"], "builder");
    run(&["room", "roles", "--room", "project", "--remove", "reviewer"]);
    let roster = String::from_utf8(run(&["room", "roles", "--room", "project"])).unwrap();
    assert!(!roster.contains("reviewer"));
    assert!(String::from_utf8(run(&["tree"]))
        .unwrap()
        .contains("project"));
}

#[test]
fn role_delete_preserves_rooms_and_refuses_bindings_or_unresolved_work() {
    let dir = tempfile::tempdir().unwrap();
    let _cleanup = ServiceCleanup::new(dir.path());
    let run = |args: &[&str]| command(dir.path()).args(args).output().unwrap();
    assert!(run(&["room", "init", "--name", "project"]).status.success());
    assert!(run(&[
        "role",
        "init",
        "--name",
        "reviewer",
        "--agent-type",
        "pi",
        "--command",
        "pi"
    ])
    .status
    .success());
    assert!(run(&[
        "send",
        "--room",
        "project",
        "--text",
        "keep this",
        "--as",
        "person"
    ])
    .status
    .success());
    assert!(
        run(&["room", "roles", "--room", "project", "--add", "reviewer"])
            .status
            .success()
    );
    let bound = run(&["role", "delete", "--name", "reviewer"]);
    assert!(!bound.status.success());
    assert!(String::from_utf8_lossy(&bound.stderr).contains("assigned to a Room"));
    assert!(
        run(&["room", "roles", "--room", "project", "--remove", "reviewer"])
            .status
            .success()
    );

    let workspace = tearoom::store::Store::find(dir.path()).unwrap();
    let held = tearoom::collab::dispatch_lock(&workspace).unwrap();
    let busy = run(&["role", "delete", "--name", "reviewer"]);
    assert!(!busy.status.success());
    assert!(String::from_utf8_lossy(&busy.stderr).contains("dispatcher holds"));
    drop(held);

    std::fs::write(
        dir.path().join(".tearoom/dispatch.json"),
        r#"{"version":1,"cursor":{},"intents":[]}"#,
    )
    .unwrap();
    let workspace_store = tearoom::store::Store::open(dir.path()).unwrap();
    workspace_store
        .append(
            "project",
            "person",
            tearoom::core::AppendRequest {
                content: r#"{"text":"Please review this","mentions":["reviewer"]}"#,
                idempotency_key: "unscanned-role-delete-check",
            },
        )
        .unwrap();
    let unresolved = run(&["role", "delete", "--name", "reviewer"]);
    assert!(!unresolved.status.success());
    assert!(String::from_utf8_lossy(&unresolved.stderr).contains("unscanned messages"));

    std::fs::write(
        dir.path().join(".tearoom/dispatch.json"),
        r#"{"version":1,"cursor":{"project":2},"intents":[{"room":"project","seq":2,"text":"review","deliveries":[{"role":"reviewer","panel":"tr-test","state":"skipped","error":""}]}]}"#,
    )
    .unwrap();
    let deleted = run(&["role", "delete", "--name", "reviewer"]);
    assert!(
        deleted.status.success(),
        "{}",
        String::from_utf8_lossy(&deleted.stderr)
    );
    let listed = run(&["role", "list"]);
    assert!(listed.status.success());
    assert!(listed.stdout.is_empty());
    assert!(
        String::from_utf8_lossy(&run(&["read", "--room", "project"]).stdout).contains("keep this")
    );
}

#[test]
fn concurrent_send_and_role_delete_do_not_drop_a_cli_handoff() {
    let dir = tempfile::tempdir().unwrap();
    let _cleanup = ServiceCleanup::new(dir.path());
    let run = |args: &[&str]| command(dir.path()).args(args).output().unwrap();
    assert!(run(&["room", "init", "--name", "project"]).status.success());
    assert!(run(&[
        "role",
        "init",
        "--name",
        "reviewer",
        "--agent-type",
        "pi",
        "--command",
        "pi"
    ])
    .status
    .success());
    assert!(
        run(&["room", "roles", "--room", "project", "--add", "reviewer"])
            .status
            .success()
    );

    let barrier = std::sync::Arc::new(std::sync::Barrier::new(3));
    let workspace = dir.path().to_path_buf();
    let send_barrier = barrier.clone();
    let send = std::thread::spawn(move || {
        send_barrier.wait();
        command(&workspace)
            .args([
                "send",
                "--room",
                "project",
                "--text",
                "review please",
                "--to",
                "@reviewer",
            ])
            .output()
            .unwrap()
    });
    let workspace = dir.path().to_path_buf();
    let delete_barrier = barrier.clone();
    let delete = std::thread::spawn(move || {
        delete_barrier.wait();
        let remove = command(&workspace)
            .args(["room", "roles", "--room", "project", "--remove", "reviewer"])
            .output()
            .unwrap();
        let delete = command(&workspace)
            .args(["role", "delete", "--name", "reviewer"])
            .output()
            .unwrap();
        (remove, delete)
    });
    barrier.wait();
    let send = send.join().unwrap();
    let (remove, delete) = delete.join().unwrap();
    assert!(remove.status.success());
    let read = run(&["read", "--room", "project"]);
    let history = String::from_utf8(read.stdout).unwrap();
    if send.status.success() {
        assert!(!delete.status.success());
        assert!(String::from_utf8_lossy(&delete.stderr).contains("unscanned messages"));
        assert!(history.contains("review please"));
    } else {
        assert!(delete.status.success());
        assert!(history.is_empty());
    }
}

#[test]
fn room_delete_requires_confirmation_resolves_children_and_unresolved_deliveries() {
    let dir = tempfile::tempdir().unwrap();
    let run = |args: &[&str]| command(dir.path()).args(args).output().unwrap();
    assert!(run(&["room", "init", "--name", "root"]).status.success());
    assert!(
        run(&["room", "init", "--name", "child", "--parent", "root"])
            .status
            .success()
    );
    assert!(run(&[
        "role",
        "init",
        "--name",
        "reviewer",
        "--agent-type",
        "pi",
        "--command",
        "pi"
    ])
    .status
    .success());
    assert!(
        run(&["room", "roles", "--room", "child", "--add", "reviewer"])
            .status
            .success()
    );
    for text in ["keep this", "review please"] {
        assert!(
            run(&["send", "--room", "child", "--text", text, "--as", "person"])
                .status
                .success()
        );
    }

    let parent = run(&["room", "delete", "--room", "root"]);
    assert!(!parent.status.success());
    assert!(String::from_utf8_lossy(&parent.stderr).contains("child rooms"));

    let unconfirmed = run(&["room", "delete", "--room", "child"]);
    assert!(!unconfirmed.status.success());
    assert!(String::from_utf8_lossy(&unconfirmed.stderr).contains("record(s) permanently"));

    std::fs::write(
        dir.path().join(".tearoom/dispatch.json"),
        r#"{"version":1,"cursor":{"child":2},"intents":[{"room":"child","seq":2,"text":"review please","deliveries":[{"role":"reviewer","panel":"tr-test","state":"pending","error":""}]}]}"#,
    )
    .unwrap();
    let unresolved = run(&["room", "delete", "--room", "child"]);
    assert!(!unresolved.status.success());
    assert!(String::from_utf8_lossy(&unresolved.stderr).contains("unresolved deliveries"));

    std::fs::write(
        dir.path().join(".tearoom/dispatch.json"),
        r#"{"version":1,"cursor":{"child":2},"intents":[{"room":"child","seq":2,"text":"review please","deliveries":[{"role":"reviewer","panel":"tr-test","state":"completed","error":""}]}]}"#,
    )
    .unwrap();
    let deleted = run(&["room", "delete", "--room", "child", "--confirm"]);
    assert!(
        deleted.status.success(),
        "{}",
        String::from_utf8_lossy(&deleted.stderr)
    );
    let report: serde_json::Value = serde_json::from_slice(&deleted.stdout).unwrap();
    assert_eq!(report["deleted"], true);
    assert_eq!(report["records"], 2);
    let stderr = String::from_utf8_lossy(&deleted.stderr);
    assert!(stderr.contains("panel tr-"), "{stderr}");
    assert!(stderr.contains("(role reviewer)"), "{stderr}");
    assert!(stderr.contains("hc kill"));
    let tree = String::from_utf8(run(&["tree"]).stdout).unwrap();
    assert_eq!(tree, "root\n");
    assert_eq!(run(&["read", "--room", "child"]).status.code(), Some(1));
    assert!(!run(&["room", "roles", "--room", "child"]).status.success());
    let listed = String::from_utf8(run(&["role", "list"]).stdout).unwrap();
    assert!(
        listed.contains("reviewer"),
        "role definitions survive: {listed}"
    );
    let dispatch = std::fs::read_to_string(dir.path().join(".tearoom/dispatch.json")).unwrap();
    assert!(!dispatch.contains("\"child\""), "{dispatch}");
    assert!(
        !dir.path().join(".tearoom/herdr.json").exists()
            || !std::fs::read_to_string(dir.path().join(".tearoom/herdr.json"))
                .unwrap()
                .contains("\"child\"")
    );
}

#[test]
fn exit_codes_and_signature_follow_the_cli_contract() {
    let dir = tempfile::tempdir().unwrap();
    let run = |args: &[&str]| {
        command(dir.path())
            .env_remove("TEAROOM_ACTOR")
            .args(args)
            .output()
            .unwrap()
    };
    assert_eq!(run(&["read"]).status.code(), Some(4));
    assert!(run(&["room", "init", "--name", "project"]).status.success());
    let appended = run(&["send", "--as", "  alice  ", "--text", "hello"]);
    assert!(appended.status.success());
    let record: serde_json::Value = serde_json::from_slice(&appended.stdout).unwrap();
    assert_eq!(record["author"], "alice");
    assert_eq!(run(&["read", "--room", "missing"]).status.code(), Some(1));
}

#[test]
fn command_specific_flags_and_boolean_values_are_checked() {
    let dir = tempfile::tempdir().unwrap();
    let run = |args: &[&str]| command(dir.path()).args(args).output().unwrap();
    assert!(run(&["room", "init", "--name", "root"]).status.success());
    assert_eq!(
        run(&["role", "list", "--name", "unexpected"]).status.code(),
        Some(1)
    );
    assert_eq!(
        run(&["collab", "deliveries", "--room", "root"])
            .status
            .code(),
        Some(1)
    );
    assert!(run(&["read", "--subtree=false"]).status.success());
    assert_eq!(run(&["read", "--subtree=maybe"]).status.code(), Some(1));
    let help = String::from_utf8(run(&["collab", "resolve", "--help"]).stdout).unwrap();
    assert!(help.contains("--outcome"));
}

#[test]
fn room_update_edits_labels_without_touching_identity() {
    let dir = tempfile::tempdir().unwrap();
    let run = |args: &[&str]| {
        let output = command(dir.path()).args(args).output().unwrap();
        assert!(
            output.status.success(),
            "{}",
            String::from_utf8_lossy(&output.stderr)
        );
        output.stdout
    };
    run(&[
        "room",
        "init",
        "--name",
        "child",
        "--description",
        "early work",
    ]);
    run(&[
        "send", "--room", "child", "--text", "kept", "--as", "person",
    ]);
    run(&[
        "send", "--room", "child", "--text", "anchor", "--as", "person",
    ]);
    // A reference written before the edit anchors the Room id.
    run(&[
        "send",
        "--room",
        "child",
        "--text",
        "ack",
        "--as",
        "person",
        "--reply-to",
        "child#2",
    ]);

    let updated: serde_json::Value = serde_json::from_slice(&run(&[
        "room",
        "update",
        "--room",
        "child",
        "--name",
        "workflow",
        "--description",
        "payments review",
    ]))
    .unwrap();
    assert_eq!(updated["id"], "child");
    assert_eq!(updated["name"], "workflow");
    assert_eq!(updated["description"], "payments review");

    // The id remains the address: history, the stored reference, and reads
    // by id all survive the label edit.
    let history = String::from_utf8(run(&["read", "--room", "workflow"])).unwrap();
    assert_eq!(history.lines().count(), 3);
    assert!(history.contains("room\":\"child\""), "{history}");
    assert!(
        history.contains("\"reply_to\":{\"room\":\"child\",\"seq\":2}"),
        "{history}"
    );
    assert_eq!(
        String::from_utf8(run(&["read", "--room", "child"])).unwrap(),
        history
    );
    assert_eq!(String::from_utf8(run(&["tree"])).unwrap(), "workflow\n");
    let card: serde_json::Value =
        serde_json::from_slice(&run(&["room", "show", "--room", "workflow"])).unwrap();
    assert_eq!(card["id"], "child");
    assert_eq!(card["name"], "workflow");
    assert_eq!(card["description"], "payments review");
    // Updating by the id and clearing a description both work.
    run(&["room", "update", "--room", "child", "--description", ""]);
    let card: serde_json::Value =
        serde_json::from_slice(&run(&["room", "show", "--room", "workflow"])).unwrap();
    assert_eq!(card["description"], "");
    let empty = command(dir.path())
        .args(["room", "update", "--room", "workflow"])
        .output()
        .unwrap();
    assert!(!empty.status.success());
    let unknown = command(dir.path())
        .args(["read", "--room", "missing"])
        .output()
        .unwrap();
    assert_eq!(unknown.status.code(), Some(1));
}

#[test]
fn a_birth_name_stays_reserved_and_labels_edit_freely() {
    let dir = tempfile::tempdir().unwrap();
    let run = |args: &[&str]| {
        let output = command(dir.path()).args(args).output().unwrap();
        assert!(
            output.status.success(),
            "{}",
            String::from_utf8_lossy(&output.stderr)
        );
        output.stdout
    };
    run(&["room", "init", "--name", "alpha"]);
    run(&[
        "send", "--room", "alpha", "--text", "old work", "--as", "person",
    ]);
    run(&["room", "update", "--room", "alpha", "--name", "beta"]);

    let tree = String::from_utf8(run(&["tree"])).unwrap();
    assert_eq!(tree, "beta\n");
    // The birth name is the id and stays reserved: neither a new Room nor
    // another Room's label may take it.
    let reused = command(dir.path())
        .args(["room", "init", "--name", "alpha"])
        .output()
        .unwrap();
    assert!(!reused.status.success());
    assert!(String::from_utf8_lossy(&reused.stderr).contains("already in use"));
    run(&["room", "init", "--name", "gamma"]);
    let clash = command(dir.path())
        .args(["room", "update", "--room", "gamma", "--name", "alpha"])
        .output()
        .unwrap();
    assert!(!clash.status.success());
    // Both rooms read their own history by id and by current name.
    let old_history = String::from_utf8(run(&["read", "--room", "beta"])).unwrap();
    assert!(old_history.contains("old work"));
    assert_eq!(
        String::from_utf8(run(&["read", "--room", "alpha"])).unwrap(),
        old_history
    );
    let card: serde_json::Value =
        serde_json::from_slice(&run(&["room", "show", "--room", "beta"])).unwrap();
    assert_eq!(card["id"], "alpha");
    assert_eq!(card["name"], "beta");
    let new_history = String::from_utf8(run(&["read", "--room", "gamma"])).unwrap();
    assert_eq!(new_history.lines().count(), 0);
}
