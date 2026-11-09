//! Start or enter an isolated Herdr session without touching other sessions.

use anyhow::{bail, Context, Result};
#[cfg(unix)]
use std::os::unix::process::CommandExt;
use std::process::{Command, Stdio};
use std::thread;
use std::time::{Duration, Instant};
use std::{io::Read, io::Seek, io::SeekFrom};

pub(in crate::herdr) fn ensure_session(session: &str) -> Result<()> {
    match probe(session) {
        Ok(()) => return Ok(()),
        Err(error) if server_not_running(&error) => {}
        Err(error) => return Err(error).context("check Herdr session availability"),
    }
    let executable = std::env::var_os("TEAROOM_HERDR_BIN").unwrap_or_else(|| "herdr".into());
    let mut log = tempfile::tempfile().context("create temporary Herdr startup log")?;
    let stderr = log.try_clone()?;
    let mut command = Command::new(executable);
    command
        .env_remove("HERDR_SOCKET_PATH")
        .env("HERDR_SESSION", session)
        .arg("--session")
        .arg(session)
        .arg("server")
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::from(stderr));
    #[cfg(unix)]
    command.process_group(0);
    let mut child = command.spawn().context("start named Herdr server")?;
    let deadline = Instant::now() + Duration::from_secs(10);
    loop {
        if probe(session).is_ok() {
            thread::spawn(move || {
                let _ = child.wait();
            });
            return Ok(());
        }
        if let Some(status) = child.try_wait()? {
            let _ = child.wait()?;
            if probe(session).is_ok() {
                return Ok(());
            }
            log.seek(SeekFrom::Start(0))?;
            let mut detail = String::new();
            log.read_to_string(&mut detail)?;
            bail!(
                "Herdr server exited before session became ready ({status}): {}; check Herdr and retry",
                detail.trim()
            );
        }
        if Instant::now() >= deadline {
            let _ = child.kill();
            let _ = child.wait();
            bail!("Herdr session {session:?} did not become ready within 10 seconds");
        }
        thread::sleep(Duration::from_millis(100));
    }
}

fn probe(session: &str) -> Result<()> {
    let executable = std::env::var_os("TEAROOM_HERDR_BIN").unwrap_or_else(|| "herdr".into());
    let mut child = Command::new(executable)
        .env_remove("HERDR_SOCKET_PATH")
        .env("HERDR_SESSION", session)
        .arg("--session")
        .arg(session)
        .args(["workspace", "list"])
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .context("check Herdr session")?;
    let deadline = Instant::now() + Duration::from_secs(3);
    loop {
        if child.try_wait()?.is_some() {
            let output = child.wait_with_output()?;
            if output.status.success() {
                return Ok(());
            }
            bail!("{}", String::from_utf8_lossy(&output.stderr).trim());
        }
        if Instant::now() >= deadline {
            let _ = child.kill();
            let _ = child.wait();
            bail!("Herdr session probe timed out after 3 seconds");
        }
        thread::sleep(Duration::from_millis(40));
    }
}

fn server_not_running(error: &anyhow::Error) -> bool {
    error
        .to_string()
        .contains("\"code\":\"server_not_running\"")
}

pub(in crate::herdr) fn open_session(session: &str) -> Result<()> {
    let executable = std::env::var_os("TEAROOM_HERDR_BIN").unwrap_or_else(|| "herdr".into());
    let status = Command::new(executable)
        .env_remove("HERDR_SOCKET_PATH")
        .env("HERDR_SESSION", session)
        .args(["session", "attach", session])
        .stdin(Stdio::inherit())
        .stdout(Stdio::inherit())
        .stderr(Stdio::inherit())
        .status()
        .context("open Herdr session")?;
    if !status.success() {
        bail!("Herdr session {session:?} exited with {status}");
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn only_the_explicit_not_running_error_can_start_a_server() {
        let stopped = anyhow::anyhow!(
            "{}",
            r#"Herdr command failed: {"error":{"code":"server_not_running"}}"#
        );
        let denied = anyhow::anyhow!(
            "{}",
            r#"Herdr command failed: {"error":{"code":"permission_denied"}}"#
        );
        assert!(server_not_running(&stopped));
        assert!(!server_not_running(&denied));
    }
}
