//! Local HTTP transport for the Room store; hc integration lives elsewhere.
use crate::core::{AppendRequest, Record, Room};
use crate::store::Store;
use anyhow::{anyhow, bail, Context, Result};
use serde::Deserialize;
use serde_json::{json, value::RawValue};
use std::net::{IpAddr, SocketAddr, ToSocketAddrs};
use std::time::Duration;
use tiny_http::{Header, Method, Request, Response, Server as HttpServer, StatusCode};
use url::Url;

/// Restrict an unauthenticated local server to a literal loopback address or localhost.
pub fn require_loopback(address: &str) -> Result<()> {
    let (host, _) = address
        .rsplit_once(':')
        .context("listen address needs a port")?;
    let host = host.trim_start_matches('[').trim_end_matches(']');
    if host.eq_ignore_ascii_case("localhost")
        || host.parse::<IpAddr>().is_ok_and(|ip| ip.is_loopback())
    {
        Ok(())
    } else {
        bail!("refusing listen address {address:?}: the local profile serves loopback only")
    }
}

/// HTTP is allowed only on loopback; HTTPS may target a remote trusted endpoint.
pub fn require_safe_url(raw: &str) -> Result<Url> {
    let url = Url::parse(raw).context("TEAROOM_SERVER must be an http(s) URL")?;
    if !matches!(url.scheme(), "http" | "https") {
        bail!("TEAROOM_SERVER must be an http(s) URL");
    }
    let authority = raw
        .split_once("://")
        .map_or("", |(_, tail)| tail.split('/').next().unwrap_or(""));
    if authority.contains('@')
        || !url.username().is_empty()
        || url.password().is_some()
        || url.query().is_some()
        || url.fragment().is_some()
        || url.path() != "/"
    {
        bail!("TEAROOM_SERVER must be a bare server URL");
    }
    let host = url.host_str().context("TEAROOM_SERVER must have a host")?;
    let clean_host = host.trim_start_matches('[').trim_end_matches(']');
    if url.scheme() == "http"
        && !clean_host.eq_ignore_ascii_case("localhost")
        && !clean_host
            .parse::<IpAddr>()
            .is_ok_and(|ip| ip.is_loopback())
    {
        bail!("plaintext http targets loopback only, got {host:?}");
    }
    Ok(url)
}

/// A local server owns one Store and no authentication or routing state.
pub struct Server {
    http: HttpServer,
    store: Store,
}
impl Server {
    pub fn bind(store: Store, address: &str) -> Result<Self> {
        require_loopback(address)?;
        let addrs = address.to_socket_addrs()?.collect::<Vec<SocketAddr>>();
        if addrs.is_empty() || addrs.iter().any(|a| !a.ip().is_loopback()) {
            bail!("listen address does not resolve exclusively to loopback");
        }
        let http = HttpServer::http(address).map_err(|e| anyhow!("bind server: {e}"))?;
        Ok(Self { http, store })
    }
    pub fn address(&self) -> String {
        self.http.server_addr().to_string()
    }
    pub fn serve(&self) -> Result<()> {
        for request in self.http.incoming_requests() {
            self.handle(request);
        }
        Ok(())
    }
    fn handle(&self, mut req: Request) {
        let result = self.route(&mut req);
        let (status, body) = match result {
            Ok(value) => (200, value),
            Err((status, code, msg)) => (
                status,
                json!({"error":{"code":code,"message":msg}}).to_string(),
            ),
        };
        let response = Response::from_string(format!("{}\n", body))
            .with_status_code(StatusCode(status))
            .with_header(
                Header::from_bytes(&b"Content-Type"[..], &b"application/json"[..]).unwrap(),
            );
        let _ = req.respond(response);
    }
    fn route(&self, req: &mut Request) -> std::result::Result<String, (u16, &'static str, String)> {
        let url = Url::parse(&format!("http://localhost{}", req.url())).map_err(bad)?;
        let path = url.path();
        match (req.method(), path) {
            (&Method::Get, "/api/rooms") => encode(self.store.rooms()),
            (&Method::Post, "/api/rooms") => {
                #[derive(Deserialize)]
                #[serde(deny_unknown_fields)]
                struct Body {
                    #[serde(default)]
                    name: String,
                    #[serde(default)]
                    parent: String,
                }
                let body: Body = decode(req)?;
                encode(self.store.create_room(&body.name, &body.parent))
            }
            (&Method::Get, "/api/events") => {
                let mut room = String::new();
                let mut after = String::new();
                for (key, value) in url.query_pairs() {
                    if key == "room" && room.is_empty() {
                        room = value.into_owned();
                    } else if key == "after" && after.is_empty() {
                        after = value.into_owned();
                    }
                }
                let room = room.trim();
                if room.is_empty() {
                    return Err((400, "invalid", "room is a required query parameter".into()));
                }
                let n = if after.trim().is_empty() {
                    0
                } else {
                    after.parse::<u64>().map_err(|_| {
                        (
                            400,
                            "invalid",
                            "after must be a non-negative integer".into(),
                        )
                    })?
                };
                encode(self.store.read(room, n))
            }
            (&Method::Post, "/api/events") => {
                #[derive(Deserialize)]
                #[serde(deny_unknown_fields)]
                struct Body {
                    #[serde(default)]
                    room: String,
                    content: Box<RawValue>,
                }
                let author = header(req, "X-Tearoom-Author");
                let key = header(req, "Idempotency-Key");
                let body: Body = decode(req)?;
                encode(self.store.append(
                    &body.room,
                    author.trim(),
                    AppendRequest {
                        content: body.content.get(),
                        idempotency_key: key.trim(),
                    },
                ))
            }
            (_, "/api/rooms" | "/api/events") => Err((
                405,
                "method_not_allowed",
                format!("method not allowed: {}", req.method()),
            )),
            _ => Err((404, "not_found", "not found".into())),
        }
    }
}
fn header(req: &Request, name: &str) -> String {
    req.headers()
        .iter()
        .find(|h| h.field.as_str().as_str().eq_ignore_ascii_case(name))
        .map(|h| h.value.as_str().to_string())
        .unwrap_or_default()
}
fn bad(e: impl std::fmt::Display) -> (u16, &'static str, String) {
    (400, "invalid", e.to_string())
}
fn decode<T: serde::de::DeserializeOwned>(
    req: &mut Request,
) -> std::result::Result<T, (u16, &'static str, String)> {
    let mut body = String::new();
    req.as_reader().read_to_string(&mut body).map_err(bad)?;
    serde_json::from_str(&body).map_err(bad)
}
fn encode<T: serde::Serialize>(
    value: Result<T>,
) -> std::result::Result<String, (u16, &'static str, String)> {
    match value {
        Ok(value) => serde_json::to_string(&value).map_err(bad),
        Err(err) => {
            let msg = format!("{err:#}");
            if msg.contains("not found") {
                Err((404, "not_found", msg))
            } else if msg.contains("idempotency key") && msg.contains("conflict") {
                Err((409, "conflict", msg))
            } else {
                Err((400, "invalid", msg))
            }
        }
    }
}

/// Client for the same local wire contract, with a bounded request timeout.
pub struct Client {
    base: Url,
    agent: ureq::Agent,
}
impl Client {
    pub fn new(base: &str) -> Result<Self> {
        Ok(Self {
            base: require_safe_url(base)?,
            agent: ureq::AgentBuilder::new()
                .timeout(Duration::from_secs(30))
                .build(),
        })
    }
    fn request<T: serde::de::DeserializeOwned>(
        &self,
        method: &str,
        path: &str,
        body: Option<&str>,
        headers: &[(&str, &str)],
    ) -> Result<T> {
        let url = self.base.join(path)?;
        let mut req = self.agent.request(method, url.as_str());
        for &(name, value) in headers {
            if !value.is_empty() {
                req = req.set(name, value);
            }
        }
        let response = match body {
            Some(body) => req
                .set("Content-Type", "application/json")
                .send_string(body),
            None => req.call(),
        };
        let response = match response {
            Ok(r) => r,
            Err(ureq::Error::Status(status, r)) => {
                let value: serde_json::Value = r.into_json().unwrap_or_default();
                let message = value
                    .pointer("/error/message")
                    .and_then(|v| v.as_str())
                    .unwrap_or("server returned an error");
                bail!("server returned {status}: {message}");
            }
            Err(e) => bail!("tearoom server unreachable: {e}"),
        };
        Ok(response.into_json()?)
    }
    pub fn rooms(&self) -> Result<Vec<Room>> {
        self.request("GET", "/api/rooms", None, &[])
    }
    pub fn create_room(&self, name: &str, parent: &str) -> Result<Room> {
        self.request(
            "POST",
            "/api/rooms",
            Some(&json!({"name":name,"parent":parent}).to_string()),
            &[],
        )
    }
    pub fn read(&self, room: &str, after: u64) -> Result<Vec<Record>> {
        let query = url::form_urlencoded::Serializer::new(String::new())
            .append_pair("room", room)
            .append_pair("after", &after.to_string())
            .finish();
        self.request("GET", &format!("/api/events?{query}"), None, &[])
    }
    pub fn append(&self, room: &str, author: &str, request: AppendRequest<'_>) -> Result<Record> {
        let content: Box<RawValue> = serde_json::from_str(request.content)?;
        let body = format!(
            "{{\"room\":{},\"content\":{}}}",
            serde_json::to_string(room)?,
            content.get()
        );
        self.request(
            "POST",
            "/api/events",
            Some(&body),
            &[
                ("X-Tearoom-Author", author),
                ("Idempotency-Key", request.idempotency_key),
            ],
        )
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::thread;

    #[test]
    fn loopback_and_server_url_boundaries() {
        assert!(require_loopback("127.0.0.1:0").is_ok());
        assert!(require_loopback("[::1]:0").is_ok());
        assert!(require_loopback("0.0.0.0:80").is_err());
        assert!(require_safe_url("http://127.0.0.1:4").is_ok());
        assert!(require_safe_url("http://localhost:4").is_ok());
        assert!(require_safe_url("http://192.0.2.1:4").is_err());
        assert!(require_safe_url("http://127.0.0.1:4/path").is_err());
        assert!(require_safe_url("http://@127.0.0.1:4").is_err());
        assert!(require_safe_url("https://example.org").is_ok());
    }

    #[test]
    fn wire_round_trip_strict_errors_and_idempotency() {
        let temp = tempfile::tempdir().unwrap();
        let store = Store::init(temp.path()).unwrap();
        let server = Server::bind(store, "127.0.0.1:0").unwrap();
        let base = format!("http://{}", server.address());
        let handle = thread::spawn(move || {
            for _ in 0..8 {
                server.handle(server.http.recv().unwrap());
            }
        });
        let client = Client::new(&base).unwrap();
        client.create_room("root", "").unwrap();
        let first = client
            .append(
                "root",
                "actor",
                AppendRequest {
                    content: "{\"z\": 1, \"a\": \"<\"}",
                    idempotency_key: "key",
                },
            )
            .unwrap();
        let second = client
            .append(
                "root",
                "actor",
                AppendRequest {
                    content: "{\"z\":1,\"a\":\"<\"}",
                    idempotency_key: "key",
                },
            )
            .unwrap();
        assert_eq!(first.seq, second.seq);
        assert_eq!(first.content.get(), "{\"z\":1,\"a\":\"<\"}");
        let conflict = client
            .append(
                "root",
                "actor",
                AppendRequest {
                    content: "2",
                    idempotency_key: "key",
                },
            )
            .unwrap_err();
        assert!(conflict.to_string().contains("409"));
        assert_eq!(client.read("root", 0).unwrap().len(), 1);
        assert!(client
            .read("missing", 0)
            .unwrap_err()
            .to_string()
            .contains("404"));
        let response = ureq::post(&format!("{base}/api/events"))
            .send_string(r#"{"room":"root","content":1,"actor":"x"}"#)
            .unwrap_err();
        assert!(matches!(response, ureq::Error::Status(400, _)));
        let response = ureq::post(&format!("{base}/api/events"))
            .send_string(r#"{"room":"root","content":1} {}"#)
            .unwrap_err();
        assert!(matches!(response, ureq::Error::Status(400, _)));
        handle.join().unwrap();
    }

    #[test]
    #[ignore = "requires a built Go baseline in TEAROOM_GO_BIN"]
    fn go_cli_can_use_rust_server() {
        let go = std::env::var("TEAROOM_GO_BIN").expect("set TEAROOM_GO_BIN");
        let temp = tempfile::tempdir().unwrap();
        let store = Store::init(temp.path()).unwrap();
        store.create_room("root", "").unwrap();
        let server = Server::bind(store, "127.0.0.1:0").unwrap();
        let base = format!("http://{}", server.address());
        let handle = thread::spawn(move || {
            for _ in 0..2 {
                server.handle(server.http.recv().unwrap());
            }
        });
        for args in [
            vec!["send", "--room", "root", "--as", "a", "--text", "hello"],
            vec!["read", "--room", "root"],
        ] {
            let output = std::process::Command::new(&go)
                .current_dir(temp.path())
                .env("TEAROOM_SERVER", &base)
                .args(&args)
                .output()
                .unwrap();
            assert!(
                output.status.success(),
                "{args:?}: {}",
                String::from_utf8_lossy(&output.stderr)
            );
            if args[0] == "read" {
                assert!(String::from_utf8_lossy(&output.stdout).contains("hello"));
            }
        }
        handle.join().unwrap();
    }

    #[test]
    #[ignore = "requires a built Go baseline in TEAROOM_GO_BIN"]
    fn rust_client_can_use_go_server() {
        let go = std::env::var("TEAROOM_GO_BIN").expect("set TEAROOM_GO_BIN");
        let temp = tempfile::tempdir().unwrap();
        let store = Store::init(temp.path()).unwrap();
        store.create_room("root", "").unwrap();
        let port = std::net::TcpListener::bind("127.0.0.1:0")
            .unwrap()
            .local_addr()
            .unwrap()
            .port();
        struct ChildGuard(std::process::Child);
        impl Drop for ChildGuard {
            fn drop(&mut self) {
                let _ = self.0.kill();
                let _ = self.0.wait();
            }
        }
        let child = std::process::Command::new(go)
            .current_dir(temp.path())
            .args(["server", "start", "--address", &format!("127.0.0.1:{port}")])
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .spawn()
            .unwrap();
        let _guard = ChildGuard(child);
        let client = Client::new(&format!("http://127.0.0.1:{port}")).unwrap();
        let mut ready = false;
        for _ in 0..100 {
            if client.rooms().is_ok() {
                ready = true;
                break;
            }
            thread::sleep(Duration::from_millis(50));
        }
        assert!(ready, "Go server failed to start");
        let record = client
            .append(
                "root",
                "a",
                AppendRequest {
                    content: "{\"z\":1,\"a\":\"<\"}",
                    idempotency_key: "k",
                },
            )
            .unwrap();
        assert_eq!(record.seq, 1);
        assert_eq!(client.read("root", 0).unwrap().len(), 1);
        assert_eq!(
            client
                .append(
                    "root",
                    "a",
                    AppendRequest {
                        content: "{\"z\":1,\"a\":\"<\"}",
                        idempotency_key: "k"
                    }
                )
                .unwrap()
                .seq,
            1
        );
    }
}
