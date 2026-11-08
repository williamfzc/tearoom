//! Black-box contract for room and record commands.
use std::process::Command;

#[test]
fn room_tree_append_and_tail() {
    let dir = tempfile::tempdir().unwrap();
    let run = |args: &[&str]| {
        let output = Command::new(env!("CARGO_BIN_EXE_tearoom"))
            .current_dir(dir.path())
            .args(args)
            .output()
            .unwrap();
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
fn roles_and_room_mentions_are_discoverable() {
    let dir = tempfile::tempdir().unwrap();
    let run = |args: &[&str]| {
        let output = Command::new(env!("CARGO_BIN_EXE_tearoom"))
            .current_dir(dir.path())
            .args(args)
            .output()
            .unwrap();
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
    run(&["room", "roles", "--room", "project", "--add", "reviewer"]);
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
    assert_eq!(
        serde_json::from_slice::<serde_json::Value>(&run(&["collab", "status"])).unwrap(),
        serde_json::json!([])
    );
}

#[test]
fn exit_codes_and_signature_follow_the_cli_contract() {
    let dir = tempfile::tempdir().unwrap();
    let run = |args: &[&str]| {
        Command::new(env!("CARGO_BIN_EXE_tearoom"))
            .current_dir(dir.path())
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
    let run = |args: &[&str]| {
        Command::new(env!("CARGO_BIN_EXE_tearoom"))
            .current_dir(dir.path())
            .args(args)
            .output()
            .unwrap()
    };
    assert!(run(&["room", "init", "--name", "root"]).status.success());
    assert_eq!(
        run(&["role", "list", "--name", "unexpected"]).status.code(),
        Some(1)
    );
    assert_eq!(
        run(&["collab", "status", "--room", "root"]).status.code(),
        Some(1)
    );
    assert!(run(&["read", "--subtree=false"]).status.success());
    assert_eq!(run(&["read", "--subtree=maybe"]).status.code(), Some(1));
    let help = String::from_utf8(run(&["collab", "resolve", "--help"]).stdout).unwrap();
    assert!(help.contains("--outcome"));
}
