//! The hc boundary: status observations and conservative task submission.
use super::Role;
use crate::hc::HcClient;
use anyhow::{bail, Context, Result};
use serde_json::{json, Value};
use std::path::{Path, PathBuf};
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

    fn preflight_template(&self, name: &str, agent_type: &str) -> Result<()> {
        if name.is_empty() || name == "." || name == ".." || name.contains(['/', '\\']) {
            bail!("invalid hc template name");
        }
        let listed = self.call("templates.list", json!({}))?;
        let template = listed["templates"]
            .as_array()
            .and_then(|a| a.iter().find(|v| v["name"] == name))
            .with_context(|| format!("hc template {name:?} is missing; launch refused"))?;
        if let Some(error) = template["error"].as_str().filter(|s| !s.is_empty()) {
            bail!("hc template {name:?} is invalid: {error}");
        }
        let command = template["command"]
            .as_str()
            .context("hc template command missing")?;
        if agent_type == "codex"
            && !command
                .split_whitespace()
                .any(|part| part.trim_matches(['\'', '"']) == "--no-daemon")
        {
            bail!("hc template {name:?} must start Codex with --no-daemon");
        }
        let config_home = std::env::var_os("XDG_CONFIG_HOME")
            .filter(|s| !s.is_empty())
            .map(PathBuf::from)
            .or_else(|| std::env::var_os("HOME").map(|p| PathBuf::from(p).join(".config")))
            .context("find hc config directory")?;
        let contents = std::fs::read_to_string(
            config_home
                .join("herdr-core/templates")
                .join(format!("{name}.md")),
        )?
        .replace("\r\n", "\n");
        let front = contents
            .strip_prefix("---\n")
            .and_then(|s| s.split_once("\n---").map(|(f, _)| f))
            .context("hc template has no valid frontmatter")?;
        let mut local_command = "";
        let mut ready = "";
        for line in front.lines() {
            if let Some((key, value)) = line.split_once(':') {
                match key.trim() {
                    "command" => local_command = value.trim(),
                    "ready" => ready = value.trim(),
                    _ => {}
                }
            }
        }
        if local_command != command {
            bail!("hc template changed during readiness preflight");
        }
        if !matches!(ready, "" | "report") {
            bail!("hc template readiness {ready:?} is not reliable for automated delivery");
        }
        Ok(())
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

    fn ensure(&self, request: &PanelRequest<'_>) -> Result<SubmitResult> {
        let role = request.role;
        let argv = if !role.template.is_empty() {
            self.preflight_template(&role.template, &role.agent_type)?;
            vec![role.template.clone(), request.task.to_owned()]
        } else {
            if role.command.is_empty() {
                bail!("role has neither an hc template nor a command");
            }
            if role.agent_type == "codex" && !role.command.iter().any(|a| a == "--no-daemon") {
                bail!("Codex command must include --no-daemon");
            }
            role.command.clone()
        };
        let room_label = stable_label(request.room);
        let result = self.call(
            "terminal.run",
            json!({"argv":argv,"name":request.name,"cwd":request.directory,"no_parent":true,
            "labels":{"tearoom.room":room_label,"tearoom.role":request.role_name}}),
        )?;
        if !role.template.is_empty() {
            let first = result
                .get("first_message")
                .filter(|v| !v.is_null())
                .context("hc template launch omitted first_message")?;
            return Ok(SubmitResult {
                accepted: first["sent"].as_bool().unwrap_or(false),
                started: first["started"].as_bool(),
                pending: false,
            });
        }
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
