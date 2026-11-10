//! Stable, readable Herdr labels derived from Tearoom room and role identities.

use crate::collab::runtime::PanelStatus;
use sha2::{Digest, Sha256};
use std::path::Path;

pub(super) fn room_label(cwd: &Path, room: &str) -> String {
    let mut digest = Sha256::new();
    digest.update(cwd.to_string_lossy().as_bytes());
    digest.update([0]);
    digest.update(room.as_bytes());
    let slug: String = room
        .chars()
        .filter(|c| c.is_ascii_alphanumeric() || *c == '-')
        .take(8)
        .collect();
    let readable = if slug.is_empty() { "room" } else { &slug };
    format!("tr-{readable}-{}", hex::encode(&digest.finalize()[..8]))
}

pub(super) fn room_session(workspace: &Path, room: &str) -> String {
    let label = room_label(workspace, room);
    format!("tearoom-{}", label.rsplit('-').next().unwrap_or("room"))
}

pub(super) fn open_command(room: &str) -> String {
    format!("tearoom room open --room '{}'", room.replace('\'', "'\\''"))
}

pub(super) fn panel_label(role: &str, panel: &str) -> String {
    let mut digest = Sha256::new();
    digest.update(panel.as_bytes());
    let slug: String = role
        .chars()
        .filter(|c| c.is_ascii_alphanumeric() || matches!(c, '-' | '_'))
        .take(8)
        .map(|c| c.to_ascii_lowercase())
        .collect();
    let readable = if slug.is_empty() { "role" } else { &slug };
    format!("r-{readable}-{}", hex::encode(&digest.finalize()[..8]))
}

pub(super) fn herdr_state(status: PanelStatus) -> &'static str {
    match status {
        PanelStatus::Working => "working",
        PanelStatus::Blocked => "blocked",
        PanelStatus::Idle | PanelStatus::Done => "idle",
        PanelStatus::Missing | PanelStatus::Unknown => "unknown",
    }
}
