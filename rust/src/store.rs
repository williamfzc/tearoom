//! Format-4 JSONL workspace and cross-process append serialization.
mod encoding;
mod recovery;

use crate::core::{compact_json, fingerprint, AppendRequest, Record, Room};
use anyhow::{bail, Context, Result};
use chrono::Utc;
use encoding::*;
use fs2::FileExt;
use serde::{Deserialize, Serialize};
use serde_json::value::RawValue;
use std::fs::{self, File};
use std::path::{Path, PathBuf};

#[derive(Clone, Debug)]
pub struct Store {
    root: PathBuf,
}
#[derive(Clone, Debug, Deserialize, Serialize, PartialEq, Eq)]
struct Ledger {
    author: String,
    key: String,
    fingerprint: String,
    seq: u64,
}
#[derive(Deserialize, Serialize)]
struct Pending {
    record: Record,
    ledger: Ledger,
}
#[derive(Deserialize, Serialize)]
struct Marker {
    format: u32,
    backend: String,
}

impl Store {
    pub fn find(start: impl AsRef<Path>) -> Result<PathBuf> {
        let mut dir = fs::canonicalize(start)?;
        loop {
            let root = dir.join(".tearoom");
            if root.is_dir() {
                return Ok(root);
            }
            if !dir.pop() {
                bail!("no workspace: run 'tearoom room init' first");
            }
        }
    }
    pub fn open(start: impl AsRef<Path>) -> Result<Self> {
        let root = Self::find(start)?;
        let marker: Marker = serde_json::from_slice(&fs::read(root.join("store.json"))?)?;
        if matches!(marker.format, 2 | 3) && marker.backend == "sqlite" {
            bail!(
                "legacy store format {} requires an earlier Tearoom version to migrate",
                marker.format
            );
        }
        if marker.format != 4 || marker.backend != "jsonl" {
            bail!(
                "incompatible store format {} backend {:?}: format 4 required",
                marker.format,
                marker.backend
            );
        }
        if !root.join("lock").is_file() {
            bail!("workspace lock is missing");
        }
        Ok(Self { root })
    }
    pub fn init(dir: impl AsRef<Path>) -> Result<Self> {
        let dir = dir.as_ref();
        fs::create_dir_all(dir)?;
        let root = dir.join(".tearoom");
        if root.exists() {
            bail!("workspace already exists at {}", root.display());
        }
        let temp = tempfile::Builder::new()
            .prefix(".tearoom-init-")
            .tempdir_in(dir)?;
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            fs::set_permissions(temp.path(), fs::Permissions::from_mode(0o755))?;
        }
        fs::create_dir(temp.path().join("rooms"))?;
        replace(
            &temp.path().join("store.json"),
            &go_json_pretty(&Marker {
                format: 4,
                backend: "jsonl".into(),
            })?,
        )?;
        File::create(temp.path().join("lock"))?.sync_all()?;
        sync_dir(temp.path())?;
        fs::rename(temp.path(), &root)?;
        sync_dir(dir)?;
        Self::open(dir)
    }
    pub fn root(&self) -> &Path {
        &self.root
    }
    fn lock(&self, exclusive: bool) -> Result<File> {
        if !cfg!(unix) {
            bail!("cross-process locking is unsupported on this platform");
        }
        let file = File::open(self.root.join("lock"))?;
        if exclusive {
            file.lock_exclusive()?;
        } else {
            FileExt::lock_shared(&file)?;
        }
        Ok(file)
    }
    pub fn create_room(&self, name: &str, parent: &str) -> Result<Room> {
        let name = name.trim();
        let parent = parent.trim();
        validate_name(name)?;
        if !parent.is_empty() {
            validate_name(parent)?;
        }
        let _guard = self.lock(true)?;
        if !parent.is_empty() {
            self.require_room(parent)?;
        }
        let rooms = self.root.join("rooms");
        if rooms.join(name).exists() {
            bail!("room {name:?} already exists: conflict");
        }
        let room = Room {
            name: name.into(),
            parent: parent.into(),
            created: now(),
        };
        let temp = tempfile::Builder::new()
            .prefix(".tmp-")
            .tempdir_in(&rooms)?;
        replace(&temp.path().join("room.json"), &go_json_pretty(&room)?)?;
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            fs::set_permissions(temp.path(), fs::Permissions::from_mode(0o755))?;
        }
        fs::rename(temp.path(), rooms.join(name))?;
        sync_dir(&rooms)?;
        Ok(room)
    }
    pub fn rooms(&self) -> Result<Vec<Room>> {
        let _guard = self.lock(false)?;
        let mut dirs =
            fs::read_dir(self.root.join("rooms"))?.collect::<std::io::Result<Vec<_>>>()?;
        dirs.sort_by_key(|d| d.file_name());
        let mut rooms = Vec::new();
        for dir in dirs {
            let name = dir.file_name().to_string_lossy().into_owned();
            if name.starts_with('.') {
                continue;
            }
            let path = dir.path().join("room.json");
            if !path.is_file() {
                continue;
            }
            let room: Room = serde_json::from_slice(&fs::read(path)?)?;
            if room.name != name {
                bail!("room directory {name:?} declares name {:?}", room.name);
            }
            rooms.push(room);
        }
        Ok(rooms)
    }
    pub fn read(&self, room: &str, after: u64) -> Result<Vec<Record>> {
        let room = room.trim();
        validate_name(room)?;
        let _guard = self.lock(false)?;
        self.require_room(room)?;
        Ok(self
            .records(room)?
            .into_iter()
            .filter(|r| r.seq > after)
            .collect())
    }
    pub fn append(&self, room: &str, author: &str, request: AppendRequest<'_>) -> Result<Record> {
        let room = room.trim();
        validate_name(room)?;
        let author = author.trim();
        let key = request.idempotency_key.trim();
        let content = compact_json(request.content).context("invalid content")?;
        let digest = fingerprint(author, &content);
        let _guard = self.lock(true)?;
        self.require_room(room)?;
        self.recover(room)?;
        let ledger: Vec<Ledger> = read_jsonl(&self.path(room, "idempotency.jsonl"))?;
        let records = self.records(room)?;
        if !key.is_empty() {
            if let Some(entry) = ledger.iter().find(|e| e.author == author && e.key == key) {
                if entry.fingerprint != digest {
                    bail!("idempotency key {key:?} already describes another record: conflict");
                }
                return records
                    .into_iter()
                    .find(|r| r.seq == entry.seq)
                    .context("idempotency key lost its original record");
            }
        }
        let record = Record {
            room: room.into(),
            seq: records.last().map_or(1, |r| r.seq + 1),
            author: author.into(),
            at: now(),
            content: RawValue::from_string(content)?,
        };
        if key.is_empty() {
            append_bytes(&self.path(room, "records.jsonl"), &record_line(&record)?)?;
        } else {
            let pending = Pending {
                ledger: Ledger {
                    author: author.into(),
                    key: key.into(),
                    fingerprint: digest,
                    seq: record.seq,
                },
                record: record.clone(),
            };
            replace(
                &self.path(room, "append-pending.json"),
                &pending_line(&pending)?,
            )?;
            self.recover(room)?;
        }
        Ok(record)
    }
    fn path(&self, room: &str, name: &str) -> PathBuf {
        self.root.join("rooms").join(room).join(name)
    }
    fn records(&self, room: &str) -> Result<Vec<Record>> {
        let records: Vec<Record> = read_jsonl(&self.path(room, "records.jsonl"))?;
        for (index, record) in records.iter().enumerate() {
            if record.room != room || record.seq != index as u64 + 1 {
                bail!("record stream has an invalid room or sequence");
            }
        }
        Ok(records)
    }
    fn require_room(&self, room: &str) -> Result<()> {
        if !self.path(room, "room.json").is_file() {
            bail!("room {room:?}: not found");
        }
        Ok(())
    }
}

fn validate_name(name: &str) -> Result<()> {
    if name.is_empty() || name.starts_with('.') || name.contains('/') || name.contains('\\') {
        bail!("invalid room name {name:?}");
    }
    Ok(())
}
fn now() -> String {
    Utc::now().to_rfc3339_opts(chrono::SecondsFormat::AutoSi, true)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs::OpenOptions;
    use std::io::Write;

    #[test]
    fn legacy_sqlite_is_rejected_without_modifying_workspace() {
        for format in [2, 3] {
            let dir = tempfile::tempdir().unwrap();
            let root = dir.path().join(".tearoom");
            fs::create_dir(&root).unwrap();
            let marker = format!("{{\"format\":{format},\"backend\":\"sqlite\"}}\n");
            fs::write(root.join("store.json"), &marker).unwrap();
            let data = b"legacy database bytes";
            fs::write(root.join("room.db"), data).unwrap();
            fs::write(root.join("room.db-wal"), b"legacy wal bytes").unwrap();

            let err = Store::open(dir.path()).unwrap_err().to_string();
            assert!(err.contains("earlier Tearoom version"), "{err}");
            assert_eq!(
                fs::read(root.join("store.json")).unwrap(),
                marker.as_bytes()
            );
            assert_eq!(fs::read(root.join("room.db")).unwrap(), data);
            assert_eq!(
                fs::read(root.join("room.db-wal")).unwrap(),
                b"legacy wal bytes"
            );
            assert_eq!(fs::read_dir(root).unwrap().count(), 3);
        }
    }

    #[test]
    fn noncontiguous_records_are_rejected_without_writes() {
        let dir = tempfile::tempdir().unwrap();
        let store = Store::init(dir.path()).unwrap();
        store.create_room("root", "").unwrap();
        for value in ["1", "2"] {
            store
                .append(
                    "root",
                    "a",
                    AppendRequest {
                        content: value,
                        idempotency_key: "",
                    },
                )
                .unwrap();
        }
        let path = store.path("root", "records.jsonl");
        let content = fs::read_to_string(&path)
            .unwrap()
            .replace("\"seq\":2", "\"seq\":3");
        assert!(content.contains("\"seq\":3"));
        fs::write(&path, &content).unwrap();
        assert!(store.read("root", 0).is_err());
        assert!(store
            .append(
                "root",
                "a",
                AppendRequest {
                    content: "3",
                    idempotency_key: ""
                }
            )
            .is_err());
        assert_eq!(fs::read_to_string(path).unwrap(), content);
    }

    #[test]
    fn tree_and_idempotent_append() {
        let dir = tempfile::tempdir().unwrap();
        let store = Store::init(dir.path()).unwrap();
        store.create_room("root", "").unwrap();
        store.create_room("child", "root").unwrap();
        assert_eq!(store.rooms().unwrap()[0].parent, "root");
        let first = store
            .append(
                "child",
                " agent ",
                AppendRequest {
                    content: " { \"b\": 2, \"a\":1 } ",
                    idempotency_key: "x",
                },
            )
            .unwrap();
        let retry = store
            .append(
                "child",
                "agent",
                AppendRequest {
                    content: "{\"b\":2,\"a\":1}",
                    idempotency_key: "x",
                },
            )
            .unwrap();
        assert_eq!(first.seq, retry.seq);
        assert!(store
            .append(
                "child",
                "agent",
                AppendRequest {
                    content: "{\"a\":1,\"b\":2}",
                    idempotency_key: "x"
                }
            )
            .is_err());
        assert_eq!(store.read("child", 0).unwrap().len(), 1);
        assert_eq!(store.read("child", 1).unwrap().len(), 0);
        assert_eq!(Store::open(dir.path()).unwrap().rooms().unwrap().len(), 2);
    }
    #[test]
    fn corrupt_pending_does_not_truncate_unrelated_tail() {
        let dir = tempfile::tempdir().unwrap();
        let store = Store::init(dir.path()).unwrap();
        store.create_room("root", "").unwrap();
        store
            .append(
                "root",
                "a",
                AppendRequest {
                    content: "1",
                    idempotency_key: "",
                },
            )
            .unwrap();
        let record = Record {
            room: "root".into(),
            seq: 99,
            author: "a".into(),
            at: now(),
            content: RawValue::from_string("2".into()).unwrap(),
        };
        let ledger = Ledger {
            author: "a".into(),
            key: "x".into(),
            fingerprint: fingerprint("a", "2"),
            seq: 99,
        };
        replace(
            &store.path("root", "append-pending.json"),
            &pending_line(&Pending {
                record: record.clone(),
                ledger,
            })
            .unwrap(),
        )
        .unwrap();
        let records = store.path("root", "records.jsonl");
        let mut f = OpenOptions::new().append(true).open(&records).unwrap();
        f.write_all(&record_line(&record).unwrap()[..10]).unwrap();
        drop(f);
        let before = fs::read(&records).unwrap();
        assert!(store
            .append(
                "root",
                "b",
                AppendRequest {
                    content: "3",
                    idempotency_key: ""
                }
            )
            .is_err());
        assert_eq!(before, fs::read(records).unwrap());
        assert!(store.path("root", "append-pending.json").exists());
    }
    #[test]
    fn pending_append_recovers_an_interrupted_record_and_ledger() {
        let dir = tempfile::tempdir().unwrap();
        let store = Store::init(dir.path()).unwrap();
        store.create_room("root", "").unwrap();
        let record = Record {
            room: "root".into(),
            seq: 1,
            author: "a".into(),
            at: now(),
            content: RawValue::from_string("{\"x\":1}".into()).unwrap(),
        };
        let ledger = Ledger {
            author: "a".into(),
            key: "x".into(),
            fingerprint: fingerprint("a", record.content.get()),
            seq: 1,
        };
        let pending = Pending {
            record: record.clone(),
            ledger: ledger.clone(),
        };
        replace(
            &store.path("root", "append-pending.json"),
            &pending_line(&pending).unwrap(),
        )
        .unwrap();
        let record_line = record_line(&record).unwrap();
        fs::write(
            store.path("root", "records.jsonl"),
            &record_line[..record_line.len() / 2],
        )
        .unwrap();
        let ledger_line = line(&ledger).unwrap();
        fs::write(
            store.path("root", "idempotency.jsonl"),
            &ledger_line[..ledger_line.len() - 1],
        )
        .unwrap();
        let recovered = store
            .append(
                "root",
                "a",
                AppendRequest {
                    content: "{\"x\":1}",
                    idempotency_key: "x",
                },
            )
            .unwrap();
        assert_eq!(recovered.seq, 1);
        assert_eq!(
            fs::read(store.path("root", "records.jsonl")).unwrap(),
            record_line
        );
        assert_eq!(
            fs::read(store.path("root", "idempotency.jsonl")).unwrap(),
            ledger_line
        );
        assert!(!store.path("root", "append-pending.json").exists());
    }
    #[test]
    fn unterminated_record_or_ledger_never_gets_appended_to() {
        for filename in ["records.jsonl", "idempotency.jsonl"] {
            let dir = tempfile::tempdir().unwrap();
            let store = Store::init(dir.path()).unwrap();
            store.create_room("root", "").unwrap();
            store
                .append(
                    "root",
                    "a",
                    AppendRequest {
                        content: "1",
                        idempotency_key: "k",
                    },
                )
                .unwrap();
            let path = store.path("root", filename);
            let mut original = fs::read(&path).unwrap();
            assert_eq!(original.pop(), Some(b'\n'));
            fs::write(&path, &original).unwrap();
            assert!(store
                .append(
                    "root",
                    "a",
                    AppendRequest {
                        content: "2",
                        idempotency_key: "next"
                    }
                )
                .is_err());
            assert_eq!(fs::read(path).unwrap(), original);
        }
    }
    #[test]
    fn pending_append_recovers_complete_unterminated_record_and_ledger() {
        let dir = tempfile::tempdir().unwrap();
        let store = Store::init(dir.path()).unwrap();
        store.create_room("root", "").unwrap();
        let record = Record {
            room: "root".into(),
            seq: 1,
            author: "a".into(),
            at: now(),
            content: RawValue::from_string("1".into()).unwrap(),
        };
        let ledger = Ledger {
            author: "a".into(),
            key: "k".into(),
            fingerprint: fingerprint("a", "1"),
            seq: 1,
        };
        let record_line = record_line(&record).unwrap();
        let ledger_line = line(&ledger).unwrap();
        replace(
            &store.path("root", "append-pending.json"),
            &pending_line(&Pending { record, ledger }).unwrap(),
        )
        .unwrap();
        fs::write(
            store.path("root", "records.jsonl"),
            &record_line[..record_line.len() - 1],
        )
        .unwrap();
        fs::write(
            store.path("root", "idempotency.jsonl"),
            &ledger_line[..ledger_line.len() - 1],
        )
        .unwrap();
        let result = store
            .append(
                "root",
                "a",
                AppendRequest {
                    content: "1",
                    idempotency_key: "k",
                },
            )
            .unwrap();
        assert_eq!(result.seq, 1);
        assert_eq!(
            fs::read(store.path("root", "records.jsonl")).unwrap(),
            record_line
        );
        assert_eq!(
            fs::read(store.path("root", "idempotency.jsonl")).unwrap(),
            ledger_line
        );
        assert!(!store.path("root", "append-pending.json").exists());
    }
    #[test]
    fn compact_keeps_key_order_and_escapes() {
        let content = "  { \"z\" : 1, \"a\" : \"a \\\" b\" } ";
        assert_eq!(
            compact_json(content).unwrap(),
            "{\"z\":1,\"a\":\"a \\\" b\"}"
        );
        assert_ne!(
            fingerprint("a", "{\"x\":1,\"y\":2}"),
            fingerprint("a", "{\"y\":2,\"x\":1}")
        );
    }
}
