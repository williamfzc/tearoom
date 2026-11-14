//! Durable Room-to-panel dispatch, serialized by the workspace lock.
#[path = "dispatch/routing.rs"]
mod routing;
#[path = "dispatch/task.rs"]
mod task;

use super::runtime::{quote, PanelRequest, PanelStatus, Runtime};
use super::{dispatch_lock, panel_name, Config, DeliveryState, DispatchState, Intent};
use crate::store::Store;
use anyhow::{bail, Context, Result};
use routing::{head_of_queue, mark_reply, record_intent};
use std::collections::HashSet;
use std::path::PathBuf;

/// Mark interrupted work uncertain after its workspace terminals have stopped.
pub fn record_stopped_panels(root: &std::path::Path, stopped: &[String]) -> Result<()> {
    if stopped.is_empty() {
        return Ok(());
    }
    let stopped = stopped.iter().collect::<HashSet<_>>();
    let _lock = dispatch_lock(root)?;
    let mut state = DispatchState::load(root)?;
    for intent in &mut state.intents {
        for delivery in &mut intent.deliveries {
            if !stopped.contains(&delivery.panel) {
                continue;
            }
            if delivery.state == DeliveryState::Submitted {
                delivery.state = DeliveryState::Unknown;
                delivery.error = "workspace stopped; inspect the Room before resolving this interrupted delivery".into();
            }
            state
                .fresh
                .entry(intent.room.clone())
                .or_default()
                .insert(delivery.role.clone());
        }
    }
    state.save(root)
}

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
        let _lock = match dispatch_lock(root) {
            Ok(lock) => lock,
            Err(error)
                if error
                    .downcast_ref::<std::io::Error>()
                    .is_some_and(|e| e.kind() == std::io::ErrorKind::WouldBlock) =>
            {
                return Ok(())
            }
            Err(error) => return Err(error),
        };
        let mut state = DispatchState::load(root)?;
        state.recover_submitting();
        state.save(root)?;
        let config = Config::load(root)?;
        for room in self.store.rooms()? {
            for record in self
                .store
                .read(&room.id, *state.cursor.get(&room.id).unwrap_or(&0))?
            {
                mark_reply(&mut state, &record);
                record_intent(&mut state, &config, root, &record)?;
                state.cursor.insert(room.id.clone(), record.seq);
                state.save(root)?;
            }
        }
        for intent in &mut state.intents {
            for delivery in &mut intent.deliveries {
                if delivery.state == DeliveryState::Submitted {
                    match self.runtime.status(&delivery.panel) {
                        Ok(PanelStatus::Done) => delivery.state = DeliveryState::Completed,
                        Ok(PanelStatus::Missing) => {
                            delivery.state = DeliveryState::Unknown;
                            delivery.error =
                                "role panel disappeared during delivery; inspect before resolving"
                                    .into();
                        }
                        _ => {}
                    }
                }
            }
        }
        state.save(root)?;
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
        if outcome == "delivered" {
            if let Some(roles) = state.fresh.get_mut(room) {
                roles.remove(role);
                if roles.is_empty() {
                    state.fresh.remove(room);
                }
            }
        }
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
        let previous = state.intents[..i]
            .iter()
            .filter(|prior| prior.room == room)
            .filter(|prior| {
                prior.deliveries.iter().any(|d| {
                    d.role == role_name
                        && matches!(d.state, DeliveryState::Submitted | DeliveryState::Completed)
                })
            })
            .map(|prior| prior.seq)
            .max()
            .unwrap_or(0);
        let fresh = state
            .fresh
            .get(&room)
            .is_some_and(|roles| roles.contains(&role_name));
        if status == PanelStatus::Missing && previous > 0 && !fresh {
            state.intents[i].deliveries[j].state = DeliveryState::Unknown;
            state.intents[i].deliveries[j].error =
                "role panel disappeared; inspect and explicitly restart before retry".into();
            return Ok(());
        }
        let after = if status == PanelStatus::Missing || fresh {
            0
        } else {
            previous
        };
        let records: Vec<_> = self
            .store
            .read(&room, after)?
            .into_iter()
            .filter(|record| record.seq <= seq)
            .collect();
        let history = serde_json::to_string(&records)?;
        let card = self.store.rooms()?.into_iter().find(|card| card.id == room);
        let task = task::compose(card.as_ref(), &room, &role_name, seq, &history, &cli);
        let task = if after == 0 {
            format!("{}\n\n{task}", role.instructions).trim().to_owned()
        } else {
            task
        };
        let dir = role.directory(root)?;
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
                if let Some(roles) = state.fresh.get_mut(&room) {
                    roles.remove(&role_name);
                    if roles.is_empty() {
                        state.fresh.remove(&room);
                    }
                }
            }
            Ok(_) => {
                delivery.state = DeliveryState::Unknown;
                delivery.error = "hc did not confirm that the task started".into();
            }
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::super::runtime::SubmitResult;
    use super::super::{Delivery, Message, RecordRef, Role, RoomRoles};
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
        fn launch(&self, _: &str, _: &str, _: &str, _: &Role, _: &std::path::Path) -> Result<()> {
            unreachable!("dispatch tests reach submit through an idle panel")
        }
        fn stop(&self, _: &str) -> Result<()> {
            unreachable!("dispatch tests do not stop panels")
        }
        fn ensure(&self, _: &PanelRequest<'_>) -> Result<SubmitResult> {
            unreachable!("dispatch tests reach submit through an idle panel")
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
    fn stopping_panels_marks_submitted_unknown_and_only_their_roles_fresh() {
        let (_temp, dispatcher) = setup(false);
        let root = dispatcher.store.root();
        let panel = panel_name(root, "project", "reviewer").unwrap();
        let other = panel_name(root, "other", "reviewer").unwrap();
        DispatchState {
            version: 1,
            cursor: Default::default(),
            fresh: Default::default(),
            intents: vec![
                Intent {
                    room: "project".into(),
                    seq: 1,
                    text: "in flight".into(),
                    deliveries: vec![Delivery {
                        role: "reviewer".into(),
                        panel: panel.clone(),
                        state: DeliveryState::Submitted,
                        error: String::new(),
                    }],
                },
                Intent {
                    room: "project".into(),
                    seq: 2,
                    text: "already uncertain".into(),
                    deliveries: vec![Delivery {
                        role: "reviewer".into(),
                        panel: panel.clone(),
                        state: DeliveryState::Unknown,
                        error: "keep this confirmation".into(),
                    }],
                },
                Intent {
                    room: "other".into(),
                    seq: 1,
                    text: "unrelated".into(),
                    deliveries: vec![Delivery {
                        role: "reviewer".into(),
                        panel: other,
                        state: DeliveryState::Submitted,
                        error: String::new(),
                    }],
                },
            ],
        }
        .save(root)
        .unwrap();

        record_stopped_panels(root, &[panel]).unwrap();
        let state = DispatchState::load(root).unwrap();
        assert_eq!(state.intents[0].deliveries[0].state, DeliveryState::Unknown);
        assert!(state.intents[0].deliveries[0]
            .error
            .contains("workspace stopped"));
        assert_eq!(
            state.intents[1].deliveries[0].error,
            "keep this confirmation"
        );
        assert_eq!(
            state.intents[2].deliveries[0].state,
            DeliveryState::Submitted
        );
        assert!(state.fresh["project"].contains("reviewer"));
        assert!(!state.fresh.contains_key("other"));
    }

    #[test]
    fn lock_errors_other_than_contention_are_not_ignored() {
        let (_temp, dispatcher) = setup(false);
        std::fs::create_dir(dispatcher.store.root().join(".dispatch.lock")).unwrap();
        assert!(dispatcher.pump().is_err());
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
            fn launch(
                &self,
                _: &str,
                _: &str,
                _: &str,
                _: &Role,
                _: &std::path::Path,
            ) -> Result<()> {
                unreachable!("the trace overrides ensure")
            }
            fn stop(&self, _: &str) -> Result<()> {
                unreachable!("dispatch tests do not stop panels")
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
                assert!(prompt.starts_with(
                    "Review only; never edit.\n\nRoom project, addressed to reviewer"
                ));
                assert!(prompt.contains("review"));
                assert_eq!(prompt.matches("Review only; never edit.").count(), 1);
            }
            if initial == PanelStatus::Missing {
                assert_eq!(prompts[0], prompts[1]);
            }
        }
    }

    #[test]
    fn the_room_description_rides_along_in_dispatch_prompts() {
        struct Trace {
            prompts: RefCell<Vec<String>>,
        }
        impl Runtime for Trace {
            fn status(&self, _: &str) -> Result<PanelStatus> {
                Ok(PanelStatus::Idle)
            }
            fn launch(
                &self,
                _: &str,
                _: &str,
                _: &str,
                _: &Role,
                _: &std::path::Path,
            ) -> Result<()> {
                unreachable!()
            }
            fn stop(&self, _: &str) -> Result<()> {
                unreachable!()
            }
            fn ensure(&self, _: &PanelRequest<'_>) -> Result<SubmitResult> {
                unreachable!()
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
        let (_temp, base) = setup(false);
        base.store
            .update_room("project", None, Some("review payments changes"))
            .unwrap();
        let seq = append(
            &base.store,
            "builder",
            &Message {
                text: "review".into(),
                mentions: vec!["reviewer".into()],
                reply_to: None,
            },
        );
        let dispatcher = Dispatcher {
            store: base.store,
            runtime: Trace {
                prompts: RefCell::new(vec![]),
            },
        };
        dispatcher.pump().unwrap();
        let prompts = dispatcher.runtime.prompts.borrow();
        assert_eq!(prompts.len(), 1);
        assert!(
            prompts[0].contains("Room project (review payments changes), addressed to reviewer"),
            "{:?}",
            prompts[0]
        );
        assert!(prompts[0].contains(&format!("record {seq}")));
    }

    #[test]
    fn done_releases_queue_and_delivers_only_new_records() {
        struct Trace {
            status: Cell<PanelStatus>,
            prompts: RefCell<Vec<String>>,
        }
        impl Runtime for Trace {
            fn status(&self, _: &str) -> Result<PanelStatus> {
                Ok(self.status.get())
            }
            fn launch(
                &self,
                _: &str,
                _: &str,
                _: &str,
                _: &Role,
                _: &std::path::Path,
            ) -> Result<()> {
                unreachable!()
            }
            fn stop(&self, _: &str) -> Result<()> {
                unreachable!()
            }
            fn ensure(&self, _: &PanelRequest<'_>) -> Result<SubmitResult> {
                unreachable!()
            }
            fn submit(&self, _: &str, task: &str, _: PanelStatus) -> Result<SubmitResult> {
                self.prompts.borrow_mut().push(task.to_owned());
                self.status.set(PanelStatus::Working);
                Ok(SubmitResult {
                    accepted: true,
                    started: Some(true),
                    pending: false,
                })
            }
        }
        let (_temp, base) = setup(false);
        let dispatcher = Dispatcher {
            store: base.store,
            runtime: Trace {
                status: Cell::new(PanelStatus::Idle),
                prompts: RefCell::new(vec![]),
            },
        };
        let addressed = |text: &str| Message {
            text: text.into(),
            mentions: vec!["reviewer".into()],
            reply_to: None,
        };
        append(&dispatcher.store, "builder", &addressed("first task"));
        dispatcher.pump().unwrap();
        append(
            &dispatcher.store,
            "human",
            &Message {
                text: "shared update".into(),
                ..Message::default()
            },
        );
        append(&dispatcher.store, "builder", &addressed("second task"));
        dispatcher.pump().unwrap();
        assert_eq!(dispatcher.runtime.prompts.borrow().len(), 1);
        dispatcher.runtime.status.set(PanelStatus::Done);
        dispatcher.pump().unwrap();
        let prompts = dispatcher.runtime.prompts.borrow();
        assert_eq!(prompts.len(), 2);
        assert!(prompts[0].contains("first task"));
        assert!(!prompts[1].contains("first task"));
        assert!(prompts[1].contains("shared update"));
        assert!(prompts[1].contains("second task"));
        drop(prompts);
        let mut state = DispatchState::load(dispatcher.store.root()).unwrap();
        state
            .fresh
            .entry("project".into())
            .or_default()
            .insert("reviewer".into());
        state.save(dispatcher.store.root()).unwrap();
        append(&dispatcher.store, "builder", &addressed("after restart"));
        dispatcher.runtime.status.set(PanelStatus::Done);
        dispatcher.pump().unwrap();
        let prompts = dispatcher.runtime.prompts.borrow();
        assert!(prompts[2].contains("first task"));
        assert!(prompts[2].contains("after restart"));
        assert!(DispatchState::load(dispatcher.store.root())
            .unwrap()
            .fresh
            .is_empty());
    }
}
