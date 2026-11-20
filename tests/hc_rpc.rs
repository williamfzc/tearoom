//! Optional contract smoke against an isolated local hc runtime and test shell.
#![cfg(unix)]

use std::fs;
use std::path::{Path, PathBuf};
use std::process::Command;
use std::time::{Duration, Instant};
use tearoom::collab::runtime::{HcRuntime, PanelRequest, PanelStatus, Runtime};
use tearoom::collab::Role;
use tearoom::hc::{HcClient, HcError};

struct HcSession {
    socket: PathBuf,
}
impl Drop for HcSession {
    fn drop(&mut self) {
        let _ = Command::new(env!("CARGO_BIN_EXE_tearoom"))
            .args(["hc", "server", "stop", "--json"])
            .output();
        let _ = fs::remove_file(&self.socket);
    }
}

#[test]
#[ignore = "starts an isolated hc core and terminal"]
fn direct_rpc_and_delivery_use_an_isolated_core() {
    let temp = tempfile::tempdir().unwrap();
    let socket = PathBuf::from(format!("/tmp/tearoom-hc-{}.sock", std::process::id()));
    std::env::set_var("HC_SESSION", format!("tearoom-test-{}", std::process::id()));
    std::env::set_var("HC_SOCKET", &socket);
    std::env::set_var("XDG_CONFIG_HOME", temp.path().join("config"));
    std::env::set_var("XDG_STATE_HOME", temp.path().join("state"));
    let _session = HcSession { socket };

    let start = Command::new(env!("CARGO_BIN_EXE_tearoom"))
        .args(["hc", "ls", "--json"])
        .output()
        .unwrap();
    assert!(
        start.status.success(),
        "{}",
        String::from_utf8_lossy(&start.stderr)
    );

    let script = temp.path().join("agent.sh");
    fs::write(&script, "#!/bin/sh\nprintf '\\033]7501;state=idle\\007'\nwhile IFS= read -r line; do\n  printf '\\033]7501;state=working\\007'\n  sleep 1\n  printf '\\033]7501;state=done\\007'\ndone\n").unwrap();
    let client = HcClient::from_env().unwrap();
    let timeout = Duration::from_secs(15);
    let list = client
        .call("terminal.list", serde_json::json!({"all":false}), timeout)
        .unwrap();
    assert!(list["terminals"].as_array().unwrap().is_empty());
    assert!(matches!(
        client.call(
            "terminal.get",
            serde_json::json!({"terminal":"missing"}),
            timeout
        ),
        Err(HcError::Remote(_))
    ));

    let runtime = HcRuntime {
        client,
        timeout,
        workspace_label: Some("isolated-rpc-test".into()),
        caller: None,
    };
    let role = Role {
        agent_type: "probe".into(),
        command: vec!["sh".into(), script.to_string_lossy().into_owned()],
        ..Role::default()
    };
    let request = PanelRequest {
        name: "direct-probe",
        room: "project",
        role_name: "reviewer",
        role: &role,
        directory: Path::new("/tmp"),
        task: "first task",
    };
    let result = runtime.ensure(&request).unwrap();
    assert!(result.accepted);
    assert_eq!(result.started, Some(true));
    assert!(matches!(
        runtime.status("direct-probe").unwrap(),
        PanelStatus::Idle | PanelStatus::Working | PanelStatus::Done
    ));
    runtime
        .client
        .call(
            "terminal.kill",
            serde_json::json!({"terminal":"direct-probe"}),
            timeout,
        )
        .unwrap();

    let blocked = Role {
        command: vec![
            "sh".into(),
            "-c".into(),
            "printf 'Choose a release target.\\n\\033]7501;state=blocked:kind=question\\007'; sleep 30".into(),
        ],
        ..role
    };
    runtime
        .launch(
            "blocked-probe",
            "project",
            "worker",
            &blocked,
            Path::new("/tmp"),
        )
        .unwrap();
    let deadline = Instant::now() + timeout;
    let observation = loop {
        if let Some(panel) = runtime.blocked_panels().unwrap().remove("blocked-probe") {
            break panel;
        }
        assert!(Instant::now() < deadline, "hc blockage was not observed");
        std::thread::sleep(Duration::from_millis(50));
    };
    assert_eq!(
        runtime.status("blocked-probe").unwrap(),
        PanelStatus::Blocked
    );
    let report = runtime
        .client
        .call(
            "terminal.get",
            serde_json::json!({"terminal":observation.address}),
            timeout,
        )
        .unwrap();
    assert_eq!(report["name"], "blocked-probe");
    assert!(report["status_ms"].is_u64());
    assert_eq!(observation.reason, "question");
    let screen = runtime
        .client
        .call(
            "terminal.read",
            serde_json::json!({"terminal":observation.address, "lines":80}),
            timeout,
        )
        .unwrap();
    assert!(screen["text"]
        .as_str()
        .unwrap()
        .contains("Choose a release target."));
    let same = runtime.blocked_panels().unwrap();
    assert_eq!(same["blocked-probe"].episode, observation.episode);
    runtime.stop("blocked-probe").unwrap();
}
