//! CLI entry point, argument parsing and shared backend selection.
mod collaboration;
mod observation;
mod rooms;

use crate::core::{AppendRequest, Record, Room};
use crate::store::Store;
use crate::transport::Client;
use anyhow::{anyhow, bail, Result};
use serde::Serialize;
use std::collections::{HashMap, HashSet};

const HELP: &str = "tearoom — a durable room tree for agents\n\nUsage: tearoom <command> [flags]\n\nCommands:\n  room      create rooms and manage collaborators\n  role      define and inspect collaborator roles\n  collab    run or inspect the hc dispatcher\n  send      append a record\n  read      read a room or subtree\n  tree      print the room tree\n  watch     follow notifications\n  server    serve the workspace over local HTTP\n  skill     print the agent guide\n\nOptions:\n  --version show build and store format information\n\nEnvironment:\n  TEAROOM_ACTOR   default signature for --as\n  TEAROOM_SERVER  local server URL instead of the nearest workspace\n\nUse \"tearoom <command> --help\" for more information.\n";

pub fn run() -> i32 {
    let args: Vec<String> = std::env::args().skip(1).collect();
    match execute(&args) {
        Ok(()) => 0,
        Err(error) => {
            eprintln!("{error:#}");
            let detail = format!("{error:#}");
            if detail.contains("no workspace") || detail.contains("server unreachable") {
                4
            } else {
                1
            }
        }
    }
}
fn execute(args: &[String]) -> Result<()> {
    let Some((command, rest)) = args.split_first() else {
        print!("{HELP}");
        return Ok(());
    };
    if command == "--help" || command == "-h" {
        print!("{HELP}");
        return Ok(());
    }
    if command == "--version" {
        println!(
            "tearoom version={} commit={} store-format=4",
            option_env!("TEAROOM_BUILD_VERSION").unwrap_or("dev"),
            option_env!("TEAROOM_BUILD_COMMIT").unwrap_or("unknown")
        );
        return Ok(());
    }
    if command == "skill" {
        let parsed = parse(rest, &[], &[])?;
        if parsed.help("Usage: tearoom skill") {
            return Ok(());
        }
        parsed.no_positional()?;
        print!("{}", include_str!("skill.txt"));
        return Ok(());
    }
    match command.as_str() {
        "room" => rooms::room_command(rest),
        "role" => collaboration::role_command(rest),
        "collab" => collaboration::collab_command(rest),
        "send" => rooms::send_command(rest),
        "read" => rooms::read_command(rest),
        "tree" => rooms::tree_command(rest),
        "watch" => observation::watch_command(rest),
        "server" => observation::server_command(rest),
        _ => bail!("unknown command {command:?}"),
    }
}

#[derive(Default)]
struct Args {
    values: HashMap<String, String>,
    repeated: HashMap<String, Vec<String>>,
    switches: HashSet<String>,
    positional: Vec<String>,
}
fn parse(args: &[String], values: &[&str], switches: &[&str]) -> Result<Args> {
    let mut result = Args::default();
    let mut i = 0;
    let mut literal = false;
    while i < args.len() {
        let arg = &args[i];
        if arg == "--" && !literal {
            literal = true;
            i += 1;
            continue;
        }
        if literal || !arg.starts_with('-') || arg == "-" {
            result.positional.push(arg.clone());
            i += 1;
            continue;
        }
        if arg == "--help" || arg == "-h" {
            result.switches.insert("help".into());
            i += 1;
            continue;
        }
        let word = arg.trim_start_matches('-');
        let (name, inline) = word
            .split_once('=')
            .map_or((word, None), |(n, v)| (n, Some(v)));
        if switches.contains(&name) {
            match inline {
                None | Some("1" | "t" | "T" | "true" | "TRUE" | "True") => {
                    result.switches.insert(name.into());
                }
                Some("0" | "f" | "F" | "false" | "FALSE" | "False") => {
                    result.switches.remove(name);
                }
                Some(value) => bail!("invalid boolean {value:?} for --{name}"),
            }
        } else if values.contains(&name) {
            let value = if let Some(value) = inline {
                value.to_owned()
            } else {
                i += 1;
                args.get(i)
                    .ok_or_else(|| anyhow!("flag needs an argument: --{name}"))?
                    .clone()
            };
            result
                .repeated
                .entry(name.into())
                .or_default()
                .push(value.clone());
            result.values.insert(name.into(), value);
        } else {
            bail!("flag provided but not defined: --{name}");
        }
        i += 1;
    }
    Ok(result)
}
impl Args {
    fn get(&self, name: &str) -> &str {
        self.values.get(name).map_or("", String::as_str)
    }
    fn no_positional(&self) -> Result<()> {
        if !self.positional.is_empty() {
            bail!("unexpected arguments: {}", self.positional.join(" "));
        }
        Ok(())
    }
    fn help(&self, text: &str) -> bool {
        if self.switches.contains("help") {
            println!("{text}");
            true
        } else {
            false
        }
    }
}
fn print_json(value: &impl Serialize) -> Result<()> {
    println!("{}", serde_json::to_string(value)?);
    Ok(())
}
enum Backend {
    Local(Store),
    Remote(Client),
}
impl Backend {
    fn rooms(&self) -> Result<Vec<Room>> {
        match self {
            Self::Local(s) => s.rooms(),
            Self::Remote(s) => s.rooms(),
        }
    }
    fn read(&self, room: &str, after: u64) -> Result<Vec<Record>> {
        match self {
            Self::Local(s) => s.read(room, after),
            Self::Remote(s) => s.read(room, after),
        }
    }
    fn append(&self, room: &str, author: &str, req: AppendRequest<'_>) -> Result<Record> {
        match self {
            Self::Local(s) => s.append(room, author, req),
            Self::Remote(s) => s.append(room, author, req),
        }
    }
    fn root(&self) -> Result<std::path::PathBuf> {
        match self {
            Self::Local(s) => Ok(s.root().to_path_buf()),
            Self::Remote(_) => Store::find(std::env::current_dir()?),
        }
    }
}
fn open() -> Result<Backend> {
    let server = std::env::var("TEAROOM_SERVER").unwrap_or_default();
    if !server.trim().is_empty() {
        return Ok(Backend::Remote(Client::new(
            server.trim().trim_end_matches('/'),
        )?));
    }
    Ok(Backend::Local(Store::open(std::env::current_dir()?)?))
}
fn resolve(store: &Backend, room: &str) -> Result<String> {
    let room = room.trim();
    if !room.is_empty() {
        return Ok(room.into());
    }
    let rooms = store.rooms()?;
    match rooms.as_slice() {
        [room] => Ok(room.name.clone()),
        [] => bail!("no workspace: run 'tearoom room init' first"),
        _ => bail!(
            "this workspace has {} rooms; pass --room (see 'tearoom tree')",
            rooms.len()
        ),
    }
}
