//! Domain objects shared by the room store and its callers.
use anyhow::{bail, Result};
use serde::{Deserialize, Serialize};
use serde_json::value::RawValue;
use sha2::{Digest, Sha256};

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq, Eq)]
pub struct Room {
    pub name: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub parent: String,
    pub created: String,
}

#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct Record {
    pub room: String,
    pub seq: u64,
    pub author: String,
    pub at: String,
    pub content: Box<RawValue>,
}

pub struct AppendRequest<'a> {
    pub content: &'a str,
    pub idempotency_key: &'a str,
}

/// Match Go's json.Compact without parsing and re-serializing string escapes or key order.
pub fn compact_json(raw: &str) -> Result<String> {
    if raw.is_empty() {
        bail!("content is required");
    }
    let _: Box<RawValue> = serde_json::from_str(raw)?;
    let mut out = String::with_capacity(raw.len());
    let mut quoted = false;
    let mut escaped = false;
    for ch in raw.chars() {
        if quoted {
            out.push(ch);
            if escaped {
                escaped = false;
            } else if ch == '\\' {
                escaped = true;
            } else if ch == '"' {
                quoted = false;
            }
        } else if ch == '"' {
            quoted = true;
            out.push(ch);
        } else if !matches!(ch, ' ' | '\t' | '\n' | '\r') {
            out.push(ch);
        }
    }
    Ok(out)
}

pub fn fingerprint(author: &str, compact: &str) -> String {
    let mut digest = Sha256::new();
    digest.update(author.as_bytes());
    digest.update([0]);
    digest.update(compact.as_bytes());
    hex::encode(digest.finalize())
}
