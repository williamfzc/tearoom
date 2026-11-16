//! Black-box probes for the LAN face: raw HTTP over a real socket against
//! an in-process `tearoom::lan` server, with the send path driven through
//! the real CLI binary. The matrix (auth, pair lifecycle, read boundaries,
//! idempotent sends, malformed and oversized requests) revives the probes
//! the removed loopback browser face kept in tests/cli_web.rs.

use std::io::{Read, Write};
use std::net::{SocketAddr, TcpStream};
use std::path::PathBuf;
use std::process::Command;
use std::sync::Arc;
use std::time::Duration;

use tearoom::lan::{self, LanState, SendArgs, Sender, Shared};
use tearoom::store::Store;

struct Face {
    addr: SocketAddr,
    shared: Shared,
    /// The canonical `.tearoom` root the face was paired against.
    root: PathBuf,
    _workspace: tempfile::TempDir,
    _state: tempfile::TempDir,
}

impl Face {
    fn workspace_dir(&self) -> PathBuf {
        self.root
            .parent()
            .expect("workspace root has a parent")
            .to_path_buf()
    }
}

/// One face per test: its own workspace, its own ephemeral port, its own
/// `lan.json` sandbox, and a sender that runs the real CLI binary.
fn face() -> Face {
    let workspace = tempfile::tempdir().unwrap();
    let store = Store::init(workspace.path()).unwrap();
    store.create_room("project", "").unwrap();
    let root = store.root().to_path_buf();
    let state = tempfile::tempdir().unwrap();
    let shared = LanState::new(state.path().join("lan.json"), true, 0);
    let sender = cli_sender();
    let server_state = shared.clone();
    std::thread::spawn(move || lan::serve(server_state, sender));
    let addr = wait_bound(&shared);
    Face {
        addr,
        shared,
        root,
        _workspace: workspace,
        _state: state,
    }
}

fn wait_bound(shared: &Shared) -> SocketAddr {
    for _ in 0..200 {
        if let Some(addr) = shared.lock().unwrap().bound {
            return addr;
        }
        std::thread::sleep(Duration::from_millis(25));
    }
    panic!("the LAN face never bound a port");
}

/// The send path under test is the production one — the CLI `send` command —
/// invoked the way `lan::cli_sender` invokes the installed binary, but with
/// the test binary's path and a scrubbed environment.
fn cli_sender() -> Sender {
    Arc::new(|args: &SendArgs| {
        let mut command = Command::new(env!("CARGO_BIN_EXE_tearoom"));
        command
            .current_dir(args.workspace.parent().expect("workspace has a parent"))
            .env("HC_SOCKET", "/nonexistent-tearoom-lan-test")
            .env(
                "TEAROOM_SERVICE_DIR",
                args.workspace.join("service-state-sandbox"),
            )
            .args([
                "send",
                "--room",
                &args.room,
                "--text",
                &args.text,
                "--idempotency-key",
                &args.key,
            ]);
        if !args.author.trim().is_empty() {
            command.args(["--as", &args.author]);
        }
        let output = command.output().expect("run the tearoom CLI");
        if !output.status.success() {
            let stderr = String::from_utf8_lossy(&output.stderr).into_owned();
            anyhow::bail!(
                "{}",
                stderr
                    .lines()
                    .rev()
                    .find(|l| !l.trim().is_empty())
                    .unwrap_or("send failed")
            );
        }
        let stdout = String::from_utf8_lossy(&output.stdout);
        stdout
            .lines()
            .rev()
            .find_map(|line| serde_json::from_str(line).ok())
            .ok_or_else(|| anyhow::anyhow!("the tearoom CLI did not return a record"))
    })
}

fn wire(method: &str, target: &str, token: Option<&str>, body: Option<&str>) -> String {
    let mut request = format!("{method} {target} HTTP/1.1\r\nhost: probe\r\nconnection: close\r\n");
    if let Some(token) = token {
        request.push_str(&format!("authorization: Bearer {token}\r\n"));
    }
    if let Some(body) = body {
        request.push_str(&format!(
            "content-type: application/json\r\ncontent-length: {}\r\n",
            body.len()
        ));
    }
    request.push_str("\r\n");
    if let Some(body) = body {
        request.push_str(body);
    }
    request
}

/// Send raw bytes and collect the status line, headers, and body. The write
/// is best-effort: the face may reject an oversized request and close before
/// the probe finishes sending it, and the refusal itself is the assertion.
fn probe(addr: SocketAddr, request: &str) -> (u16, String, String) {
    let mut stream = TcpStream::connect(addr).expect("connect to the face");
    stream
        .set_read_timeout(Some(Duration::from_secs(5)))
        .unwrap();
    let _ = stream.write_all(request.as_bytes());
    let _ = stream.flush();
    let mut response = Vec::new();
    stream
        .read_to_end(&mut response)
        .expect("read the response");
    let text = String::from_utf8_lossy(&response).into_owned();
    let (head, body) = text.split_once("\r\n\r\n").unwrap_or((&text, ""));
    let status: u16 = head
        .split_whitespace()
        .nth(1)
        .expect("status code")
        .parse()
        .expect("numeric status");
    (status, head.to_owned(), body.to_owned())
}

fn pair_token(face: &Face, code: &str) -> (u16, String) {
    let body = format!("{{\"code\":\"{code}\"}}");
    let (status, _, body) = probe(face.addr, &wire("POST", "/v1/pair", None, Some(&body)));
    let token = serde_json::from_str::<serde_json::Value>(&body)
        .ok()
        .and_then(|value| value["token"].as_str().map(str::to_owned))
        .unwrap_or_default();
    (status, token)
}

#[test]
fn pair_issues_a_single_use_token() {
    let face = face();
    let code = lan::mint_pairing(&face.shared, &face.root, 0).unwrap();
    let (status, _) = pair_token(&face, "000000");
    assert_eq!(status, 403);
    let (status, token) = pair_token(&face, &code);
    assert_eq!(status, 200);
    assert_eq!(token.len(), 64);
    let (status, _) = pair_token(&face, &code);
    assert_eq!(status, 403, "a consumed code must not pair twice");
}

#[test]
fn rejected_codes_capped_then_invalidated() {
    let face = face();
    let code = lan::mint_pairing(&face.shared, &face.root, 0).unwrap();
    for attempt in 0..lan::PAIR_MAX_ATTEMPTS {
        let (status, _) = pair_token(&face, &format!("9{attempt}9999"));
        assert_eq!(status, 403);
    }
    let (status, _) = pair_token(&face, &code);
    assert_eq!(status, 403, "the cap must invalidate even the real code");
}

#[test]
fn an_expired_pairing_code_is_refused() {
    let face = face();
    lan::mint_pairing(&face.shared, &face.root, 0).unwrap();
    face.shared.lock().unwrap().pending = Some(tearoom::lan::PairCode {
        code: "123456".into(),
        expires: std::time::Instant::now() - Duration::from_secs(1),
        workspace: face.root.clone(),
        attempts: 0,
    });
    let (status, _) = pair_token(&face, "123456");
    assert_eq!(status, 403);
}

#[test]
fn pairing_without_a_pending_code_is_refused() {
    let face = face();
    let (status, _) = pair_token(&face, "123456");
    assert_eq!(status, 403);
}

#[test]
fn routes_require_the_paired_token() {
    let face = face();
    let (status, _, _) = probe(face.addr, &wire("GET", "/v1/rooms", None, None));
    assert_eq!(status, 401);
    let wrong_token = "f".repeat(64);
    let (status, _, _) = probe(
        face.addr,
        &wire("GET", "/v1/rooms", Some(&wrong_token), None),
    );
    assert_eq!(status, 401);
    let code = lan::mint_pairing(&face.shared, &face.root, 0).unwrap();
    let (_, token) = pair_token(&face, &code);
    let (status, _, body) = probe(face.addr, &wire("GET", "/v1/rooms", Some(&token), None));
    assert_eq!(status, 200);
    let rooms: serde_json::Value = serde_json::from_str(&body).unwrap();
    assert_eq!(rooms["rooms"][0]["name"], "project");
}

#[test]
fn records_honor_the_after_boundary() {
    let face = face();
    let store = Store::open(face.workspace_dir()).unwrap();
    let id = store.resolve_room("project").unwrap();
    store
        .append(
            &id,
            "tester",
            tearoom::core::AppendRequest {
                content: "\"one\"",
                idempotency_key: "a",
            },
        )
        .unwrap();
    store
        .append(
            &id,
            "tester",
            tearoom::core::AppendRequest {
                content: "\"two\"",
                idempotency_key: "b",
            },
        )
        .unwrap();
    let code = lan::mint_pairing(&face.shared, &face.root, 0).unwrap();
    let (_, token) = pair_token(&face, &code);
    let (status, _, body) = probe(
        face.addr,
        &wire(
            "GET",
            "/v1/records?room=project&after=0",
            Some(&token),
            None,
        ),
    );
    assert_eq!(status, 200);
    let records: serde_json::Value = serde_json::from_str(&body).unwrap();
    assert_eq!(records["records"].as_array().unwrap().len(), 2);
    let (status, _, body) = probe(
        face.addr,
        &wire(
            "GET",
            "/v1/records?room=project&after=e1",
            Some(&token),
            None,
        ),
    );
    assert_eq!(status, 200);
    let records: serde_json::Value = serde_json::from_str(&body).unwrap();
    assert_eq!(records["records"][0]["seq"], 2);
    let (status, _, _) = probe(
        face.addr,
        &wire(
            "GET",
            "/v1/records?room=missing&after=0",
            Some(&token),
            None,
        ),
    );
    assert_eq!(status, 404);
}

#[test]
fn send_appends_and_retries_idempotently() {
    let face = face();
    let code = lan::mint_pairing(&face.shared, &face.root, 0).unwrap();
    let (_, token) = pair_token(&face, &code);
    let body = r#"{"room":"project","text":"hello from the phone","author":"phone","key":"k1"}"#;
    let (status, _, response) = probe(
        face.addr,
        &wire("POST", "/v1/send", Some(&token), Some(body)),
    );
    assert_eq!(status, 200);
    let record: serde_json::Value = serde_json::from_str(&response).unwrap();
    assert_eq!(record["record"]["seq"], 1);
    assert_eq!(record["record"]["author"], "phone");

    // The same key with the same content replays the original record.
    let (status, _, response) = probe(
        face.addr,
        &wire("POST", "/v1/send", Some(&token), Some(body)),
    );
    assert_eq!(status, 200);
    let replay: serde_json::Value = serde_json::from_str(&response).unwrap();
    assert_eq!(replay["record"]["seq"], 1);

    // The same key with different content is a hard conflict.
    let conflict = r#"{"room":"project","text":"different","author":"phone","key":"k1"}"#;
    let (status, _, _) = probe(
        face.addr,
        &wire("POST", "/v1/send", Some(&token), Some(conflict)),
    );
    assert_eq!(status, 400);

    // The record landed in the same stream every other writer reads.
    let store = Store::open(face.workspace_dir()).unwrap();
    let id = store.resolve_room("project").unwrap();
    let records = store.read(&id, 0).unwrap();
    assert_eq!(records.len(), 1);
    assert!(records[0].content.get().contains("hello from the phone"));
}

#[test]
fn a_token_only_sees_its_paired_workspace() {
    let workspace = tempfile::tempdir().unwrap();
    let first = Store::init(workspace.path().join("ws-a")).unwrap();
    first.create_room("alpha", "").unwrap();
    let second = Store::init(workspace.path().join("ws-b")).unwrap();
    second.create_room("beta", "").unwrap();
    let state = tempfile::tempdir().unwrap();
    let shared = LanState::new(state.path().join("lan.json"), true, 0);
    let sender = cli_sender();
    let server_state = shared.clone();
    std::thread::spawn(move || lan::serve(server_state, sender));
    let addr = wait_bound(&shared);
    let code_a = lan::mint_pairing(&shared, first.root(), 0).unwrap();
    let (_, token_a) = {
        let body = format!("{{\"code\":\"{code_a}\"}}");
        let (status, _, body) = probe(addr, &wire("POST", "/v1/pair", None, Some(&body)));
        assert_eq!(status, 200);
        let token: serde_json::Value = serde_json::from_str(&body).unwrap();
        (status, token["token"].as_str().unwrap().to_owned())
    };
    let (status, _, body) = probe(addr, &wire("GET", "/v1/rooms", Some(&token_a), None));
    assert_eq!(status, 200);
    assert!(body.contains("alpha"), "{body}");
    assert!(!body.contains("beta"), "{body}");
}

#[test]
fn malformed_requests_are_rejected_without_writes() {
    let face = face();
    let code = lan::mint_pairing(&face.shared, &face.root, 0).unwrap();
    let (_, token) = pair_token(&face, &code);
    let (status, _, _) = probe(
        face.addr,
        &wire("POST", "/v1/send", Some(&token), Some("not json")),
    );
    assert_eq!(status, 400);
    let (status, _, _) = probe(face.addr, &wire("POST", "/v1/pair", None, Some("not json")));
    assert_eq!(status, 400);
    let (status, _, _) = probe(
        face.addr,
        &wire(
            "POST",
            "/v1/send",
            Some(&token),
            Some(r#"{"room":"project","text":"x"}"#),
        ),
    );
    assert_eq!(status, 400, "a send without an idempotency key is refused");
    let (status, _, _) = probe(face.addr, &wire("GET", "/v1/nope", Some(&token), None));
    assert_eq!(status, 404);
    let (status, _, _) = probe(face.addr, &wire("DELETE", "/v1/rooms", Some(&token), None));
    assert_eq!(status, 405);
    let store = Store::open(face.workspace_dir()).unwrap();
    let id = store.resolve_room("project").unwrap();
    assert!(
        store.read(&id, 0).unwrap().is_empty(),
        "rejected requests wrote nothing"
    );
}

#[test]
fn oversized_requests_are_refused() {
    let face = face();
    let oversized = format!("\"{}\"", "x".repeat(lan::MAX_BODY));
    let request = wire(
        "POST",
        "/v1/pair",
        None,
        Some(&format!("{{\"code\":{oversized}}}")),
    );
    let (status, _, _) = probe(face.addr, &request);
    assert_eq!(status, 413);
    let huge_header = format!(
        "GET /v1/rooms HTTP/1.1\r\nx-bulk: {}\r\n\r\n",
        "y".repeat(lan::MAX_HEADERS + 1)
    );
    let (status, _, _) = probe(face.addr, &huge_header);
    assert_eq!(status, 431);
}

#[test]
fn preflight_is_answered_with_cors() {
    let face = face();
    let (status, head, _) = probe(
        face.addr,
        "OPTIONS /v1/records HTTP/1.1\r\nhost: probe\r\n\r\n",
    );
    assert_eq!(status, 204);
    assert!(
        head.to_lowercase()
            .contains("access-control-allow-origin: *"),
        "{head}"
    );
    assert!(
        head.to_lowercase()
            .contains("access-control-allow-headers: authorization"),
        "{head}"
    );
}
