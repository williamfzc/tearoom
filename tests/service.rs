//! Process-level checks for the per-user collaboration service.
#![cfg(unix)]

use std::fs;
use std::os::unix::fs::DirBuilderExt;
use std::path::{Path, PathBuf};
use std::process::{Command, Output};
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

static NEXT: AtomicU64 = AtomicU64::new(0);

struct ServiceSandbox {
    _temp: tempfile::TempDir,
    binary: &'static str,
    runtime: PathBuf,
    session: String,
    service_dir: PathBuf,
    other_sessions: Vec<(String, PathBuf)>,
}

impl ServiceSandbox {
    fn new() -> Self {
        let temp = tempfile::tempdir().unwrap();
        let suffix = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_nanos();
        let key = format!(
            "{}-{suffix}-{}",
            std::process::id(),
            NEXT.fetch_add(1, Ordering::Relaxed)
        );
        let runtime = PathBuf::from(format!("/tmp/tearoom-service-{key}"));
        fs::DirBuilder::new().mode(0o700).create(&runtime).unwrap();
        let service_dir = temp.path().join("service");
        Self {
            _temp: temp,
            binary: env!("CARGO_BIN_EXE_tearoom"),
            runtime,
            session: format!("tearoom-service-{key}"),
            service_dir,
            other_sessions: Vec::new(),
        }
    }

    fn command(&self, cwd: &Path) -> Command {
        self.command_with(cwd, &self.session, &self.runtime.join("hc.sock"))
    }

    fn command_with(&self, cwd: &Path, session: &str, socket: &Path) -> Command {
        let runtime = socket.parent().unwrap_or(&self.runtime);
        let mut command = Command::new(self.binary);
        command
            .current_dir(cwd)
            .env("HC_SESSION", session)
            .env("HC_SOCKET", socket)
            .env("TEAROOM_SERVICE_DIR", &self.service_dir)
            .env("XDG_RUNTIME_DIR", runtime)
            .env("XDG_CONFIG_HOME", self._temp.path().join("config"))
            .env("XDG_STATE_HOME", self._temp.path().join("state"))
            .env("HOME", self._temp.path().join("home"))
            .env("PATH", "/usr/bin:/bin")
            .env("TEAROOM_HERDR_BIN", self._temp.path().join("missing-herdr"));
        command
    }

    fn output(&self, cwd: &Path, args: &[&str]) -> Output {
        self.command(cwd).args(args).output().unwrap()
    }

    fn success(&self, cwd: &Path, args: &[&str]) -> Output {
        let output = self.output(cwd, args);
        assert!(
            output.status.success(),
            "tearoom {} failed:\n{}",
            args.join(" "),
            String::from_utf8_lossy(&output.stderr)
        );
        output
    }

    fn status(&self, cwd: &Path) -> serde_json::Value {
        let output = self.success(cwd, &["service", "status"]);
        serde_json::from_slice(&output.stdout).unwrap()
    }

    fn setup_workspace(&self, path: &Path, script: &Path) {
        fs::create_dir_all(path).unwrap();
        self.success(path, &["room", "init", "--name", "project"]);
        self.success(
            path,
            &[
                "role",
                "init",
                "--name",
                "worker",
                "--agent-type",
                "probe",
                "--command",
                "sh",
                "--arg",
                script.to_str().unwrap(),
            ],
        );
        self.success(
            path,
            &["room", "roles", "--room", "project", "--add", "worker"],
        );
    }

    fn send(&self, path: &Path, text: &str) {
        self.success(
            path,
            &[
                "send", "--room", "project", "--as", "human", "--to", "@worker", "--text", text,
            ],
        );
    }

    fn wait_for(&self, description: &str, mut ready: impl FnMut() -> bool) {
        let deadline = Instant::now() + Duration::from_secs(30);
        while Instant::now() < deadline {
            if ready() {
                return;
            }
            std::thread::sleep(Duration::from_millis(100));
        }
        panic!("timed out waiting for {description}");
    }

    fn live_terminals(&self) -> Vec<String> {
        let root = self._temp.path();
        let output = self.success(root, &["hc", "ls", "--json"]);
        let value: serde_json::Value = serde_json::from_slice(&output.stdout).unwrap();
        value["terminals"]
            .as_array()
            .unwrap()
            .iter()
            .filter(|terminal| terminal["state"] == "live")
            .filter_map(|terminal| terminal["name"].as_str().map(str::to_owned))
            .collect()
    }
}

impl Drop for ServiceSandbox {
    fn drop(&mut self) {
        let root = self._temp.path().to_path_buf();
        let _ = self.output(&root, &["service", "stop"]);
        let _ = self.output(&root, &["hc", "server", "stop", "--json"]);
        for (session, runtime) in &self.other_sessions {
            let mut command = Command::new(self.binary);
            let _ = command
                .args(["hc", "server", "stop", "--json"])
                .env("HC_SESSION", session)
                .env("HC_SOCKET", runtime.join("hc.sock"))
                .env("TEAROOM_SERVICE_DIR", &self.service_dir)
                .env("XDG_RUNTIME_DIR", runtime)
                .env("XDG_CONFIG_HOME", self._temp.path().join("config"))
                .env("XDG_STATE_HOME", self._temp.path().join("state"))
                .env("HOME", self._temp.path().join("home"))
                .env("PATH", "/usr/bin:/bin")
                .output();
        }
        let _ = fs::remove_dir_all(&self.runtime);
    }
}

fn agent_script(path: &Path, marker: &Path) {
    fs::write(
        path,
        format!(
            "#!/bin/sh\nprintf '\\033]7501;state=idle\\007'\nwhile IFS= read -r line; do printf '%s\\n' \"$line\" >> '{}'; printf '\\033]7501;state=idle\\007'; done\n",
            marker.display()
        ),
    )
    .unwrap();
}

#[test]
#[ignore = "starts an isolated user service and hc server"]
fn addressed_send_starts_service_that_outlives_the_cli() {
    let sandbox = ServiceSandbox::new();
    let root = sandbox._temp.path().join("workspace");
    let script = sandbox._temp.path().join("agent.sh");
    let marker = sandbox._temp.path().join("seen");
    agent_script(&script, &marker);
    sandbox.setup_workspace(&root, &script);

    sandbox.send(&root, "stay alive");
    sandbox.wait_for("agent delivery after the send command exits", || {
        fs::read_to_string(&marker)
            .unwrap_or_default()
            .contains("stay alive")
    });
    let status = sandbox.status(&root);
    assert_eq!(status["running"], true);
    assert_eq!(status["workspaces"].as_array().unwrap().len(), 1);
}

#[test]
#[ignore = "starts an isolated user service and hc server"]
fn service_survives_a_client_that_connects_and_closes_without_a_request() {
    use std::os::unix::net::UnixStream;

    let sandbox = ServiceSandbox::new();
    let root = sandbox._temp.path().join("workspace");
    let script = sandbox._temp.path().join("agent.sh");
    let marker = sandbox._temp.path().join("seen");
    agent_script(&script, &marker);
    sandbox.setup_workspace(&root, &script);
    sandbox.send(&root, "start before probe");
    sandbox.wait_for("agent delivery before disconnect", || {
        fs::read_to_string(&marker)
            .unwrap_or_default()
            .contains("start before probe")
    });

    drop(UnixStream::connect(sandbox.service_dir.join("service.sock")).unwrap());
    std::thread::sleep(Duration::from_millis(750));
    let status = sandbox.status(&root);
    assert_eq!(status["running"], true);
    assert_eq!(status["workspaces"].as_array().unwrap().len(), 1);
}

#[test]
#[ignore = "starts an isolated user service and hc server"]
fn one_service_dispatches_two_workspaces_and_duplicate_activations_register_once() {
    let sandbox = ServiceSandbox::new();
    let first = sandbox._temp.path().join("workspace-one");
    let second = sandbox._temp.path().join("workspace-two");
    let first_script = sandbox._temp.path().join("agent-one.sh");
    let second_script = sandbox._temp.path().join("agent-two.sh");
    let first_marker = sandbox._temp.path().join("one.seen");
    let second_marker = sandbox._temp.path().join("two.seen");
    agent_script(&first_script, &first_marker);
    agent_script(&second_script, &second_marker);
    sandbox.setup_workspace(&first, &first_script);
    sandbox.setup_workspace(&second, &second_script);

    sandbox.send(&first, "first workspace");
    sandbox.send(&second, "second workspace");
    sandbox.success(&first, &["collab", "run"]);
    sandbox.success(&second, &["collab", "run"]);
    sandbox.wait_for("both workspace agents", || {
        fs::read_to_string(&first_marker)
            .unwrap_or_default()
            .contains("first workspace")
            && fs::read_to_string(&second_marker)
                .unwrap_or_default()
                .contains("second workspace")
    });

    let status = sandbox.status(&first);
    let workspaces = status["workspaces"].as_array().unwrap();
    assert_eq!(status["running"], true);
    assert_eq!(workspaces.len(), 2);
    let first_root = fs::canonicalize(first.join(".tearoom")).unwrap();
    let second_root = fs::canonicalize(second.join(".tearoom")).unwrap();
    assert!(workspaces
        .iter()
        .any(|entry| entry["root"] == first_root.to_str().unwrap()));
    assert!(workspaces
        .iter()
        .any(|entry| entry["root"] == second_root.to_str().unwrap()));
    let terminals = sandbox.live_terminals();
    let first_panel =
        tearoom::collab::panel_name(&first.join(".tearoom"), "project", "worker").unwrap();
    let second_panel =
        tearoom::collab::panel_name(&second.join(".tearoom"), "project", "worker").unwrap();
    assert!(
        terminals.contains(&first_panel),
        "first agent missing: {terminals:?}"
    );
    assert!(
        terminals.contains(&second_panel),
        "second agent missing: {terminals:?}"
    );
}

#[test]
#[ignore = "starts isolated user services and hc servers"]
fn service_rejects_a_second_hc_session_without_disturbing_either_session() {
    let mut sandbox = ServiceSandbox::new();
    let first = sandbox._temp.path().join("workspace-one");
    let second = sandbox._temp.path().join("workspace-two");
    let first_script = sandbox._temp.path().join("agent-one.sh");
    let second_script = sandbox._temp.path().join("agent-two.sh");
    let first_marker = sandbox._temp.path().join("one.seen");
    let second_marker = sandbox._temp.path().join("two.seen");
    agent_script(&first_script, &first_marker);
    agent_script(&second_script, &second_marker);
    sandbox.setup_workspace(&first, &first_script);
    sandbox.setup_workspace(&second, &second_script);
    sandbox.send(&first, "first session owns service");
    sandbox.wait_for("first session agent", || {
        fs::read_to_string(&first_marker)
            .unwrap_or_default()
            .contains("first session owns service")
    });

    let other_session = format!("{}-other", sandbox.session);
    let other_runtime = sandbox._temp.path().join("other-runtime");
    fs::DirBuilder::new()
        .mode(0o700)
        .create(&other_runtime)
        .unwrap();
    sandbox
        .other_sessions
        .push((other_session.clone(), other_runtime.clone()));
    let other_socket = other_runtime.join("hc.sock");
    let unrelated = sandbox
        .command_with(&second, &other_session, &other_socket)
        .args([
            "hc",
            "run",
            "--bg",
            "--name",
            "unrelated",
            "--no-parent",
            "/bin/sh",
            "-c",
            "exec sleep 3600",
        ])
        .output()
        .unwrap();
    assert!(
        unrelated.status.success(),
        "{}",
        String::from_utf8_lossy(&unrelated.stderr)
    );
    let foreign_send = sandbox
        .command_with(&second, &other_session, &other_socket)
        .args([
            "send",
            "--room",
            "project",
            "--as",
            "human",
            "--to",
            "@worker",
            "--text",
            "must stay saved",
        ])
        .output()
        .unwrap();
    assert!(!foreign_send.status.success());
    let error = String::from_utf8_lossy(&foreign_send.stderr);
    assert!(error.contains("record saved"), "{error}");
    assert!(error.contains("uses hc socket"), "{error}");
    assert!(
        fs::read_to_string(second.join(".tearoom/rooms/project/records.jsonl"))
            .unwrap()
            .contains("must stay saved")
    );

    let original_status = sandbox.status(&first);
    assert_eq!(original_status["running"], true);
    assert_eq!(original_status["workspaces"].as_array().unwrap().len(), 1);
    let primary_live = sandbox.live_terminals();
    assert!(primary_live.contains(&"tearoom-service".to_owned()));
    let foreign_live = sandbox
        .command_with(&second, &other_session, &other_socket)
        .args(["hc", "ls", "--json"])
        .output()
        .unwrap();
    assert!(foreign_live.status.success());
    let foreign_live: serde_json::Value = serde_json::from_slice(&foreign_live.stdout).unwrap();
    assert!(foreign_live["terminals"]
        .as_array()
        .unwrap()
        .iter()
        .any(|terminal| terminal["name"] == "unrelated" && terminal["state"] == "live"));
    assert!(!foreign_live["terminals"]
        .as_array()
        .unwrap()
        .iter()
        .any(|terminal| terminal["name"] == "tearoom-service"));
}

#[test]
#[ignore = "starts an isolated user service and hc server"]
fn a_new_addressed_message_restarts_a_stopped_service() {
    let sandbox = ServiceSandbox::new();
    let root = sandbox._temp.path().join("workspace");
    let script = sandbox._temp.path().join("agent.sh");
    let marker = sandbox._temp.path().join("seen");
    fs::write(
        &script,
        format!(
            "#!/bin/sh\nprintf '\\033]7501;state=idle\\007'\nwhile IFS= read -r line; do printf '\\033]7501;state=working\\007'; printf '%s\\n' \"$line\" >> '{}'; sleep 1; case \"$line\" in *'record 3'*) reply_seq=3;; *) reply_seq=1;; esac; '{}' send --room project --as worker --reply-to \"project#$reply_seq\" --text received; printf '\\033]7501;state=done\\007'; done\n",
            marker.display(),
            env!("CARGO_BIN_EXE_tearoom")
        ),
    )
    .unwrap();
    sandbox.setup_workspace(&root, &script);

    sandbox.send(&root, "before stop");
    sandbox.wait_for("first delivery", || {
        fs::read_to_string(&marker)
            .unwrap_or_default()
            .contains("before stop")
    });
    sandbox.wait_for("first delivery completion", || {
        let output = sandbox.output(&root, &["collab", "deliveries"]);
        serde_json::from_slice::<serde_json::Value>(&output.stdout)
            .ok()
            .and_then(|intents| {
                intents.as_array().and_then(|intents| {
                    intents
                        .iter()
                        .find(|intent| intent["seq"] == 1)
                        .and_then(|intent| intent["deliveries"].as_array())
                        .and_then(|deliveries| deliveries.first())
                        .map(|delivery| delivery["state"] == "completed")
                })
            })
            .unwrap_or(false)
    });
    sandbox.success(&root, &["service", "stop"]);
    assert_eq!(sandbox.status(&root)["running"], false);

    sandbox.send(&root, "after stop");
    sandbox.wait_for("service restart delivery", || {
        fs::read_to_string(&marker)
            .unwrap_or_default()
            .contains("after stop")
    });
    let status = sandbox.status(&root);
    assert_eq!(status["running"], true);
    assert_eq!(status["workspaces"].as_array().unwrap().len(), 1);
    assert!(
        fs::read_to_string(root.join(".tearoom/rooms/project/records.jsonl"))
            .unwrap()
            .contains("after stop")
    );
    let deliveries: serde_json::Value =
        serde_json::from_slice(&sandbox.success(&root, &["collab", "deliveries"]).stdout).unwrap();
    assert_eq!(deliveries[0]["seq"], 1);
    assert_eq!(deliveries[0]["deliveries"][0]["state"], "completed");
    assert_eq!(deliveries[1]["seq"], 3);
    assert_eq!(deliveries[1]["deliveries"][0]["state"], "completed");
}

#[test]
#[ignore = "starts an isolated user service and hc server"]
fn stop_reports_replaced_workspace_without_killing_new_workspace_terminals() {
    use tearoom::collab::panel_name;

    let sandbox = ServiceSandbox::new();
    let root = sandbox._temp.path().join("workspace");
    let old_root = sandbox._temp.path().join("workspace-old");
    let script = sandbox._temp.path().join("old-agent.sh");
    let marker = sandbox._temp.path().join("old-agent.seen");
    agent_script(&script, &marker);
    sandbox.setup_workspace(&root, &script);
    sandbox.send(&root, "registered before replacement");
    sandbox.wait_for("old workspace agent", || {
        fs::read_to_string(&marker)
            .unwrap_or_default()
            .contains("registered before replacement")
    });
    let old_panel = panel_name(&root.join(".tearoom"), "project", "worker").unwrap();

    fs::rename(&root, &old_root).unwrap();
    fs::create_dir_all(&root).unwrap();
    sandbox.success(&root, &["room", "init", "--name", "project"]);
    sandbox.success(
        &root,
        &[
            "hc",
            "run",
            "--bg",
            "--name",
            "new-workspace-terminal",
            "--no-parent",
            "/bin/sh",
            "-c",
            "exec sleep 3600",
        ],
    );
    sandbox.wait_for("new workspace terminal", || {
        sandbox
            .live_terminals()
            .contains(&"new-workspace-terminal".to_owned())
    });

    let stopped = sandbox.output(&root, &["service", "stop"]);
    assert!(!stopped.status.success());
    let result: serde_json::Value = serde_json::from_slice(&stopped.stdout).unwrap();
    let cleanup_errors = result["cleanup_errors"].as_array().unwrap();
    assert!(!cleanup_errors.is_empty());
    assert!(cleanup_errors
        .iter()
        .any(|error| error.as_str().unwrap_or_default().contains("identity")));
    let error = String::from_utf8_lossy(&stopped.stderr);
    assert!(error.contains("retained for cleanup retry"), "{error}");

    let live = sandbox.live_terminals();
    assert!(
        !live.contains(&old_panel),
        "old service Agent survived replacement cleanup: {live:?}"
    );
    assert!(live.contains(&"new-workspace-terminal".to_owned()));
}

#[test]
#[ignore = "starts an isolated user service and hc server"]
fn stop_cleans_service_agents_but_keeps_unrelated_terminal_and_reads_do_not_restart() {
    let sandbox = ServiceSandbox::new();
    let root = sandbox._temp.path().join("workspace");
    let script = sandbox._temp.path().join("agent.sh");
    let marker = sandbox._temp.path().join("seen");
    agent_script(&script, &marker);
    sandbox.setup_workspace(&root, &script);

    sandbox.success(
        &root,
        &[
            "hc",
            "run",
            "--bg",
            "--name",
            "unrelated",
            "--no-parent",
            "/bin/sh",
            "-c",
            "exec sleep 3600",
        ],
    );
    sandbox.send(&root, "stop me");
    sandbox.wait_for("service agent before stop", || {
        fs::read_to_string(&marker)
            .unwrap_or_default()
            .contains("stop me")
    });
    let service_panels = sandbox.live_terminals();
    sandbox.success(&root, &["service", "stop"]);
    sandbox.wait_for("service agent cleanup", || {
        let live = sandbox.live_terminals();
        service_panels
            .iter()
            .filter(|name| name.as_str() != "unrelated")
            .all(|name| !live.contains(name))
    });
    assert!(sandbox.live_terminals().contains(&"unrelated".to_owned()));

    sandbox.success(&root, &["read", "--room", "project"]);
    assert_eq!(sandbox.status(&root)["running"], false);
}

#[test]
#[ignore = "starts an isolated user service and hc server"]
fn killing_service_terminal_kills_its_agents_but_preserves_unrelated_terminal() {
    use tearoom::collab::panel_name;

    let sandbox = ServiceSandbox::new();
    let root = sandbox._temp.path().join("workspace");
    let script = sandbox._temp.path().join("agent.sh");
    let marker = sandbox._temp.path().join("seen");
    agent_script(&script, &marker);
    sandbox.setup_workspace(&root, &script);
    sandbox.send(&root, "watch parent death");
    sandbox.wait_for("service agent to receive its work", || {
        fs::read_to_string(&marker)
            .unwrap_or_default()
            .contains("watch parent death")
    });
    sandbox.success(
        &root,
        &[
            "hc",
            "run",
            "--bg",
            "--name",
            "unrelated",
            "--no-parent",
            "/bin/sh",
            "-c",
            "exec sleep 3600",
        ],
    );

    let agent = panel_name(&root.join(".tearoom"), "project", "worker").unwrap();
    sandbox.wait_for("service and agent terminals", || {
        let live = sandbox.live_terminals();
        live.contains(&"tearoom-service".to_owned())
            && live.contains(&agent)
            && live.contains(&"unrelated".to_owned())
    });
    sandbox.success(&root, &["hc", "kill", "tearoom-service"]);
    sandbox.wait_for("hc parent-terminal cleanup", || {
        let live = sandbox.live_terminals();
        !live.contains(&"tearoom-service".to_owned())
            && !live.contains(&agent)
            && live.contains(&"unrelated".to_owned())
    });
    assert_eq!(sandbox.status(&root)["running"], false);
}

#[test]
#[ignore = "starts an isolated user service and hc server"]
fn restart_marks_uncertain_delivery_without_replaying_it() {
    let sandbox = ServiceSandbox::new();
    let root = sandbox._temp.path().join("workspace");
    let script = sandbox._temp.path().join("agent.sh");
    let marker = sandbox._temp.path().join("seen");
    agent_script(&script, &marker);
    sandbox.setup_workspace(&root, &script);
    sandbox.send(&root, "once only");
    sandbox.wait_for("initial delivery", || {
        fs::read_to_string(&marker)
            .unwrap_or_default()
            .contains("once only")
    });
    let first_count = fs::read_to_string(&marker)
        .unwrap()
        .matches("once only")
        .count();
    assert_eq!(first_count, 1);

    sandbox.success(&root, &["service", "stop"]);
    sandbox.success(&root, &["collab", "run"]);
    std::thread::sleep(Duration::from_secs(2));
    let deliveries = sandbox.success(&root, &["collab", "deliveries"]);
    let text = String::from_utf8_lossy(&deliveries.stdout);
    assert!(
        text.contains("unknown"),
        "delivery was not marked unknown: {text}"
    );
    let count = fs::read_to_string(&marker)
        .unwrap()
        .matches("once only")
        .count();
    assert_eq!(count, 1, "uncertain delivery was replayed");
}
