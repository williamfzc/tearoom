//! Destructive Room removal guarded by tree, delivery, and confirmation checks.

use super::{open, parse, print_json};
use crate::collab::{dispatch_lock, panel_name, DeliveryState, DispatchState};
use anyhow::{bail, Result};

pub(super) fn delete_room_command(args: &[String]) -> Result<()> {
    let args = parse(args, &["room"], &["confirm"])?;
    if args.help("Usage: tearoom room delete --room <name> [--confirm]") {
        return Ok(());
    }
    args.no_positional()?;
    let name = args.get("room");
    if name.trim().is_empty() {
        bail!("--room is required");
    }
    let store = open()?;
    let root = store.root();
    let _lock = dispatch_lock(root)?;
    let state = DispatchState::load(root)?;
    if state.intents.iter().any(|intent| {
        intent.room == name
            && intent.deliveries.iter().any(|delivery| {
                !matches!(
                    delivery.state,
                    DeliveryState::Completed | DeliveryState::Skipped
                )
            })
    }) {
        bail!("room {name:?} has unresolved deliveries; resolve them before deleting it");
    }
    let removal = store.room_removal_plan(name)?;
    if !args.switches.contains("confirm") {
        bail!(
            "deleting room {name:?} removes its {} record(s) permanently and cannot be undone; repeat with --confirm",
            removal.records
        );
    }
    let roster = crate::collab::Config::load(root)?
        .rooms
        .get(name)
        .map_or_else(Vec::new, |binding| binding.roles.clone());
    let mut state = DispatchState::load(root)?;
    state.intents.retain(|intent| intent.room != name);
    state.cursor.remove(name);
    state.fresh.remove(name);
    state.save(root)?;
    crate::collab::Config::update(root, |config| {
        config.rooms.remove(name);
        Ok(())
    })?;
    match crate::herdr::forget_room(root, name) {
        Ok(Some(session)) => {
            if let Err(error) = crate::herdr::stop_session(&session) {
                eprintln!("Herdr session {session} for room {name:?} is still running: {error:#}");
            }
        }
        Ok(None) => {}
        Err(error) => eprintln!("Herdr binding for room {name:?} is still present: {error:#}"),
    }
    let records = store.delete_room(name)?;
    for role in &roster {
        if let Ok(panel) = panel_name(root, name, role) {
            eprintln!("panel {panel} (role {role}) keeps its running Agent; stop it with: tearoom hc kill {panel}");
        }
    }
    eprintln!("room {name:?} deleted; {records} record(s) removed");
    print_json(&serde_json::json!({"name":name,"deleted":true,"records":records}))
}
