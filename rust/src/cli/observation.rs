//! HTTP server and notification watch commands at the CLI boundary.
use super::{open, parse, print_json};
use crate::store::Store;
use crate::transport::Server;
use anyhow::{bail, Result};
use serde::Serialize;
use std::collections::BTreeMap;
use std::io::{self, Write};
use std::time::Duration;

pub(super) fn server_command(args: &[String]) -> Result<()> {
    let Some((action, args)) = args.split_first() else {
        println!("Usage: tearoom server start [--address <address>]");
        return Ok(());
    };
    if action == "--help" || action == "-h" {
        println!("Usage: tearoom server start [--address <address>]");
        return Ok(());
    }
    if action != "start" {
        bail!("unknown server command {action:?}");
    }
    let args = parse(args, &["address"], &[])?;
    if args.help("Usage: tearoom server start [--address <address>]") {
        return Ok(());
    }
    args.no_positional()?;
    let address = if args.get("address").is_empty() {
        "127.0.0.1:8787"
    } else {
        args.get("address")
    };
    let store = Store::open(std::env::current_dir()?)?;
    let server = Server::bind(store, address)?;
    eprintln!("tearoom serving on {}", server.address());
    server.serve()
}
pub(super) fn watch_command(args: &[String]) -> Result<()> {
    let args = parse(args, &["room"], &["json"])?;
    if args.help("Usage: tearoom watch [--room <room>] [--json]") {
        return Ok(());
    }
    args.no_positional()?;
    let filter = args.get("room").trim();
    let store = open()?;
    if !filter.is_empty() {
        store.read(filter, 0)?;
    }
    let mut heads = BTreeMap::new();
    loop {
        for room in store.rooms()? {
            if !filter.is_empty() && filter != room.name {
                continue;
            }
            let head = heads.get(&room.name).copied();
            for record in store.read(&room.name, head.unwrap_or(0))? {
                if head.is_some() {
                    if args.switches.contains("json") {
                        #[derive(Serialize)]
                        struct Notification<'a> {
                            room: &'a str,
                            seq: u64,
                            author: &'a str,
                            at: &'a str,
                        }
                        print_json(&Notification {
                            room: &record.room,
                            seq: record.seq,
                            author: &record.author,
                            at: &record.at,
                        })?;
                    } else {
                        println!(
                            "{} #{} {} {}",
                            record.room, record.seq, record.author, record.at
                        );
                    }
                    io::stdout().flush()?;
                }
                heads.insert(room.name.clone(), record.seq);
            }
            heads.entry(room.name).or_insert(0);
        }
        std::thread::sleep(Duration::from_millis(500));
    }
}
