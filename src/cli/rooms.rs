//! Room creation and record commands at the CLI boundary.
use super::roles::room_roles;
use super::room_delete::delete_room_command;
use super::room_info::{show_room_command, update_room_command};
use super::{append_text, boundary, open, parse, print_json, resolve, room_herdr, signature};
use crate::collab::{Config, Message, RecordRef, RoomRoles};
use crate::core::{AppendRequest, Room};
use crate::mentions;
use crate::store::Store;
use anyhow::{anyhow, bail, Result};

pub(super) fn room_command(args: &[String]) -> Result<()> {
    let Some((action, args)) = args.split_first() else {
        println!("Usage: tearoom room <init|update|show|delete|move|open|herdr|roles> [flags]");
        return Ok(());
    };
    if action == "--help" || action == "-h" {
        println!("Usage: tearoom room <init|update|show|delete|move|open|herdr|roles> [flags]");
        return Ok(());
    }
    if action == "roles" {
        return room_roles(args);
    }
    if action == "herdr" {
        return room_herdr::herdr_command(args);
    }
    if action == "open" {
        return room_herdr::open_room_command(args);
    }
    if action == "move" {
        return move_room_command(args);
    }
    if action == "delete" {
        return delete_room_command(args);
    }
    if action == "update" {
        return update_room_command(args);
    }
    if action == "show" {
        return show_room_command(args);
    }
    if action != "init" {
        bail!("unknown room command {action:?}");
    }
    let args = parse(args, &["name", "parent", "role", "description"], &[])?;
    if args.help("Usage: tearoom room init --name <name> [--parent <room>] [--role <roles>] [--description <text>]") {
        return Ok(());
    }
    args.no_positional()?;
    if args.get("name").trim().is_empty() {
        bail!("--name is required: rooms are addressed by name");
    }
    let cwd = std::env::current_dir()?;
    let store = match Store::find(&cwd) {
        Ok(_) => Store::open(&cwd)?,
        Err(e) if e.to_string().contains("no workspace") => Store::init(&cwd)?,
        Err(e) => return Err(e),
    };
    let selected = mentions::parse_explicit(args.get("role")).map_err(|e| anyhow!(e))?;
    let parent = if args.get("parent").trim().is_empty() {
        String::new()
    } else {
        resolve(&store, args.get("parent"))?
    };
    let room = if selected.is_empty() {
        store.create_room(args.get("name"), &parent)?
    } else {
        Config::update(store.root(), |config| {
            for role in &selected {
                if !config.roles.contains_key(role) {
                    bail!("unknown role {role:?}; init it first with 'tearoom role init'");
                }
            }
            let room = store.create_room(args.get("name"), &parent)?;
            config.rooms.insert(
                room.id.clone(),
                RoomRoles {
                    roles: selected.clone(),
                },
            );
            Ok(room)
        })?
    };
    let room = if args.get("description").trim().is_empty() {
        room
    } else {
        store.update_room(&room.id, None, Some(args.get("description")))?
    };
    match crate::herdr::ensure_room(store.root(), &room.id) {
        Ok(binding) => eprintln!("Herdr session ready: {}\nOpen it with: {}", binding["session"].as_str().unwrap_or("unknown"), binding["open_command"].as_str().unwrap_or("tearoom room open --room <name>")),
        Err(error) => eprintln!("Room created, but its Herdr session is pending: {error:#}. Retry with: tearoom room herdr ensure --room '{}'", room.name.replace('\'', "'\\''")),
    }
    print_json(&room)
}

fn move_room_command(args: &[String]) -> Result<()> {
    let args = parse(args, &["room", "parent"], &[])?;
    if args.help("Usage: tearoom room move --room <name> [--parent <room>]") {
        return Ok(());
    }
    args.no_positional()?;
    let name = args.get("room");
    if name.trim().is_empty() {
        bail!("--room is required");
    }
    let store = open()?;
    let room = resolve(&store, name)?;
    let parent = if args.get("parent").trim().is_empty() {
        String::new()
    } else {
        resolve(&store, args.get("parent"))?
    };
    print_json(&store.move_room(&room, &parent)?)
}

pub(super) fn send_command(args: &[String]) -> Result<()> {
    let args = parse(
        args,
        &[
            "room",
            "as",
            "text",
            "json",
            "idempotency-key",
            "to",
            "reply-to",
        ],
        &[],
    )?;
    if args.help("Usage: tearoom send [--room <room>] [--as <author>] [--text <text> | --json <json>] [--to @role] [--reply-to room#seq]") { return Ok(()); }
    if args.values.contains_key("to") && args.get("to").trim().is_empty() {
        bail!("--to requires at least one role");
    }
    let positional = args.positional.join(" ");
    let text = args.get("text");
    let raw = args.get("json");
    if [!positional.is_empty(), !text.is_empty(), !raw.is_empty()]
        .iter()
        .filter(|v| **v)
        .count()
        != 1
    {
        bail!("provide exactly one of positional text, --text, or --json");
    }
    let store = open()?;
    let room = resolve(&store, args.get("room"))?;
    let author = signature(args.get("as"));
    let record = if !raw.is_empty() {
        if !args.get("to").is_empty() || !args.get("reply-to").is_empty() {
            bail!("--to and --reply-to require text content");
        }
        if serde_json::from_str::<Message>(raw).is_ok_and(|message| !message.mentions.is_empty()) {
            bail!("--json cannot address roles; use text with --to");
        }
        store.append(
            &room,
            &author,
            AppendRequest {
                content: raw,
                idempotency_key: args.get("idempotency-key"),
            },
        )?
    } else {
        let plain = if !text.is_empty() { text } else { &positional };
        let reply = if args.get("reply-to").is_empty() {
            None
        } else {
            let (ref_room, seq) = args
                .get("reply-to")
                .split_once('#')
                .ok_or_else(|| anyhow!("invalid --reply-to"))?;
            let seq: u64 = seq.parse().map_err(|_| anyhow!("invalid --reply-to"))?;
            if ref_room.trim().is_empty() || seq == 0 {
                bail!("invalid --reply-to");
            }
            // References anchor to the Room id so a later label edit survives.
            Some(RecordRef {
                room: resolve(&store, ref_room)?,
                seq,
            })
        };
        append_text(
            &store,
            &room,
            &author,
            plain,
            args.get("to"),
            reply,
            args.get("idempotency-key"),
        )?
    };
    let service_error = super::activate_addressed(&record, store.root());
    print_json(&record)?;
    if let Some(error) = service_error {
        return Err(anyhow!("{error} (record {}#{})", record.room, record.seq));
    }
    Ok(())
}
fn subtree(rooms: &[Room], root: &str) -> Result<Vec<String>> {
    if !rooms.iter().any(|r| r.id == root) {
        bail!("room {root:?}: not found");
    }
    fn visit(rooms: &[Room], room: &str, output: &mut Vec<String>) {
        output.push(room.to_owned());
        for child in rooms.iter().filter(|r| r.parent == room) {
            visit(rooms, &child.id, output);
        }
    }
    let mut result = Vec::new();
    visit(rooms, root, &mut result);
    Ok(result)
}
pub(super) fn read_command(args: &[String]) -> Result<()> {
    let args = parse(args, &["room", "after", "tail"], &["subtree"])?;
    if args.help("Usage: tearoom read [--room <room>] [--after e<seq>] [--tail <n>] [--subtree]") {
        return Ok(());
    }
    args.no_positional()?;
    let after = boundary(args.get("after"))?;
    let tail: Option<usize> = args
        .values
        .get("tail")
        .map(|v| {
            v.parse()
                .map_err(|_| anyhow!("--tail must be a non-negative integer"))
        })
        .transpose()?;
    let store = open()?;
    let room = resolve(&store, args.get("room"))?;
    let names = if args.switches.contains("subtree") {
        subtree(&store.rooms()?, &room)?
    } else {
        vec![room]
    };
    for name in names {
        let records = store.read(&name, after)?;
        let start = tail.map_or(0, |n| records.len().saturating_sub(n));
        for record in &records[start..] {
            print_json(record)?;
        }
    }
    Ok(())
}
pub(super) fn tree_command(args: &[String]) -> Result<()> {
    let args = parse(args, &[], &[])?;
    if args.help("Usage: tearoom tree") {
        return Ok(());
    }
    args.no_positional()?;
    fn walk(rooms: &[Room], id: &str, prefix: &str, connector: &str) {
        let room = rooms.iter().find(|r| r.id == id);
        println!(
            "{prefix}{connector}{}",
            room.map_or(id, |room| room.name.as_str())
        );
        let base = match connector {
            "├── " => format!("{prefix}│   "),
            "└── " => format!("{prefix}    "),
            _ => prefix.into(),
        };
        let children: Vec<_> = rooms.iter().filter(|room| room.parent == id).collect();
        for (index, child) in children.iter().enumerate() {
            walk(
                rooms,
                &child.id,
                &base,
                if index + 1 == children.len() {
                    "└── "
                } else {
                    "├── "
                },
            );
        }
    }
    let rooms = open()?.rooms()?;
    for room in rooms.iter().filter(|r| r.parent.is_empty()) {
        walk(&rooms, &room.id, "", "");
    }
    Ok(())
}
