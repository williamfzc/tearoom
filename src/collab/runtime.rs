//! The hc boundary: status observations and conservative task submission.
use super::Role;
use crate::hc::HcClient;
use anyhow::{bail, Context, Result};
use serde_json::{json, Value};
use std::collections::BTreeMap;
use std::path::Path;
use std::time::Duration;

/// Stable identifier used to limit runtime cleanup to one workspace.
pub fn workspace_label(workspace: &Path) -> Result<String> {
    use sha2::{Digest, Sha256};
    let canonical = std::fs::canonicalize(workspace).context("resolve workspace root")?;
    let mut hash = Sha256::new();
    #[cfg(unix)]
    {
        use std::os::unix::ffi::OsStrExt;
        hash.update(canonical.as_os_str().as_bytes());
    }
    #[cfg(not(unix))]
    hash.update(canonical.to_string_lossy().as_bytes());
    Ok(hex::encode(&hash.finalize()[..12]))
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum PanelStatus {
    Missing,
    Idle,
    Done,
    Working,
    Blocked,
    Unknown,
}

impl PanelStatus {
    fn reported(terminal: &Value) -> Self {
        match terminal["status"]["state"].as_str() {
            Some("idle") => Self::Idle,
            Some("done") => Self::Done,
            Some("working") => Self::Working,
            Some("blocked" | "error") => Self::Blocked,
            _ => Self::Unknown,
        }
    }

    pub fn as_str(self) -> &'static str {
        match self {
            Self::Missing => "missing",
            Self::Idle => "idle",
            Self::Done => "done",
            Self::Working => "working",
            Self::Blocked => "blocked",
            Self::Unknown => "unknown",
        }
    }
}

#[derive(Clone)]
pub struct BlockedPanel {
    pub address: String,
    pub episode: String,
    pub reason: String,
}

#[derive(Clone, Debug, Default)]
pub struct SubmitResult {
    pub accepted: bool,
    pub started: Option<bool>,
    pub pending: bool,
}

pub struct PanelRequest<'a> {
    pub name: &'a str,
    pub room: &'a str,
    pub role_name: &'a str,
    pub role: &'a Role,
    pub directory: &'a Path,
    pub task: &'a str,
}

pub trait Runtime {
    fn status(&self, panel: &str) -> Result<PanelStatus>;
    fn blocked_panels(&self) -> Result<BTreeMap<String, BlockedPanel>> {
        Ok(BTreeMap::new())
    }
    fn launch(
        &self,
        name: &str,
        room: &str,
        role_name: &str,
        role: &Role,
        directory: &Path,
    ) -> Result<()>;
    fn stop(&self, panel: &str) -> Result<()>;
    fn ensure(&self, request: &PanelRequest<'_>) -> Result<SubmitResult>;
    fn submit(&self, panel: &str, task: &str, status: PanelStatus) -> Result<SubmitResult>;
}

pub struct HcRuntime {
    pub client: HcClient,
    pub timeout: Duration,
    pub workspace_label: Option<String>,
    pub caller: Option<String>,
}

#[derive(Default)]
pub struct WorkspaceStop {
    pub stopped: Vec<String>,
    pub failed: Vec<String>,
}

impl HcRuntime {
    fn call(&self, method: &str, params: Value) -> Result<Value> {
        Ok(self.client.call(method, params, self.timeout)?)
    }

    fn live_terminals(&self, filter: Value) -> Result<Vec<Value>> {
        let mut list = self.call("terminal.list", filter)?;
        serde_json::from_value(list["terminals"].take()).context("invalid hc terminal list")
    }

    /// Stop this workspace's live terminals, plus known terminals created before workspace labels.
    pub fn stop_workspace(&self, known: &[String]) -> Result<WorkspaceStop> {
        use std::collections::BTreeSet;
        let Some(label) = self.workspace_label.as_deref() else {
            bail!("workspace identity is unavailable; refusing broad terminal cleanup");
        };
        let listed =
            match self
                .client
                .call_existing("terminal.list", json!({"all":false}), self.timeout)
            {
                Ok(listed) => listed,
                Err(crate::hc::HcError::Connect(error))
                    if matches!(
                        error.kind(),
                        std::io::ErrorKind::NotFound | std::io::ErrorKind::ConnectionRefused
                    ) =>
                {
                    return Ok(WorkspaceStop::default())
                }
                Err(error) => return Err(error.into()),
            };
        let terminals = listed["terminals"]
            .as_array()
            .context("invalid hc terminal list during workspace shutdown")?;
        let known = known.iter().collect::<BTreeSet<_>>();
        let names = terminals
            .iter()
            .filter_map(|terminal| {
                let name = terminal["name"].as_str()?;
                let owned = terminal["labels"]["tearoom.workspace"].as_str() == Some(label)
                    || known.contains(&name.to_owned());
                owned.then(|| name.to_owned())
            })
            .collect::<BTreeSet<_>>();
        let mut result = WorkspaceStop::default();
        for panel in names {
            match self.client.call_existing(
                "terminal.kill",
                json!({"terminal":panel}),
                self.timeout,
            ) {
                Ok(_) => result.stopped.push(panel),
                Err(error) => result.failed.push(format!("{panel}: {error}")),
            }
        }
        Ok(result)
    }
}

impl Runtime for HcRuntime {
    fn status(&self, panel: &str) -> Result<PanelStatus> {
        Ok(self
            .live_terminals(json!({}))?
            .iter()
            .find(|terminal| terminal["name"] == panel && terminal["state"] == "live")
            .map_or(PanelStatus::Missing, PanelStatus::reported))
    }

    fn blocked_panels(&self) -> Result<BTreeMap<String, BlockedPanel>> {
        let Some(label) = &self.workspace_label else {
            return Ok(BTreeMap::new());
        };
        Ok(self
            .live_terminals(json!({"labels":{"tearoom.workspace":label}}))?
            .iter()
            .filter_map(|terminal| {
                let address = terminal["address"].as_str()?;
                if address.contains('/') || PanelStatus::reported(terminal) != PanelStatus::Blocked
                {
                    return None;
                }
                // hc stamps each transition of the terminal's reported state.
                let episode = format!(
                    "{address}-{}-{}",
                    terminal["created_ms"].as_u64()?,
                    terminal["status_ms"].as_u64()?
                );
                let status = &terminal["status"];
                let reason = status["kind"].as_str().or(status["state"].as_str())?;
                let blockage = BlockedPanel {
                    address: address.into(),
                    episode,
                    reason: reason.into(),
                };
                Some((terminal["name"].as_str()?.to_owned(), blockage))
            })
            .collect())
    }

    fn launch(
        &self,
        name: &str,
        room: &str,
        role_name: &str,
        role: &Role,
        directory: &Path,
    ) -> Result<()> {
        let argv = &role.command;
        if argv.is_empty() {
            bail!("role has no command");
        }
        if role.agent_type == "codex" && !argv.iter().any(|a| a == "--no-daemon") {
            bail!("Codex command must include --no-daemon");
        }
        let room_label = stable_label(room);
        let mut labels = json!({"tearoom.room":room_label,"tearoom.role":role_name});
        if let Some(workspace) = &self.workspace_label {
            labels["tearoom.workspace"] = json!(workspace);
        }
        let mut params = json!({"argv":argv,"name":name,"cwd":directory,
            "no_parent":self.caller.is_none(),"labels":labels});
        if let Some(caller) = &self.caller {
            params["caller"] = json!(caller);
        }
        self.call("terminal.run", params)?;
        Ok(())
    }

    fn stop(&self, panel: &str) -> Result<()> {
        self.call("terminal.kill", json!({"terminal":panel}))?;
        Ok(())
    }

    fn ensure(&self, request: &PanelRequest<'_>) -> Result<SubmitResult> {
        self.launch(
            request.name,
            request.room,
            request.role_name,
            request.role,
            request.directory,
        )?;
        let status = self.status(request.name)?;
        self.submit(request.name, request.task, status)
    }

    fn submit(&self, panel: &str, task: &str, mut status: PanelStatus) -> Result<SubmitResult> {
        if status == PanelStatus::Done {
            status = self.status(panel)?;
        }
        if !matches!(status, PanelStatus::Idle | PanelStatus::Done) {
            return Ok(SubmitResult {
                pending: true,
                ..Default::default()
            });
        }
        // hc 0.1.18 has no atomic done guard; another controller can race this send.
        let guard = if status == PanelStatus::Idle {
            Some("idle")
        } else {
            None
        };
        let result = self.call(
            "terminal.send",
            json!({"terminal":panel,"text":task,"if":guard}),
        )?;
        let started = result["started"].as_bool();
        Ok(SubmitResult {
            accepted: started == Some(true),
            started,
            pending: false,
        })
    }
}

fn stable_label(value: &str) -> String {
    use sha2::{Digest, Sha256};
    if value.len() <= 32 && !value.contains(['=', ',', '\n', '\r']) {
        return value.into();
    }
    format!(
        "lbl-{}",
        hex::encode(&Sha256::digest(value.as_bytes())[..12])
    )
}

pub(super) fn quote(value: &str) -> String {
    format!("'{}'", value.replace('\'', "'\\''"))
}
