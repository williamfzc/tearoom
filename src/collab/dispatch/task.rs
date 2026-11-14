//! Dispatch task composition: the prompt one delivery hands to a panel.
//! Pure text assembly; routing and state live in the sibling modules.

use crate::core::Room;

/// Composes the delivery prompt. The Room is shown by its current display
/// name and a non-empty description rides along, so a freshly launched panel
/// learns what the Room is for; the record history itself stays id-addressed
/// JSON.
pub(super) fn compose(
    room: Option<&Room>,
    id: &str,
    role_name: &str,
    seq: u64,
    history: &str,
    cli: &str,
) -> String {
    let display = room.map_or(id, |room| room.name.as_str());
    let head = match room.filter(|room| !room.description.is_empty()) {
        Some(room) => format!(
            "Room {display} ({}), addressed to {role_name} at record {seq}.",
            room.description
        ),
        None => format!("Room {display}, addressed to {role_name} at record {seq}."),
    };
    format!("{head} New Room records (JSON):\n{history}\nTearoom CLI: {cli}")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn undescribed_rooms_keep_the_plain_task_shape() {
        let task = compose(None, "project", "reviewer", 2, "[history]", "'tearoom'");
        assert_eq!(
            task,
            "Room project, addressed to reviewer at record 2. New Room records (JSON):\n[history]\nTearoom CLI: 'tearoom'"
        );
    }

    #[test]
    fn a_described_room_rides_its_purpose_into_the_prompt() {
        let room = Room {
            name: "workflow".into(),
            id: "project".into(),
            parent: String::new(),
            description: "payments review".into(),
            created: "t".into(),
        };
        let task = compose(Some(&room), "project", "reviewer", 1, "[]", "'tearoom'");
        assert!(task.starts_with("Room workflow (payments review), addressed to reviewer"));
        assert!(task.contains("New Room records (JSON):\n[]"));
    }
}
