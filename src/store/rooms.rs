//! Room-tree lifecycle on the workspace: create, move, list, and delete.
//! Record append and read remain in the parent module; this file owns the
//! per-Room directory shape (room.json, records, ledgers) as a unit.

use super::encoding::{go_json_pretty, replace, sync_dir};
use super::{now, validate_name, Store};
use crate::core::Room;
use anyhow::{bail, Context, Result};
use std::fs;

/// What deleting one Room would remove; children always refuse the removal.
#[derive(Clone, Debug, serde::Serialize)]
pub struct RoomRemoval {
    pub room: String,
    pub records: usize,
}

impl Store {
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

    /// Changes a Room's parent without moving its identity or stored data.
    pub fn move_room(&self, name: &str, parent: &str) -> Result<Room> {
        let name = name.trim();
        let parent = parent.trim();
        validate_name(name)?;
        if !parent.is_empty() {
            validate_name(parent)?;
        }
        let _guard = self.lock(true)?;
        let rooms = self.rooms_unlocked()?;
        let mut room = rooms
            .iter()
            .find(|room| room.name == name)
            .cloned()
            .with_context(|| format!("room {name:?}: not found"))?;
        if !parent.is_empty() && !rooms.iter().any(|room| room.name == parent) {
            bail!("room {parent:?}: not found");
        }
        let mut ancestor = parent;
        let mut seen = std::collections::HashSet::new();
        while !ancestor.is_empty() {
            if ancestor == name {
                bail!("cannot move room {name:?} under itself or one of its descendants");
            }
            if !seen.insert(ancestor) {
                bail!("room tree contains a parent cycle at {ancestor:?}");
            }
            ancestor = rooms
                .iter()
                .find(|room| room.name == ancestor)
                .map_or("", |room| room.parent.as_str());
        }
        room.parent = parent.into();
        replace(&self.path(name, "room.json"), &go_json_pretty(&room)?)?;
        Ok(room)
    }

    pub fn rooms(&self) -> Result<Vec<Room>> {
        let _guard = self.lock(false)?;
        self.rooms_unlocked()
    }

    /// Reports what deleting one leaf Room would remove without removing it.
    pub fn room_removal_plan(&self, name: &str) -> Result<RoomRemoval> {
        let name = name.trim();
        validate_name(name)?;
        let _guard = self.lock(false)?;
        self.require_room(name)?;
        self.removal_plan_unlocked(name)
    }

    /// Removes a leaf Room with its whole record stream and returns the count.
    /// The removal is irreversible; role definitions and other Rooms survive.
    pub fn delete_room(&self, name: &str) -> Result<usize> {
        let name = name.trim();
        validate_name(name)?;
        let _guard = self.lock(true)?;
        self.require_room(name)?;
        let removal = self.removal_plan_unlocked(name)?;
        fs::remove_dir_all(self.root.join("rooms").join(name))?;
        sync_dir(&self.root.join("rooms"))?;
        Ok(removal.records)
    }

    fn removal_plan_unlocked(&self, name: &str) -> Result<RoomRemoval> {
        let children: Vec<String> = self
            .rooms_unlocked()?
            .iter()
            .filter(|room| room.parent == name)
            .map(|room| room.name.clone())
            .collect();
        if !children.is_empty() {
            bail!(
                "room {name:?} has child rooms {:?}; move or delete them first",
                children
            );
        }
        Ok(RoomRemoval {
            room: name.into(),
            records: self.records(name)?.len(),
        })
    }

    fn rooms_unlocked(&self) -> Result<Vec<Room>> {
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
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::core::AppendRequest;

    #[test]
    fn delete_refuses_children_then_removes_a_leaf_and_reports_its_records() {
        let dir = tempfile::tempdir().unwrap();
        let store = Store::init(dir.path()).unwrap();
        store.create_room("root", "").unwrap();
        store.create_room("child", "root").unwrap();
        store
            .append(
                "child",
                "a",
                AppendRequest {
                    content: "1",
                    idempotency_key: "",
                },
            )
            .unwrap();
        store
            .append(
                "child",
                "a",
                AppendRequest {
                    content: "2",
                    idempotency_key: "",
                },
            )
            .unwrap();

        let plan = store.room_removal_plan("root").unwrap_err().to_string();
        assert!(plan.contains("child rooms"), "{plan}");
        assert!(store.delete_room("root").is_err());
        assert!(store.room_removal_plan("child").is_ok());

        let removed = store.delete_room("child").unwrap();
        assert_eq!(removed, 2);
        assert!(!store.root.join("rooms").join("child").exists());
        assert!(store.read("child", 0).is_err());
        assert!(store.rooms().unwrap().iter().any(|r| r.name == "root"));
        assert!(store.delete_room("child").is_err());
        assert!(store.room_removal_plan("missing").is_err());
    }
}
