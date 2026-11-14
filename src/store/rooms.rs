//! Room-tree lifecycle on the workspace: create, edit, resolve, move, list,
//! and delete. Record append and read remain in the parent module; this file
//! owns the per-Room directory shape (room.json, records, ledgers) as a unit.
//! A Room's id is its birth directory name and every stored reference anchors
//! to it; the name and description are editable labels.

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
    /// Creates a Room whose id is its birth directory name: the name it is
    /// created with stays reserved for as long as the Room lives.
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
        let rooms = self.rooms_unlocked()?;
        if rooms
            .iter()
            .any(|room| room.name == name || room.id == name)
        {
            bail!("room name {name:?} is already in use: conflict");
        }
        if self.root.join("rooms").join(name).exists() {
            bail!("room directory {name:?} exists without a Room; inspect it first");
        }
        let room = Room {
            name: name.into(),
            id: name.to_owned(),
            parent: parent.into(),
            description: String::new(),
            created: now(),
        };
        self.write_room_dir(&room)?;
        Ok(room)
    }

    /// Edits a Room's labels: its display name and description. The id,
    /// parent, records, and every stored reference stay untouched.
    pub fn update_room(
        &self,
        id: &str,
        name: Option<&str>,
        description: Option<&str>,
    ) -> Result<Room> {
        let id = id.trim();
        validate_name(id)?;
        let _guard = self.lock(true)?;
        let mut rooms = self.rooms_unlocked()?;
        let index = rooms
            .iter()
            .position(|room| room.id == id)
            .with_context(|| format!("room {id:?}: not found"))?;
        if let Some(name) = name {
            let name = name.trim();
            validate_name(name)?;
            if rooms
                .iter()
                .any(|other| other.id != id && (other.name == name || other.id == name))
            {
                bail!("room name {name:?} is already in use: conflict");
            }
            rooms[index].name = name.into();
        }
        if let Some(description) = description {
            rooms[index].description = description.trim().into();
        }
        let room = rooms[index].clone();
        replace(&self.path(id, "room.json"), &go_json_pretty(&room)?)?;
        Ok(room)
    }

    /// Resolves a Room reference — id or current name — to its id. Ids are
    /// canonical: a reference matching an id wins over a name match.
    pub fn resolve_room(&self, room_ref: &str) -> Result<String> {
        let room_ref = room_ref.trim();
        let _guard = self.lock(false)?;
        let rooms = self.rooms_unlocked()?;
        if rooms.iter().any(|room| room.id == room_ref) {
            return Ok(room_ref.to_owned());
        }
        let matches: Vec<&Room> = rooms.iter().filter(|room| room.name == room_ref).collect();
        match matches.as_slice() {
            [room] => Ok(room.id.clone()),
            [] => bail!("room {room_ref:?}: not found"),
            _ => bail!("room name {room_ref:?} matches several rooms; pass its id"),
        }
    }

    /// Changes a Room's parent without moving its identity or stored data.
    pub fn move_room(&self, id: &str, parent: &str) -> Result<Room> {
        let id = id.trim();
        let parent = parent.trim();
        validate_name(id)?;
        if !parent.is_empty() {
            validate_name(parent)?;
        }
        let _guard = self.lock(true)?;
        let rooms = self.rooms_unlocked()?;
        let mut room = rooms
            .iter()
            .find(|room| room.id == id)
            .cloned()
            .with_context(|| format!("room {id:?}: not found"))?;
        if !parent.is_empty() && !rooms.iter().any(|room| room.id == parent) {
            bail!("room {parent:?}: not found");
        }
        let mut ancestor = parent.to_owned();
        let mut seen = std::collections::HashSet::new();
        while !ancestor.is_empty() {
            if ancestor == id {
                bail!("cannot move room {id:?} under itself or one of its descendants");
            }
            if !seen.insert(ancestor.clone()) {
                bail!("room tree contains a parent cycle at {ancestor:?}");
            }
            ancestor = rooms
                .iter()
                .find(|room| room.id == ancestor)
                .map_or(String::new(), |room| room.parent.clone());
        }
        room.parent = parent.into();
        replace(&self.path(&room.id, "room.json"), &go_json_pretty(&room)?)?;
        Ok(room)
    }

    pub fn rooms(&self) -> Result<Vec<Room>> {
        let _guard = self.lock(false)?;
        self.rooms_unlocked()
    }

    /// Reports what deleting one leaf Room would remove without removing it.
    pub fn room_removal_plan(&self, id: &str) -> Result<RoomRemoval> {
        let id = id.trim();
        validate_name(id)?;
        let _guard = self.lock(false)?;
        self.require_room(id)?;
        self.removal_plan_unlocked(id)
    }

    /// Removes a leaf Room with its whole record stream and returns the count.
    /// The removal is irreversible; role definitions and other Rooms survive.
    pub fn delete_room(&self, id: &str) -> Result<usize> {
        let id = id.trim();
        validate_name(id)?;
        let _guard = self.lock(true)?;
        self.require_room(id)?;
        let removal = self.removal_plan_unlocked(id)?;
        fs::remove_dir_all(self.root.join("rooms").join(id))?;
        sync_dir(&self.root.join("rooms"))?;
        Ok(removal.records)
    }

    fn removal_plan_unlocked(&self, id: &str) -> Result<RoomRemoval> {
        let children: Vec<String> = self
            .rooms_unlocked()?
            .iter()
            .filter(|room| room.parent == id)
            .map(|room| room.id.clone())
            .collect();
        if !children.is_empty() {
            bail!(
                "room {id:?} has child rooms {:?}; move or delete them first",
                children
            );
        }
        Ok(RoomRemoval {
            room: id.into(),
            records: self.records(id)?.len(),
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
            let mut room: Room = serde_json::from_slice(&fs::read(path)?)?;
            // The id is anchored to the directory: a moved or hand-edited
            // directory fails closed instead of silently re-identifying.
            // A file without an id predates the field; its name was the id.
            if room.id.is_empty() {
                if room.name != name {
                    bail!("room directory {name:?} declares name {:?}", room.name);
                }
                room.id = name;
            } else if room.id != name {
                bail!("room directory {name:?} declares id {:?}", room.id);
            }
            rooms.push(room);
        }
        Ok(rooms)
    }

    fn write_room_dir(&self, room: &Room) -> Result<()> {
        let rooms = self.root.join("rooms");
        let temp = tempfile::Builder::new()
            .prefix(".tmp-")
            .tempdir_in(&rooms)?;
        replace(&temp.path().join("room.json"), &go_json_pretty(room)?)?;
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            fs::set_permissions(temp.path(), fs::Permissions::from_mode(0o755))?;
        }
        fs::rename(temp.path(), rooms.join(&room.id))?;
        sync_dir(&rooms)?;
        Ok(())
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

    #[test]
    fn update_changes_labels_while_identity_records_and_references_survive() {
        let dir = tempfile::tempdir().unwrap();
        let store = Store::init(dir.path()).unwrap();
        let room = store.create_room("project", "").unwrap();
        store
            .append(
                "project",
                "a",
                AppendRequest {
                    content: "\"kept\"",
                    idempotency_key: "",
                },
            )
            .unwrap();

        let edited = store
            .update_room(&room.id, Some("workflow"), Some("payments review"))
            .unwrap();
        assert_eq!(edited.id, "project");
        assert_eq!(edited.name, "workflow");
        assert_eq!(edited.description, "payments review");
        // The id is the address: reads and appends by id keep working, and
        // the record stream is untouched.
        assert_eq!(
            store.read("project", 0).unwrap()[0].content.get(),
            "\"kept\""
        );
        store
            .append(
                "project",
                "a",
                AppendRequest {
                    content: "2",
                    idempotency_key: "",
                },
            )
            .unwrap();
        let listed = store.rooms().unwrap();
        let listed = listed.iter().find(|r| r.id == "project").unwrap();
        assert_eq!(listed.name, "workflow");
        assert_eq!(listed.created, room.created);
        // The current name and the id both resolve; nothing else does.
        assert_eq!(store.resolve_room("workflow").unwrap(), "project");
        assert_eq!(store.resolve_room("project").unwrap(), "project");
        assert!(store.resolve_room("missing").is_err());
    }

    #[test]
    fn update_refuses_names_that_collision_with_other_rooms() {
        let dir = tempfile::tempdir().unwrap();
        let store = Store::init(dir.path()).unwrap();
        store.create_room("alpha", "").unwrap();
        let beta = store.create_room("beta", "alpha").unwrap();
        assert!(store.update_room(&beta.id, Some("alpha"), None).is_err());
        // A renamed Room keeps its birth name as id; that id stays reserved.
        store.update_room("alpha", Some("gamma"), None).unwrap();
        assert!(store.update_room(&beta.id, Some("alpha"), None).is_err());
        // Clearing a description is explicit.
        store
            .update_room(&beta.id, None, Some("temporary"))
            .unwrap();
        assert_eq!(
            store
                .update_room(&beta.id, None, Some(""))
                .unwrap()
                .description,
            ""
        );
    }

    #[test]
    fn a_legacy_room_file_adopts_its_directory_name_as_id() {
        let dir = tempfile::tempdir().unwrap();
        let store = Store::init(dir.path()).unwrap();
        let rooms = store.root.join("rooms");
        std::fs::create_dir_all(rooms.join("old")).unwrap();
        std::fs::write(
            rooms.join("old").join("room.json"),
            r#"{"name":"old","created":"t"}"#,
        )
        .unwrap();
        let listed = store.rooms().unwrap();
        assert_eq!(listed[0].id, "old");
        assert!(store.resolve_room("old").unwrap() == "old");
        // The first edit persists the adopted id.
        store.update_room("old", Some("renamed"), None).unwrap();
        let on_disk = std::fs::read_to_string(rooms.join("old").join("room.json")).unwrap();
        assert!(on_disk.contains(r#""id": "old""#), "{on_disk}");
        assert!(on_disk.contains(r#""name": "renamed""#), "{on_disk}");
    }

    #[test]
    fn a_birth_name_stays_reserved_by_its_room() {
        let dir = tempfile::tempdir().unwrap();
        let store = Store::init(dir.path()).unwrap();
        store.create_room("alpha", "").unwrap();
        store.update_room("alpha", Some("beta"), None).unwrap();
        // The birth name is the id: a new Room cannot take it while the
        // renamed Room lives, and neither can another name collide with it.
        let conflict = store.create_room("alpha", "").unwrap_err();
        assert!(
            conflict.to_string().contains("already in use"),
            "{conflict}"
        );
        let sibling = store.create_room("gamma", "").unwrap();
        assert!(store.update_room(&sibling.id, Some("alpha"), None).is_err());
        // Both rooms resolve by id and by current display name.
        assert_eq!(store.resolve_room("alpha").unwrap(), "alpha");
        assert_eq!(store.resolve_room("beta").unwrap(), "alpha");
        assert_eq!(store.resolve_room("gamma").unwrap(), "gamma");
    }
}
