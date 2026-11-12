//! Short-lived commands for activating and controlling the user service.
//! The parent module owns shared paths, identity and registry storage.
use super::server;
use super::{
    call, call_timeout, canonical_hc_socket, dir, identity, operation_lock, secure_state_dir,
    socket,
};
use crate::cli::{parse, print_json};
use anyhow::{bail, Context, Result};
use serde_json::json;
use std::fs;
use std::os::unix::net::UnixStream;
use std::path::{Path, PathBuf};
use std::time::Duration;

pub(in crate::cli) fn activate(root: &Path) -> Result<()> {
    let _operation = operation_lock()?;
    ensure_running()?;
    register(root)
}

pub(in crate::cli) fn restart(root: &Path, room: &str, role: &str, confirm: bool) -> Result<()> {
    let _operation = operation_lock()?;
    ensure_running()?;
    register(root)?;
    let result = call(&json!({
        "action":"restart","root":fs::canonicalize(root)?,
        "room":room,"role":role,"confirm":confirm
    }))?;
    println!(
        "restarted {} ({})",
        result["panel"].as_str().unwrap_or("panel"),
        result["status"].as_str().unwrap_or("unknown")
    );
    Ok(())
}

fn register(root: &Path) -> Result<()> {
    let root = fs::canonicalize(root)?;
    let identity = identity(&root)?;
    let client = crate::hc::HcClient::from_env()?;
    let hc_socket = canonical_hc_socket(client.socket())?;
    call(&json!({"action":"register","root":root,"identity":identity,"hc_socket":hc_socket}))?;
    Ok(())
}

fn ensure_running() -> Result<()> {
    if UnixStream::connect(socket()?).is_ok() {
        return Ok(());
    }
    secure_state_dir()?;
    let deadline = std::time::Instant::now() + Duration::from_secs(10);
    let mut start_error = None;
    for _ in 0..100 {
        if call_timeout(&json!({"action":"status"}), Duration::from_secs(1)).is_ok() {
            return Ok(());
        }
        if std::time::Instant::now() >= deadline {
            break;
        }
        if let Err(error) = start_hc_service_terminal() {
            start_error = Some(error);
        }
        std::thread::sleep(Duration::from_millis(100));
    }
    match start_error {
        Some(error) => {
            Err(error
                .context("local Tearoom service did not start; workspace records remain saved"))
        }
        None => bail!("local Tearoom service did not start; workspace records remain saved"),
    }
}

fn start_hc_service_terminal() -> Result<bool> {
    let client = crate::hc::HcClient::from_env()?;
    let listed = client.call(
        "terminal.list",
        json!({"all":false}),
        Duration::from_secs(3),
    )?;
    if let Some(terminal) = listed["terminals"].as_array().and_then(|terminals| {
        terminals
            .iter()
            .find(|terminal| terminal["name"] == "tearoom-service")
    }) {
        if terminal["labels"]["tearoom.service"] == "global" {
            return Ok(false);
        }
        bail!("an unverified hc terminal already uses the name tearoom-service; refusing to replace it");
    }
    let executable = std::env::current_exe()?;
    let mut argv = vec!["/usr/bin/env".to_owned()];
    for name in [
        "HOME",
        "PATH",
        "HC_SESSION",
        "HC_SOCKET",
        "XDG_CONFIG_HOME",
        "XDG_RUNTIME_DIR",
        "XDG_STATE_HOME",
    ] {
        if let Some(value) = std::env::var_os(name) {
            argv.push(format!("{name}={}", value.to_string_lossy()));
        }
    }
    argv.push(format!("TEAROOM_SERVICE_DIR={}", dir()?.to_string_lossy()));
    argv.extend([
        executable.to_string_lossy().into_owned(),
        "__service".into(),
    ]);
    client
        .call(
            "terminal.run",
            json!({
                "argv": argv,
                "name": "tearoom-service",
                "cwd": std::env::current_dir()?,
                "no_parent": true,
                "labels": {"tearoom.service":"global"}
            }),
            Duration::from_secs(10),
        )
        .context("start the Tearoom service terminal through hc")?;
    Ok(true)
}

pub(in crate::cli) fn service_command(args: &[String]) -> Result<()> {
    let Some((action, rest)) = args.split_first() else {
        bail!("Usage: tearoom service <status|stop|forget <workspace-root> --confirm>")
    };
    if action == "--help" || action == "-h" {
        println!("Usage: tearoom service <status|stop|forget <workspace-root> --confirm>");
        return Ok(());
    }
    let parsed = parse(rest, &[], &["confirm"])?;
    let _operation = if action == "stop" || action == "forget" {
        Some(operation_lock()?)
    } else {
        None
    };
    if action == "forget" {
        if parsed.positional.len() != 1 {
            bail!("Usage: tearoom service forget <workspace-root> --confirm");
        }
        if !parsed.switches.contains("confirm") {
            bail!("forgetting a workspace may leave Agent terminals unmanaged; pass --confirm after checking service status");
        }
        let root = PathBuf::from(&parsed.positional[0]);
        if !root.is_absolute() {
            bail!("workspace root must be an absolute path from service status");
        }
        print_json(&call(
            &json!({"action":"forget","root":root,"confirm":true}),
        )?)?;
        return Ok(());
    }
    parsed.no_positional()?;
    match action.as_str() {
        "status" => match call(&json!({"action":"status"})) {
            Ok(result) => print_json(&result),
            Err(error) if error.to_string().contains("not running") => {
                print_json(&json!({"running":false,"stopping":false,"workspaces":[]}))
            }
            Err(error) => Err(error),
        },
        "stop" => match call(&json!({"action":"stop"})) {
            Ok(result) => {
                print_json(&result)?;
                if result["cleanup_errors"]
                    .as_array()
                    .is_some_and(|errors| !errors.is_empty())
                {
                    bail!("service is stopped for dispatch but retained for cleanup retry")
                }
                Ok(())
            }
            Err(error) if error.to_string().contains("not running") => {
                println!("service is not running");
                Ok(())
            }
            Err(error) => Err(error),
        },
        _ => bail!("unknown service command {action:?}; use status, stop, or forget"),
    }
}

pub(in crate::cli) fn run_service() -> Result<()> {
    server::serve()
}
