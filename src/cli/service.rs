//! Per-user collaboration service control and persistent workspace identity.
//! Dispatch policy remains in collab::dispatch; the service owns activation.
use anyhow::{bail, Context, Result};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::fs;
use std::path::{Path, PathBuf};

#[cfg(unix)]
use std::os::unix::fs::{MetadataExt, PermissionsExt};
#[cfg(unix)]
use std::os::unix::net::UnixStream;

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq, Eq)]
pub(super) struct Identity {
    dir_dev: u64,
    dir_ino: u64,
    store_dev: u64,
    store_ino: u64,
    store_hash: String,
}

#[derive(Clone, Debug, Deserialize, Serialize)]
pub(super) struct Workspace {
    pub root: PathBuf,
    pub identity: Identity,
    #[serde(default)]
    pub owner_label: String,
    #[serde(default)]
    pub legacy_warning: String,
    #[serde(default)]
    pub error: String,
}

#[derive(Default, Deserialize, Serialize)]
pub(super) struct Registry {
    #[serde(default)]
    pub workspaces: Vec<Workspace>,
    #[serde(default)]
    pub stopping: bool,
}

#[derive(Deserialize)]
pub(super) struct Request {
    pub action: String,
    #[serde(default)]
    pub root: Option<PathBuf>,
    #[serde(default)]
    pub identity: Option<Identity>,
    #[serde(default)]
    pub hc_socket: Option<PathBuf>,
    #[serde(default)]
    pub room: Option<String>,
    #[serde(default)]
    pub role: Option<String>,
    #[serde(default)]
    pub confirm: bool,
}

pub(super) fn dir() -> Result<PathBuf> {
    if let Some(path) = std::env::var_os("TEAROOM_SERVICE_DIR") {
        return Ok(PathBuf::from(path));
    }
    let base = std::env::var_os("XDG_STATE_HOME")
        .map(PathBuf::from)
        .or_else(|| std::env::var_os("HOME").map(|home| PathBuf::from(home).join(".local/state")))
        .context("cannot locate user state directory (set TEAROOM_SERVICE_DIR)")?;
    Ok(base.join("tearoom"))
}

#[cfg(unix)]
pub(super) fn socket() -> Result<PathBuf> {
    Ok(dir()?.join("service.sock"))
}

fn registry_path() -> Result<PathBuf> {
    Ok(dir()?.join("workspaces.json"))
}

#[cfg(unix)]
pub(super) fn operation_lock() -> Result<std::fs::File> {
    use fs2::FileExt;
    secure_state_dir()?;
    let file = fs::OpenOptions::new()
        .create(true)
        .truncate(false)
        .write(true)
        .open(dir()?.join("operation.lock"))?;
    file.lock_exclusive()?;
    Ok(file)
}

#[cfg(unix)]
pub(super) fn secure_state_dir() -> Result<PathBuf> {
    let path = dir()?;
    fs::create_dir_all(&path)?;
    let metadata = fs::symlink_metadata(&path)?;
    if !metadata.is_dir() || metadata.file_type().is_symlink() {
        bail!("Tearoom service state path must be a real directory");
    }
    if metadata.uid() != unsafe { libc::geteuid() } {
        bail!("Tearoom service state directory is owned by another user");
    }
    fs::set_permissions(&path, fs::Permissions::from_mode(0o700))?;
    Ok(path)
}

#[cfg(unix)]
pub(super) fn identity(root: &Path) -> Result<Identity> {
    use sha2::{Digest, Sha256};
    let root = fs::canonicalize(root)?;
    let dir_meta = fs::metadata(&root)?;
    let marker = root.join("store.json");
    let store_meta = fs::metadata(&marker).context("workspace marker store.json is missing")?;
    let bytes = fs::read(&marker)?;
    Ok(Identity {
        dir_dev: dir_meta.dev(),
        dir_ino: dir_meta.ino(),
        store_dev: store_meta.dev(),
        store_ino: store_meta.ino(),
        store_hash: hex::encode(Sha256::digest(bytes)),
    })
}

pub(super) fn owner_label(identity: &Identity) -> Result<String> {
    use sha2::{Digest, Sha256};
    let bytes = serde_json::to_vec(identity)?;
    Ok(format!(
        "tearoom-{}",
        hex::encode(&Sha256::digest(bytes)[..12])
    ))
}

pub(super) fn canonical_hc_socket(path: &Path) -> Result<PathBuf> {
    let absolute = if path.is_absolute() {
        path.to_path_buf()
    } else {
        std::env::current_dir()?.join(path)
    };
    let Some(name) = absolute.file_name() else {
        bail!("hc socket path has no file name");
    };
    let parent = absolute.parent().context("hc socket path has no parent")?;
    Ok(fs::canonicalize(parent)
        .unwrap_or_else(|_| parent.to_path_buf())
        .join(name))
}

pub(super) fn read_registry() -> Result<Registry> {
    match fs::read(registry_path()?) {
        Ok(data) => Ok(serde_json::from_slice(&data).context("read service workspace registry")?),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(Registry::default()),
        Err(error) => Err(error.into()),
    }
}

pub(super) fn write_registry(registry: &Registry) -> Result<()> {
    let path = registry_path()?;
    fs::create_dir_all(path.parent().context("service state path has no parent")?)?;
    let tmp = path.with_extension("tmp");
    let mut file = fs::OpenOptions::new()
        .create(true)
        .truncate(true)
        .write(true)
        .open(&tmp)?;
    serde_json::to_writer_pretty(&mut file, registry)?;
    file.sync_all()?;
    drop(file);
    fs::rename(tmp, path)?;
    Ok(())
}

#[cfg(unix)]
pub(super) fn call(value: &Value) -> Result<Value> {
    let seconds = if value["action"] == "stop" { 300 } else { 10 };
    call_timeout(value, std::time::Duration::from_secs(seconds))
}

#[cfg(unix)]
pub(super) fn call_timeout(value: &Value, timeout: std::time::Duration) -> Result<Value> {
    use std::io::Write;
    let path = socket()?;
    let mut stream = UnixStream::connect(&path)
        .with_context(|| format!("local Tearoom service is not running ({})", path.display()))?;
    stream.write_all(serde_json::to_string(value)?.as_bytes())?;
    stream.write_all(b"\n")?;
    let response = read_line(&mut stream, timeout)?;
    let response: Value = serde_json::from_str(&response).context("invalid service response")?;
    if response["ok"] != true {
        bail!(
            "{}",
            response["error"]
                .as_str()
                .unwrap_or("service request failed")
        );
    }
    Ok(response["result"].clone())
}

#[cfg(unix)]
pub(super) fn read_line(stream: &mut UnixStream, timeout: std::time::Duration) -> Result<String> {
    use std::io::Read;
    use std::time::{Duration, Instant};
    stream.set_nonblocking(true)?;
    let deadline = Instant::now() + timeout;
    let mut bytes = Vec::new();
    let mut buffer = [0; 4096];
    loop {
        match stream.read(&mut buffer) {
            Ok(0) => break,
            Ok(read) => {
                bytes.extend_from_slice(&buffer[..read]);
                if bytes.contains(&b'\n') {
                    break;
                }
                if bytes.len() > 64 * 1024 {
                    bail!("local service message exceeds 64 KiB");
                }
            }
            Err(error) if error.kind() == std::io::ErrorKind::WouldBlock => {
                if std::time::Instant::now() >= deadline {
                    bail!("timed out waiting for local service response");
                }
                std::thread::sleep(Duration::from_millis(10));
            }
            Err(error) if error.kind() == std::io::ErrorKind::Interrupted => {}
            Err(error) => return Err(error.into()),
        }
    }
    let line = bytes
        .split(|byte| *byte == b'\n')
        .next()
        .unwrap_or_default();
    Ok(std::str::from_utf8(line)?.to_owned())
}

#[cfg(unix)]
#[path = "service_control.rs"]
mod control;
#[cfg(unix)]
#[path = "service_server.rs"]
mod server;

#[cfg(unix)]
pub(super) use control::{activate, restart, run_service, service_command};
