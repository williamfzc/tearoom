//! Room creation and record commands at the CLI boundary.
use super::roles::room_roles;
use super::room_delete::delete_room_command;
use super::{open, parse, print_json, resolve, room_herdr};
use crate::collab::{Config, Message, RecordRef, RoomRoles};
use crate::core::{AppendRequest, Room};
use crate::mentions;
use crate::store::Store;
use anyhow::{anyhow, bail, Result};

pub(super) fn room_command(args: &[String]) -> Result<()> {
    let Some((action, args)) = args.split_first() else {
        println!("Usage: tearoom room <init|delete|move|open|herdr|roles> [flags]");
        return Ok(());
    };
    if action == "--help" || action == "-h" {
        println!("Usage: tearoom room <init|delete|move|open|herdr|roles> [flags]");
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
    if action != "init" {
        bail!("unknown room command {action:?}");
    }
    let args = parse(args, &["name", "parent", "role"], &[])?;
    if args.help("Usage: tearoom room init --name <name> [--parent <room>] [--role <roles>]") {
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
    let room = if selected.is_empty() {
        store.create_room(args.get("name"), args.get("parent"))?
    } else {
        Config::update(store.root(), |config| {
            for role in &selected {
                if !config.roles.contains_key(role) {
                    bail!("unknown role {role:?}; init it first with 'tearoom role init'");
                }
            }
            let room = store.create_room(args.get("name"), args.get("parent"))?;
            config.rooms.insert(
                room.name.clone(),
                RoomRoles {
                    roles: selected.clone(),
                },
            );
            Ok(room)
        })?
    };
    match crate::herdr::ensure_room(store.root(), &room.name) {
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
    print_json(&store.move_room(name, args.get("parent"))?)
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
    let author = if args.get("as").trim().is_empty() {
        std::env::var("TEAROOM_ACTOR")
            .unwrap_or_default()
            .trim()
            .to_owned()
    } else {
        args.get("as").trim().into()
    };
    let record = Config::with_lock(store.root(), |config| {
        let content = if !raw.is_empty() {
            if !args.get("to").is_empty() || !args.get("reply-to").is_empty() {
                bail!("--to and --reply-to require text content");
            }
            if serde_json::from_str::<Message>(raw)
                .is_ok_and(|message| !message.mentions.is_empty())
            {
                bail!("--json cannot address roles; use text with --to");
            }
            raw.to_owned()
        } else {
            let plain = if !text.is_empty() { text } else { &positional };
            let roster = config.rooms.get(&room).map(|binding| binding.roles.clone());
            let names = mentions::resolve(plain, args.get("to"), &room, roster.as_deref())
                .map_err(|e| anyhow!(e))?;
            let reply = if args.get("reply-to").is_empty() {
                None
            } else {
                let (name, seq) = args
                    .get("reply-to")
                    .split_once('#')
                    .ok_or_else(|| anyhow!("invalid --reply-to"))?;
                let seq: u64 = seq.parse().map_err(|_| anyhow!("invalid --reply-to"))?;
                if name.trim().is_empty() || seq == 0 {
                    bail!("invalid --reply-to");
                }
                Some(RecordRef {
                    room: name.trim().into(),
                    seq,
                })
            };
            let json = if names.is_empty() && reply.is_none() {
                serde_json::to_string(plain)?
            } else {
                serde_json::to_string(&Message {
                    text: plain.into(),
                    mentions: names,
                    reply_to: reply,
                })?
            };
            json.replace('&', "\\u0026")
                .replace('<', "\\u003c")
                .replace('>', "\\u003e")
                .replace('\u{2028}', "\\u2028")
                .replace('\u{2029}', "\\u2029")
        };
        store.append(
            &room,
            &author,
            AppendRequest {
                content: &content,
                idempotency_key: args.get("idempotency-key"),
            },
        )
    })?;
    print_json(&record)
}
fn boundary(value: &str) -> Result<u64> {
    let value = value.trim();
    if value.is_empty() {
        return Ok(0);
    }
    let Some(rest) = value.strip_prefix('e') else {
        bail!("invalid --after {value:?}: use e<seq>");
    };
    if rest.is_empty() || !rest.bytes().all(|c| c.is_ascii_digit()) {
        bail!("invalid --after {value:?}: use e<seq>");
    }
    Ok(rest.parse()?)
}
fn subtree(rooms: &[Room], root: &str) -> Result<Vec<String>> {
    if !rooms.iter().any(|r| r.name == root) {
        bail!("room {root:?}: not found");
    }
    fn visit(rooms: &[Room], room: &str, output: &mut Vec<String>) {
        output.push(room.to_owned());
        for child in rooms.iter().filter(|r| r.parent == room) {
            visit(rooms, &child.name, output);
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
    fn walk(rooms: &[Room], name: &str, prefix: &str, connector: &str) {
        println!("{prefix}{connector}{name}");
        let base = match connector {
            "├── " => format!("{prefix}│   "),
            "└── " => format!("{prefix}    "),
            _ => prefix.into(),
        };
        let children: Vec<_> = rooms.iter().filter(|room| room.parent == name).collect();
        for (index, child) in children.iter().enumerate() {
            walk(
                rooms,
                &child.name,
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
        walk(&rooms, &room.name, "", "");
    }
    Ok(())
}
