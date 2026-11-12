//! Recovery and safe cleanup for service-owned Agent terminals.
//! The service acquires the workspace lock before inspecting or killing them.
use super::super::{identity, Registry, Workspace};
use crate::collab::{dispatch_lock, DeliveryState, DispatchState};
use anyhow::{bail, Result};
use std::path::Path;
use std::time::Duration;

pub(super) fn mark_uncertain(root: &Path) -> Result<()> {
    let _lock = dispatch_lock(root)?;
    let mut state = DispatchState::load(root)?;
    for intent in &mut state.intents {
        for delivery in &mut intent.deliveries {
            if delivery.state == DeliveryState::Submitted {
                delivery.state = DeliveryState::Unknown;
                delivery.error = "service restarted; inspect before resolving".into();
            }
        }
    }
    state.save(root)
}

pub(super) fn cleanup(registry: &Registry) -> Vec<String> {
    let mut failures = Vec::new();
    for workspace in &registry.workspaces {
        let identity_matches =
            matches!(identity(&workspace.root), Ok(found) if found == workspace.identity);
        let result = (|| -> Result<()> {
            if workspace.owner_label.is_empty() {
                bail!("no verified owner label; refusing broad cleanup");
            }
            let lock = if identity_matches {
                let lock = dispatch_lock(&workspace.root)?;
                if !matches!(identity(&workspace.root), Ok(found) if found == workspace.identity) {
                    bail!("workspace identity changed while waiting for its dispatcher lock");
                }
                Some(lock)
            } else {
                None
            };
            let runtime = hc_runtime_for(workspace)?;
            let stopped = runtime.stop_workspace(&[])?;
            if !identity_matches {
                let failed = if stopped.failed.is_empty() {
                    String::new()
                } else {
                    format!("; failed terminals: {}", stopped.failed.join(", "))
                };
                bail!(
                    "workspace identity changed; stopped only {} identity-owned terminals{}; delivery state was not updated, and old path-labeled terminals need manual review",
                    stopped.stopped.len(),
                    failed,
                );
            }
            if !matches!(identity(&workspace.root), Ok(found) if found == workspace.identity) {
                bail!("workspace identity changed during cleanup; delivery state was not updated");
            }
            let legacy = legacy_panels(workspace)?;
            drop(lock);
            crate::collab::dispatch::record_stopped_panels(&workspace.root, &stopped.stopped)?;
            if !stopped.failed.is_empty() {
                bail!("{}", stopped.failed.join("; "));
            }
            if !legacy.is_empty() {
                bail!(
                    "unverified path-labeled terminals were left running: {}; stop them explicitly, then retry service stop",
                    legacy.join(", ")
                );
            }
            Ok(())
        })();
        if let Err(error) = result {
            failures.push(format!("{}: {error:#}", workspace.root.display()));
        }
    }
    failures
}

fn legacy_panels(workspace: &Workspace) -> Result<Vec<String>> {
    let path_label = crate::collab::runtime::workspace_label(&workspace.root)?;
    let client = crate::hc::HcClient::from_env()?;
    let listed = client.call(
        "terminal.list",
        serde_json::json!({"all":false}),
        Duration::from_secs(3),
    )?;
    Ok(listed["terminals"]
        .as_array()
        .into_iter()
        .flatten()
        .filter(|terminal| terminal["labels"]["tearoom.workspace"] == path_label)
        .filter_map(|terminal| terminal["name"].as_str().map(str::to_owned))
        .collect())
}

pub(super) fn report_cleanup(failures: Vec<String>) -> Result<()> {
    if failures.is_empty() {
        Ok(())
    } else {
        bail!(
            "service stopped with cleanup failures: {}",
            failures.join("; ")
        )
    }
}

pub(super) fn hc_runtime_for(workspace: &Workspace) -> Result<crate::collab::runtime::HcRuntime> {
    if workspace.owner_label.is_empty() {
        bail!("registered workspace has no verified owner label");
    }
    Ok(crate::collab::runtime::HcRuntime {
        client: crate::hc::HcClient::from_env()?,
        timeout: Duration::from_secs(30),
        workspace_label: Some(workspace.owner_label.clone()),
        caller: std::env::var("HC_TERMINAL_ID")
            .ok()
            .filter(|id| !id.is_empty()),
    })
}
