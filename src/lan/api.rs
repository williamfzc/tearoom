//! The LAN face's four routes and their JSON shapes. Responses mirror the
//! desktop app's command payloads (`rooms`, `records`, `record`) so the
//! phone UI consumes the same contracts as the desktop UI. Every non-pair
//! route requires the bearer token issued at pairing time; a token is bound
//! to one workspace root and sees nothing else.
use super::auth;
use super::server::Request;
use super::{SendArgs, Sender, Shared, TokenEntry, PAIR_MAX_ATTEMPTS};
use crate::store::Store;
use anyhow::Result;
use serde_json::{json, Value};
use std::path::Path;
use std::time::Instant;

pub(super) const CORS_HEADERS: &str = "access-control-allow-origin: *\r\n\
access-control-allow-headers: authorization, content-type\r\n\
access-control-allow-methods: get, post, options\r\n";

pub(super) struct Response {
    pub status: u16,
    pub body: Vec<u8>,
}

pub(super) fn error(status: u16, message: &str) -> Response {
    respond(status, &json!({"error": message}))
}

fn respond(status: u16, value: &Value) -> Response {
    Response {
        status,
        body: serde_json::to_vec(value).expect("serializable response"),
    }
}

fn empty(status: u16) -> Response {
    Response {
        status,
        body: Vec::new(),
    }
}

pub(super) fn route(shared: &Shared, sender: &Sender, request: &Request) -> Response {
    let (path, query) = match request.target.split_once('?') {
        Some((path, query)) => (path, query),
        None => (request.target.as_str(), ""),
    };
    if request.method == "OPTIONS" {
        return empty(204);
    }
    match (request.method.as_str(), path) {
        ("POST", "/v1/pair") => pair(shared, &request.body),
        ("GET", "/v1/rooms") | ("GET", "/v1/records") | ("POST", "/v1/send") => {
            let workspace = match authorize(shared, &request.authorization) {
                Ok(workspace) => workspace,
                Err(response) => return response,
            };
            match (request.method.as_str(), path) {
                ("GET", "/v1/rooms") => rooms(&workspace),
                ("GET", "/v1/records") => records(&workspace, query),
                ("POST", "/v1/send") => send(&workspace, sender, &request.body),
                _ => unreachable!("matched above"),
            }
        }
        (_, "/v1/pair" | "/v1/rooms" | "/v1/records" | "/v1/send") => {
            error(405, "method not allowed on this route")
        }
        _ => error(404, "unknown route"),
    }
}

/// Resolve the token to its workspace root; the token is the only authority.
fn authorize(
    shared: &Shared,
    authorization: &Option<String>,
) -> Result<std::path::PathBuf, Response> {
    let raw = authorization
        .as_deref()
        .and_then(|value| value.strip_prefix("Bearer "))
        .map(str::trim)
        .filter(|value| !value.is_empty());
    let Some(raw) = raw else {
        return Err(error(401, "pair first; requests need a bearer token"));
    };
    let hash = auth::token_hash(raw);
    let state = shared.lock().expect("lan state mutex");
    state
        .tokens
        .iter()
        .find(|entry| auth::ct_eq(&entry.hash, &hash))
        .map(|entry| entry.workspace.clone())
        .ok_or_else(|| error(401, "unknown or revoked token"))
}

/// Consume the pending pairing code and mint one bearer token. A wrong code
/// counts against the attempt cap; an expired or exhausted code forces a new
/// `collab pair` run on the Mac.
fn pair(shared: &Shared, body: &[u8]) -> Response {
    let request: Value = match serde_json::from_slice(body) {
        Ok(value) => value,
        Err(_) => return error(400, "request body must be JSON"),
    };
    let Some(code) = request["code"]
        .as_str()
        .map(str::trim)
        .filter(|code| !code.is_empty())
    else {
        return error(400, "pairing code is required");
    };
    let mut state = shared.lock().expect("lan state mutex");
    let Some(pending) = state.pending.take() else {
        return error(
            403,
            "no pairing is pending; run 'tearoom collab pair' on the Mac",
        );
    };
    if Instant::now() >= pending.expires {
        return error(
            403,
            "the pairing code expired; run 'tearoom collab pair' again",
        );
    }
    if pending.code != code {
        let attempts = pending.attempts + 1;
        if attempts >= PAIR_MAX_ATTEMPTS {
            return error(
                403,
                "too many rejected codes; run 'tearoom collab pair' again",
            );
        }
        state.pending = Some(super::PairCode {
            attempts,
            ..pending
        });
        return error(403, "pairing code rejected");
    }
    let Ok(raw) = auth::mint_token() else {
        return error(500, "cannot generate a pairing token");
    };
    state.tokens.push(TokenEntry {
        hash: auth::token_hash(&raw),
        workspace: pending.workspace.clone(),
    });
    if let Err(issue) = state.save() {
        return error(500, &format!("cannot persist the pairing: {issue:#}"));
    }
    respond(
        200,
        &json!({"token": raw, "workspace": workspace_name(&pending.workspace)}),
    )
}

fn rooms(workspace: &Path) -> Response {
    let store = match open_store(workspace) {
        Ok(store) => store,
        Err(response) => return response,
    };
    match store.rooms() {
        Ok(rooms) => respond(200, &json!({"rooms": rooms})),
        Err(issue) => error(500, &format!("{issue:#}")),
    }
}

fn records(workspace: &Path, query: &str) -> Response {
    let params = parse_query(query);
    let Some(room) = params
        .iter()
        .find(|(name, _)| name == "room")
        .map(|(_, value)| value.as_str())
        .filter(|room| !room.is_empty())
    else {
        return error(400, "room is required");
    };
    let after = params
        .iter()
        .find(|(name, _)| name == "after")
        .and_then(|(_, value)| value.trim().trim_start_matches('e').parse::<u64>().ok())
        .unwrap_or(0);
    let store = match open_store(workspace) {
        Ok(store) => store,
        Err(response) => return response,
    };
    let read = store
        .resolve_room(room)
        .and_then(|id| store.read(&id, after));
    match read {
        Ok(records) => respond(200, &json!({"records": records})),
        Err(issue) => store_error(issue),
    }
}

fn send(workspace: &Path, sender: &Sender, body: &[u8]) -> Response {
    let request: Value = match serde_json::from_slice(body) {
        Ok(value) => value,
        Err(_) => return error(400, "request body must be JSON"),
    };
    let field = |name: &str| {
        request[name]
            .as_str()
            .map(str::trim)
            .filter(|value| !value.is_empty())
    };
    let (Some(room), Some(text), Some(key)) = (field("room"), field("text"), field("key")) else {
        return error(400, "room, text, and key are required");
    };
    let args = SendArgs {
        workspace: workspace.to_path_buf(),
        room: room.to_owned(),
        text: text.to_owned(),
        to: request["to"]
            .as_array()
            .map(|values| {
                values
                    .iter()
                    .filter_map(|value| value.as_str().map(str::to_owned))
                    .collect()
            })
            .unwrap_or_default(),
        author: field("author").unwrap_or_default().to_owned(),
        key: key.to_owned(),
    };
    match sender(&args) {
        Ok(record) => respond(200, &json!({"record": record})),
        Err(issue) => error(400, &format!("{issue:#}")),
    }
}

fn open_store(workspace: &Path) -> Result<Store, Response> {
    let dir = workspace
        .parent()
        .ok_or_else(|| error(500, "workspace root has no parent directory"))?;
    Store::open(dir).map_err(|issue| error(500, &format!("{issue:#}")))
}

fn store_error(issue: anyhow::Error) -> Response {
    let message = format!("{issue:#}");
    if message.contains("not found") {
        error(404, &message)
    } else {
        error(400, &message)
    }
}

fn parse_query(query: &str) -> Vec<(String, String)> {
    query
        .split('&')
        .filter(|pair| !pair.is_empty())
        .map(|pair| match pair.split_once('=') {
            Some((name, value)) => (decode(name), decode(value)),
            None => (decode(pair), String::new()),
        })
        .collect()
}

/// Percent-decoding for query values, as the phone client's fetch produces.
fn decode(value: &str) -> String {
    let bytes = value.as_bytes();
    let mut out = Vec::with_capacity(bytes.len());
    let mut index = 0;
    while index < bytes.len() {
        match (bytes[index], bytes.get(index + 1), bytes.get(index + 2)) {
            (b'%', Some(first), Some(second))
                if first.is_ascii_hexdigit() && second.is_ascii_hexdigit() =>
            {
                let hex = [*first, *second];
                let hex = std::str::from_utf8(&hex).expect("ascii digits");
                out.push(u8::from_str_radix(hex, 16).expect("hex digits"));
                index += 3;
            }
            (byte, _, _) => {
                out.push(byte);
                index += 1;
            }
        }
    }
    String::from_utf8_lossy(&out).into_owned()
}

fn workspace_name(root: &Path) -> String {
    root.parent()
        .and_then(|parent| parent.file_name())
        .map(|name| name.to_string_lossy().into_owned())
        .unwrap_or_default()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn query_values_decode_percent_escapes() {
        let params = parse_query("room=%E8%8C%B6%E5%AE%A4&after=e12");
        assert_eq!(params[0].1, "茶室");
        assert_eq!(params[1].1, "e12");
    }

    #[test]
    fn query_without_pairs_is_empty() {
        assert!(parse_query("").is_empty());
    }
}
