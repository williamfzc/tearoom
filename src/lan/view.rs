//! The LAN face's wire projections: Room payloads shaped like the desktop
//! dashboard's (roles, last-record preview), so the phone UI consumes the
//! same JSON contracts as the desktop UI. The endpoint routing is in api.rs.
use crate::collab::Config;
use crate::core::Record;
use crate::store::Store;
use serde_json::{json, Value};

/// One Room as the dashboard renders it: labels, roster, and the last
/// record's preview for the rail.
pub(super) fn room_json(store: &Store, config: &Config, room: &crate::core::Room) -> Value {
    let roles = config
        .rooms
        .get(&room.id)
        .map(|binding| binding.roles.clone())
        .unwrap_or_default();
    let last = match store.read(&room.id, 0) {
        Ok(records) => records.last().map_or(Value::Null, last_json),
        Err(_) => Value::Null,
    };
    json!({
        "id": room.id,
        "name": room.name,
        "parent": room.parent,
        "description": room.description,
        "created": room.created,
        "roles": roles,
        "last": last,
    })
}

/// The plain preview text of one record: a message's text, a plain JSON
/// string, or a bracketed placeholder for any other content shape.
fn preview_text(content: &Value) -> String {
    match content {
        Value::String(text) => text.clone(),
        Value::Object(map) => match map.get("text") {
            Some(Value::String(text)) => text.clone(),
            _ => "[data]".into(),
        },
        _ => "[data]".into(),
    }
}

fn last_json(record: &Record) -> Value {
    let content: Value =
        serde_json::from_str(record.content.get()).unwrap_or(Value::String(String::new()));
    json!({
        "author": record.author,
        "text": preview_text(&content),
        "at": record.at,
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn previews_follow_the_message_shapes() {
        assert_eq!(preview_text(&json!("plain")), "plain");
        assert_eq!(
            preview_text(&json!({"text": "hello", "mentions": ["a"]})),
            "hello"
        );
        assert_eq!(preview_text(&json!({"custom": 1})), "[data]");
    }
}
