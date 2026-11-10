//! Dispatcher commands at the CLI boundary.
use super::{hc_runtime, parse, print_json};
use crate::collab::dispatch::Dispatcher;
use crate::collab::runtime::{PanelStatus, Runtime};
use crate::collab::{dispatch_lock, panel_name, Config, DeliveryState, DispatchState};
use crate::store::Store;
use anyhow::{anyhow, bail, Context, Result};
use fs2::FileExt;
use std::fs::OpenOptions;
use std::time::Duration;

pub(super) fn collab_command(args: &[String]) -> Result<()> {
    let Some((action, rest)) = args.split_first() else {
        println!("Usage: tearoom collab <run|deliveries|resolve|restart>");
        return Ok(());
    };
    if action == "--help" || action == "-h" {
        println!("Usage: tearoom collab <run|deliveries|resolve|restart>");
        return Ok(());
    }
    let args = match action.as_str() {
        "run" | "deliveries" => parse(rest, &[], &[])?,
        "resolve" => parse(rest, &["room", "seq", "role", "outcome"], &["confirm"])?,
        "restart" => parse(rest, &["room", "role"], &["confirm"])?,
        _ => bail!("unknown collab command {action:?}"),
    };
    if args.help("Usage: tearoom collab run\n       tearoom collab deliveries\n       tearoom collab resolve --room <room> --seq <seq> --role <role> --outcome <delivered|skip|retry> [--confirm]\n       tearoom collab restart --room <room> --role <role> [--confirm]") {
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
        "run" => {
            let owner = OpenOptions::new()
                .create(true)
                .truncate(false)
                .write(true)
                .open(dispatcher.store.root().join(".dispatcher.lock"))?;
            owner
                .try_lock_exclusive()
                .context("another Room dispatcher is running")?;
            loop {
                dispatcher.pump()?;
                let intents = dispatcher.deliveries()?;
                crate::collab::herdr::enqueue(dispatcher.store.root(), &intents);
                std::thread::sleep(Duration::from_millis(500));
            }
        }
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
        "restart" => {
            let (room, role) = (args.get("room"), args.get("role"));
            if room.is_empty() || role.is_empty() {
                bail!("--room and --role are required");
            }
            restart(&dispatcher, room, role, args.switches.contains("confirm"))
        }
        _ => bail!("unknown collab command {action:?}"),
    }
}

/// Replace one Room-role panel without claiming its Agent conversation survived.
fn restart<R: Runtime>(
    dispatcher: &Dispatcher<R>,
    room: &str,
    role_name: &str,
    confirm: bool,
) -> Result<()> {
    let root = dispatcher.store.root();
    let _lock = dispatch_lock(root)?;
    let config = Config::load(root)?;
    let role = config
        .roles
        .get(role_name)
        .with_context(|| format!("unknown role {role_name:?}"))?;
    let bound = config
        .rooms
        .get(room)
        .is_some_and(|rooms| rooms.roles.iter().any(|bound| bound == role_name));
    if !bound {
        bail!("role {role_name:?} is not configured in room {room:?}");
    }
    let panel = panel_name(root, room, role_name)?;
    let status = dispatcher.runtime.status(&panel)?;
    if status == PanelStatus::Working {
        bail!("panel {panel} is working; wait for its turn to end, or stop it directly with hc kill {panel}");
    }
    let waiting: Vec<String> = DispatchState::load(root)?
        .intents
        .iter()
        .filter(|intent| intent.room == room)
        .filter(|intent| {
            intent.deliveries.iter().any(|delivery| {
                delivery.role == role_name
                    && matches!(
                        delivery.state,
                        DeliveryState::Submitted | DeliveryState::Unknown
                    )
            })
        })
        .map(|intent| format!("{room}#{}", intent.seq))
        .collect();
    if !waiting.is_empty() && !confirm {
        bail!("delivery {} is submitted or uncertain; inspect it, then pass --confirm to replace the panel", waiting.join(", "));
    }
    if status != PanelStatus::Missing {
        dispatcher.runtime.stop(&panel)?;
    }
    dispatcher
        .runtime
        .launch(&panel, room, role_name, role, &role.directory(root)?)?;
    println!(
        "restarted {panel} ({})",
        dispatcher.runtime.status(&panel)?.as_str()
    );
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::collab::runtime::{PanelRequest, SubmitResult};
    use crate::collab::{Delivery, Intent, Role, RoomRoles};
    use std::cell::RefCell;
    use std::path::Path;

    struct Recorder {
        status: PanelStatus,
        calls: RefCell<Vec<String>>,
    }

    impl Runtime for Recorder {
        fn status(&self, _: &str) -> Result<PanelStatus> {
            Ok(self.status)
        }
        fn launch(&self, name: &str, _: &str, _: &str, _: &Role, _: &Path) -> Result<()> {
            self.calls.borrow_mut().push(format!("launch {name}"));
            Ok(())
        }
        fn stop(&self, panel: &str) -> Result<()> {
            self.calls.borrow_mut().push(format!("stop {panel}"));
            Ok(())
        }
        fn ensure(&self, _: &PanelRequest<'_>) -> Result<SubmitResult> {
            unreachable!("restart never submits a task")
        }
        fn submit(&self, _: &str, _: &str, _: PanelStatus) -> Result<SubmitResult> {
            unreachable!("restart never submits a task")
        }
    }

    fn setup(status: PanelStatus) -> (tempfile::TempDir, Dispatcher<Recorder>) {
        let temp = tempfile::tempdir().unwrap();
        let store = Store::init(temp.path()).unwrap();
        store.create_room("project", "").unwrap();
        Config::update(store.root(), |config| {
            config.roles.insert(
                "reviewer".into(),
                Role {
                    agent_type: "pi".into(),
                    command: vec!["pi".into()],
                    ..Role::default()
                },
            );
            config.roles.insert(
                "builder".into(),
                Role {
                    agent_type: "pi".into(),
                    command: vec!["pi".into()],
                    ..Role::default()
                },
            );
            config.rooms.insert(
                "project".into(),
                RoomRoles {
                    roles: vec!["reviewer".into()],
                },
            );
            Ok(())
        })
        .unwrap();
        let runtime = Recorder {
            status,
            calls: RefCell::new(Vec::new()),
        };
        (temp, Dispatcher { store, runtime })
    }

    fn record_delivery(dispatcher: &Dispatcher<Recorder>, state: DeliveryState) {
        let panel = panel_name(dispatcher.store.root(), "project", "reviewer").unwrap();
        DispatchState {
            version: 1,
            cursor: Default::default(),
            intents: vec![Intent {
                room: "project".into(),
                seq: 1,
                text: "review".into(),
                deliveries: vec![Delivery {
                    role: "reviewer".into(),
                    panel,
                    state,
                    error: String::new(),
                }],
            }],
        }
        .save(dispatcher.store.root())
        .unwrap();
    }

    #[test]
    fn a_working_panel_is_not_replaced() {
        let (_temp, dispatcher) = setup(PanelStatus::Working);
        let error = restart(&dispatcher, "project", "reviewer", false)
            .unwrap_err()
            .to_string();
        assert!(error.contains("working"), "{error}");
        assert!(dispatcher.runtime.calls.borrow().is_empty());
    }

    #[test]
    fn an_unreconciled_delivery_needs_confirmation() {
        let (_temp, dispatcher) = setup(PanelStatus::Idle);
        record_delivery(&dispatcher, DeliveryState::Submitted);
        let error = restart(&dispatcher, "project", "reviewer", false)
            .unwrap_err()
            .to_string();
        assert!(error.contains("project#1"), "{error}");
        assert!(error.contains("--confirm"), "{error}");
        assert!(dispatcher.runtime.calls.borrow().is_empty());
        restart(&dispatcher, "project", "reviewer", true).unwrap();
        assert_eq!(dispatcher.runtime.calls.borrow().len(), 2);
    }

    #[test]
    fn a_missing_panel_is_started_without_stopping() {
        let (_temp, dispatcher) = setup(PanelStatus::Missing);
        restart(&dispatcher, "project", "reviewer", false).unwrap();
        let panel = panel_name(dispatcher.store.root(), "project", "reviewer").unwrap();
        assert_eq!(
            *dispatcher.runtime.calls.borrow(),
            vec![format!("launch {panel}")]
        );
    }

    #[test]
    fn a_role_outside_the_room_is_refused() {
        let (_temp, dispatcher) = setup(PanelStatus::Idle);
        let error = restart(&dispatcher, "project", "builder", false)
            .unwrap_err()
            .to_string();
        assert!(error.contains("not configured"), "{error}");
        assert!(dispatcher.runtime.calls.borrow().is_empty());
    }
}
