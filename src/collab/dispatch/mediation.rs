//! Wakes the Room's mediator for quiet stops and reported terminal blockages.
//! Each wake is an ordinary addressed Record routed by the dispatcher.
use super::super::runtime::{BlockedPanel, PanelStatus};
use super::super::state::{DeliveryState, DispatchState};
use super::super::{message_content, panel_name, Config, Message};
use crate::core::{AppendRequest, Record};
use crate::store::Store;
use anyhow::Result;
use std::collections::BTreeMap;

pub const RUNTIME_AUTHOR: &str = "tearoom";
const ATTENTION_PREFIX: &str = "Needs attention:";

fn silence_wake_recipient(record: &Record) -> Option<String> {
    if record.author != RUNTIME_AUTHOR {
        return None;
    }
    let mut message = serde_json::from_str::<Message>(record.content.get()).ok()?;
    if message.mentions.len() != 1 || message.text.starts_with(ATTENTION_PREFIX) {
        return None;
    }
    message.mentions.pop().filter(|role| !role.is_empty())
}

fn third_party(record: &Record, mediator: &str) -> bool {
    record.author != RUNTIME_AUTHOR && record.author != mediator
}

/// A stop may be mediated when the previous wake (if any) was followed by
/// records from someone other than the runtime and the woken mediator.
fn rearmed(since_wake: &[Record], woken: bool, mediator: &str) -> bool {
    !woken
        || since_wake
            .iter()
            .any(|record| third_party(record, mediator))
}

/// A Room is quiet when none of its deliveries is pending, submitted, or
/// awaiting inspection; recovering or busy work is not silence.
fn quiet(state: &DispatchState, room: &str) -> bool {
    state
        .intents
        .iter()
        .filter(|intent| intent.room == room)
        .flat_map(|intent| &intent.deliveries)
        .all(|delivery| {
            matches!(
                delivery.state,
                DeliveryState::Completed | DeliveryState::Skipped
            )
        })
}

/// Notify the mediator of reported blockages or a quiet stop.
/// Runs under the dispatch lock. `state.mediated` is the durable progress
/// marker — the seq of the last known wake — so each pass reads only the
/// records from that wake onward; a first pass without a marker scans the Room.
/// The append precedes the marker update, so a crash between them is healed
/// by the next pass reading the already-appended wake.
pub(super) fn mediate(
    config: &Config,
    store: &Store,
    state: &mut DispatchState,
    blocked_panels: impl FnOnce() -> Result<BTreeMap<String, BlockedPanel>>,
    panel_status: impl Fn(&str) -> Result<PanelStatus>,
) -> Result<()> {
    if !config
        .rooms
        .values()
        .any(|room| room.effective_mediator().is_some())
    {
        return Ok(());
    }
    let blocked = blocked_panels()?;
    state
        .attention
        .retain(|panel, _| blocked.contains_key(panel));
    'rooms: for card in store.rooms()? {
        let Some(binding) = config.rooms.get(&card.id) else {
            continue;
        };
        let Some(mediator) = binding.effective_mediator() else {
            continue;
        };
        let mut room_blocked = false;
        if !blocked.is_empty() {
            for role in &binding.roles {
                let panel = panel_name(store.root(), &card.id, role)?;
                if let Some(blockage) = blocked.get(&panel) {
                    room_blocked = true;
                    if role != mediator && state.attention.get(&panel) != Some(&blockage.episode) {
                        let task = state
                            .intents
                            .iter()
                            .find(|intent| {
                                intent.room == card.id
                                    && intent.deliveries.iter().any(|delivery| {
                                        delivery.panel == panel
                                            && delivery.state == DeliveryState::Submitted
                                    })
                            })
                            .map(|intent| intent.seq);
                        request_attention(store, &card.id, role, mediator, blockage, task)?;
                        state.attention.insert(panel, blockage.episode.clone());
                    }
                }
            }
        }
        if room_blocked || !quiet(state, &card.id) {
            continue;
        }
        let last_wake = state.mediated.get(&card.id).copied().unwrap_or(0);
        let mut records = store.read(&card.id, last_wake.saturating_sub(1))?;
        if records.first().is_some_and(|record| {
            record.seq == last_wake && silence_wake_recipient(record).is_none()
        }) {
            // An older build could track a wake addressed to the empty string.
            state.mediated.remove(&card.id);
            records = store.read(&card.id, 0)?;
        }
        let previous = records
            .iter()
            .enumerate()
            .rev()
            .find_map(|(at, record)| silence_wake_recipient(record).map(|role| (at, role)));
        let (since_wake, previous_mediator) = match &previous {
            Some((at, role)) => {
                state.mediated.insert(card.id.clone(), records[*at].seq);
                (&records[at + 1..], role.as_str())
            }
            None => (&records[..], mediator),
        };
        if !rearmed(since_wake, previous.is_some(), previous_mediator) {
            continue;
        }
        let scanned = state.cursor.get(&card.id).copied().unwrap_or_default();
        if records.last().is_some_and(|record| record.seq > scanned) {
            continue;
        }
        for role in &binding.roles {
            let panel = panel_name(store.root(), &card.id, role)?;
            if matches!(
                panel_status(&panel)?,
                PanelStatus::Working | PanelStatus::Blocked
            ) {
                continue 'rooms;
            }
        }
        let seq = scanned + 1;
        let text = format!(
            "Room {} is quiet with no work in flight. Mediate the stop: if the goal is met, \
             record a closing sentence; if work should continue, address the next role with \
             --to; if blocked, record the blocker and stop.",
            card.name
        );
        if let Some(record) = store.append_at_tip(
            &card.id,
            RUNTIME_AUTHOR,
            AppendRequest {
                content: &message_content(&Message {
                    text,
                    mentions: vec![mediator.to_owned()],
                    reply_to: None,
                })?,
                idempotency_key: &format!("silence-{seq}"),
            },
            scanned,
        )? {
            state.mediated.insert(card.id.clone(), record.seq);
        }
    }
    Ok(())
}

fn request_attention(
    store: &Store,
    room: &str,
    role: &str,
    mediator: &str,
    blockage: &BlockedPanel,
    task: Option<u64>,
) -> Result<()> {
    let BlockedPanel {
        address, reason, ..
    } = blockage;
    let task = task.map_or_else(
        || " No submitted Tearoom task was found for this panel.".into(),
        |seq| format!(" Its submitted task is {room}#{seq}."),
    );
    let text = format!(
        "{ATTENTION_PREFIX} role {role} in Room {room} reported a terminal blockage ({reason}).{task} \
         Inspect its status with `tearoom hc get {address}` and its current screen with `tearoom hc read {address}`. \
         If you need recent output, request history with `tearoom hc read {address} --lines 80`. Check the task in the Room before acting. \
         Resolve it if authorized and safe. If a person needs to act, record what they need to do in the Room."
    );
    store.append_event(
        room,
        RUNTIME_AUTHOR,
        AppendRequest {
            content: &message_content(&Message {
                text,
                mentions: vec![mediator.to_owned()],
                reply_to: None,
            })?,
            idempotency_key: &format!("attention-{}-{mediator}", blockage.episode),
        },
    )?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn record(room: &str, seq: u64, author: &str, mentions: &[&str]) -> Record {
        let message = Message {
            text: "x".into(),
            mentions: mentions.iter().map(|role| role.to_string()).collect(),
            reply_to: None,
        };
        Record {
            room: room.into(),
            seq,
            at: "t".into(),
            author: author.into(),
            content: serde_json::value::RawValue::from_string(
                serde_json::to_string(&message).unwrap(),
            )
            .unwrap(),
        }
    }

    #[test]
    fn a_stop_is_armed_until_a_wake_then_waits_for_third_parties() {
        let mediator = "referee";
        assert!(rearmed(&[], false, mediator));
        assert!(rearmed(&[record("r", 1, "builder", &[])], false, mediator));
        // A wake exists; neither silence nor the mediator's own words rearm it.
        assert!(!rearmed(&[], true, mediator));
        assert!(!rearmed(&[record("r", 2, mediator, &[])], true, mediator));
        assert!(!rearmed(
            &[record("r", 2, RUNTIME_AUTHOR, &[mediator])],
            true,
            mediator
        ));
        // A third party speaking after the wake rearms the next stop.
        assert!(rearmed(&[record("r", 2, "builder", &[])], true, mediator));
    }

    #[test]
    fn only_silence_wakes_identify_the_previous_mediator() {
        let mediator = "referee";
        assert_eq!(
            silence_wake_recipient(&record("r", 1, RUNTIME_AUTHOR, &[mediator])).as_deref(),
            Some(mediator)
        );
        assert!(silence_wake_recipient(&record("r", 1, RUNTIME_AUTHOR, &[])).is_none());
        assert!(silence_wake_recipient(&record("r", 1, RUNTIME_AUTHOR, &[""])).is_none());
        assert!(silence_wake_recipient(&record("r", 1, "builder", &[mediator])).is_none());
        assert!(
            silence_wake_recipient(&record("r", 1, RUNTIME_AUTHOR, &[mediator, "other"])).is_none()
        );
        let mut attention = record("r", 1, RUNTIME_AUTHOR, &[mediator]);
        attention.content = serde_json::value::to_raw_value(&Message {
            text: format!("{ATTENTION_PREFIX} role builder"),
            mentions: vec![mediator.to_owned()],
            reply_to: None,
        })
        .unwrap();
        assert!(silence_wake_recipient(&attention).is_none());
    }

    #[test]
    fn quiet_requires_every_delivery_terminal() {
        use crate::collab::state::{Delivery, Intent};
        let mut state = DispatchState::default();
        assert!(quiet(&state, "r"));
        state.intents = vec![Intent {
            room: "r".into(),
            seq: 1,
            text: String::new(),
            deliveries: vec![Delivery {
                role: "referee".into(),
                panel: "p".into(),
                state: DeliveryState::Completed,
                error: String::new(),
            }],
        }];
        assert!(quiet(&state, "r"));
        state.intents[0].deliveries[0].state = DeliveryState::Submitted;
        assert!(!quiet(&state, "r"));
        state.intents[0].deliveries[0].state = DeliveryState::Unknown;
        assert!(!quiet(&state, "r"));
        state.intents[0].deliveries[0].state = DeliveryState::Pending;
        assert!(!quiet(&state, "r"));
        state.intents[0].room = "other".into();
        assert!(quiet(&state, "r"));
    }

    #[test]
    fn mediate_appends_one_wake_per_stop_and_marks_progress() {
        let dir = tempfile::tempdir().unwrap();
        let store = Store::init(dir.path()).unwrap();
        store.create_room("project", "").unwrap();
        let mut config = Config::default();
        config.rooms.insert(
            "project".into(),
            crate::collab::RoomRoles {
                roles: vec!["referee".into()],
                mediator: None,
            },
        );
        let mut state = DispatchState::default();
        let no_blockage = || Ok(BTreeMap::new());
        let idle = |_: &str| Ok(PanelStatus::Idle);
        mediate(&config, &store, &mut state, no_blockage, idle).unwrap();
        let wake = store.read("project", 0).unwrap();
        assert_eq!(wake.len(), 1);
        assert_eq!(wake[0].author, RUNTIME_AUTHOR);
        let message: Message = serde_json::from_str(wake[0].content.get()).unwrap();
        assert_eq!(message.mentions, ["referee"]);
        assert!(message.text.contains("Mediate the stop"));
        assert_eq!(state.mediated.get("project"), Some(&1));
        state.cursor.insert("project".into(), 1);
        // The same quiet state mediates nothing more.
        mediate(&config, &store, &mut state, no_blockage, idle).unwrap();
        assert_eq!(store.read("project", 0).unwrap().len(), 1);
        let binding = config.rooms.get_mut("project").unwrap();
        binding.roles.push("auditor".into());
        mediate(&config, &store, &mut state, no_blockage, idle).unwrap();
        assert_eq!(store.read("project", 0).unwrap().len(), 1);
        // A reply from the role originally woken leaves the stop closed.
        store
            .append(
                "project",
                "referee",
                AppendRequest {
                    content: "\"closed: goal met\"",
                    idempotency_key: "",
                },
            )
            .unwrap();
        state.cursor.insert("project".into(), 2);
        mediate(&config, &store, &mut state, no_blockage, idle).unwrap();
        assert_eq!(store.read("project", 0).unwrap().len(), 2);
        // A third party speaking arms the next stop.
        store
            .append(
                "project",
                "builder",
                AppendRequest {
                    content: "\"new work\"",
                    idempotency_key: "",
                },
            )
            .unwrap();
        state.cursor.insert("project".into(), 3);
        mediate(&config, &store, &mut state, no_blockage, idle).unwrap();
        let records = store.read("project", 0).unwrap();
        assert_eq!(records.len(), 4);
        assert_eq!(records[3].author, RUNTIME_AUTHOR);
        let message: Message = serde_json::from_str(records[3].content.get()).unwrap();
        assert_eq!(message.mentions, ["auditor"]);
        assert_eq!(state.mediated.get("project"), Some(&4));
    }

    #[test]
    fn disabled_mediator_stays_quiet_and_a_blank_wake_does_not_count() {
        let dir = tempfile::tempdir().unwrap();
        let store = Store::init(dir.path()).unwrap();
        store.create_room("project", "").unwrap();
        let mut config = Config::default();
        config.rooms.insert(
            "project".into(),
            crate::collab::RoomRoles {
                roles: vec!["referee".into()],
                mediator: Some(String::new()),
            },
        );
        let mut state = DispatchState::default();
        mediate(
            &config,
            &store,
            &mut state,
            || unreachable!("disabled mediator does not observe panels"),
            |_| unreachable!("disabled mediator does not observe panels"),
        )
        .unwrap();
        assert!(store.read("project", 0).unwrap().is_empty());
        store
            .append(
                "project",
                RUNTIME_AUTHOR,
                AppendRequest {
                    content: record("project", 1, RUNTIME_AUTHOR, &[""]).content.get(),
                    idempotency_key: "silence-1",
                },
            )
            .unwrap();
        state.cursor.insert("project".into(), 1);
        state.mediated.insert("project".into(), 1);
        config.rooms.get_mut("project").unwrap().mediator = None;
        mediate(
            &config,
            &store,
            &mut state,
            || Ok(BTreeMap::new()),
            |_| Ok(PanelStatus::Idle),
        )
        .unwrap();
        let records = store.read("project", 0).unwrap();
        assert_eq!(records.len(), 2);
        assert_eq!(
            silence_wake_recipient(&records[1]).as_deref(),
            Some("referee")
        );
        assert_eq!(state.mediated.get("project"), Some(&2));
    }
}
