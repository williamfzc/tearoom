//! Role configuration and dispatch commands at the CLI boundary.
use super::{open, parse, print_json};
use crate::collab::dispatch::Dispatcher;
use crate::collab::runtime::{HcRuntime, Runtime};
use crate::collab::{Config, Role, RoomRoles};
use crate::hc::HcClient;
use crate::mentions;
use crate::store::Store;
use anyhow::{anyhow, bail, Result};
use std::time::Duration;

fn runtime() -> Result<HcRuntime> {
    Ok(HcRuntime {
        client: HcClient::from_env()?,
        timeout: Duration::from_secs(30),
    })
}
pub(super) fn role_command(args: &[String]) -> Result<()> {
    let Some((action, rest)) = args.split_first() else {
        println!("Usage: tearoom role <define|list|show>");
        return Ok(());
    };
    if action == "--help" || action == "-h" {
        println!("Usage: tearoom role <define|list|show>");
        return Ok(());
    }
    let options: &[&str] = match action.as_str() {
        "define" => &[
            "name",
            "description",
            "agent-type",
            "command",
            "template",
            "arg",
            "cwd",
            "instructions",
        ],
        "show" => &["name"],
        "list" => &[],
        _ => bail!("unknown role command {action:?}"),
    };
    let args = parse(rest, options, &[])?;
    if args.help("Usage: tearoom role define --name <name> --agent-type <type> (--template <name> | --command <program>) [--arg <arg>] [--cwd <dir>] [--description <text>] [--instructions <text>]\n       tearoom role list\n       tearoom role show --name <name>") {
        return Ok(());
    }
    args.no_positional()?;
    let root = Store::find(std::env::current_dir()?)?;
    match action.as_str() {
        "define" => {
            let name = args.get("name");
            if name.trim().is_empty() {
                bail!("--name is required");
            }
            let command = args.get("command");
            let template = args.get("template");
            if command.trim().is_empty() == template.trim().is_empty() {
                bail!("provide exactly one of --command and --template");
            }
            if args.get("agent-type").trim().is_empty() {
                bail!("--agent-type is required");
            }
            let extra = args.repeated.get("arg").cloned().unwrap_or_default();
            if command.is_empty() && !extra.is_empty() {
                bail!("--arg requires --command");
            }
            let mut argv = Vec::new();
            if !command.is_empty() {
                argv.push(command.into());
                argv.extend(extra);
            }
            let role = Role {
                description: args.get("description").trim().into(),
                agent_type: args.get("agent-type").trim().into(),
                command: argv,
                template: template.trim().into(),
                directory: args.get("cwd").trim().into(),
                instructions: args.get("instructions").into(),
            };
            Config::update(&root, |config| {
                config.roles.insert(name.into(), role.clone());
                Ok(())
            })?;
            print_json(&serde_json::json!({"name":name,"role":role}))
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
pub(super) fn room_roles(args: &[String]) -> Result<()> {
    let args = parse(args, &["room", "add", "remove"], &["status"])?;
    if args.help(
        "Usage: tearoom room roles --room <name> [--add <roles>] [--remove <roles>] [--status]",
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
                    bail!("unknown role {name:?}; define it first with 'tearoom role define'");
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
        if args.switches.contains("status") {
            let panel = crate::collab::panel_name(root, room, &name)?;
            let status = runtime().and_then(|runtime| runtime.status(&panel));
            entry["panel"] = serde_json::json!(panel);
            match status {
                Ok(value) => {
                    entry["status"] = serde_json::json!(format!("{value:?}").to_ascii_lowercase())
                }
                Err(error) => {
                    entry["status"] = serde_json::json!("unknown");
                    entry["status_error"] = serde_json::json!(error.to_string());
                }
            }
        }
        print_json(&entry)?;
    }
    Ok(())
}
pub(super) fn collab_command(args: &[String]) -> Result<()> {
    let Some((action, rest)) = args.split_first() else {
        println!("Usage: tearoom collab <run|status|resolve>");
        return Ok(());
    };
    if action == "--help" || action == "-h" {
        println!("Usage: tearoom collab <run|status|resolve>");
        return Ok(());
    }
    let args = match action.as_str() {
        "run" | "status" => parse(rest, &[], &[])?,
        "resolve" => parse(rest, &["room", "seq", "role", "outcome"], &["confirm"])?,
        _ => bail!("unknown collab command {action:?}"),
    };
    if args.help("Usage: tearoom collab run\n       tearoom collab status\n       tearoom collab resolve --room <room> --seq <seq> --role <role> --outcome <delivered|skip|retry> [--confirm]") {
        return Ok(());
    }
    args.no_positional()?;
    let store = Store::open(std::env::current_dir()?)?;
    let dispatcher = Dispatcher {
        store,
        runtime: runtime()?,
    };
    match action.as_str() {
        "status" => print_json(&dispatcher.status()?),
        "run" => loop {
            dispatcher.pump()?;
            std::thread::sleep(Duration::from_millis(500));
        },
        "resolve" => {
            let seq: u64 = args.get("seq").parse().map_err(|_| {
                anyhow!("--room, positive --seq, --role, and --outcome are required")
            })?;
            let (room, role, outcome) = (args.get("room"), args.get("role"), args.get("outcome"));
            if room.is_empty() || seq == 0 || role.is_empty() || outcome.is_empty() {
                bail!("--room, positive --seq, --role, and --outcome are required");
            }
            let confirm = args.switches.contains("confirm");
            if confirm && outcome != "retry" {
                bail!("--confirm is only valid with --outcome retry");
            }
            dispatcher.resolve(room, seq, role, outcome, confirm)?;
            println!("resolved {room}#{seq} @{role} as {outcome}");
            Ok(())
        }
        _ => bail!("unknown collab command {action:?}"),
    }
}
