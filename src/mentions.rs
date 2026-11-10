// mentions.rs parses and validates explicit Room-role addresses at the CLI boundary.
use std::collections::HashSet;
use unicode_general_category::{get_general_category, GeneralCategory};

fn letter_or_digit(ch: char) -> bool {
    matches!(
        get_general_category(ch),
        GeneralCategory::UppercaseLetter
            | GeneralCategory::LowercaseLetter
            | GeneralCategory::TitlecaseLetter
            | GeneralCategory::ModifierLetter
            | GeneralCategory::OtherLetter
            | GeneralCategory::DecimalNumber
    )
}

fn name_char(ch: char) -> bool {
    letter_or_digit(ch) || ch == '_' || ch == '-'
}

pub fn valid_role_name(name: &str) -> bool {
    let mut chars = name.chars();
    matches!(chars.next(), Some(ch) if letter_or_digit(ch)) && chars.all(name_char)
}

pub fn parse_explicit(value: &str) -> Result<Vec<String>, String> {
    if value.trim().is_empty() {
        return Ok(Vec::new());
    }
    let mut names = Vec::new();
    let mut seen = HashSet::new();
    for item in value.split(',') {
        let name = item.trim().strip_prefix('@').unwrap_or(item.trim());
        if !valid_role_name(name) {
            return Err(format!("invalid role mention {item:?}"));
        }
        if !seen.insert(name) {
            return Err(format!("role {name:?} mentioned more than once"));
        }
        names.push(name.to_owned());
    }
    Ok(names)
}

// Message bodies are shared Room text; only explicit CLI targets route delivery.
pub fn resolve(
    _text: &str,
    to: &str,
    room: &str,
    available: Option<&[String]>,
) -> Result<Vec<String>, String> {
    let names = parse_explicit(to)?;
    if names.iter().any(|name| name == "all") {
        return Err("@all is not supported; mention a configured role explicitly".into());
    }
    if !names.is_empty() {
        let available = available
            .ok_or_else(|| format!("room {room:?} has no collaborator roles configured"))?;
        for name in &names {
            if !available.contains(name) {
                return Err(format!("role {name:?} is not configured in room {room:?}"));
            }
        }
    }
    Ok(names)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn role_names_follow_unicode_letter_and_decimal_categories() {
        assert!(valid_role_name("审查١_2"));
        assert!(!valid_role_name("_role"));
        assert!(!valid_role_name("role\u{301}"));
        assert!(!valid_role_name("role²"));
    }

    #[test]
    fn explicit_mentions_are_ordered_and_duplicates_rejected() {
        assert_eq!(parse_explicit(" @alice, bob ").unwrap(), ["alice", "bob"]);
        assert!(parse_explicit("@alice,alice")
            .unwrap_err()
            .contains("more than once"));
        assert!(parse_explicit("alice,")
            .unwrap_err()
            .contains("invalid role mention"));
    }

    #[test]
    fn resolve_uses_only_explicit_targets() {
        let roles = vec!["alice".to_owned(), "bob".to_owned()];
        assert_eq!(
            resolve("@alice", "@bob,@alice", "work", Some(&roles)).unwrap(),
            ["bob", "alice"]
        );
        assert_eq!(
            resolve("@alice", "@bob", "work", Some(&roles)).unwrap(),
            ["bob"]
        );
        assert!(resolve("@alice then @bob", "", "work", Some(&roles))
            .unwrap()
            .is_empty());
        assert!(resolve("@all", "", "work", Some(&roles))
            .unwrap()
            .is_empty());
        assert_eq!(
            resolve("@all", "@alice", "work", Some(&roles)).unwrap(),
            ["alice"]
        );
        assert!(resolve("@unknown", "", "work", Some(&roles))
            .unwrap()
            .is_empty());
        assert!(resolve("@alice", "", "work", None).unwrap().is_empty());
        assert!(resolve("ordinary", "", "work", None).unwrap().is_empty());
    }
}
