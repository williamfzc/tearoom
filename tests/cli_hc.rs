//! Optional end-to-end CLI dispatch against an isolated hc core and shell agent.
#![cfg(unix)]
use std::fs;
use std::os::unix::fs::DirBuilderExt;
use std::path::PathBuf;
use std::process::{Command, Stdio};
use std::time::{Duration, Instant};

struct EmbeddedHc {
    binary: &'static str,
    socket: PathBuf,
    config: PathBuf,
    state: PathBuf,
    home: PathBuf,
    session: String,
}

impl EmbeddedHc {
    fn command(&self) -> Command {
        let mut command = Command::new(self.binary);
        command
            .env("HC_SESSION", &self.session)
            .env("HC_SOCKET", &self.socket)
            .env("XDG_CONFIG_HOME", &self.config)
            .env("XDG_STATE_HOME", &self.state)
            .env("HOME", &self.home)
            .env("PATH", "/usr/bin:/bin")
            .env("TEAROOM_HERDR_BIN", self.home.join("missing-herdr"));
        command
    }

    fn run(&self, args: &[&str]) -> std::process::Output {
        self.command().args(args).output().unwrap()
    }
}

impl Drop for EmbeddedHc {
    fn drop(&mut self) {
        let _ = self.run(&["hc", "kill", "tearoom-restart-probe", "--json"]);
        let _ = self.run(&["hc", "server", "stop", "--json"]);
        let _ = fs::remove_file(&self.socket);
    }
}

struct DefaultSocketCleanup {
    binary: &'static str,
    session: String,
    config: PathBuf,
    state: PathBuf,
    home: PathBuf,
    runtime: PathBuf,
}

impl DefaultSocketCleanup {
    fn stop(&self) {
        let _ = Command::new(self.binary)
            .args(["hc", "server", "stop", "--json"])
            .env("HC_SESSION", &self.session)
            .env_remove("HC_SOCKET")
            .env("XDG_RUNTIME_DIR", &self.runtime)
            .env("XDG_CONFIG_HOME", &self.config)
            .env("XDG_STATE_HOME", &self.state)
            .env("HOME", &self.home)
            .output();
    }
}

impl Drop for DefaultSocketCleanup {
    fn drop(&mut self) {
        self.stop();
        let _ = fs::remove_dir_all(&self.runtime);
    }
}

#[test]
#[ignore = "starts an isolated hc core and terminal"]
fn embedded_hc_runs_and_keeps_a_terminal_across_restart() {
    let dir = tempfile::tempdir().unwrap();
    let binary = env!("CARGO_BIN_EXE_tearoom");
    let hc = EmbeddedHc {
        binary,
        socket: PathBuf::from(format!("/tmp/tr-hc-{}.sock", std::process::id())),
        config: dir.path().join("config"),
        state: dir.path().join("state"),
        home: dir.path().join("home"),
        session: format!("tearoom-restart-{}", std::process::id()),
    };
    let original = fs::read(binary).unwrap();

    let refused = hc.run(&["hc", "update", "--json"]);
    assert!(!refused.status.success());
    assert!(String::from_utf8_lossy(&refused.stderr).contains("update Tearoom instead"));
    assert_eq!(fs::read(binary).unwrap(), original);

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
        "tearoom-restart-probe",
        "--no-parent",
        "/bin/sh",
        "-c",
        "echo EMBEDDED-TERMINAL; exec sleep 3600",
    ]);
    assert!(
        run.status.success(),
        "{}",
        String::from_utf8_lossy(&run.stderr)
    );

    let read = hc.run(&["hc", "read", "tearoom-restart-probe", "--lines", "10"]);
    assert!(
        read.status.success(),
        "{}",
        String::from_utf8_lossy(&read.stderr)
    );
    assert!(String::from_utf8_lossy(&read.stdout).contains("EMBEDDED-TERMINAL"));

    let restart = hc.run(&["hc", "server", "restart", "--json"]);
    assert!(
        restart.status.success(),
        "{}",
        String::from_utf8_lossy(&restart.stderr)
    );
    let read_after_restart = hc.run(&["hc", "read", "tearoom-restart-probe", "--lines", "10"]);
    assert!(
        read_after_restart.status.success(),
        "{}",
        String::from_utf8_lossy(&read_after_restart.stderr)
    );
    assert!(String::from_utf8_lossy(&read_after_restart.stdout).contains("EMBEDDED-TERMINAL"));

    let killed = hc.run(&["hc", "kill", "tearoom-restart-probe", "--json"]);
    assert!(
        killed.status.success(),
        "{}",
        String::from_utf8_lossy(&killed.stderr)
    );
}

#[test]
#[ignore = "starts an isolated hc core and terminal"]
fn cli_delivers_a_room_mention_to_hc() {
    let dir = tempfile::tempdir().unwrap();
    let session = format!("t{}", std::process::id());
    let config = dir.path().join("config");
    let state = dir.path().join("state");
    let home = dir.path().join("home");
    let runtime = PathBuf::from(format!("/tmp/trr{}", std::process::id()));
    fs::DirBuilder::new().mode(0o700).create(&runtime).unwrap();
    let binary = env!("CARGO_BIN_EXE_tearoom");
    let cleanup = DefaultSocketCleanup {
        binary,
        session: session.clone(),
        config: config.clone(),
        state: state.clone(),
        home: home.clone(),
        runtime: runtime.clone(),
    };
    let script = dir.path().join("agent.sh");
    fs::write(&script, format!("#!/bin/sh\nprintf '\\033]7501;state=idle\\007'\nwhile IFS= read -r line; do\n  printf '\\033]7501;state=working\\007'\n  '{binary}' send --room project --as reviewer --reply-to project#1 --text reviewed\n  printf '\\033]7501;state=done\\007'\ndone\n")).unwrap();
    let build = |program: &str| {
        let mut command = Command::new(program);
        command
            .current_dir(dir.path())
            .env("HC_SESSION", &session)
            .env_remove("HC_SOCKET")
            .env("XDG_RUNTIME_DIR", &runtime)
            .env("XDG_CONFIG_HOME", &config)
            .env("XDG_STATE_HOME", &state)
            .env("HOME", &home)
            .env("PATH", "/usr/bin:/bin")
            .env("TEAROOM_HERDR_BIN", dir.path().join("missing-herdr"));
        command
    };
    let run = |args: &[&str]| {
        let output = build(binary).args(args).output().unwrap();
        assert!(
            output.status.success(),
            "{}",
            String::from_utf8_lossy(&output.stderr)
        );
    };
    run(&["room", "init", "--name", "project"]);
    run(&[
        "role",
        "init",
        "--name",
        "reviewer",
        "--agent-type",
        "probe",
        "--command",
        "sh",
        "--arg",
        script.to_str().unwrap(),
    ]);
    run(&[
        "role",
        "init",
        "--name",
        "builder",
        "--agent-type",
        "probe",
        "--command",
        "sh",
        "--arg",
        script.to_str().unwrap(),
    ]);
    run(&[
        "room",
        "roles",
        "--room",
        "project",
        "--add",
        "builder,reviewer",
    ]);
    run(&[
        "send",
        "--room",
        "project",
        "--as",
        "human",
        "--to",
        "@reviewer",
        "--text",
        "Please review; @builder will follow up",
    ]);
    let mut dispatcher = build(binary)
        .args(["collab", "run"])
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .unwrap();
    let path = dir.path().join(".tearoom/rooms/project/records.jsonl");
    let deadline = Instant::now() + Duration::from_secs(30);
    let mut delivered = false;
    while Instant::now() < deadline {
        let records = fs::read_to_string(&path).unwrap_or_default();
        if records.contains("reviewed") {
            delivered = true;
            break;
        }
        std::thread::sleep(Duration::from_millis(100));
    }
    let _ = dispatcher.kill();
    let _ = dispatcher.wait();
    cleanup.stop();
    assert!(delivered, "the reviewer did not record its reply");
    let state: serde_json::Value =
        serde_json::from_slice(&fs::read(dir.path().join(".tearoom/dispatch.json")).unwrap())
            .unwrap();
    assert_eq!(
        state["intents"][0]["deliveries"].as_array().unwrap().len(),
        1
    );
    assert_eq!(state["intents"][0]["deliveries"][0]["role"], "reviewer");
}

struct Dispatchers(Vec<std::process::Child>);

impl Dispatchers {
    fn stop(&mut self, index: usize) {
        if let Some(child) = self.0.get_mut(index) {
            unsafe { libc::kill(child.id() as i32, libc::SIGTERM) };
            let _ = child.wait();
        }
    }
}

impl Drop for Dispatchers {
    fn drop(&mut self) {
        for child in &mut self.0 {
            let _ = child.kill();
            let _ = child.wait();
        }
    }
}

struct SharedWorkspaceCleanup {
    binary: &'static str,
    session: String,
    config: PathBuf,
    state: PathBuf,
    home: PathBuf,
    runtime: PathBuf,
}

impl SharedWorkspaceCleanup {
    fn command(&self) -> Command {
        let mut command = Command::new(self.binary);
        command
            .env("HC_SESSION", &self.session)
            .env_remove("HC_SOCKET")
            .env("XDG_RUNTIME_DIR", &self.runtime)
            .env("XDG_CONFIG_HOME", &self.config)
            .env("XDG_STATE_HOME", &self.state)
            .env("HOME", &self.home)
            .env("PATH", "/usr/bin:/bin");
        command
    }

    fn live_panels(&self) -> Vec<String> {
        let output = self
            .command()
            .args(["hc", "ls", "--json"])
            .output()
            .unwrap();
        assert!(
            output.status.success(),
            "{}",
            String::from_utf8_lossy(&output.stderr)
        );
        let value: serde_json::Value = serde_json::from_slice(&output.stdout).unwrap();
        value["terminals"]
            .as_array()
            .unwrap()
            .iter()
            .filter(|terminal| terminal["state"] == "live")
            .filter_map(|terminal| terminal["name"].as_str().map(str::to_owned))
            .collect()
    }
}

impl Drop for SharedWorkspaceCleanup {
    fn drop(&mut self) {
        let _ = self
            .command()
            .args(["hc", "server", "stop", "--json"])
            .output();
        let _ = fs::remove_dir_all(&self.runtime);
    }
}

#[test]
#[ignore = "starts an isolated hc core and two terminals"]
fn stopping_one_workspace_dispatcher_only_kills_its_agents() {
    use tearoom::collab::panel_name;

    let temp = tempfile::tempdir().unwrap();
    let binary = env!("CARGO_BIN_EXE_tearoom");
    let runtime = PathBuf::from(format!("/tmp/trmulti{}", std::process::id()));
    fs::DirBuilder::new().mode(0o700).create(&runtime).unwrap();
    let cleanup = SharedWorkspaceCleanup {
        binary,
        session: format!("multi-{}", std::process::id()),
        config: temp.path().join("config"),
        state: temp.path().join("state"),
        home: temp.path().join("home"),
        runtime,
    };
    let mut dispatchers = Dispatchers(Vec::new());
    let workspaces = [temp.path().join("one"), temp.path().join("two")];
    let script = temp.path().join("agent.sh");
    fs::write(
        &script,
        "printf '\\033]7501;state=idle\\007'\nwhile IFS= read -r line; do printf '\\033]7501;state=done\\007'; done\n",
    )
    .unwrap();

    for workspace in &workspaces {
        fs::create_dir_all(workspace).unwrap();
        let mut setup = cleanup.command();
        let output = setup
            .current_dir(workspace)
            .args(["room", "init", "--name", "project"])
            .output()
            .unwrap();
        assert!(
            output.status.success(),
            "{}",
            String::from_utf8_lossy(&output.stderr)
        );
        let mut setup = cleanup.command();
        let output = setup
            .current_dir(workspace)
            .args([
                "role",
                "init",
                "--name",
                "worker",
                "--agent-type",
                "probe",
                "--command",
                "sh",
                "--arg",
            ])
            .arg(&script)
            .output()
            .unwrap();
        assert!(
            output.status.success(),
            "{}",
            String::from_utf8_lossy(&output.stderr)
        );
        let mut bind = cleanup.command();
        let output = bind
            .current_dir(workspace)
            .args(["room", "roles", "--room", "project", "--add", "worker"])
            .output()
            .unwrap();
        assert!(
            output.status.success(),
            "{}",
            String::from_utf8_lossy(&output.stderr)
        );
        let mut send = cleanup.command();
        let output = send
            .current_dir(workspace)
            .args([
                "send", "--room", "project", "--as", "human", "--to", "@worker", "--text", "start",
            ])
            .output()
            .unwrap();
        assert!(
            output.status.success(),
            "{}",
            String::from_utf8_lossy(&output.stderr)
        );
    }

    for workspace in &workspaces {
        let mut dispatcher = cleanup.command();
        dispatchers.0.push(
            dispatcher
                .current_dir(workspace)
                .args(["collab", "run"])
                .stdout(Stdio::null())
                .stderr(Stdio::null())
                .spawn()
                .unwrap(),
        );
    }

    let panels = workspaces
        .iter()
        .map(|workspace| panel_name(&workspace.join(".tearoom"), "project", "worker").unwrap())
        .collect::<Vec<_>>();
    let deadline = Instant::now() + Duration::from_secs(30);
    loop {
        let live = cleanup.live_panels();
        if panels.iter().all(|panel| live.contains(panel)) {
            break;
        }
        assert!(
            Instant::now() < deadline,
            "both workspace agents did not start; live={live:?}"
        );
        std::thread::sleep(Duration::from_millis(100));
    }

    dispatchers.stop(0);
    let deadline = Instant::now() + Duration::from_secs(15);
    loop {
        let live = cleanup.live_panels();
        if !live.contains(&panels[0]) && live.contains(&panels[1]) {
            break;
        }
        assert!(
            Instant::now() < deadline,
            "stopping one workspace did not isolate cleanup; live={live:?}"
        );
        std::thread::sleep(Duration::from_millis(100));
    }

    dispatchers.stop(1);
    let deadline = Instant::now() + Duration::from_secs(15);
    loop {
        let live = cleanup.live_panels();
        if !panels.iter().any(|panel| live.contains(panel)) {
            break;
        }
        assert!(
            Instant::now() < deadline,
            "stopping the second workspace left its agent running; live={live:?}"
        );
        std::thread::sleep(Duration::from_millis(100));
    }
}
