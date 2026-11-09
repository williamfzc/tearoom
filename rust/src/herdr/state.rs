//! Locked local bindings between Tearoom Rooms and Herdr workspaces/panes.

use anyhow::{bail, Context, Result};
use fs2::FileExt;
use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;
use std::fs::{self, OpenOptions};
use std::path::Path;

const STATE: &str = "herdr.json";
const LOCK: &str = "herdr.lock";

#[derive(Clone, Debug, Default, Deserialize, Serialize)]
#[serde(default)]
pub(super) struct State {
    version: u32,
    pub(super) rooms: BTreeMap<String, RoomBinding>,
}

#[derive(Clone, Debug, Default, Deserialize, Serialize)]
#[serde(default)]
pub(super) struct RoomBinding {
    pub(super) session: String,
    pub(super) previous: Option<LegacyBinding>,
    pub(super) label: String,
    pub(super) workspace_id: String,
    pub(super) root_pane_id: String,
    pub(super) panes: BTreeMap<String, PaneBinding>,
    pub(super) pending_split: Option<PendingSplit>,
}

/// Preserves IDs from the former shared default session during the one-time split.
#[derive(Clone, Debug, Default, Deserialize, Serialize)]
#[serde(default)]
pub(super) struct LegacyBinding {
    pub(super) session: String,
    pub(super) label: String,
    pub(super) workspace_id: String,
    pub(super) root_pane_id: String,
    pub(super) panes: BTreeMap<String, PaneBinding>,
    pub(super) pending_split: Option<PendingSplit>,
}

#[derive(Clone, Debug, Default, Deserialize, Serialize)]
#[serde(default)]
pub(super) struct PendingSplit {
    pub(super) panel: String,
    pub(super) before_panes: Vec<String>,
}

#[derive(Clone, Debug, Default, Deserialize, Serialize)]
#[serde(default)]
pub(super) struct PaneBinding {
    pub(super) pane_id: String,
    pub(super) attached: bool,
    pub(super) state: String,
    pub(super) terminal_id: String,
    pub(super) checked_at: u64,
}

pub(super) fn update<T>(root: &Path, change: impl FnOnce(&mut State) -> Result<T>) -> Result<T> {
    let lock = OpenOptions::new()
        .create(true)
        .truncate(false)
        .read(true)
        .write(true)
        .open(root.join(LOCK))?;
    lock.lock_exclusive()?;
    let mut state = match fs::read(root.join(STATE)) {
        Ok(bytes) => serde_json::from_slice(&bytes).context("read Herdr binding state")?,
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => State {
            version: 1,
            ..Default::default()
        },
        Err(error) => return Err(error.into()),
    };
    if state.version != 1 {
        bail!("unsupported Herdr binding state version {}", state.version);
    }
    let result = change(&mut state)?;
    save(root, &state)?;
    Ok(result)
}

pub(super) fn save(root: &Path, state: &State) -> Result<()> {
    let bytes = serde_json::to_vec_pretty(state)?;
    let mut temp = tempfile::NamedTempFile::new_in(root)?;
    use std::io::Write;
    temp.write_all(&bytes)?;
    temp.as_file().sync_all()?;
    temp.persist(root.join(STATE))
        .map_err(|error| error.error)?;
    Ok(())
}
