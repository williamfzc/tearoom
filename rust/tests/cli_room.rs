//! Black-box contract for room and record commands.
use std::path::Path;
use std::process::Command;

/// A Tearoom command isolated from any Herdr session the developer may have open.
fn command(dir: &Path) -> Command {
    let mut command = Command::new(env!("CARGO_BIN_EXE_tearoom"));
    command
        .current_dir(dir)
        .env("TEAROOM_HERDR_BIN", dir.join("missing-herdr"));
    command
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
        "define",
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
        "define",
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
            "define",
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
            "one",
            "--arg",
            "two",
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
    assert_eq!(launch_change["role"]["directory"], "workdir");
    run(&["room", "roles", "--room", "project", "--add", "reviewer"]);
    run(&[
        "role",
        "define",
        "--name",
        "builder",
        "--agent-type",
        "pi",
        "--command",
        "pi",
    ]);
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
    assert_eq!(
        record["content"]["mentions"],
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
    assert_eq!(
        serde_json::from_slice::<serde_json::Value>(&run(&["collab", "deliveries"])).unwrap(),
        serde_json::json!([])
    );
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
    let run = |args: &[&str]| command(dir.path()).args(args).output().unwrap();
    assert!(run(&["room", "init", "--name", "project"]).status.success());
    assert!(run(&[
        "role",
        "define",
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
    assert!(run(&[
        "send",
        "--room",
        "project",
        "--text",
        "Please review this",
        "--to",
        "@reviewer"
    ])
    .status
    .success());

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
    let run = |args: &[&str]| command(dir.path()).args(args).output().unwrap();
    assert!(run(&["room", "init", "--name", "project"]).status.success());
    assert!(run(&[
        "role",
        "define",
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
