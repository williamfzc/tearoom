//! Room-scoped commands for ensuring and opening the mapped Herdr session.

use super::{open, parse, print_json};
use anyhow::{bail, Result};

pub(super) fn herdr_command(args: &[String]) -> Result<()> {
    let Some((action, rest)) = args.split_first() else {
        println!("Usage: tearoom room herdr ensure --room <name>");
        return Ok(());
    };
    if action != "ensure" {
        bail!("unknown room herdr command {action:?}");
    }
    let args = parse(rest, &["room"], &[])?;
    if args.help("Usage: tearoom room herdr ensure --room <name>") {
        return Ok(());
    }
    args.no_positional()?;
    let room = args.get("room");
    if room.trim().is_empty() {
        bail!("--room is required");
    }
    let store = open()?;
    if !store.rooms()?.iter().any(|entry| entry.name == room) {
        bail!("room {room:?}: not found");
    }
    print_json(
        &serde_json::json!({"room":room,"herdr":crate::herdr::ensure_room(store.root(), room)?}),
    )
}

pub(super) fn open_room_command(args: &[String]) -> Result<()> {
    let args = parse(args, &["room"], &[])?;
    if args.help("Usage: tearoom room open --room <name>") {
        return Ok(());
    }
    args.no_positional()?;
    let room = args.get("room");
    if room.trim().is_empty() {
        bail!("--room is required");
    }
    let store = open()?;
    if !store.rooms()?.iter().any(|entry| entry.name == room) {
        bail!("room {room:?}: not found");
    }
    crate::herdr::open_room(store.root(), room)
}
