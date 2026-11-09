//! Durable Room-to-panel dispatch, serialized by the workspace lock.
use super::runtime::{quote, PanelRequest, PanelStatus, Runtime};
use super::{
    dispatch_lock, panel_name, Config, Delivery, DeliveryState, DispatchState, Intent, Message,
};
use crate::core::Record;
use crate::store::Store;
use anyhow::{bail, Context, Result};
use std::collections::BTreeSet;
use std::path::PathBuf;

pub struct Dispatcher<R> {
    pub store: Store,
    pub runtime: R,
}

impl<R: Runtime> Dispatcher<R> {
    pub fn deliveries(&self) -> Result<Vec<Intent>> {
        Ok(DispatchState::load(self.store.root())?.intents)
    }

    pub fn pump(&self) -> Result<()> {
        let root = self.store.root();
        let _lock = dispatch_lock(root)?;
        let mut state = DispatchState::load(root)?;
        state.recover_submitting();
        state.save(root)?;
        let config = Config::load(root)?;
        for room in self.store.rooms()? {
            for record in self
                .store
                .read(&room.name, *state.cursor.get(&room.name).unwrap_or(&0))?
            {
                mark_reply(&mut state, &record);
                record_intent(&mut state, &config, root, &record)?;
                state.cursor.insert(room.name.clone(), record.seq);
                state.save(root)?;
            }
        }
        for i in 0..state.intents.len() {
            for j in 0..state.intents[i].deliveries.len() {
                if !head_of_queue(&state, i, j)
                    || state.intents[i].deliveries[j].state != DeliveryState::Pending
                {
                    continue;
                }
                self.deliver(&config, &mut state, i, j)?;
                state.save(root)?;
            }
        }
        Ok(())
    }

    pub fn resolve(
        &self,
        room: &str,
        seq: u64,
        role: &str,
        outcome: &str,
        confirm: bool,
    ) -> Result<()> {
        let root = self.store.root();
        let _lock = dispatch_lock(root)?;
        let mut state = DispatchState::load(root)?;
        let delivery = state
            .intents
            .iter_mut()
            .find(|i| i.room == room && i.seq == seq)
            .and_then(|intent| intent.deliveries.iter_mut().find(|d| d.role == role))
            .context("delivery not found")?;
        if delivery.state != DeliveryState::Unknown {
            bail!("only unknown deliveries can be resolved");
        }
        delivery.state = match outcome {
            "delivered" => DeliveryState::Submitted,
            "skip" => DeliveryState::Skipped,
            "retry" => {
                if !confirm {
                    bail!("retry may duplicate a delivered action; pass --confirm after inspecting the panel and Room");
                }
                delivery.panel = panel_name(root, room, role)?;
                DeliveryState::Pending
            }
            _ => bail!("unknown resolution {outcome:?} (expected delivered, skip, or retry)"),
        };
        delivery.error.clear();
        state.save(root)
    }

    fn deliver(
        &self,
        config: &Config,
        state: &mut DispatchState,
        i: usize,
        j: usize,
    ) -> Result<()> {
        let root = self.store.root();
        let intent = &state.intents[i];
        let delivery = &intent.deliveries[j];
        if delivery.panel != panel_name(root, &intent.room, &delivery.role)? {
            state.intents[i].deliveries[j].state = DeliveryState::Unknown;
            state.intents[i].deliveries[j].error = "stored panel predates workspace-scoped identity; inspect it, then explicitly resolve before retry".into();
            return Ok(());
        }
        let status = match self.runtime.status(&delivery.panel) {
            Ok(status) => status,
            Err(error) => {
                state.intents[i].deliveries[j].state = DeliveryState::Unknown;
                state.intents[i].deliveries[j].error = error.to_string();
                return Ok(());
            }
        };
        match status {
            PanelStatus::Working | PanelStatus::Blocked | PanelStatus::Unknown => {
                state.intents[i].deliveries[j].error = match status {
                    PanelStatus::Working => "panel is working; task remains queued",
                    PanelStatus::Blocked => {
                        "panel is blocked; inspect it before resolving delivery"
                    }
                    _ => "panel has not reported idle; task remains queued",
                }
                .into();
                return Ok(());
            }
            _ => {}
        }
        let room = intent.room.clone();
        let seq = intent.seq;
        let panel = delivery.panel.clone();
        let role_name = delivery.role.clone();
        let role = match config.roles.get(&role_name) {
            Some(role) => role,
            None => {
                state.intents[i].deliveries[j].state = DeliveryState::Unknown;
                state.intents[i].deliveries[j].error = "target role definition is missing".into();
                return Ok(());
            }
        };
        let executable = std::env::current_exe().unwrap_or_else(|_| PathBuf::from("tearoom"));
        let cli = quote(&executable.to_string_lossy());
        let room_arg = quote(&room);
        let task = format!("You are the {role_name} role only. Do not perform, impersonate, or write under any other role, and do not review your own work. After handing off to another role, end your turn and wait. Use the Tearoom CLI at {cli}. Read Room {room_arg} with {cli} read --room {room_arg}. Handle record e{seq} there. Write your result or blocker into that same Room with --as {role_name} and --reply-to {room_arg}#{seq} (for example: {cli} send --room {room_arg} --as {role_name} --reply-to {room_arg}#{seq} --text 'done: ...'). The author label is a collaboration convention, not authenticated identity.");
        let task = format!("{}\n\n{task}", role.instructions).trim().to_owned();
        let workspace = std::fs::canonicalize(root.parent().context("workspace has no parent")?)?;
        let dir = if role.directory.is_empty() {
            workspace
        } else {
            let path = PathBuf::from(&role.directory);
            if path.is_absolute() {
                path
            } else {
                workspace.join(path)
            }
        };
        state.intents[i].deliveries[j].state = DeliveryState::Submitting;
        state.intents[i].deliveries[j].error.clear();
        state.save(root)?; // Durable before any hc side effect.
        let result = if status == PanelStatus::Missing {
            self.runtime.ensure(&PanelRequest {
                name: &panel,
                room: &room,
                role_name: &role_name,
                role,
                directory: &dir,
                task: &task,
            })
        } else {
            self.runtime.submit(&panel, &task, status)
        };
        let delivery = &mut state.intents[i].deliveries[j];
        match result {
            Err(error) => {
                delivery.state = DeliveryState::Unknown;
                delivery.error = error.to_string();
            }
            Ok(result) if result.pending => {
                delivery.state = DeliveryState::Pending;
                delivery.error = "panel not ready; task remains queued".into();
            }
            Ok(result) if result.accepted && result.started != Some(false) => {
                delivery.state = DeliveryState::Submitted;
                delivery.error.clear();
            }
            Ok(_) => {
                delivery.state = DeliveryState::Unknown;
                delivery.error = "hc did not confirm that the task started".into();
            }
        }
        Ok(())
    }
}

fn mark_reply(state: &mut DispatchState, record: &Record) {
    let Ok(message) = serde_json::from_str::<Message>(record.content.get()) else {
        return;
    };
    let Some(reply) = message.reply_to else {
        return;
    };
    if record.author.is_empty() || record.room != reply.room {
        return;
    }
    if let Some(delivery) = state
        .intents
        .iter_mut()
        .find(|i| i.room == reply.room && i.seq == reply.seq)
        .and_then(|i| {
            i.deliveries
                .iter_mut()
                .find(|d| d.role == record.author && d.state == DeliveryState::Submitted)
        })
    {
        delivery.state = DeliveryState::Completed;
        delivery.error.clear();
    }
}

fn record_intent(
    state: &mut DispatchState,
    config: &Config,
    root: &std::path::Path,
    record: &Record,
) -> Result<()> {
    let Ok(message) = serde_json::from_str::<Message>(record.content.get()) else {
        return Ok(());
    };
    if message.text.is_empty()
        || message.mentions.is_empty()
        || state
            .intents
            .iter()
            .any(|i| i.room == record.room && i.seq == record.seq)
    {
        return Ok(());
    }
    let mut deliveries = Vec::new();
    for role in message
        .mentions
        .into_iter()
        .filter(|s| !s.is_empty())
        .collect::<BTreeSet<_>>()
    {
        let allowed = config
            .rooms
            .get(&record.room)
            .is_some_and(|r| r.roles.contains(&role));
        let error = if !allowed {
            "target role is not configured in this Room"
        } else if !config.roles.contains_key(&role) {
            "target role definition is missing"
        } else {
            ""
        };
        deliveries.push(Delivery {
            panel: panel_name(root, &record.room, &role)?,
            role,
            state: if error.is_empty() {
                DeliveryState::Pending
            } else {
                DeliveryState::Unknown
            },
            error: error.into(),
        });
    }
    if !deliveries.is_empty() {
        state.intents.push(Intent {
            room: record.room.clone(),
            seq: record.seq,
            text: message.text,
            deliveries,
        });
    }
    Ok(())
}

fn head_of_queue(state: &DispatchState, i: usize, j: usize) -> bool {
    let current = &state.intents[i];
    let role = &current.deliveries[j].role;
    !state.intents[..i]
        .iter()
        .filter(|prior| prior.room == current.room && prior.seq < current.seq)
        .flat_map(|prior| &prior.deliveries)
        .any(|prior| {
            &prior.role == role
                && !matches!(
                    prior.state,
                    DeliveryState::Completed | DeliveryState::Skipped
                )
        })
}

#[cfg(test)]
mod tests {
    use super::super::runtime::SubmitResult;
    use super::super::{RecordRef, Role, RoomRoles};
    use super::*;
    use crate::core::AppendRequest;
    use std::cell::{Cell, RefCell};

    struct Fake {
        root: PathBuf,
        sends: Cell<usize>,
        fail: bool,
    }
    impl Runtime for Fake {
        fn status(&self, _: &str) -> Result<PanelStatus> {
            Ok(PanelStatus::Idle)
        }
        fn ensure(&self, _: &PanelRequest<'_>) -> Result<SubmitResult> {
            unreachable!()
        }
        fn submit(&self, _: &str, _: &str, _: PanelStatus) -> Result<SubmitResult> {
            assert_eq!(
                DispatchState::load(&self.root)?.intents[0].deliveries[0].state,
                DeliveryState::Submitting
            );
            self.sends.set(self.sends.get() + 1);
            if self.fail {
                bail!("transport disconnected after submission");
            }
            Ok(SubmitResult {
                accepted: true,
                started: Some(true),
                pending: false,
            })
        }
    }

    fn append(store: &Store, author: &str, message: &Message) -> u64 {
        let content = serde_json::to_string(message).unwrap();
        store
            .append(
                "project",
                author,
                AppendRequest {
                    content: &content,
                    idempotency_key: "",
                },
            )
            .unwrap()
            .seq
    }

    fn setup(fail: bool) -> (tempfile::TempDir, Dispatcher<Fake>) {
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
            config.rooms.insert(
                "project".into(),
                RoomRoles {
                    roles: vec!["reviewer".into()],
                },
            );
            Ok(())
        })
        .unwrap();
        let runtime = Fake {
            root: store.root().into(),
            sends: Cell::new(0),
            fail,
        };
        (temp, Dispatcher { store, runtime })
    }

    #[test]
    fn submits_once_and_completes_on_a_later_reply() {
        let (_temp, dispatcher) = setup(false);
        let seq = append(
            &dispatcher.store,
            "builder",
            &Message {
                text: "review".into(),
                mentions: vec!["reviewer".into()],
                reply_to: None,
            },
        );
        dispatcher.pump().unwrap();
        assert_eq!(dispatcher.runtime.sends.get(), 1);
        assert_eq!(
            dispatcher.deliveries().unwrap()[0].deliveries[0].state,
            DeliveryState::Submitted
        );
        dispatcher.pump().unwrap();
        assert_eq!(dispatcher.runtime.sends.get(), 1);
        append(
            &dispatcher.store,
            "reviewer",
            &Message {
                text: "done".into(),
                mentions: vec![],
                reply_to: Some(RecordRef {
                    room: "project".into(),
                    seq,
                }),
            },
        );
        dispatcher.pump().unwrap();
        assert_eq!(
            dispatcher.deliveries().unwrap()[0].deliveries[0].state,
            DeliveryState::Completed
        );
    }

    #[test]
    fn ambiguous_submission_is_never_retried_automatically() {
        let (_temp, dispatcher) = setup(true);
        let seq = append(
            &dispatcher.store,
            "builder",
            &Message {
                text: "review".into(),
                mentions: vec!["reviewer".into()],
                reply_to: None,
            },
        );
        dispatcher.pump().unwrap();
        assert_eq!(
            dispatcher.deliveries().unwrap()[0].deliveries[0].state,
            DeliveryState::Unknown
        );
        dispatcher.pump().unwrap();
        assert_eq!(dispatcher.runtime.sends.get(), 1);
        assert!(dispatcher
            .resolve("project", seq, "reviewer", "retry", false)
            .is_err());
        dispatcher
            .resolve("project", seq, "reviewer", "retry", true)
            .unwrap();
        assert_eq!(
            dispatcher.deliveries().unwrap()[0].deliveries[0].state,
            DeliveryState::Pending
        );
    }

    #[test]
    fn role_instructions_reach_new_and_existing_panels_once() {
        struct Trace {
            status: Cell<PanelStatus>,
            prompts: RefCell<Vec<String>>,
        }
        impl Runtime for Trace {
            fn status(&self, _: &str) -> Result<PanelStatus> {
                Ok(self.status.get())
            }
            fn ensure(&self, request: &PanelRequest<'_>) -> Result<SubmitResult> {
                self.prompts.borrow_mut().push(request.task.to_owned());
                self.status.set(PanelStatus::Idle);
                Ok(SubmitResult {
                    pending: true,
                    ..Default::default()
                })
            }
            fn submit(&self, _: &str, task: &str, _: PanelStatus) -> Result<SubmitResult> {
                self.prompts.borrow_mut().push(task.to_owned());
                Ok(SubmitResult {
                    accepted: true,
                    started: Some(true),
                    pending: false,
                })
            }
        }
        for initial in [PanelStatus::Missing, PanelStatus::Idle] {
            let (_temp, existing) = setup(false);
            Config::update(existing.store.root(), |config| {
                config.roles.get_mut("reviewer").unwrap().instructions =
                    "Review only; never edit.".into();
                Ok(())
            })
            .unwrap();
            append(
                &existing.store,
                "builder",
                &Message {
                    text: "review".into(),
                    mentions: vec!["reviewer".into()],
                    reply_to: None,
                },
            );
            let dispatcher = Dispatcher {
                store: existing.store,
                runtime: Trace {
                    status: Cell::new(initial),
                    prompts: RefCell::new(vec![]),
                },
            };
            dispatcher.pump().unwrap();
            if initial == PanelStatus::Missing {
                assert_eq!(
                    dispatcher.deliveries().unwrap()[0].deliveries[0].state,
                    DeliveryState::Pending
                );
                dispatcher.pump().unwrap();
            }
            assert_eq!(
                dispatcher.deliveries().unwrap()[0].deliveries[0].state,
                DeliveryState::Submitted
            );
            let prompts = dispatcher.runtime.prompts.borrow();
            assert_eq!(
                prompts.len(),
                if initial == PanelStatus::Missing {
                    2
                } else {
                    1
                }
            );
            for prompt in prompts.iter() {
                assert!(prompt
                    .starts_with("Review only; never edit.\n\nYou are the reviewer role only."));
                assert_eq!(prompt.matches("Review only; never edit.").count(), 1);
            }
            if initial == PanelStatus::Missing {
                assert_eq!(prompts[0], prompts[1]);
            }
        }
    }
}
