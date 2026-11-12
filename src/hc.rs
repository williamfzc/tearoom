//! Local JSON-RPC client for the separate herdr-core runtime.
//! Delivery policy belongs to the collaboration layer, not this transport.

mod socket;

use serde_json::{json, Value};
use std::fmt;
use std::io::{self, BufRead, BufReader, Write};
use std::path::{Path, PathBuf};
use std::time::Duration;

/// Identifies whether an RPC may have reached hc when it failed.
#[derive(Debug)]
pub enum HcError {
    Configuration(String),
    Connect(io::Error),
    Startup(String),
    /// The core may have received some or all of the request; do not blindly retry.
    Submission(io::Error),
    /// The core may have executed the request even if its reply was not received.
    Response(io::Error),
    Protocol(String),
    Remote(Value),
    Unsupported,
}

impl HcError {
    pub fn timed_out(&self) -> bool {
        match self {
            Self::Connect(e) | Self::Submission(e) | Self::Response(e) => {
                matches!(
                    e.kind(),
                    io::ErrorKind::TimedOut | io::ErrorKind::WouldBlock
                )
            }
            _ => false,
        }
    }
}

impl fmt::Display for HcError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Configuration(e) => write!(f, "invalid hc configuration: {e}"),
            Self::Connect(e) => write!(f, "cannot connect to hc: {e}"),
            Self::Startup(e) => write!(f, "cannot start hc: {e}"),
            Self::Submission(e) => write!(f, "hc request submission is uncertain: {e}"),
            Self::Response(e) => write!(f, "hc response is uncertain: {e}"),
            Self::Protocol(e) => write!(f, "invalid hc response: {e}"),
            Self::Remote(e) => write!(f, "hc rejected request: {e}"),
            Self::Unsupported => {
                write!(f, "hc local RPC transport is unsupported on this platform")
            }
        }
    }
}

impl std::error::Error for HcError {}

/// One connection and one request per call, like hc's short-lived client.
pub struct HcClient {
    socket: PathBuf,
    autostart: bool,
}

impl HcClient {
    pub fn new(socket: impl Into<PathBuf>) -> Self {
        Self {
            socket: socket.into(),
            autostart: false,
        }
    }

    pub fn socket(&self) -> &Path {
        &self.socket
    }

    /// Use an existing runtime without starting a new one.
    pub fn call_existing(
        &self,
        method: &str,
        params: Value,
        timeout: Duration,
    ) -> Result<Value, HcError> {
        let client = Self {
            socket: self.socket.clone(),
            autostart: false,
        };
        client.call(method, params, timeout)
    }

    /// The caller chooses a timeout appropriate to the method; no retry occurs here.
    #[cfg(unix)]
    pub fn call(&self, method: &str, params: Value, timeout: Duration) -> Result<Value, HcError> {
        use std::os::unix::net::UnixStream;
        let mut stream = match UnixStream::connect(&self.socket) {
            Ok(stream) => stream,
            Err(error)
                if self.autostart
                    && matches!(
                        error.kind(),
                        io::ErrorKind::NotFound | io::ErrorKind::ConnectionRefused
                    ) =>
            {
                start_hc(timeout)?;
                UnixStream::connect(&self.socket).map_err(HcError::Connect)?
            }
            Err(error) => return Err(HcError::Connect(error)),
        };
        stream
            .set_read_timeout(Some(timeout))
            .map_err(HcError::Connect)?;
        stream
            .set_write_timeout(Some(timeout))
            .map_err(HcError::Connect)?;
        let mut request =
            serde_json::to_vec(&json!({"jsonrpc":"2.0", "id":1, "method":method, "params":params}))
                .map_err(|e| HcError::Protocol(e.to_string()))?;
        request.push(b'\n');
        stream.write_all(&request).map_err(HcError::Submission)?;
        let mut line = String::new();
        if BufReader::new(stream)
            .read_line(&mut line)
            .map_err(HcError::Response)?
            == 0
        {
            return Err(HcError::Protocol(
                "core closed the connection without a response".into(),
            ));
        }
        let response: Value =
            serde_json::from_str(&line).map_err(|e| HcError::Protocol(e.to_string()))?;
        if response.get("id") != Some(&json!(1)) || response.get("jsonrpc") != Some(&json!("2.0")) {
            return Err(HcError::Protocol(
                "response id or JSON-RPC version did not match".into(),
            ));
        }
        if let Some(error) = response.get("error") {
            return Err(HcError::Remote(error.clone()));
        }
        response
            .get("result")
            .cloned()
            .ok_or_else(|| HcError::Protocol("missing result".into()))
    }

    #[cfg(not(unix))]
    pub fn call(
        &self,
        _method: &str,
        _params: Value,
        _timeout: Duration,
    ) -> Result<Value, HcError> {
        Err(HcError::Unsupported)
    }
}

#[cfg(unix)]
fn start_hc(timeout: Duration) -> Result<(), HcError> {
    use std::process::{Command, Stdio};
    use std::time::Instant;
    // Let hc resolve its own core startup and mismatched-session safeguards.
    let mut child =
        Command::new(std::env::current_exe().map_err(|error| HcError::Startup(error.to_string()))?)
            .args(["hc", "ls", "--json"])
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn()
            .map_err(|error| HcError::Startup(error.to_string()))?;
    let start = Instant::now();
    loop {
        match child.try_wait() {
            Ok(Some(status)) if status.success() => return Ok(()),
            Ok(Some(status)) => {
                return Err(HcError::Startup(format!("hc ls exited with {status}")))
            }
            Ok(None) if start.elapsed() >= timeout => {
                let _ = child.kill();
                let _ = child.wait();
                return Err(HcError::Startup("startup timed out".into()));
            }
            Ok(None) => std::thread::sleep(Duration::from_millis(10)),
            Err(error) => return Err(HcError::Startup(error.to_string())),
        }
    }
}

#[cfg(all(test, unix))]
mod tests {
    use super::*;
    use std::os::unix::net::UnixListener;
    use std::sync::atomic::{AtomicU64, Ordering};

    static NEXT: AtomicU64 = AtomicU64::new(0);

    fn socket() -> PathBuf {
        std::env::temp_dir().join(format!(
            "tr-hc-{}-{}.sock",
            std::process::id(),
            NEXT.fetch_add(1, Ordering::Relaxed)
        ))
    }

    #[test]
    fn socket_selection_matches_hc_precedence() {
        let path = |s: &str, e: Option<&str>, c: Option<&str>, r: Option<&str>| {
            socket::socket_path(
                s,
                e.map(Into::into),
                c.map(Into::into),
                r.map(Into::into),
                "/state".into(),
            )
            .unwrap()
        };
        assert_eq!(
            path("x", Some("/explicit"), Some("/config"), Some("/run")),
            PathBuf::from("/explicit")
        );
        assert_eq!(
            path("x", None, Some("/config"), Some("/run")),
            PathBuf::from("/config")
        );
        assert_eq!(
            path(" x ", None, None, Some("/run")),
            PathBuf::from("/run/herdr-core/x.sock")
        );
        assert_eq!(
            path("", None, None, None),
            PathBuf::from("/state/herdr-core/default/default.sock")
        );
        assert!(socket::socket_path("../bad", None, None, None, "/state".into()).is_err());
    }

    #[test]
    fn sends_one_json_rpc_request_and_returns_the_result() {
        let path = socket();
        let listener = UnixListener::bind(&path).unwrap();
        let worker = std::thread::spawn(move || {
            let (mut stream, _) = listener.accept().unwrap();
            let mut line = String::new();
            BufReader::new(&stream).read_line(&mut line).unwrap();
            let request: Value = serde_json::from_str(&line).unwrap();
            assert_eq!(request["method"], "terminal.list");
            assert_eq!(request["params"], json!({"all": false}));
            stream
                .write_all(b"{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"terminals\":[]}}\n")
                .unwrap();
        });
        let result = HcClient::new(&path)
            .call(
                "terminal.list",
                json!({"all": false}),
                Duration::from_secs(1),
            )
            .unwrap();
        assert_eq!(result["terminals"], json!([]));
        worker.join().unwrap();
        std::fs::remove_file(path).unwrap();
    }

    #[test]
    fn distinguishes_connect_and_response_timeout() {
        let path = socket();
        assert!(matches!(
            HcClient::new(&path).call("terminal.list", json!({}), Duration::from_secs(1)),
            Err(HcError::Connect(_))
        ));
        let listener = UnixListener::bind(&path).unwrap();
        let worker = std::thread::spawn(move || {
            let (_stream, _) = listener.accept().unwrap();
            std::thread::sleep(Duration::from_millis(150));
        });
        let error = HcClient::new(&path)
            .call("terminal.send", json!({}), Duration::from_millis(25))
            .unwrap_err();
        assert!(matches!(error, HcError::Response(_)));
        assert!(error.timed_out());
        worker.join().unwrap();
        std::fs::remove_file(path).unwrap();
    }
}
