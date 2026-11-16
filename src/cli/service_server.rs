//! The background loop for the local collaboration service: it accepts one
//! Unix-socket request at a time, pumps each registered workspace's
//! dispatcher, and delegates request actions to the actions submodule.
//! Workspace registration and state types live in its CLI parent module.
use super::{identity, read_registry, write_registry, Registry, Request};
use crate::collab::dispatch::Dispatcher;
use crate::store::Store;
use anyhow::{bail, Context, Result};
use serde_json::{json, Value};
use std::fs;
use std::io::Write;
use std::os::unix::net::UnixListener;
use std::time::Duration;

#[path = "service_actions.rs"]
mod actions;
use actions::handle;
#[path = "service_shutdown.rs"]
mod shutdown;
use shutdown::{hc_runtime_for, mark_uncertain, report_cleanup};

pub(super) fn serve() -> Result<()> {
    use fs2::FileExt;
    let state = super::secure_state_dir()?;
    let lock = fs::OpenOptions::new()
        .create(true)
        .truncate(false)
        .write(true)
        .open(state.join("service.lock"))?;
    if lock.try_lock_exclusive().is_err() {
        return Ok(());
    }
    let socket = super::socket()?;
    if socket.exists() {
        fs::remove_file(&socket)?;
    }
    unsafe { libc::umask(0o077) };
    let listener = UnixListener::bind(&socket).context("bind local service socket")?;
    listener
        .set_nonblocking(true)
        .context("set local service socket nonblocking")?;
    let mut registry = read_registry()?;
    for workspace in &mut registry.workspaces {
        if workspace.owner_label.is_empty() {
            workspace.error = "registered workspace lacks an identity-bound owner label; inspect and clean old Agent terminals before reactivation".into();
            continue;
        }
        match identity(&workspace.root) {
            Ok(now) if now == workspace.identity => match mark_uncertain(&workspace.root) {
                Ok(()) if !workspace.error.starts_with("cleanup failed:") => {
                    workspace.error.clear()
                }
                Ok(()) => {}
                Err(error) => workspace.error = format!("recovery paused: {error:#}"),
            },
            Ok(_) => {
                workspace.error = format!(
                    "workspace identity changed; run 'tearoom service forget {}' after checking it",
                    workspace.root.display()
                )
            }
            Err(error) => {
                workspace.error = format!(
                    "workspace unavailable; clean its Agent terminals before running tearoom service forget {} --confirm ({error})",
                    workspace.root.display()
                )
            }
        }
    }
    write_registry(&registry)?;
    let lan = crate::lan::LanState::load(&state)?;
    {
        let lan = lan.clone();
        let sender = crate::lan::cli_sender();
        std::thread::Builder::new()
            .name("tearoom-lan".into())
            .spawn(move || crate::lan::serve(lan, sender))
            .context("start the LAN face thread")?;
    }
    loop {
        if let Ok((mut stream, _)) = listener.accept() {
            let result = (|| -> Result<Value> {
                let input = super::read_line(&mut stream, Duration::from_secs(2))?;
                let request: Request = serde_json::from_str(&input)?;
                handle(request, &mut registry, &lan)
            })();
            let response = match result {
                Ok(value) => json!({"ok":true,"result":value}),
                Err(error) => json!({"ok":false,"error":error.to_string()}),
            };
            let _ = stream.write_all(serde_json::to_string(&response)?.as_bytes());
            if response["result"]["exited"] == true {
                let errors = response["result"]["cleanup_errors"]
                    .as_array()
                    .cloned()
                    .unwrap_or_default()
                    .into_iter()
                    .filter_map(|value| value.as_str().map(str::to_owned))
                    .collect();
                let _ = fs::remove_file(socket);
                return report_cleanup(errors);
            }
        }
        if !registry.stopping {
            pump_workspaces(&mut registry);
        }
        std::thread::sleep(Duration::from_millis(500));
    }
}

fn pump_workspaces(registry: &mut Registry) {
    for workspace in &mut registry.workspaces {
        if workspace.error.starts_with("workspace identity")
            || workspace.error.starts_with("workspace unavailable")
            || workspace.error.starts_with("registered workspace lacks")
        {
            continue;
        }
        if !matches!(identity(&workspace.root), Ok(found) if found == workspace.identity) {
            workspace.error = format!(
                "workspace identity changed; run 'tearoom service forget {}' after checking it",
                workspace.root.display()
            );
            continue;
        }
        if workspace.error.starts_with("recovery paused") {
            if let Err(error) = mark_uncertain(&workspace.root) {
                workspace.error = format!("recovery paused: {error:#}");
                continue;
            }
            workspace.error.clear();
        }
        let result = (|| -> Result<()> {
            let store = Store::open(workspace.root.parent().context("workspace has no parent")?)?;
            if !matches!(identity(store.root()), Ok(found) if found == workspace.identity) {
                bail!("workspace identity changed while opening the dispatcher");
            }
            if std::env::var_os("HC_TERMINAL_ID").is_none() {
                bail!(
                    "hc did not provide a parent terminal ID; refusing to launch unmanaged Agents"
                );
            }
            let dispatcher = Dispatcher {
                store,
                runtime: hc_runtime_for(workspace)?,
            };
            dispatcher.pump()?;
            let intents = dispatcher.deliveries()?;
            crate::collab::herdr::enqueue(dispatcher.store.root(), &intents);
            Ok(())
        })();
        workspace.error = result
            .err()
            .map_or_else(String::new, |error| format!("dispatch delayed: {error:#}"));
    }
}
