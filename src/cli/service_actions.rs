//! Request actions for the local collaboration service: registration,
//! lifecycle control, status, and LAN pairing. The accept loop and the
//! dispatch pump live in the parent module.
use super::shutdown::{cleanup, hc_runtime_for};
use crate::cli::service::{
    canonical_hc_socket, identity, owner_label, write_registry, Registry, Request, Workspace,
};
use crate::collab::dispatch::Dispatcher;
use crate::collab::runtime::Runtime;
use crate::store::Store;
use anyhow::{bail, Context, Result};
use serde_json::{json, Value};
use std::fs;

pub(super) fn handle(
    request: Request,
    registry: &mut Registry,
    lan: &crate::lan::Shared,
) -> Result<Value> {
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
            crate::cli::collaboration::restart(
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
        "status" => {
            let lan = {
                let lan = lan.lock().expect("lan state mutex");
                json!({
                    "enabled": lan.enabled,
                    "port": lan.port,
                    "paired": lan.tokens.len(),
                    "error": lan.bind_error,
                })
            };
            Ok(
                json!({"running":true,"stopping":registry.stopping,"workspaces":registry.workspaces,"lan":lan}),
            )
        }
        "pair" => {
            if registry.stopping {
                bail!("service is stopping; retry after cleanup completes");
            }
            let root = fs::canonicalize(request.root.context("workspace root missing")?)?;
            if !registry
                .workspaces
                .iter()
                .any(|workspace| workspace.root == root)
            {
                bail!("workspace is not registered; run 'tearoom collab pair' from inside it");
            }
            let port = request.port.unwrap_or_else(|| {
                std::env::var("TEAROOM_LAN_PORT")
                    .ok()
                    .and_then(|value| value.parse().ok())
                    .unwrap_or(crate::lan::DEFAULT_PORT)
            });
            let code = crate::lan::mint_pairing(lan, &root, port)?;
            Ok(json!({
                "code": code,
                "expires_in_secs": crate::lan::PAIR_TTL_SECS,
                "url": crate::lan::lan_url(port),
            }))
        }
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
