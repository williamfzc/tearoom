//! Resolve hc socket paths using the runtime environment and configuration.

use super::{HcClient, HcError};
use std::path::PathBuf;

impl HcClient {
    pub fn from_env() -> Result<Self, HcError> {
        let env = |name: &str| {
            std::env::var_os(name)
                .filter(|v| !v.is_empty())
                .map(PathBuf::from)
        };
        let home = env("HOME").unwrap_or_else(|| PathBuf::from("/"));
        let session = std::env::var("HC_SESSION").unwrap_or_default();
        let config = env("XDG_CONFIG_HOME").unwrap_or_else(|| home.join(".config"));
        let state = env("XDG_STATE_HOME").unwrap_or_else(|| home.join(".local/state"));
        #[cfg(unix)]
        let runtime = env("XDG_RUNTIME_DIR").or_else(|| {
            let dir = PathBuf::from(format!("/run/user/{}", unsafe { libc::getuid() }));
            dir.is_dir().then_some(dir)
        });
        #[cfg(not(unix))]
        let runtime = None;
        let config_path = config.join("herdr-core/config.toml");
        let configured = std::fs::read_to_string(config_path)
            .ok()
            .and_then(|text| text.parse::<toml::Value>().ok())
            .and_then(|config| config.get("socket")?.as_str().map(PathBuf::from));
        let socket = socket_path(&session, env("HC_SOCKET"), configured, runtime, state)?;
        Ok(Self {
            socket,
            autostart: true,
        })
    }
}

pub(super) fn socket_path(
    session: &str,
    explicit: Option<PathBuf>,
    configured: Option<PathBuf>,
    runtime: Option<PathBuf>,
    state: PathBuf,
) -> Result<PathBuf, HcError> {
    let session = session.trim();
    let session = if session.is_empty() {
        "default"
    } else {
        session
    };
    if session == "."
        || session == ".."
        || session
            .chars()
            .any(|c| c.is_control() || "/\\:*?\"<>|".contains(c))
    {
        return Err(HcError::Configuration(format!(
            "invalid HC_SESSION {session:?}"
        )));
    }
    Ok(explicit.or(configured).unwrap_or_else(|| {
        let dir = runtime
            .map(|p| p.join("herdr-core"))
            .unwrap_or_else(|| state.join("herdr-core").join(session));
        dir.join(format!("{session}.sock"))
    }))
}
