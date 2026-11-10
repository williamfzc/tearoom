//! The hc boundary: status observations and conservative task submission.
use super::Role;
use crate::hc::HcClient;
use anyhow::{bail, Context, Result};
use serde_json::{json, Value};
use std::path::Path;
use std::time::Duration;

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
    /// Stable lowercase name for CLI output.
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
    /// Complete dispatch prompt, including role instructions.
    pub task: &'a str,
}

pub trait Runtime {
    fn status(&self, panel: &str) -> Result<PanelStatus>;
    /// Validate a role launch and start its panel without a task.
    fn launch(
        &self,
        name: &str,
        room: &str,
        role_name: &str,
        role: &Role,
        directory: &Path,
    ) -> Result<()>;
    /// End a live panel; its next delivery starts the configured command again.
    fn stop(&self, panel: &str) -> Result<()>;
    fn ensure(&self, request: &PanelRequest<'_>) -> Result<SubmitResult>;
    fn submit(&self, panel: &str, task: &str, status: PanelStatus) -> Result<SubmitResult>;
}

pub struct HcRuntime {
    pub client: HcClient,
    pub timeout: Duration,
}

impl HcRuntime {
    fn call(&self, method: &str, params: Value) -> Result<Value> {
        Ok(self.client.call(method, params, self.timeout)?)
    }
}

impl Runtime for HcRuntime {
    fn status(&self, panel: &str) -> Result<PanelStatus> {
        let list = self.call("terminal.list", json!({"all":false}))?;
        let terminal = list["terminals"]
            .as_array()
            .context("invalid hc terminal list")?
            .iter()
            .find(|terminal| terminal["name"] == panel && terminal["state"] == "live");
        if terminal.is_none() {
            return Ok(PanelStatus::Missing);
        }
        let details = self.call("terminal.get", json!({"terminal":panel}))?;
        Ok(match details["status"]["state"].as_str() {
            Some("idle") => PanelStatus::Idle,
            Some("done") => PanelStatus::Done,
            Some("working") => PanelStatus::Working,
            Some("blocked" | "error") => PanelStatus::Blocked,
            _ => PanelStatus::Unknown,
        })
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
        self.call(
            "terminal.run",
            json!({"argv":argv,"name":name,"cwd":directory,"no_parent":true,
            "labels":{"tearoom.room":room_label,"tearoom.role":role_name}}),
        )?;
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
        if !matches!(status, PanelStatus::Idle | PanelStatus::Done) {
            return Ok(SubmitResult {
                pending: true,
                ..Default::default()
            });
        }
        self.submit(request.name, request.task, status)
    }

    fn submit(&self, panel: &str, task: &str, mut status: PanelStatus) -> Result<SubmitResult> {
        if status == PanelStatus::Done {
            status = self.status(panel)?;
            if !matches!(status, PanelStatus::Idle | PanelStatus::Done) {
                return Ok(SubmitResult {
                    pending: true,
                    ..Default::default()
                });
            }
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
