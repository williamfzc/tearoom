//! Reusable role definitions and Room rosters at the CLI boundary.
use super::{hc_runtime, open, parse, print_json};
use crate::collab::runtime::Runtime;
use crate::collab::{dispatch_lock, Config, DeliveryState, DispatchState, Role, RoomRoles};
use crate::mentions;
use crate::store::Store;
use anyhow::{anyhow, bail, Result};

pub(super) fn role_command(args: &[String]) -> Result<()> {
    let Some((action, rest)) = args.split_first() else {
        println!("Usage: tearoom role <init|update|delete|list|show>");
        return Ok(());
    };
    if action == "--help" || action == "-h" {
        println!("Usage: tearoom role <init|update|delete|list|show>");
        return Ok(());
    }
    let definition_options = &[
        "name",
        "description",
        "agent-type",
        "command",
        "arg",
        "cwd",
        "instructions",
    ];
    let options: &[&str] = match action.as_str() {
        "init" | "update" => definition_options,
        "delete" | "show" => &["name"],
        "list" => &[],
        _ => bail!("unknown role command {action:?}"),
    };
    let args = parse(rest, options, &[])?;
    if args.help("Usage: tearoom role init --name <name> --agent-type <type> --command <program> [--arg <arg>] [--cwd <dir>] [--description <text>] [--instructions <text>]\n       tearoom role update --name <name> [--agent-type <type>] [--command <program> [--arg <arg>]] [--cwd <dir>] [--description <text>] [--instructions <text>]\n       tearoom role delete --name <name>\n       tearoom role list\n       tearoom role show --name <name>") {
        return Ok(());
    }
    args.no_positional()?;
    let root = Store::find(std::env::current_dir()?)?;
    match action.as_str() {
        "delete" => {
            let name = args.get("name");
            if name.trim().is_empty() {
                bail!("--name is required");
            }
            delete_role(&root, name)?;
            eprintln!("role {name:?} deleted; any already-running Agent process is unaffected");
            print_json(&serde_json::json!({"name":name,"deleted":true}))
        }
        "init" => {
            let name = args.get("name");
            if name.trim().is_empty() {
                bail!("--name is required");
            }
            let command = args.get("command");
            if command.trim().is_empty() {
                bail!("--command is required");
            }
            if args.get("agent-type").trim().is_empty() {
                bail!("--agent-type is required");
            }
            let argv = args.argv("command", "arg");
            let role = Role {
                description: args.get("description").trim().into(),
                agent_type: args.get("agent-type").trim().into(),
                command: argv,
                directory: args.get("cwd").trim().into(),
                instructions: args.get("instructions").into(),
            };
            Config::update(&root, |config| {
                if config.roles.contains_key(name) {
                    bail!("role {name:?} already exists; use 'tearoom role update' to change it");
                }
                config.roles.insert(name.into(), role.clone());
                Ok(())
            })?;
            print_json(&serde_json::json!({"name":name,"role":role}))
        }
        "update" => {
            let name = args.get("name");
            if name.trim().is_empty() {
                bail!("--name is required");
            }
            let has_command = args.values.contains_key("command");
            if args.repeated.contains_key("arg") && !has_command {
                bail!("--arg requires --command during role update");
            }
            let updates_definition = [
                "description",
                "agent-type",
                "command",
                "cwd",
                "instructions",
            ]
            .iter()
            .any(|key| args.values.contains_key(*key));
            if !updates_definition {
                bail!("provide at least one role field to update");
            }
            let mut updated = None;
            let mut restart_required = false;
            Config::update(&root, |config| {
                let role = config
                    .roles
                    .get_mut(name)
                    .ok_or_else(|| anyhow!("unknown role {name:?}"))?;
                if args.values.contains_key("description") {
                    role.description = args.get("description").trim().into();
                }
                if args.values.contains_key("agent-type") {
                    let agent_type = args.get("agent-type").trim();
                    restart_required |= role.agent_type != agent_type;
                    role.agent_type = agent_type.into();
                }
                if has_command {
                    let command = args.get("command");
                    if command.trim().is_empty() {
                        bail!("--command must not be empty");
                    }
                    let argv = args.argv("command", "arg");
                    restart_required |= role.command != argv;
                    role.command = argv;
                }
                if args.values.contains_key("cwd") {
                    let directory = args.get("cwd").trim();
                    restart_required |= role.directory != directory;
                    role.directory = directory.into();
                }
                if args.values.contains_key("instructions") {
                    role.instructions = args.get("instructions").into();
                }
                updated = Some(role.clone());
                Ok(())
            })?;
            if restart_required {
                eprintln!("role {name:?} launch settings changed; running role processes keep their current process and settings, while newly started processes use this configuration");
            }
            print_json(&serde_json::json!({"name":name,"role":updated.unwrap()}))
        }
        "list" => {
            for (name, role) in Config::load(&root)?.roles {
                print_json(&serde_json::json!({"name":name,"role":role}))?;
            }
            Ok(())
        }
        "show" => {
            let name = args.get("name");
            if name.is_empty() {
                bail!("--name is required");
            }
            let config = Config::load(&root)?;
            let role = config
                .roles
                .get(name)
                .ok_or_else(|| anyhow!("unknown role {name:?}"))?;
            print_json(&serde_json::json!({"name":name,"role":role}))
        }
        _ => bail!("unknown role command {action:?}"),
    }
}

fn delete_role(root: &std::path::Path, name: &str) -> Result<()> {
    let _dispatch_lock = dispatch_lock(root)?;
    let state = DispatchState::load(root)?;
    if state.intents.iter().any(|intent| {
        intent.deliveries.iter().any(|delivery| {
            delivery.role == name
                && !matches!(
                    delivery.state,
                    DeliveryState::Completed | DeliveryState::Skipped
                )
        })
    }) {
        bail!("role {name:?} has unresolved deliveries; resolve them before deleting it");
    }
    Config::update(root, |config| {
        if config
            .rooms
            .values()
            .any(|room| room.roles.iter().any(|role| role == name))
        {
            bail!(
                "role {name:?} is assigned to a Room; remove its Room bindings before deleting it"
            );
        }
        let store = Store::open(root)?;
        for room in store.rooms()? {
            for record in store.read(&room.name, *state.cursor.get(&room.name).unwrap_or(&0))? {
                if serde_json::from_str::<crate::collab::Message>(record.content.get())
                    .is_ok_and(|message| message.mentions.iter().any(|role| role == name))
                {
                    bail!("role {name:?} has unscanned messages; keep or resolve them before deleting it");
                }
            }
        }
        if config.roles.remove(name).is_none() {
            bail!("unknown role {name:?}");
        }
        Ok(())
    })
}

pub(super) fn room_roles(args: &[String]) -> Result<()> {
    let args = parse(args, &["room", "add", "remove"], &["observe"])?;
    if args.help(
        "Usage: tearoom room roles --room <name> [--add <roles>] [--remove <roles>] [--observe]",
    ) {
        return Ok(());
    }
    args.no_positional()?;
    let room = args.get("room");
    if room.trim().is_empty() {
        bail!("--room is required");
    }
    let store = open()?;
    if !store.rooms()?.iter().any(|r| r.name == room) {
        bail!("room {room:?}: not found");
    }
    let root = store.root();
    let adds = mentions::parse_explicit(args.get("add")).map_err(|e| anyhow!(e))?;
    let removes = mentions::parse_explicit(args.get("remove")).map_err(|e| anyhow!(e))?;
    if !adds.is_empty() || !removes.is_empty() {
        Config::update(root, |config| {
            let mut roles: std::collections::BTreeSet<String> = config
                .rooms
                .get(room)
                .map_or_else(Default::default, |r| r.roles.iter().cloned().collect());
            for name in &adds {
                if !config.roles.contains_key(name) {
                    bail!("unknown role {name:?}; init it first with 'tearoom role init'");
                }
                roles.insert(name.clone());
            }
            for name in &removes {
                roles.remove(name);
            }
            config.rooms.insert(
                room.into(),
                RoomRoles {
                    roles: roles.into_iter().collect(),
                },
            );
            Ok(())
        })?;
    }
    let config = Config::load(root)?;
    let mut names = config
        .rooms
        .get(room)
        .map_or_else(Vec::new, |r| r.roles.clone());
    names.sort();
    for name in names {
        let mut entry =
            serde_json::json!({"room":room,"role":name,"definition":config.roles.get(&name)});
        if args.switches.contains("observe") {
            let panel = crate::collab::panel_name(root, room, &name)?;
            let observed = hc_runtime().and_then(|runtime| runtime.status(&panel));
            entry["panel"] = serde_json::json!(panel);
            match observed {
                Ok(value) => entry["observed"] = serde_json::json!(value.as_str()),
                Err(error) => {
                    entry["observed"] = serde_json::json!("unknown");
                    entry["observed_error"] = serde_json::json!(error.to_string());
                }
            }
        }
        print_json(&entry)?;
    }
    Ok(())
}
