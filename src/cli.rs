//! CLI entry point, argument parsing and shared workspace access.
mod collaboration;
mod roles;
mod room_delete;
mod room_herdr;
mod rooms;
mod service;
mod web;

use crate::collab::runtime::{workspace_label, HcRuntime};
use crate::collab::{Config, Message, RecordRef};
use crate::core::{AppendRequest, Record};
use crate::hc::HcClient;
use crate::mentions;
use crate::store::Store;
use anyhow::{anyhow, bail, Result};
use serde::Serialize;
use std::collections::{HashMap, HashSet};
use std::path::Path;
use std::time::Duration;

const HELP: &str = "tearoom — a durable room tree for agents\n\nUsage: tearoom <command> [flags]\n\nEveryday work:\n  room      create or organize rooms and collaborators\n  send      write a message to a room\n  read      read a room or subtree\n  tree      see how rooms are organized\n  web       open the local browser interface\n\nSetup and recovery:\n  role      configure reusable collaborators\n  collab    run or inspect delivery\n  hc        inspect or manage embedded terminals\n  skill     print the agent guide\n\nOptions:\n  --version show build and store format information\n\nEnvironment:\n  TEAROOM_ACTOR   default signature for --as\n\nUse \"tearoom <command> --help\" for more information.\n";

pub fn run() -> i32 {
    let args: Vec<String> = std::env::args().skip(1).collect();
    match execute(&args) {
        Ok(()) => 0,
        Err(error) => {
            eprintln!("{error:#}");
            let detail = format!("{error:#}");
            if detail.contains("no workspace") {
                4
            } else {
                1
            }
        }
    }
}
fn print_help() {
    print!("{HELP}");
    println!("  service   inspect or stop the local collaboration service");
}
fn execute(args: &[String]) -> Result<()> {
    let Some((command, rest)) = args.split_first() else {
        print_help();
        return Ok(());
    };
    if command == "--help" || command == "-h" {
        print_help();
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
        let parsed = parse(rest, &[], &["pi-extension"])?;
        if parsed.help("Usage: tearoom skill [--pi-extension]\n\nPrint the Agent guide, or print the Pi lifecycle extension with --pi-extension.") {
            return Ok(());
        }
        parsed.no_positional()?;
        if parsed.switches.contains("pi-extension") {
            print!("{}", include_str!("../scripts/pi-hc-status.ts"));
        } else {
            print!("{}", include_str!("skill.txt"));
        }
        return Ok(());
    }
    match command.as_str() {
        "room" => rooms::room_command(rest),
        "role" => roles::role_command(rest),
        "collab" => collaboration::collab_command(rest),
        "service" => service::service_command(rest),
        "__service" => service::run_service(),
        "send" => rooms::send_command(rest),
        "read" => rooms::read_command(rest),
        "tree" => rooms::tree_command(rest),
        "web" => web::web_command(rest),
        _ => bail!("unknown command {command:?}"),
    }
}

fn hc_runtime() -> Result<HcRuntime> {
    let store = Store::open(std::env::current_dir()?)?;
    Ok(HcRuntime {
        client: HcClient::from_env()?,
        timeout: Duration::from_secs(30),
        workspace_label: Some(workspace_label(store.root())?),
        caller: None,
    })
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
    fn argv(&self, program: &str, extra: &str) -> Vec<String> {
        let extra = self.repeated.get(extra).cloned().unwrap_or_default();
        std::iter::once(self.get(program).to_owned())
            .chain(extra)
            .collect()
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

pub(super) fn activate_addressed(record: &Record, root: &Path) -> Option<String> {
    serde_json::from_str::<Message>(record.content.get())
        .ok()
        .filter(|message| !message.mentions.is_empty())
        .and_then(|_| service::activate(root).err())
        .map(|error| format!("record saved, but background collaboration did not start: {error:#}"))
}
fn open() -> Result<Store> {
    Store::open(std::env::current_dir()?)
}
fn resolve(store: &Store, room: &str) -> Result<String> {
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

pub(super) fn boundary(value: &str) -> Result<u64> {
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

fn signature(explicit: &str) -> String {
    if explicit.trim().is_empty() {
        std::env::var("TEAROOM_ACTOR")
            .unwrap_or_default()
            .trim()
            .to_owned()
    } else {
        explicit.trim().into()
    }
}

fn escape_content(json: String) -> String {
    json.replace('&', "\\u0026")
        .replace('<', "\\u003c")
        .replace('>', "\\u003e")
        .replace('\u{2028}', "\\u2028")
        .replace('\u{2029}', "\\u2029")
}

/// Append one text message, resolving explicit or body addresses against the
/// Room roster. The CLI and the local browser face share this path so message
/// encoding and roster rules have one owner.
pub(super) fn append_text(
    store: &Store,
    room: &str,
    author: &str,
    text: &str,
    to: &str,
    reply_to: Option<RecordRef>,
    idempotency_key: &str,
) -> Result<Record> {
    Config::with_lock(store.root(), |config| {
        let roster = config.rooms.get(room).map(|binding| binding.roles.clone());
        let names = mentions::resolve(text, to, room, roster.as_deref()).map_err(|e| anyhow!(e))?;
        let json = if names.is_empty() && reply_to.is_none() {
            serde_json::to_string(text)?
        } else {
            serde_json::to_string(&Message {
                text: text.into(),
                mentions: names,
                reply_to,
            })?
        };
        let content = escape_content(json);
        store.append(
            room,
            author,
            AppendRequest {
                content: &content,
                idempotency_key,
            },
        )
    })
}
