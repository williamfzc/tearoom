// mentions.rs parses explicit Room-role addresses at the CLI boundary.
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

// Extract mentions in textual order, ignoring Markdown code and email addresses.
pub fn extract(text: &str) -> Vec<String> {
    let mut names = Vec::new();
    let mut seen = HashSet::new();
    let mut fence = None;
    for line in text.split_inclusive('\n') {
        let trimmed = line.trim_start_matches([' ', '\t']);
        if let Some(marker) = fence_marker(trimmed) {
            match fence {
                None => fence = Some(marker),
                Some(open) if open == marker => fence = None,
                _ => {}
            }
            continue;
        }
        if fence.is_some() {
            continue;
        }
        let bytes = line.as_bytes();
        let mut i = 0;
        while i < bytes.len() {
            if bytes[i] == b'`' {
                let run = backtick_run(bytes, i);
                let mut end = i + run;
                while end < bytes.len() {
                    if bytes[end] != b'`' {
                        end += 1;
                        continue;
                    }
                    let current = backtick_run(bytes, end);
                    if current == run {
                        break;
                    }
                    end += current;
                }
                i = if end < bytes.len() {
                    end + run
                } else {
                    i + run
                };
                continue;
            }
            if bytes[i] != b'@' || escaped(bytes, i) || email_local_part(line, i) {
                i += 1;
                continue;
            }
            let start = i + 1;
            let mut end = start;
            for ch in line[start..].chars() {
                if !name_char(ch) {
                    break;
                }
                end += ch.len_utf8();
            }
            if end == start || bytes.get(end) == Some(&b'@') {
                i += 1;
                continue;
            }
            let name = &line[start..end];
            if valid_role_name(name) && seen.insert(name.to_owned()) {
                names.push(name.to_owned());
            }
            i = end;
        }
    }
    names
}

fn fence_marker(line: &str) -> Option<u8> {
    let bytes = line.as_bytes();
    let marker = *bytes.first()?;
    if (marker == b'`' || marker == b'~') && bytes.iter().take_while(|&&b| b == marker).count() >= 3
    {
        Some(marker)
    } else {
        None
    }
}

fn backtick_run(bytes: &[u8], start: usize) -> usize {
    bytes[start..].iter().take_while(|&&b| b == b'`').count()
}

fn escaped(bytes: &[u8], index: usize) -> bool {
    bytes[..index]
        .iter()
        .rev()
        .take_while(|&&b| b == b'\\')
        .count()
        % 2
        == 1
}

fn email_local_part(text: &str, index: usize) -> bool {
    text[..index]
        .chars()
        .next_back()
        .is_some_and(|ch| letter_or_digit(ch) || "._%+-".contains(ch))
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

// Resolve the Room roster after parsing, retaining first-mention ordering.
pub fn resolve(
    text: &str,
    to: &str,
    room: &str,
    available: Option<&[String]>,
) -> Result<Vec<String>, String> {
    let mut names = extract(text);
    for name in parse_explicit(to)? {
        if !names.contains(&name) {
            names.push(name);
        }
    }
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
    fn prose_ignores_code_email_and_escaped_mentions() {
        let text = "@builder email me@reviewer.com \\@hidden \\\\@visible `@code` ``@code2``\n```rust\n@fenced\n```\n@审查 @builder";
        assert_eq!(extract(text), ["builder", "visible", "审查"]);
    }

    #[test]
    fn fences_unmatched_backticks_and_email_edges_match_go() {
        assert_eq!(
            extract("`unclosed @yes\n~~~\n@no\n```\n@still_no\n~~~\n@after\n@x@y"),
            ["yes", "after"]
        );
    }

    #[test]
    fn role_names_follow_unicode_letter_and_decimal_categories() {
        assert!(valid_role_name("审查١_2"));
        assert!(!valid_role_name("_role"));
        assert!(!valid_role_name("role\u{301}"));
        assert!(!valid_role_name("role²"));
        assert_eq!(extract("@审查١_2 @role² @bad!"), ["审查١_2", "role", "bad"]);
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
    fn resolve_validates_all_and_roster_without_losing_order() {
        let roles = vec!["alice".to_owned(), "bob".to_owned()];
        assert_eq!(
            resolve("@alice", "@bob,@alice", "work", Some(&roles)).unwrap(),
            ["alice", "bob"]
        );
        assert!(resolve("@all", "", "work", Some(&roles)).is_err());
        assert!(resolve("@unknown", "", "work", Some(&roles))
            .unwrap_err()
            .contains("not configured"));
        assert!(resolve("@alice", "", "work", None)
            .unwrap_err()
            .contains("no collaborator"));
        assert!(resolve("ordinary", "", "work", None).unwrap().is_empty());
    }
}
