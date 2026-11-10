//! Pure record-to-delivery routing and per-role queue ordering.
use super::super::{panel_name, Config, Delivery, DeliveryState, DispatchState, Intent, Message};
use crate::core::Record;
use anyhow::Result;
use std::collections::BTreeSet;

pub(super) fn mark_reply(state: &mut DispatchState, record: &Record) {
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

pub(super) fn record_intent(
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

pub(super) fn head_of_queue(state: &DispatchState, i: usize, j: usize) -> bool {
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
