//! Room collaboration configuration and durable delivery state.
//! This layer routes shared records; it does not authenticate their authors.

#[path = "collab/dispatch.rs"]
pub mod dispatch;
pub mod herdr;
#[path = "collab/runtime.rs"]
pub mod runtime;
#[path = "collab/state.rs"]
pub mod state;

use anyhow::{bail, Context, Result};
use fs2::FileExt;
use serde::{Deserialize, Deserializer, Serialize};
use sha2::{Digest, Sha256};
use std::collections::{BTreeMap, BTreeSet};
use std::fs::{self, File, OpenOptions};
use std::io::{ErrorKind, Write};
use std::path::{Path, PathBuf};

fn null_default<'de, D, T>(deserializer: D) -> std::result::Result<T, D::Error>
where
    D: Deserializer<'de>,
    T: Deserialize<'de> + Default,
{
    Ok(Option::<T>::deserialize(deserializer)?.unwrap_or_default())
}

#[derive(Clone, Debug, Default, Deserialize, Serialize)]
pub struct Config {
    #[serde(default, deserialize_with = "null_default")]
    pub roles: BTreeMap<String, Role>,
    #[serde(default, deserialize_with = "null_default")]
    pub rooms: BTreeMap<String, RoomRoles>,
}

#[derive(Clone, Debug, Default, Deserialize, Serialize)]
#[serde(default)]
pub struct Role {
    pub description: String,
    pub agent_type: String,
    #[serde(default, deserialize_with = "null_default")]
    pub command: Vec<String>,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub directory: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub instructions: String,
}

impl Role {
    /// The working directory the workspace root implies for this role.
    pub fn directory(&self, root: &Path) -> Result<PathBuf> {
        let workspace = fs::canonicalize(root.parent().context("workspace has no parent")?)?;
        if self.directory.is_empty() {
            return Ok(workspace);
        }
        let path = PathBuf::from(&self.directory);
        Ok(if path.is_absolute() {
            path
        } else {
            workspace.join(path)
        })
    }
}

#[derive(Clone, Debug, Default, Deserialize, Serialize)]
pub struct RoomRoles {
    #[serde(default, deserialize_with = "null_default")]
    pub roles: Vec<String>,
    /// The role the runtime wakes when this Room goes quiet; must be on the roster.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub mediator: Option<String>,
}

#[derive(Clone, Debug, Default, Deserialize, Serialize)]
#[serde(default)]
pub struct Message {
    pub text: String,
    #[serde(
        default,
        deserialize_with = "null_default",
        skip_serializing_if = "Vec::is_empty"
    )]
    pub mentions: Vec<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub reply_to: Option<RecordRef>,
}

#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct RecordRef {
    pub room: String,
    pub seq: u64,
}

/// Record-content encoding shared by every writer: JSON, then the escapes
/// that keep content line-safe inside the record stream.
pub fn escape_json(json: String) -> String {
    json.replace('&', "\\u0026")
        .replace('<', "\\u003c")
        .replace('>', "\\u003e")
        .replace('\u{2028}', "\\u2028")
        .replace('\u{2029}', "\\u2029")
}

pub fn message_content(message: &Message) -> Result<String> {
    Ok(escape_json(serde_json::to_string(message)?))
}

impl Config {
    pub fn load(root: &Path) -> Result<Self> {
        match fs::read(root.join("collaboration.json")) {
            Ok(data) => serde_json::from_slice(&data).context("read collaboration.json"),
            Err(e) if e.kind() == ErrorKind::NotFound => Ok(Self::default()),
            Err(e) => Err(e.into()),
        }
    }

    pub fn update<T>(root: &Path, change: impl FnOnce(&mut Self) -> Result<T>) -> Result<T> {
        let lock = lock_file(root.join("collaboration.lock"))?;
        lock.lock_exclusive()?;
        let mut config = Self::load(root)?;
        let result = change(&mut config)?;
        config.validate()?;
        save(root, "collaboration.json", &config)?;
        Ok(result)
    }

    pub fn with_lock<T>(root: &Path, read: impl FnOnce(&Self) -> Result<T>) -> Result<T> {
        let lock = lock_file(root.join("collaboration.lock"))?;
        lock.lock_exclusive()?;
        read(&Self::load(root)?)
    }

    pub fn validate(&self) -> Result<()> {
        for (name, role) in &self.roles {
            if !valid_role_name(name) {
                bail!("invalid role name {name:?}");
            }
            if role.agent_type.trim().is_empty() {
                bail!("role {name:?} requires an agent type");
            }
            if role.command.first().is_none_or(|s| s.trim().is_empty()) {
                bail!("role {name:?} requires a command");
            }
        }
        for (name, room) in &self.rooms {
            if name.is_empty() || name.starts_with('.') || name.contains(['/', '\\', '\0']) {
                bail!("invalid room name {name:?} in collaboration config");
            }
            let mut seen = BTreeSet::new();
            for role in &room.roles {
                if !self.roles.contains_key(role) {
                    bail!("room {name:?} refers to unknown role {role:?}");
                }
                if !seen.insert(role) {
                    bail!("room {name:?} lists role {role:?} more than once");
                }
            }
            if let Some(mediator) = &room.mediator {
                if !room.roles.contains(mediator) {
                    bail!("room {name:?} mediator {mediator:?} is not on its roster");
                }
            }
        }
        Ok(())
    }
}

pub fn valid_role_name(name: &str) -> bool {
    use unicode_general_category::{get_general_category, GeneralCategory::*};
    !name.is_empty()
        && name.chars().enumerate().all(|(i, c)| {
            matches!(
                get_general_category(c),
                UppercaseLetter
                    | LowercaseLetter
                    | TitlecaseLetter
                    | ModifierLetter
                    | OtherLetter
                    | DecimalNumber
            ) || (i > 0 && matches!(c, '_' | '-'))
        })
}

pub fn panel_name(workspace: &Path, room: &str, role: &str) -> Result<String> {
    let canonical = fs::canonicalize(workspace).context("resolve workspace root")?;
    let mut hash = Sha256::new();
    #[cfg(unix)]
    {
        use std::os::unix::ffi::OsStrExt;
        hash.update(canonical.as_os_str().as_bytes());
    }
    #[cfg(not(unix))]
    hash.update(canonical.to_string_lossy().as_bytes());
    hash.update([0]);
    hash.update(room.as_bytes());
    hash.update([0]);
    hash.update(role.as_bytes());
    Ok(format!("tr-{}", hex::encode(&hash.finalize()[..12])))
}

fn lock_file(path: PathBuf) -> Result<File> {
    let mut options = OpenOptions::new();
    options.create(true).read(true).write(true);
    #[cfg(unix)]
    {
        use std::os::unix::fs::OpenOptionsExt;
        options.mode(0o600);
    }
    Ok(options.open(path)?)
}

fn save(root: &Path, name: &str, value: &impl Serialize) -> Result<()> {
    let mut temp = tempfile::Builder::new()
        .prefix(".collab-")
        .suffix(".tmp")
        .tempfile_in(root)?;
    serde_json::to_writer_pretty(&mut temp, value)?;
    temp.write_all(b"\n")?;
    temp.as_file().sync_all()?;
    temp.persist(root.join(name))?;
    File::open(root)?.sync_all()?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn null_config_rooms_are_readable() {
        let config: Config = serde_json::from_str(r#"{"roles":{"reviewer":{"description":"","agent_type":"claude","command":["reviewer"]}},"rooms":null}"#).unwrap();
        config.validate().unwrap();
    }

    #[test]
    fn config_updates_validate_before_replacing() {
        let root = tempfile::tempdir().unwrap();
        Config::update(root.path(), |config| {
            config.roles.insert(
                "reviewer".into(),
                Role {
                    agent_type: "pi".into(),
                    command: vec!["pi".into()],
                    ..Role::default()
                },
            );
            Ok(())
        })
        .unwrap();
        assert!(Config::update(root.path(), |config| {
            config.rooms.insert(
                "project".into(),
                RoomRoles {
                    roles: vec!["missing".into()],
                    ..Default::default()
                },
            );
            Ok(())
        })
        .is_err());
        assert!(Config::load(root.path()).unwrap().rooms.is_empty());
    }
}
