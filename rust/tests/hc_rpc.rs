//! Optional contract smoke against an isolated local hc runtime and test shell.
#![cfg(unix)]

use std::fs;
use std::path::{Path, PathBuf};
use std::process::Command;
use std::time::Duration;
use tearoom::collab::runtime::{HcRuntime, PanelRequest, PanelStatus, Runtime};
use tearoom::collab::Role;
use tearoom::hc::{HcClient, HcError};

struct HcSession {
    socket: PathBuf,
}
impl Drop for HcSession {
    fn drop(&mut self) {
        let _ = Command::new("hc")
            .args(["server", "stop", "--json"])
            .output();
        let _ = fs::remove_file(&self.socket);
    }
}

#[test]
#[ignore = "starts an isolated hc core and terminal; requires hc in PATH"]
fn direct_rpc_and_template_delivery_use_an_isolated_core() {
    let temp = tempfile::tempdir().unwrap();
    let socket = PathBuf::from(format!("/tmp/tearoom-hc-{}.sock", std::process::id()));
    std::env::set_var("HC_SESSION", format!("tearoom-test-{}", std::process::id()));
    std::env::set_var("HC_SOCKET", &socket);
    std::env::set_var("XDG_CONFIG_HOME", temp.path().join("config"));
    std::env::set_var("XDG_STATE_HOME", temp.path().join("state"));
    let _session = HcSession { socket };

    let script = temp.path().join("agent.sh");
    fs::write(&script, "#!/bin/sh\nprintf '\\033]7501;state=idle\\007'\nwhile IFS= read -r line; do\n  printf '\\033]7501;state=working\\007'\n  sleep 1\n  printf '\\033]7501;state=done\\007'\ndone\n").unwrap();
    let templates = temp.path().join("config/herdr-core/templates");
    fs::create_dir_all(&templates).unwrap();
    fs::write(
        templates.join("probe.md"),
        format!(
            "---\ncommand: sh {}\nready: report\n---\nTest instructions.\n",
            script.display()
        ),
    )
    .unwrap();

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

    let runtime = HcRuntime { client, timeout };
    let role = Role {
        agent_type: "probe".into(),
        template: "probe".into(),
        ..Role::default()
    };
    let request = PanelRequest {
        name: "template-probe",
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
        runtime.status("template-probe").unwrap(),
        PanelStatus::Working | PanelStatus::Done
    ));
    runtime
        .client
        .call(
            "terminal.kill",
            serde_json::json!({"terminal":"template-probe"}),
            timeout,
        )
        .unwrap();

    runtime
        .client
        .call(
            "terminal.run",
            serde_json::json!({"name":"direct-probe","cwd":"/tmp", "no_parent":true,
        "argv":["sh", script.to_str().unwrap()]}),
            timeout,
        )
        .unwrap();
    let mut ready = false;
    for _ in 0..50 {
        if runtime.status("direct-probe").unwrap() == PanelStatus::Idle {
            ready = true;
            break;
        }
        std::thread::sleep(Duration::from_millis(50));
    }
    assert!(ready);
    let sent = runtime
        .submit("direct-probe", "hello", PanelStatus::Idle)
        .unwrap();
    assert_eq!(sent.started, Some(true));
    runtime
        .client
        .call(
            "terminal.kill",
            serde_json::json!({"terminal":"direct-probe"}),
            timeout,
        )
        .unwrap();
}
