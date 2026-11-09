//! Parse Herdr's live workspace and foreground-process snapshots.

use anyhow::{anyhow, Result};
use serde_json::Value;

pub(super) fn workspace_entries(value: &Value) -> Vec<(String, String)> {
    let mut entries = Vec::new();
    visit(value, &mut |object| {
        if let (Some(id), Some(label)) = (
            object.get("workspace_id").and_then(Value::as_str),
            object.get("label").and_then(Value::as_str),
        ) {
            entries.push((id.into(), label.into()));
        }
    });
    entries.sort();
    entries.dedup();
    entries
}

pub(super) fn collect_panes(value: &Value) -> Vec<(String, String)> {
    let mut panes = Vec::new();
    visit(value, &mut |object| {
        if let (Some(pane), Some(terminal)) = (
            object.get("pane_id").and_then(Value::as_str),
            object.get("terminal_id").and_then(Value::as_str),
        ) {
            panes.push((pane.into(), terminal.into()));
        }
    });
    panes.sort();
    panes.dedup();
    panes
}

pub(super) fn tab_ids(response: &Value) -> Result<(String, String)> {
    let tab = response
        .pointer("/result/tab/tab_id")
        .or_else(|| response.pointer("/tab/tab_id"))
        .and_then(Value::as_str)
        .ok_or_else(|| anyhow!("Herdr tab create response omitted result.tab.tab_id"))?;
    let pane = response
        .pointer("/result/root_pane/pane_id")
        .or_else(|| response.pointer("/root_pane/pane_id"))
        .and_then(Value::as_str)
        .ok_or_else(|| anyhow!("Herdr tab create response omitted result.root_pane.pane_id"))?;
    Ok((tab.into(), pane.into()))
}

pub(super) fn has_hc_attach(value: &Value, panel: &str) -> bool {
    let mut found = false;
    visit(value, &mut |object| {
        if let Some(argv) = object.get("argv").and_then(Value::as_array) {
            let args = argv.iter().filter_map(Value::as_str).collect::<Vec<_>>();
            found |= args.len() >= 3
                && args[args.len() - 2] == "attach"
                && args[args.len() - 1] == panel
                && (args[args.len() - 3] == "hc" || args[args.len() - 3].ends_with("/hc"));
        }
    });
    found
}

pub(super) fn has_shell(value: &Value) -> bool {
    let mut found = false;
    visit(value, &mut |object| {
        let name = object
            .get("argv0")
            .or_else(|| object.get("name"))
            .and_then(Value::as_str)
            .unwrap_or("");
        found |= [
            "bash",
            "zsh",
            "fish",
            "nu",
            "sh",
            "tcsh",
            "elvish",
            "pwsh",
            "powershell",
        ]
        .iter()
        .any(|shell| name == *shell || name.ends_with(&format!("/{shell}")));
    });
    found
}

fn visit(value: &Value, apply: &mut impl FnMut(&serde_json::Map<String, Value>)) {
    match value {
        Value::Object(object) => {
            apply(object);
            for child in object.values() {
                visit(child, apply);
            }
        }
        Value::Array(array) => {
            for child in array {
                visit(child, apply);
            }
        }
        _ => {}
    }
}
