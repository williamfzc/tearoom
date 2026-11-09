//! Optional Herdr workspace and pane integration at the operating-system boundary.
//! Room data remains usable when the separate Herdr service is unavailable.

use crate::collab::runtime::PanelStatus;
use anyhow::{bail, Context, Result};
use serde_json::{json, Value};
use std::fs;
use std::path::Path;
use std::time::{SystemTime, UNIX_EPOCH};

mod cli;
mod labels;
mod state;
use labels::{herdr_state, open_command, panel_label, room_label, room_session};

pub fn ensure_room(root: &Path, room: &str) -> Result<Value> {
    let workspace = fs::canonicalize(root.parent().context("Tearoom workspace has no parent")?)?;
    let label = room_label(&workspace, room);
    let session = room_session(&workspace, room);
    state::update(root, |state| {
        let binding = state.rooms.entry(room.to_owned()).or_default();
        migrate_binding(binding, &session)?;
        binding.label = label.clone();
        cli::ensure_session(&session)?;
        if !binding.workspace_id.is_empty() && !binding.root_pane_id.is_empty() {
            if cli::verify_workspace(
                &session,
                &binding.workspace_id,
                &label,
                &binding.root_pane_id,
            )? {
                return Ok(
                    json!({"session":session,"workspace_id":binding.workspace_id,"pane_id":binding.root_pane_id,"open_command":open_command(room),"reused":true}),
                );
            }
            binding.workspace_id.clear();
            binding.root_pane_id.clear();
        }
        let matches = cli::find_workspaces(&session, &label)?;
        let created = match matches.as_slice() {
            [found] => found.clone(),
            [] => cli::create_workspace(&session, &workspace, &label)?,
            _ => bail!("multiple Herdr workspaces have Tearoom label {label:?}; refusing to guess"),
        };
        binding.workspace_id = created.0.clone();
        binding.root_pane_id = created.1.clone();
        Ok(
            json!({"session":session,"workspace_id":created.0,"pane_id":created.1,"open_command":open_command(room),"reused":matches.len()==1}),
        )
    })
}

pub fn open_room(root: &Path, room: &str) -> Result<()> {
    let binding = ensure_room(root, room)?;
    let session = binding["session"]
        .as_str()
        .context("Herdr session missing")?;
    cli::open_session(session)
}

fn migrate_binding(binding: &mut state::RoomBinding, session: &str) -> Result<()> {
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
        binding.previous = Some(state::LegacyBinding {
            session: binding.session.clone(),
            label: binding.label.clone(),
            workspace_id: binding.workspace_id.clone(),
            root_pane_id: binding.root_pane_id.clone(),
            panes: binding.panes.clone(),
            pending_split: binding.pending_split.clone(),
        });
    } else if binding.previous.is_none() && !binding.workspace_id.is_empty() {
        binding.previous = Some(state::LegacyBinding {
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

fn is_first_role_tab(binding: &state::RoomBinding) -> bool {
    binding.panes.is_empty()
}

pub fn sync_panel(
    root: &Path,
    room: &str,
    role: &str,
    panel: &str,
    status: PanelStatus,
) -> Result<()> {
    let workspace = fs::canonicalize(root.parent().context("Tearoom workspace has no parent")?)?;
    let agent = panel_label(role, panel);
    let state_name = herdr_state(status).to_owned();
    let now = SystemTime::now().duration_since(UNIX_EPOCH)?.as_secs();
    state::update(root, |state| {
        let session = room_session(&workspace, room);
        let binding = state.rooms.entry(room.to_owned()).or_default();
        migrate_binding(binding, &session)?;
        cli::ensure_session(&session)?;
        if state
            .rooms
            .get(room)
            .and_then(|binding| binding.panes.get(panel))
            .is_some_and(|binding| {
                binding.state == state_name && now.saturating_sub(binding.checked_at) < 2
            })
        {
            return Ok(());
        }
        let (pane, terminal_id, attached, fresh) =
            ensure_panel_pane(root, state, room, panel, &agent, &workspace, &session)?;
        if let Some(entry) = state
            .rooms
            .get_mut(room)
            .and_then(|b| b.panes.get_mut(panel))
        {
            entry.checked_at = now;
        }
        state::save(root, state)?;
        if !attached {
            if !fresh {
                if let Some(entry) = state
                    .rooms
                    .get_mut(room)
                    .and_then(|b| b.panes.get_mut(panel))
                {
                    entry.attached = false;
                    entry.state = "unknown".into();
                }
                state::save(root, state)?;
                let _ = cli::report(&session, &pane, &agent, role, "unknown");
                bail!("Herdr pane {pane} no longer contains hc terminal {panel}; status is unknown. Reconnect it manually from Herdr; Tearoom will not type into an existing pane");
            }
            // Only the pane just created with --no-focus is eligible for a launch.
            if let Some(entry) = state
                .rooms
                .get_mut(room)
                .and_then(|b| b.panes.get_mut(panel))
            {
                entry.attached = false;
                entry.terminal_id = terminal_id.clone();
            }
            state::save(root, state)?;
            if let Err(error) = cli::attach(&session, &pane, panel) {
                let _ = cli::report(&session, &pane, &agent, role, "unknown");
                return Err(error).context("Herdr pane status was reported unknown");
            }
            if !cli::wait_attached(&session, &pane, panel)? {
                let _ = cli::report(&session, &pane, &agent, role, "unknown");
                bail!("Herdr pane {pane} did not attach hc terminal {panel}; status was reported unknown");
            }
            if let Some(entry) = state
                .rooms
                .get_mut(room)
                .and_then(|b| b.panes.get_mut(panel))
            {
                entry.attached = true;
                entry.terminal_id = terminal_id.clone();
            }
        }
        cli::report(&session, &pane, &agent, role, &state_name)?;
        if let Some(entry) = state
            .rooms
            .get_mut(room)
            .and_then(|b| b.panes.get_mut(panel))
        {
            entry.pane_id = pane;
            entry.state = state_name;
            entry.terminal_id = terminal_id;
            entry.checked_at = now;
        }
        state::save(root, state)
    })
}

fn ensure_panel_pane(
    root: &Path,
    state: &mut state::State,
    room: &str,
    panel: &str,
    tab_label: &str,
    cwd: &Path,
    session: &str,
) -> Result<(String, String, bool, bool)> {
    let label = room_label(cwd, room);
    {
        let binding = state.rooms.entry(room.to_owned()).or_default();
        binding.label = label.clone();
        ensure_binding(binding, cwd, &label, session)?;
        if let Some(pending) = &binding.pending_split {
            bail!("Herdr pane creation for {} is unresolved in workspace {} (panes before creation: {:?}); refusing to create another pane. Inspect `herdr pane list --workspace {}` and resolve the pending operation before retrying", pending.panel, binding.workspace_id, pending.before_panes, binding.workspace_id);
        }
    }
    let existing = state.rooms.get(room).and_then(|binding| {
        binding
            .panes
            .get(panel)
            .map(|pane| (binding.workspace_id.clone(), pane.pane_id.clone()))
    });
    let (pane, fresh) = if let Some((workspace_id, pane_id)) = existing {
        let _ = workspace_id;
        (pane_id, false)
    } else {
        let (workspace_id, before_panes) = {
            let binding = state.rooms.get(room).expect("binding was ensured");
            let panes = cli::workspace_panes(session, &binding.workspace_id)?;
            (
                binding.workspace_id.clone(),
                panes.into_iter().map(|p| p.0).collect::<Vec<_>>(),
            )
        };
        state
            .rooms
            .get_mut(room)
            .expect("binding was ensured")
            .pending_split = Some(state::PendingSplit {
            panel: panel.into(),
            before_panes,
        });
        state::save(root, state)?;
        // Keep the workspace root available to the person; each role gets a full-size tab.
        let first_role_tab = state.rooms.get(room).is_some_and(is_first_role_tab);
        let (tab_id, pane_id) = cli::create_tab(session, &workspace_id, cwd, tab_label)?;
        let binding = state.rooms.get_mut(room).expect("binding was ensured");
        if binding.workspace_id != workspace_id {
            bail!("Herdr workspace changed during pane split");
        }
        binding.pending_split = None;
        binding.panes.insert(
            panel.into(),
            state::PaneBinding {
                pane_id: pane_id.clone(),
                attached: false,
                state: "unknown".into(),
                terminal_id: String::new(),
                checked_at: 0,
            },
        );
        state::save(root, state)?;
        if first_role_tab {
            let _ = cli::focus_tab(session, &tab_id);
        }
        (pane_id, true)
    };
    let binding = state.rooms.get(room).expect("binding was ensured");
    let terminal_id = cli::pane_terminal_id(session, &binding.workspace_id, &pane)?;
    let attached = cli::is_attached(session, &pane, panel)?;
    Ok((pane, terminal_id, attached, fresh))
}

fn ensure_binding(
    binding: &mut state::RoomBinding,
    cwd: &Path,
    label: &str,
    session: &str,
) -> Result<()> {
    if !binding.workspace_id.is_empty()
        && !binding.root_pane_id.is_empty()
        && cli::verify_workspace(session, &binding.workspace_id, label, &binding.root_pane_id)?
    {
        return Ok(());
    }
    let matches = cli::find_workspaces(session, label)?;
    let (workspace_id, pane_id) = match matches.as_slice() {
        [found] => found.clone(),
        [] => cli::create_workspace(session, cwd, label)?,
        _ => bail!("multiple Herdr workspaces have Tearoom label {label:?}; refusing to guess"),
    };
    binding.workspace_id = workspace_id;
    binding.root_pane_id = pane_id;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn room_labels_are_readable_bounded_and_workspace_specific() {
        let first = room_label(Path::new("/tmp/a"), "implementation-long");
        assert!(first.starts_with("tr-implemen-"));
        assert!(first.len() <= 32);
        assert_ne!(
            first,
            room_label(Path::new("/tmp/b"), "implementation-long")
        );
    }

    #[test]
    fn each_room_gets_a_stable_independent_session() {
        assert_eq!(
            room_session(Path::new("/tmp/a"), "writing"),
            room_session(Path::new("/tmp/a"), "writing")
        );
        assert_ne!(
            room_session(Path::new("/tmp/a"), "writing"),
            room_session(Path::new("/tmp/a"), "review")
        );
        assert_ne!(
            room_session(Path::new("/tmp/a"), "writing"),
            room_session(Path::new("/tmp/b"), "writing")
        );
    }

    #[test]
    fn migration_keeps_old_session_mapping_and_pending_pane_evidence() {
        let mut binding = state::RoomBinding {
            workspace_id: "w-old".into(),
            root_pane_id: "w-old:p1".into(),
            pending_split: Some(state::PendingSplit {
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
        let mut binding = state::RoomBinding {
            session: "tearoom-current".into(),
            workspace_id: "w-current".into(),
            previous: Some(state::LegacyBinding {
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

    #[test]
    fn only_the_first_role_tab_is_auto_focused() {
        let mut binding = state::RoomBinding::default();
        assert!(is_first_role_tab(&binding));
        binding
            .panes
            .insert("panel".into(), state::PaneBinding::default());
        assert!(!is_first_role_tab(&binding));
    }

    #[test]
    fn open_command_quotes_room_names_for_shell_use() {
        assert_eq!(
            open_command("the writer's room"),
            "tearoom room open --room 'the writer'\\''s room'"
        );
    }

    #[test]
    fn herdr_status_never_turns_unknown_or_missing_into_idle() {
        assert_eq!(herdr_state(PanelStatus::Working), "working");
        assert_eq!(herdr_state(PanelStatus::Blocked), "blocked");
        assert_eq!(herdr_state(PanelStatus::Unknown), "unknown");
        assert_eq!(herdr_state(PanelStatus::Missing), "unknown");
        assert_eq!(herdr_state(PanelStatus::Done), "idle");
    }

    #[test]
    fn agent_identity_keeps_a_readable_role_and_unique_herdr_safe_suffix() {
        let first = panel_label("Review", "tr-panel-one");
        let second = panel_label("review", "tr-panel-two");
        assert!(first.starts_with("r-review-"));
        assert!(first.len() <= 32);
        assert!(first.bytes().all(|byte| byte.is_ascii_lowercase()
            || byte.is_ascii_digit()
            || matches!(byte, b'-' | b'_')));
        assert_ne!(first, second);
        assert!(panel_label("评审", "tr-panel-one").starts_with("r-role-"));
    }
}
