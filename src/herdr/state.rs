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

/// Re-points a binding at the Room's own session, preserving the former
/// mapping once and refusing to lose two sessions of evidence.
pub(super) fn migrate_binding(binding: &mut RoomBinding, session: &str) -> Result<()> {
    if binding.session == session {
        return Ok(());
    }
    if binding.previous.is_some()
        && (!binding.workspace_id.is_empty()
            || !binding.root_pane_id.is_empty()
            || !binding.panes.is_empty()
            || binding.pending_split.is_some())
    {
        bail!("Herdr binding already preserves an earlier session and active session {:?} still has data; refusing to overwrite either mapping", binding.session);
    }
    if binding.previous.is_none() && !binding.session.is_empty() {
        binding.previous = Some(LegacyBinding {
            session: binding.session.clone(),
            label: binding.label.clone(),
            workspace_id: binding.workspace_id.clone(),
            root_pane_id: binding.root_pane_id.clone(),
            panes: binding.panes.clone(),
            pending_split: binding.pending_split.clone(),
        });
    } else if binding.previous.is_none() && !binding.workspace_id.is_empty() {
        binding.previous = Some(LegacyBinding {
            session: "default".into(),
            label: binding.label.clone(),
            workspace_id: binding.workspace_id.clone(),
            root_pane_id: binding.root_pane_id.clone(),
            panes: binding.panes.clone(),
            pending_split: binding.pending_split.clone(),
        });
    }
    binding.session = session.into();
    binding.workspace_id.clear();
    binding.root_pane_id.clear();
    binding.panes.clear();
    binding.pending_split = None;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn migration_keeps_old_session_mapping_and_pending_pane_evidence() {
        let mut binding = RoomBinding {
            workspace_id: "w-old".into(),
            root_pane_id: "w-old:p1".into(),
            pending_split: Some(PendingSplit {
                panel: "old-panel".into(),
                before_panes: vec!["w-old:p1".into()],
            }),
            ..Default::default()
        };
        migrate_binding(&mut binding, "tearoom-new").unwrap();
        assert_eq!(binding.session, "tearoom-new");
        assert!(binding.workspace_id.is_empty());
        let old = binding.previous.as_ref().unwrap();
        assert_eq!(old.session, "default");
        assert_eq!(old.workspace_id, "w-old");
        assert_eq!(old.pending_split.as_ref().unwrap().panel, "old-panel");
        migrate_binding(&mut binding, "tearoom-new").unwrap();
        assert_eq!(binding.previous.as_ref().unwrap().workspace_id, "w-old");
    }

    #[test]
    fn migration_refuses_to_replace_a_second_preserved_session() {
        let mut binding = RoomBinding {
            session: "tearoom-current".into(),
            workspace_id: "w-current".into(),
            previous: Some(LegacyBinding {
                session: "default".into(),
                workspace_id: "w-old".into(),
                ..Default::default()
            }),
            ..Default::default()
        };
        assert!(migrate_binding(&mut binding, "tearoom-new").is_err());
        assert_eq!(binding.session, "tearoom-current");
        assert_eq!(binding.workspace_id, "w-current");
        assert_eq!(binding.previous.as_ref().unwrap().workspace_id, "w-old");
    }
}
