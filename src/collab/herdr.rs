//! Coalesced, backoff-limited synchronization of hc status into Herdr.

use super::{DeliveryState, Intent};
use crate::collab::runtime::{HcRuntime, PanelStatus, Runtime};
use crate::hc::HcClient;
use anyhow::{anyhow, Result};
use std::collections::{HashMap, HashSet, VecDeque};
use std::path::{Path, PathBuf};
use std::sync::{Arc, Condvar, Mutex, OnceLock};
use std::time::{Duration, Instant};

#[derive(Clone, Debug, Hash, PartialEq, Eq)]
struct Key {
    root: PathBuf,
    room: String,
    panel: String,
}

#[derive(Clone, Debug)]
struct Job {
    key: Key,
    role: String,
}

#[derive(Clone, Copy)]
struct Failure {
    attempts: u32,
    retry_at: Instant,
    reported: bool,
}

#[derive(Default)]
struct Queue {
    latest: HashMap<Key, Job>,
    ready: VecDeque<Key>,
    scheduled: HashSet<Key>,
    active: HashSet<Key>,
    failures: HashMap<Key, Failure>,
}

struct Worker {
    queue: Mutex<Queue>,
    ready: Condvar,
}

static WORKER: OnceLock<Arc<Worker>> = OnceLock::new();

pub(crate) fn enqueue(root: &Path, intents: &[Intent]) {
    let worker = WORKER.get_or_init(|| {
        let worker = Arc::new(Worker {
            queue: Mutex::new(Queue::default()),
            ready: Condvar::new(),
        });
        let running = worker.clone();
        std::thread::Builder::new()
            .name("tearoom-herdr-status".into())
            .spawn(move || run_worker(running))
            .expect("start Herdr status worker");
        worker
    });
    let mut queue = worker.queue.lock().unwrap_or_else(|e| e.into_inner());
    for intent in intents {
        for delivery in &intent.deliveries {
            if !should_observe(&delivery.state) {
                continue;
            }
            let key = Key {
                root: root.to_path_buf(),
                room: intent.room.clone(),
                panel: delivery.panel.clone(),
            };
            queue.latest.insert(
                key.clone(),
                Job {
                    key: key.clone(),
                    role: delivery.role.clone(),
                },
            );
            schedule(&mut queue, &key, Instant::now());
        }
    }
    worker.ready.notify_one();
}

fn schedule(queue: &mut Queue, key: &Key, now: Instant) {
    if queue.active.contains(key) || queue.scheduled.contains(key) {
        return;
    }
    if queue
        .failures
        .get(key)
        .is_some_and(|failure| failure.retry_at > now)
    {
        return;
    }
    if queue.latest.contains_key(key) {
        queue.scheduled.insert(key.clone());
        queue.ready.push_back(key.clone());
    }
}

fn run_worker(worker: Arc<Worker>) {
    loop {
        let job = {
            let mut queue = worker.queue.lock().unwrap_or_else(|e| e.into_inner());
            loop {
                let now = Instant::now();
                let due = queue
                    .failures
                    .iter()
                    .filter(|(_, failure)| failure.retry_at <= now)
                    .map(|(key, _)| key.clone())
                    .collect::<Vec<_>>();
                for key in due {
                    schedule(&mut queue, &key, now);
                }
                if let Some(key) = queue.ready.pop_front() {
                    queue.scheduled.remove(&key);
                    if let Some(job) = queue.latest.remove(&key) {
                        queue.active.insert(key);
                        break job;
                    }
                    continue;
                }
                let timeout = queue
                    .failures
                    .values()
                    .map(|failure| failure.retry_at.saturating_duration_since(now))
                    .min();
                queue = match timeout {
                    Some(timeout) => {
                        worker
                            .ready
                            .wait_timeout(queue, timeout)
                            .unwrap_or_else(|e| e.into_inner())
                            .0
                    }
                    None => worker.ready.wait(queue).unwrap_or_else(|e| e.into_inner()),
                };
            }
        };

        let result = synchronize(&job);
        let mut queue = worker.queue.lock().unwrap_or_else(|e| e.into_inner());
        queue.active.remove(&job.key);
        match result {
            Ok(()) => {
                if queue.failures.remove(&job.key).is_some() {
                    eprintln!(
                        "Herdr status synchronization recovered for {} @{}",
                        job.key.room, job.role
                    );
                }
                schedule(&mut queue, &job.key, Instant::now());
            }
            Err(error) => {
                let previous = queue.failures.get(&job.key).copied();
                let attempts = previous.map_or(1, |failure| failure.attempts.saturating_add(1));
                let reported = previous.is_some_and(|failure| failure.reported);
                let delay = backoff(attempts);
                queue.failures.insert(
                    job.key.clone(),
                    Failure {
                        attempts,
                        retry_at: Instant::now() + delay,
                        reported: true,
                    },
                );
                queue
                    .latest
                    .entry(job.key.clone())
                    .or_insert_with(|| job.clone());
                if !reported {
                    eprintln!("Herdr status synchronization failed for {} @{}; retrying in {}s: {error:#}", job.key.room, job.role, delay.as_secs());
                }
            }
        }
        worker.ready.notify_one();
    }
}

fn synchronize(job: &Job) -> Result<()> {
    let runtime = match HcClient::from_env() {
        Ok(client) => HcRuntime {
            client,
            timeout: Duration::from_secs(30),
            workspace_label: None,
            caller: None,
        },
        Err(error) => {
            return Err(anyhow!("hc configuration unavailable: {error}"));
        }
    };
    match runtime.status(&job.key.panel) {
        Ok(status) => sync_if_present(job, status),
        Err(error) => Err(anyhow!(
            "hc status unavailable; Herdr remains unchanged: {error}"
        )),
    }
}

fn sync_if_present(job: &Job, status: PanelStatus) -> Result<()> {
    if status == PanelStatus::Missing {
        return Ok(());
    }
    crate::herdr::sync_panel(
        &job.key.root,
        &job.key.room,
        &job.role,
        &job.key.panel,
        status,
    )
}

fn should_observe(state: &DeliveryState) -> bool {
    matches!(
        state,
        DeliveryState::Pending
            | DeliveryState::Unknown
            | DeliveryState::Submitted
            | DeliveryState::Completed
    )
}

fn backoff(attempt: u32) -> Duration {
    Duration::from_secs(
        1u64.checked_shl(attempt.saturating_sub(1).min(5))
            .unwrap_or(30)
            .min(30),
    )
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn failed_sync_backs_off_exponentially_and_caps() {
        assert_eq!(backoff(1), Duration::from_secs(1));
        assert_eq!(backoff(2), Duration::from_secs(2));
        assert_eq!(backoff(3), Duration::from_secs(4));
        assert_eq!(backoff(6), Duration::from_secs(30));
        assert_eq!(backoff(20), Duration::from_secs(30));
    }

    #[test]
    fn repeated_panel_updates_coalesce_to_one_latest_job() {
        let key = Key {
            root: PathBuf::from("/tmp/work"),
            room: "project".into(),
            panel: "tr-panel".into(),
        };
        let mut queue = Queue::default();
        queue.latest.insert(
            key.clone(),
            Job {
                key: key.clone(),
                role: "builder".into(),
            },
        );
        schedule(&mut queue, &key, Instant::now());
        queue.latest.insert(
            key.clone(),
            Job {
                key: key.clone(),
                role: "reviewer".into(),
            },
        );
        schedule(&mut queue, &key, Instant::now());
        assert_eq!(queue.ready.len(), 1);
        assert_eq!(queue.latest.get(&key).unwrap().role, "reviewer");
    }

    #[test]
    fn pending_and_unknown_deliveries_are_observed_without_changing_them() {
        assert!(should_observe(&DeliveryState::Pending));
        assert!(should_observe(&DeliveryState::Unknown));
        assert!(should_observe(&DeliveryState::Submitted));
        assert!(should_observe(&DeliveryState::Completed));
        assert!(!should_observe(&DeliveryState::Skipped));
    }

    #[test]
    fn missing_hc_panel_is_not_sent_to_herdr_sync() {
        let job = Job {
            key: Key {
                root: PathBuf::from("/tmp/missing-panel-test"),
                room: "project".into(),
                panel: "tr-missing".into(),
            },
            role: "builder".into(),
        };
        assert!(sync_if_present(&job, PanelStatus::Missing).is_ok());
    }
}
