//! Black-box contract for `tearoom web`: loopback transport, page token,
//! room navigation state, history reads, and addressed sends.
#![cfg(unix)]
use std::io::{BufRead, BufReader, Read, Write};
use std::net::TcpStream;
use std::path::Path;
use std::process::{Child, Command, Stdio};
use std::time::Duration;

fn command(dir: &Path) -> Command {
    let mut command = Command::new(env!("CARGO_BIN_EXE_tearoom"));
    let runtime = dir.join(".test-runtime");
    std::fs::create_dir_all(&runtime).unwrap();
    use std::os::unix::fs::PermissionsExt;
    std::fs::set_permissions(&runtime, std::fs::Permissions::from_mode(0o700)).unwrap();
    command
        .current_dir(dir)
        .env(
            "HC_SESSION",
            format!("tearoom-web-test-{}", std::process::id()),
        )
        .env("HC_SOCKET", runtime.join("hc.sock"))
        .env("TEAROOM_SERVICE_DIR", dir.join(".test-service"))
        .env("XDG_RUNTIME_DIR", runtime)
        .env("XDG_CONFIG_HOME", dir.join(".test-config"))
        .env("XDG_STATE_HOME", dir.join(".test-state"))
        .env("HOME", dir.join(".test-home"))
        .env("PATH", "/usr/bin:/bin")
        .env("TEAROOM_HERDR_BIN", dir.join("missing-herdr"));
    command
}

struct ServiceCleanup(std::path::PathBuf);

impl ServiceCleanup {
    fn new(workspace: &Path) -> Self {
        Self(workspace.to_path_buf())
    }
}

impl Drop for ServiceCleanup {
    fn drop(&mut self) {
        let _ = command(&self.0).args(["service", "stop"]).output();
        let _ = command(&self.0)
            .args(["hc", "server", "stop", "--json"])
            .output();
    }
}

fn run(dir: &Path, args: &[&str]) -> std::process::Output {
    let output = command(dir).args(args).output().unwrap();
    assert!(
        output.status.success(),
        "{}",
        String::from_utf8_lossy(&output.stderr)
    );
    output
}

struct Web {
    child: Child,
    host: String,
    origin: String,
    token: String,
}

struct Response {
    status: u16,
    headers: String,
    body: String,
}

impl Web {
    fn start(dir: &Path) -> Self {
        let mut child = command(dir)
            .args(["web", "--port", "0"])
            .stdout(Stdio::piped())
            .stderr(Stdio::inherit())
            .spawn()
            .unwrap();
        let mut line = String::new();
        BufReader::new(child.stdout.take().unwrap())
            .read_line(&mut line)
            .unwrap();
        let value: serde_json::Value = serde_json::from_str(line.trim()).unwrap_or_else(|_| {
            panic!("web did not print its URL: {line:?}");
        });
        let url = value["url"].as_str().unwrap().trim_end_matches('/');
        let host = url.strip_prefix("http://").unwrap().to_owned();
        let origin = url.to_owned();
        let mut web = Self {
            child,
            host,
            origin,
            token: String::new(),
        };
        let page = web.exchange(&web.get("/", None, &[]));
        assert_eq!(page.status, 200);
        web.token = extract_token(&page.body);
        web
    }

    fn exchange(&self, request: &str) -> Response {
        let mut stream = TcpStream::connect(&self.host).unwrap();
        stream
            .set_read_timeout(Some(Duration::from_secs(5)))
            .unwrap();
        stream.write_all(request.as_bytes()).unwrap();
        let mut bytes = Vec::new();
        stream.read_to_end(&mut bytes).unwrap();
        let text = String::from_utf8_lossy(&bytes);
        let (head, body) = text.split_once("\r\n\r\n").expect("complete HTTP response");
        let status = head
            .lines()
            .next()
            .unwrap()
            .split_whitespace()
            .nth(1)
            .unwrap()
            .parse()
            .unwrap();
        Response {
            status,
            headers: head.to_ascii_lowercase(),
            body: body.into(),
        }
    }

    fn get(&self, path: &str, token: Option<&str>, extra: &[(&str, &str)]) -> String {
        self.raw("GET", path, token, None, extra, "")
    }

    fn post(
        &self,
        path: &str,
        token: Option<&str>,
        origin: Option<&str>,
        extra: &[(&str, &str)],
        body: &str,
    ) -> String {
        self.raw("POST", path, token, origin, extra, body)
    }

    fn raw(
        &self,
        method: &str,
        path: &str,
        token: Option<&str>,
        origin: Option<&str>,
        extra: &[(&str, &str)],
        body: &str,
    ) -> String {
        let mut request = format!("{method} {path} HTTP/1.1\r\nHost: {}\r\n", self.host);
        if let Some(token) = token {
            request.push_str(&format!("X-Tearoom-Token: {token}\r\n"));
        }
        if let Some(origin) = origin {
            request.push_str(&format!("Origin: {origin}\r\n"));
        }
        for (name, value) in extra {
            request.push_str(&format!("{name}: {value}\r\n"));
        }
        if !body.is_empty() {
            request.push_str("Content-Type: application/json\r\n");
        }
        request.push_str(&format!(
            "Content-Length: {}\r\nConnection: close\r\n\r\n{body}",
            body.len()
        ));
        request
    }

    fn api(&self, path: &str) -> serde_json::Value {
        let response = self.exchange(&self.get(path, Some(&self.token), &[]));
        assert_eq!(response.status, 200, "{}", response.body);
        serde_json::from_str(&response.body).unwrap()
    }
}

impl Drop for Web {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

fn extract_token(html: &str) -> String {
    let marker = "name=\"tearoom-token\" content=\"";
    let start = html.find(marker).expect("token meta tag") + marker.len();
    let end = html[start..].find('"').expect("closed token attribute") + start;
    html[start..end].into()
}

fn setup(dir: &Path) {
    run(dir, &["room", "init", "--name", "project"]);
    run(
        dir,
        &["room", "init", "--name", "review", "--parent", "project"],
    );
    run(
        dir,
        &[
            "role",
            "init",
            "--name",
            "reviewer",
            "--agent-type",
            "probe",
            "--command",
            "true",
        ],
    );
    run(
        dir,
        &[
            "role",
            "init",
            "--name",
            "builder",
            "--agent-type",
            "probe",
            "--command",
            "true",
        ],
    );
    run(
        dir,
        &[
            "room",
            "roles",
            "--room",
            "project",
            "--add",
            "builder,reviewer",
        ],
    );
    run(
        dir,
        &[
            "send",
            "--room",
            "project",
            "--as",
            "person",
            "--text",
            "existing history",
        ],
    );
}

#[test]
fn web_serves_rooms_reads_history_and_sends_addressed_messages() {
    let dir = tempfile::tempdir().unwrap();
    let _cleanup = ServiceCleanup::new(dir.path());
    setup(dir.path());
    let web = Web::start(dir.path());

    let page = web.exchange(&web.get("/", None, &[]));
    assert_eq!(page.status, 200);
    assert!(page.body.contains("id=\"rooms\""));
    assert!(page.body.contains("id=\"records\""));
    assert!(page.body.contains("id=\"composer\""));
    assert!(page.headers.contains("content-security-policy"));
    assert!(page.headers.contains("x-frame-options: deny"));
    assert!(!page.headers.contains("access-control-allow-origin"));

    let state = web.api("/api/state");
    assert_eq!(state["rooms"].as_array().unwrap().len(), 2);
    let project = state["rooms"]
        .as_array()
        .unwrap()
        .iter()
        .find(|room| room["name"] == "project")
        .unwrap();
    assert_eq!(project["roles"], serde_json::json!(["builder", "reviewer"]));

    let history = web.api("/api/rooms/project/records?after=e0");
    assert_eq!(history["records"].as_array().unwrap().len(), 1);
    assert_eq!(history["records"][0]["content"], "existing history");
    let status = run(dir.path(), &["service", "status"]);
    let status: serde_json::Value = serde_json::from_slice(&status.stdout).unwrap();
    assert_eq!(status["running"], false, "reads must not start the service");

    let body = serde_json::json!({
        "text": "@builder please inspect; @reviewer this is for you",
        "to": ["reviewer"],
        "author": "codex",
    })
    .to_string();
    let response = web.exchange(&web.post(
        "/api/rooms/project/messages",
        Some(&web.token),
        Some(&web.origin),
        &[],
        &body,
    ));
    assert_eq!(response.status, 201, "{}", response.body);
    let record: serde_json::Value = serde_json::from_str(&response.body).unwrap();
    assert_eq!(record["record"]["seq"], 2);
    assert_eq!(record["record"]["author"], "codex");
    assert_eq!(
        record["record"]["content"]["mentions"],
        serde_json::json!(["reviewer"])
    );
    let status = run(dir.path(), &["service", "status"]);
    let status: serde_json::Value = serde_json::from_slice(&status.stdout).unwrap();
    assert_eq!(
        status["running"], true,
        "addressed browser send should start the service"
    );
    assert_eq!(status["workspaces"].as_array().unwrap().len(), 1);

    let after = web.api("/api/rooms/project/records?after=e1");
    assert_eq!(after["records"].as_array().unwrap().len(), 1);
    assert_eq!(after["records"][0]["seq"], 2);

    drop(web);
    let read = String::from_utf8(run(dir.path(), &["read", "--room", "project"]).stdout).unwrap();
    assert_eq!(read.lines().count(), 2);
    assert!(read.contains("existing history"));
    assert!(read.contains("\"mentions\":[\"reviewer\"]"));
}

#[test]
fn web_refuses_other_hosts_origins_and_missing_tokens() {
    let dir = tempfile::tempdir().unwrap();
    let _cleanup = ServiceCleanup::new(dir.path());
    setup(dir.path());
    let web = Web::start(dir.path());
    let path = "/api/rooms/project/messages";
    let body = serde_json::json!({"text": "must not append"}).to_string();

    let mut evil_host = web.get("/api/state", Some(&web.token), &[]);
    evil_host = evil_host.replace(&format!("Host: {}", web.host), "Host: evil.example");
    let response = web.exchange(&evil_host);
    assert_eq!(response.status, 403);

    let response = web.exchange(&web.get("/api/state", None, &[]));
    assert_eq!(response.status, 403);

    let response = web.exchange(&web.post(
        path,
        Some(&web.token),
        Some("https://evil.example"),
        &[],
        &body,
    ));
    assert_eq!(response.status, 403);

    let response = web.exchange(&web.post(
        path,
        Some(&web.token),
        Some(&web.origin),
        &[("Sec-Fetch-Site", "cross-site")],
        &body,
    ));
    assert_eq!(response.status, 403);

    let response = web.exchange(&web.post(
        path,
        Some(&web.token),
        Some(&web.origin),
        &[],
        r#"{"text":"bad","unknown":true}"#,
    ));
    assert_eq!(response.status, 400);

    let response = web.exchange(&web.get("/api/rooms/missing/records", Some(&web.token), &[]));
    assert_eq!(response.status, 404);

    let before = run(dir.path(), &["read", "--room", "project"]);
    assert_eq!(String::from_utf8(before.stdout).unwrap().lines().count(), 1);

    let response = web.exchange(&web.post(
        path,
        Some(&web.token),
        Some(&web.origin),
        &[],
        &serde_json::json!({"text": "valid", "to": ["reviewer"]}).to_string(),
    ));
    assert_eq!(response.status, 201, "{}", response.body);
    let status = run(dir.path(), &["service", "status"]);
    let status: serde_json::Value = serde_json::from_slice(&status.stdout).unwrap();
    assert_eq!(
        status["running"], true,
        "valid addressed browser send should activate"
    );
}

#[test]
fn web_rejects_missing_token_non_json_oversized_body_and_wrong_methods() {
    let dir = tempfile::tempdir().unwrap();
    setup(dir.path());
    let web = Web::start(dir.path());
    let path = "/api/rooms/project/messages";
    let body = serde_json::json!({"text": "must not append"}).to_string();

    let response = web.exchange(&web.post(path, None, Some(&web.origin), &[], &body));
    assert_eq!(response.status, 403);

    let plain = format!(
        "POST {path} HTTP/1.1\r\nHost: {}\r\nOrigin: {}\r\nX-Tearoom-Token: {}\r\nContent-Type: text/plain\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
        web.host,
        web.origin,
        web.token,
        body.len()
    );
    assert_eq!(web.exchange(&plain).status, 415);

    let oversized = format!(
        "POST {path} HTTP/1.1\r\nHost: {}\r\nOrigin: {}\r\nX-Tearoom-Token: {}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",
        web.host,
        web.origin,
        web.token,
        1024 * 1024 + 1
    );
    assert_eq!(web.exchange(&oversized).status, 413);

    let mut padding = String::new();
    for index in 0..20 {
        padding.push_str(&format!("X-Pad-{index}: {}\r\n", "a".repeat(900)));
    }
    let oversized_headers = format!(
        "GET /api/state HTTP/1.1\r\nHost: {}\r\nX-Tearoom-Token: {}\r\n{padding}Connection: close\r\n\r\n",
        web.host,
        web.token
    );
    assert_eq!(web.exchange(&oversized_headers).status, 431);

    let response = web.exchange(&web.raw(
        "PUT",
        "/api/rooms/project/records",
        Some(&web.token),
        None,
        &[],
        "",
    ));
    assert_eq!(response.status, 405);

    let response = web.exchange(&web.get("/api/rooms/project/messages", Some(&web.token), &[]));
    assert_eq!(response.status, 405);

    let before = run(dir.path(), &["read", "--room", "project"]);
    assert_eq!(String::from_utf8(before.stdout).unwrap().lines().count(), 1);
}
