//! Dispatcher commands at the CLI boundary.
use super::{hc_runtime, parse, print_json};
use crate::collab::dispatch::Dispatcher;
use crate::store::Store;
use anyhow::{anyhow, bail, Result};
use std::time::Duration;

pub(super) fn collab_command(args: &[String]) -> Result<()> {
    let Some((action, rest)) = args.split_first() else {
        println!("Usage: tearoom collab <run|deliveries|resolve>");
        return Ok(());
    };
    if action == "--help" || action == "-h" {
        println!("Usage: tearoom collab <run|deliveries|resolve>");
        return Ok(());
    }
    let args = match action.as_str() {
        "run" | "deliveries" => parse(rest, &[], &[])?,
        "resolve" => parse(rest, &["room", "seq", "role", "outcome"], &["confirm"])?,
        _ => bail!("unknown collab command {action:?}"),
    };
    if args.help("Usage: tearoom collab run\n       tearoom collab deliveries\n       tearoom collab resolve --room <room> --seq <seq> --role <role> --outcome <delivered|skip|retry> [--confirm]") {
        return Ok(());
    }
    args.no_positional()?;
    let store = Store::open(std::env::current_dir()?)?;
    let dispatcher = Dispatcher {
        store,
        runtime: hc_runtime()?,
    };
    match action.as_str() {
        "deliveries" => print_json(&dispatcher.deliveries()?),
        "run" => loop {
            dispatcher.pump()?;
            let intents = dispatcher.deliveries()?;
            crate::collab::herdr::enqueue(dispatcher.store.root(), &intents);
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
