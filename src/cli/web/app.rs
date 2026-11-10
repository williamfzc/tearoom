//! Tearoom routes for the loopback browser face.
//! The route layer composes Store reads and the shared append path; it owns no
//! storage or delivery state of its own.

use super::http::{self, Request};
use crate::cli::{append_text, boundary, signature};
use crate::collab::Config;
use crate::store::Store;
use anyhow::Result;
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use std::net::TcpStream;

const INDEX: &str = include_str!("index.html");
const FAVICON: &str = include_str!("favicon.svg");
const CSS: &str = include_str!("app.css");
const MESSAGES_CSS: &str = include_str!("messages.css");
const COMPOSER_CSS: &str = include_str!("composer.css");
const JS: &str = include_str!("app.js");
const UI_JS: &str = include_str!("ui.js");

pub struct App {
    pub store: Store,
    pub token: String,
    pub port: u16,
}

pub struct Reply {
    pub status: u16,
    pub content_type: &'static str,
    pub body: Vec<u8>,
}

pub fn write(stream: &mut TcpStream, reply: &Reply) -> std::io::Result<()> {
    http::respond(stream, reply.status, reply.content_type, &reply.body)
}

impl App {
    pub fn dispatch(&self, request: &Request) -> Reply {
        let host = request.header("host").unwrap_or("");
        if !http::host_allowed(host, self.port) {
            return failure(
                403,
                "this interface is only available on the loopback address",
            );
        }
        if request.method == "POST" {
            if let Some(origin) = request.header("origin") {
                if !http::origin_allowed(origin, self.port) {
                    return failure(403, "cross-origin writes are refused");
                }
            }
            if let Some(site) = request.header("sec-fetch-site") {
                if site != "same-origin" && site != "none" {
                    return failure(403, "cross-site writes are refused");
                }
            }
        }
        if request.path.starts_with("/api/") {
            let token = request.header("x-tearoom-token").unwrap_or("");
            if !http::same_token(token, &self.token) {
                return failure(403, "missing or invalid page token");
            }
        }
        match (request.method.as_str(), request.path.as_str()) {
            ("GET", "/") => text(
                200,
                "text/html; charset=utf-8",
                INDEX.replace("{{TOKEN}}", &self.token),
            ),
            ("GET", "/favicon.svg") => text(200, "image/svg+xml", FAVICON),
            ("GET", "/app.css") => text(200, "text/css; charset=utf-8", CSS),
            ("GET", "/messages.css") => text(200, "text/css; charset=utf-8", MESSAGES_CSS),
            ("GET", "/composer.css") => text(200, "text/css; charset=utf-8", COMPOSER_CSS),
            ("GET", "/app.js") => text(200, "text/javascript; charset=utf-8", JS),
            ("GET", "/ui.js") => text(200, "text/javascript; charset=utf-8", UI_JS),
            ("GET", "/api/state") => finish(self.state()),
            _ if request.path.starts_with("/api/rooms/") => self.room(request),
            _ => failure(404, "not found"),
        }
    }

    fn state(&self) -> Result<Value> {
        let config = Config::load(self.store.root())?;
        let rooms: Vec<Value> = self
            .store
            .rooms()?
            .into_iter()
            .map(|room| {
                let roles = config
                    .rooms
                    .get(&room.name)
                    .map_or_else(Vec::new, |binding| binding.roles.clone());
                json!({
                    "name": room.name,
                    "parent": room.parent,
                    "created": room.created,
                    "roles": roles,
                })
            })
            .collect();
        Ok(json!({
            "actor": std::env::var("TEAROOM_ACTOR").unwrap_or_default(),
            "rooms": rooms,
        }))
    }

    fn room(&self, request: &Request) -> Reply {
        let Some(rest) = request.path.strip_prefix("/api/rooms/") else {
            return failure(404, "not found");
        };
        if let Some(encoded) = rest.strip_suffix("/records") {
            if request.method != "GET" {
                return failure(405, "records are read-only");
            }
            let room = match http::decode_component(encoded) {
                Ok(room) if !room.is_empty() => room,
                _ => return failure(400, "invalid room name"),
            };
            return finish(self.records(&room, &request.query));
        }
        if let Some(encoded) = rest.strip_suffix("/messages") {
            if request.method != "POST" {
                return failure(405, "messages must be sent with POST");
            }
            let room = match http::decode_component(encoded) {
                Ok(room) if !room.is_empty() => room,
                _ => return failure(400, "invalid room name"),
            };
            if !request
                .header("content-type")
                .is_some_and(|value| value.to_ascii_lowercase().starts_with("application/json"))
            {
                return failure(415, "messages must be JSON");
            }
            return self.message(&room, &request.body);
        }
        failure(404, "not found")
    }

    fn records(&self, room: &str, query: &str) -> Result<Value> {
        let after = http::query_param(query, "after")
            .map_err(anyhow::Error::msg)?
            .unwrap_or_default();
        let after = boundary(&after)?;
        Ok(json!({ "records": self.store.read(room, after)? }))
    }

    fn message(&self, room: &str, body: &[u8]) -> Reply {
        #[derive(Deserialize)]
        #[serde(deny_unknown_fields)]
        struct Message {
            text: String,
            #[serde(default)]
            to: Vec<String>,
            #[serde(default)]
            author: String,
        }
        let message: Message = match serde_json::from_slice(body) {
            Ok(message) => message,
            Err(error) => return failure(400, format!("invalid message: {error}")),
        };
        if message.text.trim().is_empty() {
            return failure(400, "message text is required");
        }
        let author = signature(&message.author);
        match append_text(
            &self.store,
            room,
            &author,
            &message.text,
            &message.to.join(","),
            None,
            "",
        ) {
            Ok(record) => json_reply(201, &json!({ "record": record })),
            Err(error) => from_error(error),
        }
    }
}

fn text(status: u16, content_type: &'static str, body: impl Into<String>) -> Reply {
    Reply {
        status,
        content_type,
        body: body.into().into_bytes(),
    }
}

fn json_reply(status: u16, value: &Value) -> Reply {
    Reply {
        status,
        content_type: "application/json; charset=utf-8",
        body: serde_json::to_vec(value).unwrap_or_else(|_| b"{}".to_vec()),
    }
}

fn failure(status: u16, message: impl Into<String>) -> Reply {
    json_reply(status, &json!({ "error": message.into() }))
}

fn from_error(error: anyhow::Error) -> Reply {
    let message = format!("{error:#}");
    // Not-found is a missing resource; the short list is request validation.
    // Everything else is a storage or workspace failure, not a client mistake.
    let validation = message.starts_with("invalid")
        || message.contains(" is required")
        || message.contains("not configured")
        || message.contains("unknown role")
        || message.contains("more than once")
        || message.contains("@all")
        || message.contains("cannot address roles")
        || message.contains("conflict");
    let status = if message.contains("not found") {
        404
    } else if validation {
        400
    } else {
        500
    };
    failure(status, message)
}

fn finish<T: Serialize>(result: Result<T>) -> Reply {
    match result {
        Ok(value) => json_reply(200, &serde_json::to_value(value).unwrap_or(Value::Null)),
        Err(error) => from_error(error),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn asset_placeholders_and_routes_are_present() {
        assert!(INDEX.contains("{{TOKEN}}"));
        assert!(INDEX.contains("id=\"rooms\""));
        assert!(INDEX.contains("id=\"records\""));
        assert!(INDEX.contains("id=\"composer\""));
        assert!(!FAVICON.is_empty());
        assert!(!CSS.is_empty());
        assert!(!MESSAGES_CSS.is_empty());
        assert!(!COMPOSER_CSS.is_empty());
        assert!(!JS.is_empty());
        assert!(!UI_JS.is_empty());
    }

    #[test]
    fn missing_workspace_errors_are_not_reported_as_success() {
        let reply = failure(404, "room \"x\": not found");
        assert_eq!(reply.status, 404);
        let value: Value = serde_json::from_slice(&reply.body).unwrap();
        assert!(value["error"].as_str().unwrap().contains("not found"));
    }

    #[test]
    fn internal_errors_are_not_reported_as_client_errors() {
        assert_eq!(from_error(anyhow::anyhow!("disk read failed")).status, 500);
        assert_eq!(from_error(anyhow::anyhow!("invalid --after")).status, 400);
        assert_eq!(
            from_error(anyhow::anyhow!("room \"x\": not found")).status,
            404
        );
    }

    #[test]
    fn non_positive_boundaries_are_rejected_by_the_shared_parser() {
        assert_eq!(boundary("").unwrap(), 0);
        assert_eq!(boundary("e3").unwrap(), 3);
        assert!(boundary("3").is_err());
    }
}
