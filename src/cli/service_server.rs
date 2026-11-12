//! The background loop for the local collaboration service.
//! Workspace registration and state types live in its CLI parent module.
use super::{
    canonical_hc_socket, identity, owner_label, read_registry, write_registry, Registry, Request,
    Workspace,
};
use crate::collab::dispatch::Dispatcher;
use crate::collab::runtime::Runtime;
use crate::store::Store;
use anyhow::{bail, Context, Result};
use serde_json::{json, Value};
use std::fs;
use std::io::Write;
use std::os::unix::net::UnixListener;
use std::time::Duration;

#[path = "service_shutdown.rs"]
mod shutdown;
use shutdown::{cleanup, hc_runtime_for, mark_uncertain, report_cleanup};

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
    loop {
        if let Ok((mut stream, _)) = listener.accept() {
            let result = (|| -> Result<Value> {
                let input = super::read_line(&mut stream, Duration::from_secs(2))?;
                let request: Request = serde_json::from_str(&input)?;
                handle(request, &mut registry)
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

fn handle(request: Request, registry: &mut Registry) -> Result<Value> {
    match request.action.as_str() {
        "register" => {
            if registry.stopping {
                bail!("service is stopping after incomplete cleanup; retry 'tearoom service stop' first");
            }
            let root = fs::canonicalize(request.root.context("workspace root missing")?)?;
            let requested = request.identity.context("workspace identity missing")?;
            let requested_socket = request.hc_socket.context("hc socket identity missing")?;
            let current_socket = canonical_hc_socket(crate::hc::HcClient::from_env()?.socket())?;
            if requested_socket != current_socket {
                bail!(
                    "the running Tearoom service uses hc socket {}; this command uses {}; stop the service before switching hc sessions",
                    current_socket.display(),
                    requested_socket.display()
                );
            }
            if Store::open(root.parent().context("workspace has no parent")?)?.root() != root {
                bail!("workspace marker mismatch");
            }
            if identity(&root)? != requested {
                bail!("workspace identity changed; activation refused");
            }
            if registry
                .workspaces
                .iter()
                .any(|workspace| workspace.root == root && workspace.identity != requested)
            {
                bail!("workspace identity changed; explicit recovery is required");
            }
            if let Some(previous) = registry
                .workspaces
                .iter()
                .find(|workspace| workspace.identity == requested && workspace.root != root)
            {
                bail!(
                    "workspace path changed from {}; verify and clean its Agent terminals, then run tearoom service forget {} --confirm before activating the new path",
                    previous.root.display(),
                    previous.root.display()
                );
            }
            registry
                .workspaces
                .retain(|workspace| workspace.root != root);
            registry.workspaces.push(Workspace {
                owner_label: owner_label(&requested)?,
                legacy_warning:
                    "older path-labeled Agent terminals are detected but never killed automatically"
                        .into(),
                root,
                identity: requested,
                error: String::new(),
            });
            write_registry(registry)?;
            Ok(json!({"registered":true}))
        }
        "forget" => {
            if !request.confirm {
                bail!("forgetting a workspace may leave Agent terminals unmanaged; confirmation required");
            }
            let root = request.root.context("workspace root missing")?;
            if !root.is_absolute() {
                bail!("workspace root must be absolute");
            }
            let Some(index) = registry.workspaces.iter().position(|w| w.root == root) else {
                bail!("workspace registration not found");
            };
            match identity(&root) {
                Ok(found) if found == registry.workspaces[index].identity => {
                    if !registry.workspaces[index].owner_label.is_empty() {
                        bail!("workspace identity is still valid; refusing to forget an active registration")
                    }
                }
                Ok(_) => {}
                Err(error)
                    if error
                        .downcast_ref::<std::io::Error>()
                        .is_some_and(|io| io.kind() == std::io::ErrorKind::NotFound) => {}
                Err(error) => {
                    return Err(
                        error.context("cannot verify workspace identity; registration retained")
                    )
                }
            }
            registry.workspaces.remove(index);
            write_registry(registry)?;
            Ok(json!({"forgotten":root,"agents_stopped":false}))
        }
        "restart" => {
            if registry.stopping {
                bail!("service is stopping; retry after cleanup completes");
            }
            let root = fs::canonicalize(request.root.context("workspace root missing")?)?;
            if !matches!(identity(&root), Ok(found) if registry.workspaces.iter().any(|w| w.root == root && w.identity == found))
            {
                bail!("workspace identity is not registered; restart refused");
            }
            let store = Store::open(root.parent().context("workspace has no parent")?)?;
            let workspace = registry
                .workspaces
                .iter()
                .find(|workspace| workspace.root == root)
                .context("workspace registration not found")?;
            let runtime = hc_runtime_for(workspace)?;
            if runtime.caller.is_none() {
                bail!("hc did not provide a parent terminal ID; refusing to launch an unmanaged Agent");
            }
            let dispatcher = Dispatcher { store, runtime };
            super::super::collaboration::restart(
                &dispatcher,
                request.room.as_deref().context("room name missing")?,
                request.role.as_deref().context("role name missing")?,
                request.confirm,
            )?;
            let panel = crate::collab::panel_name(
                &root,
                request.room.as_deref().unwrap_or_default(),
                request.role.as_deref().unwrap_or_default(),
            )?;
            let status = dispatcher.runtime.status(&panel)?;
            Ok(json!({"panel":panel,"status":status.as_str()}))
        }
        "status" => Ok(
            json!({"running":true,"stopping":registry.stopping,"workspaces":registry.workspaces}),
        ),
        "stop" => {
            registry.stopping = true;
            let errors = cleanup(registry);
            for workspace in &mut registry.workspaces {
                let prefix = format!("{}:", workspace.root.display());
                if let Some(error) = errors.iter().find(|error| error.starts_with(&prefix)) {
                    workspace.error = format!("cleanup failed: {error}");
                } else if workspace.error.starts_with("cleanup failed:") {
                    workspace.error.clear();
                }
            }
            let exited = errors.is_empty();
            if exited {
                registry.stopping = false;
                write_registry(registry)?;
                Ok(json!({"stopping":true,"stopped":true,"exited":true,"cleanup_errors":errors}))
            } else {
                write_registry(registry)?;
                Ok(json!({"stopping":true,"stopped":false,"exited":false,"cleanup_errors":errors}))
            }
        }
        _ => bail!("unknown service request"),
    }
}
