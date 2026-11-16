//! The opt-in LAN face of a workspace: a minimal HTTP server, hosted by the
//! local collaboration service, that lets a paired phone read Room streams
//! and append records. Records land in the same Store streams as every other
//! writer, so dispatch and recovery treat a phone send like any other send.
//! Pairing and token policy live in [docs/reference/lan-face.md].
//!
//! This module is a face, not a host: it takes explicit paths and a sender
//! closure. The service (`cli::service_server`) composes it with the service
//! state directory and the CLI `send` path; tests inject their own sender.

mod api;
mod auth;
mod server;

use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::net::SocketAddr;
use std::path::{Path, PathBuf};
use std::process::Command;
use std::sync::{Arc, Mutex};
use std::time::Instant;

pub const DEFAULT_PORT: u16 = 7761;
pub const PAIR_TTL_SECS: u64 = 300;
pub const PAIR_MAX_ATTEMPTS: u32 = 5;
pub const MAX_BODY: usize = 1024 * 1024;
pub const MAX_HEADERS: usize = 16 * 1024;
/// Requests larger than `MAX_BODY` are still consumed up to this ceiling
/// before the refusal is written, so the client is never mid-write when the
/// connection closes; anything beyond it is dropped outright.
pub const MAX_CONSUME: usize = 8 * 1024 * 1024;

/// Persistent and live state of the LAN face. `enabled`, `port` and `tokens`
/// survive service restarts (`lan.json` in the service state directory); the
/// pending pairing code, bound address and bind error live only in memory.
#[derive(Clone)]
pub struct LanState {
    path: PathBuf,
    pub enabled: bool,
    pub port: u16,
    pub tokens: Vec<TokenEntry>,
    pub pending: Option<PairCode>,
    pub bound: Option<SocketAddr>,
    pub bind_error: String,
}

#[derive(Clone, Deserialize, Serialize)]
pub struct TokenEntry {
    /// SHA-256 of the raw token; the raw token exists only on the phone.
    pub hash: String,
    /// The canonical `.tearoom` root this token may read and write.
    pub workspace: PathBuf,
}

#[derive(Clone)]
pub struct PairCode {
    pub code: String,
    pub expires: Instant,
    pub workspace: PathBuf,
    pub attempts: u32,
}

/// One remote send. `workspace` is the canonical `.tearoom` root resolved
/// from the request's bearer token; `key` is the phone's idempotency key.
#[derive(Clone, Debug)]
pub struct SendArgs {
    pub workspace: PathBuf,
    pub room: String,
    pub text: String,
    pub to: Vec<String>,
    pub author: String,
    pub key: String,
}

/// How the face appends a record. Production shells out to the CLI `send`
/// command so message composition, roster rules and service activation keep
/// one owner; tests substitute their own sender.
pub type Sender = Arc<dyn Fn(&SendArgs) -> Result<Value> + Send + Sync>;

pub type Shared = Arc<Mutex<LanState>>;

impl LanState {
    /// A face for one workspace root, enabled or not. Tests start one with
    /// `enabled: true, port: 0` for an ephemeral bind.
    pub fn new(path: impl Into<PathBuf>, enabled: bool, port: u16) -> Shared {
        Arc::new(Mutex::new(LanState {
            path: path.into(),
            enabled,
            port,
            tokens: Vec::new(),
            pending: None,
            bound: None,
            bind_error: String::new(),
        }))
    }

    /// Load the persisted face state from `lan.json` beside the service
    /// socket; a missing file is a disabled face on the default port.
    pub fn load(state_dir: impl AsRef<Path>) -> Result<Shared> {
        let path = state_dir.as_ref().join("lan.json");
        let persisted: Persisted = match std::fs::read(&path) {
            Ok(data) => serde_json::from_slice(&data).context("read LAN face state")?,
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => Persisted::default(),
            Err(error) => return Err(error.into()),
        };
        Ok(Arc::new(Mutex::new(LanState {
            path,
            enabled: persisted.enabled,
            port: persisted.port,
            tokens: persisted.tokens,
            pending: None,
            bound: None,
            bind_error: String::new(),
        })))
    }

    /// Persist `enabled`, `port` and `tokens`; call sites hold the lock.
    pub fn save(&self) -> Result<()> {
        let persisted = Persisted {
            enabled: self.enabled,
            port: self.port,
            tokens: self.tokens.clone(),
        };
        let tmp = self.path.with_extension("tmp");
        let data = serde_json::to_vec_pretty(&persisted)?;
        std::fs::write(&tmp, data)?;
        std::fs::rename(&tmp, &self.path)?;
        Ok(())
    }
}

#[derive(Default, Deserialize, Serialize)]
struct Persisted {
    #[serde(default)]
    enabled: bool,
    #[serde(default)]
    port: u16,
    #[serde(default)]
    tokens: Vec<TokenEntry>,
}

/// Start a pairing for one workspace: the face turns on at `port`, a fresh
/// single-use code replaces any previous one, and the state is persisted.
/// Returns the code the phone must present within `PAIR_TTL_SECS`.
pub fn mint_pairing(shared: &Shared, workspace: &Path, port: u16) -> Result<String> {
    let mut state = shared.lock().unwrap();
    state.enabled = true;
    state.port = port;
    state.pending = Some(PairCode {
        code: auth::mint_code()?,
        expires: Instant::now() + std::time::Duration::from_secs(PAIR_TTL_SECS),
        workspace: workspace.to_path_buf(),
        attempts: 0,
    });
    state.save()?;
    Ok(state.pending.as_ref().expect("just minted").code.clone())
}

/// The LAN URL advertised by `collab pair`, discovered by asking the OS how
/// it would route to the internet; `None` means offline or no interface.
pub fn lan_url(port: u16) -> Option<String> {
    let socket = std::net::UdpSocket::bind("0.0.0.0:0").ok()?;
    socket.connect("8.8.8.8:80").ok()?;
    let ip = socket.local_addr().ok()?.ip();
    Some(format!("http://{ip}:{port}/v1"))
}

/// The production sender: shell out to this binary's own `send` command in
/// the workspace directory, mirroring the desktop app's send path.
pub fn cli_sender() -> Sender {
    Arc::new(|args: &SendArgs| {
        let exe = std::env::current_exe().context("cannot locate the tearoom binary")?;
        let dir = args
            .workspace
            .parent()
            .context("workspace root has no parent")?;
        let mut command = Command::new(exe);
        command
            .current_dir(dir)
            .args(["send", "--room", &args.room, "--text", &args.text]);
        let author = args.author.trim();
        if !author.is_empty() {
            command.args(["--as", author]);
        }
        let targets: Vec<String> = args
            .to
            .iter()
            .filter_map(|target| {
                let name = target.trim().trim_start_matches('@');
                (!name.is_empty()).then(|| format!("@{name}"))
            })
            .collect();
        if !targets.is_empty() {
            command.arg("--to").arg(targets.join(","));
        }
        command.args(["--idempotency-key", &args.key]);
        let output = command.output().context("cannot run the tearoom CLI")?;
        let stdout = String::from_utf8_lossy(&output.stdout);
        let stderr = String::from_utf8_lossy(&output.stderr);
        let record: Option<Value> = stdout
            .lines()
            .rev()
            .find_map(|line| serde_json::from_str(line).ok());
        if output.status.success() {
            return record
                .ok_or_else(|| anyhow::anyhow!("the tearoom CLI did not return a record"));
        }
        Err(anyhow::anyhow!(
            "{}",
            stderr
                .lines()
                .rev()
                .find(|line| !line.trim().is_empty())
                .unwrap_or("the tearoom CLI refused the message")
        ))
    })
}

/// The accept loop; one thread per connection, `Connection: close` responses.
/// Runs until the process exits; the service never joins it.
pub fn serve(shared: Shared, sender: Sender) {
    server::run(shared, sender);
}
