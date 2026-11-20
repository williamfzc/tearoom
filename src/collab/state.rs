//! Durable dispatch bookkeeping: the delivery ledger, its scan cursors, and
//! the workspace dispatch lock. Progress lives here; routing policy lives in
//! the dispatch module. Format owners: dispatch.json, version 1.
use super::{lock_file, null_default, save};
use anyhow::{bail, Context, Result};
use fs2::FileExt;
use serde::{Deserialize, Serialize};
use std::collections::{BTreeMap, BTreeSet};
use std::fs::{self, File};
use std::io::ErrorKind;
use std::path::Path;

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
pub enum DeliveryState {
    Pending,
    Submitting,
    Submitted,
    Completed,
    Skipped,
    Unknown,
}

#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct Delivery {
    pub role: String,
    pub panel: String,
    pub state: DeliveryState,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub error: String,
}

#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct Intent {
    pub room: String,
    pub seq: u64,
    pub text: String,
    #[serde(default, deserialize_with = "null_default")]
    pub deliveries: Vec<Delivery>,
}

#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct DispatchState {
    pub version: u32,
    #[serde(default, deserialize_with = "null_default")]
    pub cursor: BTreeMap<String, u64>,
    /// Whether a pump checked receipts ignored by the previous routing rules.
    #[serde(default)]
    pub historical_replies_reconciled: bool,
    #[serde(default, deserialize_with = "null_default")]
    pub intents: Vec<Intent>,
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub fresh: BTreeMap<String, BTreeSet<String>>,
    /// Room id -> sequence of the last silence-wake record the runtime appended.
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub mediated: BTreeMap<String, u64>,
}

impl Default for DispatchState {
    fn default() -> Self {
        Self {
            version: 1,
            cursor: BTreeMap::new(),
            historical_replies_reconciled: false,
            intents: Vec::new(),
            fresh: BTreeMap::new(),
            mediated: BTreeMap::new(),
        }
    }
}

impl DispatchState {
    pub fn load(root: &Path) -> Result<Self> {
        let state: Self = match fs::read(root.join("dispatch.json")) {
            Ok(data) => {
                serde_json::from_slice(&data).context("read collaboration dispatch state")?
            }
            Err(e) if e.kind() == ErrorKind::NotFound => return Ok(Self::default()),
            Err(e) => return Err(e.into()),
        };
        if state.version != 1 {
            bail!(
                "unsupported collaboration dispatch state version {}",
                state.version
            );
        }
        Ok(state)
    }

    pub fn save(&self, root: &Path) -> Result<()> {
        if self.version != 1 {
            bail!(
                "unsupported collaboration dispatch state version {}",
                self.version
            );
        }
        save(root, "dispatch.json", self)
    }

    pub fn recover_submitting(&mut self) {
        for delivery in self.intents.iter_mut().flat_map(|i| &mut i.deliveries) {
            if delivery.state == DeliveryState::Submitting {
                delivery.state = DeliveryState::Unknown;
                delivery.error =
                    "dispatcher stopped while hc submission outcome was unknown".into();
            }
        }
    }
}

/// Hold the returned file for the whole pump or manual resolution.
pub fn dispatch_lock(root: &Path) -> Result<File> {
    let lock = lock_file(root.join(".dispatch.lock"))?;
    lock.try_lock_exclusive()
        .context("another Room dispatcher holds the workspace lock")?;
    Ok(lock)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn null_ledger_fields_are_readable() {
        let state: DispatchState =
            serde_json::from_str(r#"{"version":1,"cursor":null,"intents":null}"#).unwrap();
        assert!(state.intents.is_empty());
        assert!(!state.historical_replies_reconciled);
    }

    #[test]
    fn dispatch_lock_and_unknown_recovery_survive_reopen() {
        let root = tempfile::tempdir().unwrap();
        let held = dispatch_lock(root.path()).unwrap();
        assert!(dispatch_lock(root.path()).is_err());
        let mut state = DispatchState {
            intents: vec![Intent {
                room: "project".into(),
                seq: 1,
                text: "work".into(),
                deliveries: vec![Delivery {
                    role: "builder".into(),
                    panel: "tr-1".into(),
                    state: DeliveryState::Submitting,
                    error: String::new(),
                }],
            }],
            ..DispatchState::default()
        };
        state.save(root.path()).unwrap();
        drop(held);
        let _held = dispatch_lock(root.path()).unwrap();
        let mut loaded = DispatchState::load(root.path()).unwrap();
        loaded.recover_submitting();
        loaded.save(root.path()).unwrap();
        state.intents[0].deliveries[0].state = DeliveryState::Unknown;
        assert_eq!(
            DispatchState::load(root.path()).unwrap().intents[0].deliveries[0].state,
            state.intents[0].deliveries[0].state
        );
    }
}
