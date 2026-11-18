//! Silence mediation: waking the configured role when a Room goes quiet.
//! The wake is an ordinary addressed record — signed by the runtime and routed
//! by the normal pipeline — and each stop is mediated once: a new wake needs
//! third-party records after the previous one.
use super::super::state::{DeliveryState, DispatchState};
use super::super::{message_content, Config, Message};
use crate::core::{AppendRequest, Record};
use crate::store::Store;
use anyhow::Result;

/// Signature the runtime signs silence-wake records with.
pub const SILENCE_AUTHOR: &str = "tearoom";

fn wake(record: &Record, mediator: &str) -> bool {
    record.author == SILENCE_AUTHOR
        && serde_json::from_str::<Message>(record.content.get())
            .is_ok_and(|message| message.mentions.iter().any(|role| role == mediator))
}

fn third_party(record: &Record, mediator: &str) -> bool {
    record.author != SILENCE_AUTHOR && record.author != mediator
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

/// Append one silence-wake record for every mediated Room that went quiet.
/// Runs under the dispatch lock. `state.mediated` is the durable progress
/// marker — the seq of the last known wake — so each pass reads only the
/// records since it; a first pass without a marker scans the whole Room once.
/// The append precedes the marker update, so a crash between them is healed
/// by the next pass reading the already-appended wake.
pub(super) fn mediate(config: &Config, store: &Store, state: &mut DispatchState) -> Result<()> {
    for card in store.rooms()? {
        let Some(mediator) = config
            .rooms
            .get(&card.id)
            .and_then(|binding| binding.mediator.clone())
        else {
            continue;
        };
        if !quiet(state, &card.id) {
            continue;
        }
        let mut last_wake = state.mediated.get(&card.id).copied().unwrap_or(0);
        let records = store.read(&card.id, last_wake)?;
        let since_wake = match records.iter().rposition(|record| wake(record, &mediator)) {
            Some(at) => {
                last_wake = records[at].seq;
                state.mediated.insert(card.id.clone(), last_wake);
                &records[at + 1..]
            }
            None => &records[..],
        };
        if !rearmed(since_wake, last_wake != 0, &mediator) {
            continue;
        }
        let seq = records.last().map_or(1, |record| record.seq + 1);
        let text = format!(
            "Room {} is quiet with no work in flight. Mediate the stop: if the goal is met, \
             record a closing sentence; if work should continue, address the next role with \
             --to; if blocked, record the blocker and stop.",
            card.name
        );
        let record = store.append(
            &card.id,
            SILENCE_AUTHOR,
            AppendRequest {
                content: &message_content(&Message {
                    text,
                    mentions: vec![mediator],
                    reply_to: None,
                })?,
                idempotency_key: &format!("silence-{seq}"),
            },
        )?;
        state.mediated.insert(card.id.clone(), record.seq);
    }
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
            &[record("r", 2, SILENCE_AUTHOR, &[mediator])],
            true,
            mediator
        ));
        // A third party speaking after the wake rearms the next stop.
        assert!(rearmed(&[record("r", 2, "builder", &[])], true, mediator));
    }

    #[test]
    fn a_wake_is_identified_by_signature_and_addressed_mediator() {
        let mediator = "referee";
        assert!(wake(&record("r", 1, SILENCE_AUTHOR, &[mediator]), mediator));
        assert!(!wake(&record("r", 1, SILENCE_AUTHOR, &[]), mediator));
        assert!(!wake(&record("r", 1, "builder", &[mediator]), mediator));
        assert!(!wake(&record("r", 1, SILENCE_AUTHOR, &["other"]), mediator));
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
                mediator: Some("referee".into()),
            },
        );
        let mut state = DispatchState::default();
        mediate(&config, &store, &mut state).unwrap();
        let wake = store.read("project", 0).unwrap();
        assert_eq!(wake.len(), 1);
        assert_eq!(wake[0].author, SILENCE_AUTHOR);
        let message: Message = serde_json::from_str(wake[0].content.get()).unwrap();
        assert_eq!(message.mentions, ["referee"]);
        assert!(message.text.contains("Mediate the stop"));
        assert_eq!(state.mediated.get("project"), Some(&1));
        // The same quiet state mediates nothing more.
        mediate(&config, &store, &mut state).unwrap();
        assert_eq!(store.read("project", 0).unwrap().len(), 1);
        // The mediator answering alone does not rearm the stop.
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
        mediate(&config, &store, &mut state).unwrap();
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
        mediate(&config, &store, &mut state).unwrap();
        let records = store.read("project", 0).unwrap();
        assert_eq!(records.len(), 4);
        assert_eq!(records[3].author, SILENCE_AUTHOR);
        assert_eq!(state.mediated.get("project"), Some(&4));
    }
}
