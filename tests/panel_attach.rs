//! Optional end-to-end attach test against an isolated hc core: the framed
//! viewer stream relays a panel's first paint and echoes typed bytes, and
//! detaching leaves the panel running.
#![cfg(unix)]
use std::fs;
use std::path::PathBuf;
use std::process::Command;
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use tearoom::hc::attach::{self, PanelEvent};

struct EmbeddedHc {
    binary: &'static str,
    base: PathBuf,
    session: String,
}

impl EmbeddedHc {
    fn new(tag: &str) -> Self {
        // Short on purpose: unix socket paths cap near 104 bytes, and the
        // core refuses longer session sockets.
        let base = std::env::temp_dir().join(format!("ta-{}-{}", tag, std::process::id()));
        fs::create_dir_all(&base).unwrap();
        Self {
            binary: env!("CARGO_BIN_EXE_tearoom"),
            session: format!("ta-{}", std::process::id()),
            base,
        }
    }

    fn env(&self, key: &str) -> PathBuf {
        self.base.join(key.to_lowercase())
    }

    fn socket(&self) -> PathBuf {
        self.base.join("hc.sock")
    }

    fn run(&self, args: &[&str]) -> std::process::Output {
        self.command().args(args).output().unwrap()
    }

    fn command(&self) -> Command {
        let mut command = Command::new(self.binary);
        command
            .env("HC_SESSION", &self.session)
            .env("HC_SOCKET", self.socket())
            .env("XDG_CONFIG_HOME", self.env("config"))
            .env("XDG_STATE_HOME", self.env("state"))
            .env("HOME", self.env("home"))
            .env("PATH", "/usr/bin:/bin")
            .env("TEAROOM_HERDR_BIN", self.env("home").join("missing-herdr"));
        command
    }
}

impl Drop for EmbeddedHc {
    fn drop(&mut self) {
        let _ = self.run(&["hc", "kill", "panel-probe", "--json"]);
        let _ = self.run(&["hc", "server", "stop", "--json"]);
        let _ = fs::remove_dir_all(&self.base);
    }
}

/// A relay collector that keeps every event for polling assertions.
#[derive(Clone)]
struct Collector {
    events: Arc<Mutex<Vec<PanelEvent>>>,
}

impl Collector {
    fn new() -> Self {
        Self {
            events: Arc::new(Mutex::new(Vec::new())),
        }
    }

    fn relay(&self) -> impl Fn(PanelEvent) + Send + 'static {
        let events = self.events.clone();
        move |event| events.lock().unwrap().push(event)
    }

    fn text(&self) -> String {
        let events = self.events.lock().unwrap();
        events
            .iter()
            .filter_map(|e| match e {
                PanelEvent::Data(bytes) => Some(String::from_utf8_lossy(bytes).into_owned()),
                PanelEvent::Closed(_) => None,
            })
            .collect()
    }

    fn closed(&self) -> bool {
        self.events
            .lock()
            .unwrap()
            .iter()
            .any(|e| matches!(e, PanelEvent::Closed(_)))
    }
}

fn wait_until(timeout: Duration, mut ready: impl FnMut() -> bool) {
    let deadline = Instant::now() + timeout;
    while !ready() {
        assert!(Instant::now() < deadline, "timed out waiting for the panel");
        std::thread::sleep(Duration::from_millis(50));
    }
}

#[test]
#[ignore = "starts an isolated hc core and terminal"]
fn attach_streams_a_panel_relays_input_and_detaches_cleanly() {
    let hc = EmbeddedHc::new("a");
    let started = hc.run(&["hc", "ls", "--json"]);
    assert!(
        started.status.success(),
        "{}",
        String::from_utf8_lossy(&started.stderr)
    );
    let run = hc.run(&[
        "hc",
        "run",
        "--bg",
        "--name",
        "panel-probe",
        "--no-parent",
        "/bin/cat",
    ]);
    assert!(
        run.status.success(),
        "{}",
        String::from_utf8_lossy(&run.stderr)
    );

    let collector = Collector::new();
    let stream =
        attach::attach(&hc.socket(), "panel-probe", 120, 36, 500, collector.relay()).unwrap();
    stream.focus(true).unwrap();

    // The core queues a first paint even for a silent screen.
    wait_until(Duration::from_secs(10), || {
        collector.closed() || !collector.text().is_empty() || {
            let events = collector.events.lock().unwrap();
            !events.is_empty()
        }
    });

    // Typed bytes echo back through the panel: `cat` writes them to its tty.
    stream.write(b"typed-echo-check\r").unwrap();
    wait_until(Duration::from_secs(10), || {
        collector.text().contains("typed-echo-check")
    });

    // Detaching must leave the panel live for the next viewer.
    stream.detach();
    std::thread::sleep(Duration::from_millis(200));
    let listed = hc.run(&["hc", "ls", "--json"]);
    let listed = String::from_utf8_lossy(&listed.stdout);
    assert!(
        listed.contains("panel-probe") && listed.contains("\"live\""),
        "panel vanished after detach: {listed}"
    );
}
