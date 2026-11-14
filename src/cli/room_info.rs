//! Room info-card commands: `room update` edits the editable labels (name,
//! description); `room show` prints the card. The Room id, records, and all
//! stored references are untouchable from here.

use super::{open, parse, print_json, resolve};
use crate::collab::Config;
use crate::store::Store;
use anyhow::{bail, Result};

/// The current display name of a Room id, for output that addresses people.
pub(crate) fn room_display(store: &Store, id: &str) -> String {
    store
        .rooms()
        .ok()
        .and_then(|rooms| {
            rooms
                .iter()
                .find(|room| room.id == id)
                .map(|r| r.name.clone())
        })
        .unwrap_or_else(|| id.to_owned())
}

pub(super) fn update_room_command(args: &[String]) -> Result<()> {
    let args = parse(args, &["room", "name", "description"], &[])?;
    if args
        .help("Usage: tearoom room update --room <room> [--name <new-name>] [--description <text>]")
    {
        return Ok(());
    }
    args.no_positional()?;
    let room = args.get("room");
    if room.trim().is_empty() {
        bail!("--room is required");
    }
    if !args.values.contains_key("name") && !args.values.contains_key("description") {
        bail!("provide --name or --description to update");
    }
    let store = open()?;
    let id = resolve(&store, room)?;
    let updated = store.update_room(
        &id,
        args.values.get("name").map(String::as_str),
        args.values.get("description").map(String::as_str),
    )?;
    print_json(&updated)
}

pub(super) fn show_room_command(args: &[String]) -> Result<()> {
    let args = parse(args, &["room"], &[])?;
    if args.help("Usage: tearoom room show --room <room>") {
        return Ok(());
    }
    args.no_positional()?;
    let room = args.get("room");
    if room.trim().is_empty() {
        bail!("--room is required");
    }
    let store = open()?;
    let id = resolve(&store, room)?;
    let room = store
        .rooms()?
        .into_iter()
        .find(|room| room.id == id)
        .ok_or_else(|| anyhow::anyhow!("room {id:?}: not found"))?;
    let config = Config::load(store.root())?;
    let parent = super::room_display(&store, &room.parent);
    let roles = config
        .rooms
        .get(&id)
        .map_or_else(Vec::new, |binding| binding.roles.clone());
    print_json(&serde_json::json!({
        "id": room.id,
        "name": room.name,
        "parent": parent,
        "description": room.description,
        "created": room.created,
        "roles": roles,
    }))
}
