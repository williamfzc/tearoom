//! Herdr 0.9.5 command and JSON protocol, isolated from Room policy.

use anyhow::{anyhow, bail, Context, Result};
use serde_json::Value;
use std::path::Path;
use std::process::{Command, Stdio};
use std::thread;
use std::time::{Duration, Instant};

mod inspect;
mod session;
use inspect::{collect_panes, has_hc_attach, has_shell, tab_ids, workspace_entries};
pub(in crate::herdr) use session::{ensure_session, open_session, stop_session};

pub(super) fn verify_workspace(
    session: &str,
    id: &str,
    label: &str,
    root_pane: &str,
) -> Result<bool> {
    let listed = run(session, &["workspace", "list"])?;
    let entries = workspace_entries(&listed);
    let Some((_, actual_label)) = entries.iter().find(|entry| entry.0 == id) else {
        return Ok(false);
    };
    if actual_label != label {
        bail!("Herdr workspace ID {id} has label {actual_label:?}, expected {label:?}; refusing to touch a possibly reused ID");
    }
    let panes = run(session, &["pane", "list", "--workspace", id])?;
    if !collect_panes(&panes)
        .iter()
        .any(|(pane, _)| pane == root_pane)
    {
        bail!(
            "Herdr pane {root_pane} is not part of workspace {id}; refusing to touch another pane"
        );
    }
    Ok(true)
}

pub(super) fn find_workspaces(session: &str, label: &str) -> Result<Vec<(String, String)>> {
    let listed = run(session, &["workspace", "list"])?;
    let ids = workspace_entries(&listed)
        .into_iter()
        .filter(|(_, name)| name == label)
        .map(|(id, _)| id)
        .collect::<Vec<_>>();
    let mut found = Vec::new();
    for id in ids {
        let panes = run(session, &["pane", "list", "--workspace", &id])?;
        let panes = collect_panes(&panes);
        match panes.as_slice() {
            [pane] => found.push((id, pane.0.clone())),
            [] => bail!("Herdr workspace {id} matches Tearoom label {label:?} but has no pane"),
            _ => bail!("Herdr workspace {id} matches Tearoom label {label:?} but has multiple panes and no saved root pane; refusing to guess"),
        }
    }
    Ok(found)
}

pub(super) fn create_workspace(session: &str, cwd: &Path, label: &str) -> Result<(String, String)> {
    let cwd = cwd.to_string_lossy().into_owned();
    let response = run(
        session,
        &[
            "workspace",
            "create",
            "--cwd",
            &cwd,
            "--label",
            label,
            "--no-focus",
        ],
    )?;
    let workspace = nested_string(&response, &["result", "workspace", "workspace_id"])
        .or_else(|| nested_string(&response, &["workspace", "workspace_id"]))
        .ok_or_else(|| anyhow!("Herdr workspace create response omitted workspace_id"))?;
    let pane = nested_string(&response, &["result", "root_pane", "pane_id"])
        .or_else(|| nested_string(&response, &["root_pane", "pane_id"]))
        .ok_or_else(|| anyhow!("Herdr workspace create response omitted root_pane.pane_id"))?;
    Ok((workspace, pane))
}

pub(super) fn create_tab(
    session: &str,
    workspace: &str,
    cwd: &Path,
    label: &str,
) -> Result<(String, String)> {
    let cwd = cwd.to_string_lossy().into_owned();
    let response = run(
        session,
        &[
            "tab",
            "create",
            "--workspace",
            workspace,
            "--cwd",
            &cwd,
            "--label",
            label,
            "--no-focus",
        ],
    )?;
    tab_ids(&response)
}

pub(super) fn focus_tab(session: &str, tab: &str) -> Result<()> {
    run(session, &["tab", "focus", tab]).map(|_| ())
}

pub(super) fn pane_terminal_id(session: &str, workspace: &str, pane: &str) -> Result<String> {
    let response = run(session, &["pane", "list", "--workspace", workspace])?;
    collect_panes(&response)
        .into_iter()
        .find(|entry| entry.0 == pane)
        .map(|entry| entry.1)
        .ok_or_else(|| anyhow!("Herdr pane list omitted terminal_id for pane {pane}"))
}

pub(super) fn workspace_panes(session: &str, workspace: &str) -> Result<Vec<(String, String)>> {
    let response = run(session, &["pane", "list", "--workspace", workspace])?;
    Ok(collect_panes(&response))
}

pub(super) fn attach(session: &str, pane: &str, panel: &str) -> Result<()> {
    let info = run(session, &["pane", "process-info", "--pane", pane])?;
    if has_hc_attach(&info, panel) {
        return Ok(());
    }
    if !has_shell(&info) {
        bail!("Herdr pane {pane} has an unexpected foreground process; refusing to send input");
    }
    let command = shell_join(&attach_args(panel)?);
    run(session, &["pane", "run", pane, &command]).map(|_| ())
}

pub(super) fn wait_attached(session: &str, pane: &str, panel: &str) -> Result<bool> {
    let deadline = Instant::now() + Duration::from_secs(5);
    loop {
        if is_attached(session, pane, panel)? {
            return Ok(true);
        }
        if Instant::now() >= deadline {
            return Ok(false);
        }
        thread::sleep(Duration::from_millis(100));
    }
}

pub(super) fn is_attached(session: &str, pane: &str, panel: &str) -> Result<bool> {
    let info = run(session, &["pane", "process-info", "--pane", pane])?;
    Ok(has_hc_attach(&info, panel))
}

pub(super) fn report(
    session: &str,
    pane: &str,
    agent: &str,
    display: &str,
    state: &str,
) -> Result<()> {
    run_owned(
        session,
        &[
            "pane".into(),
            "report-agent".into(),
            "--source".into(),
            "tearoom".into(),
            "--agent".into(),
            agent.into(),
            "--state".into(),
            state.into(),
            pane.into(),
        ],
    )?;
    run_owned(
        session,
        &[
            "pane".into(),
            "report-metadata".into(),
            pane.into(),
            "--source".into(),
            "tearoom".into(),
            "--agent".into(),
            agent.into(),
            "--display-agent".into(),
            display.into(),
        ],
    )
    .map(|_| ())
}

fn run(session: &str, args: &[&str]) -> Result<Value> {
    run_owned(
        session,
        &args.iter().map(|arg| (*arg).to_owned()).collect::<Vec<_>>(),
    )
}

fn run_owned(session: &str, args: &[String]) -> Result<Value> {
    let executable = std::env::var_os("TEAROOM_HERDR_BIN").unwrap_or_else(|| "herdr".into());
    let mut command = Command::new(executable);
    command
        .env_remove("HERDR_SOCKET_PATH")
        .env("HERDR_SESSION", session);
    command.arg("--session").arg(session);
    command.args(args);
    if args.starts_with(&["workspace".into(), "create".into()])
        || args.starts_with(&["tab".into(), "create".into()])
    {
        add_hc_env(&mut command);
    }
    let mut child = command
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .with_context(|| {
            format!(
                "start Herdr command {}",
                args.first().map(String::as_str).unwrap_or("<empty>")
            )
        })?;
    let deadline = Instant::now() + Duration::from_secs(20);
    loop {
        if child.try_wait()?.is_some() {
            let output = child.wait_with_output()?;
            if !output.status.success() {
                bail!(
                    "Herdr command failed ({}): {}",
                    output.status,
                    String::from_utf8_lossy(&output.stderr).trim()
                );
            }
            if output.stdout.iter().all(u8::is_ascii_whitespace) {
                return Ok(Value::Null);
            }
            return serde_json::from_slice(&output.stdout).context("parse Herdr JSON response");
        }
        if Instant::now() >= deadline {
            let _ = child.kill();
            let _ = child.wait();
            bail!("Herdr command timed out after 20 seconds");
        }
        thread::sleep(Duration::from_millis(40));
    }
}

fn add_hc_env(command: &mut Command) {
    for name in [
        "HC_SESSION",
        "HC_SOCKET",
        "HOME",
        "XDG_CONFIG_HOME",
        "XDG_STATE_HOME",
        "XDG_RUNTIME_DIR",
    ] {
        let value = std::env::var_os(name).unwrap_or_default();
        command
            .arg("--env")
            .arg(format!("{name}={}", value.to_string_lossy()));
    }
}

fn nested_string(value: &Value, path: &[&str]) -> Option<String> {
    path.iter()
        .try_fold(value, |value, key| value.get(*key))?
        .as_str()
        .map(str::to_owned)
}

fn shell_join(args: &[String]) -> String {
    args.iter()
        .map(|arg| format!("'{}'", arg.replace('\'', "'\\''")))
        .collect::<Vec<_>>()
        .join(" ")
}

fn attach_args(panel: &str) -> Result<Vec<String>> {
    let mut args = vec!["env".into()];
    for name in [
        "HC_SESSION",
        "HC_SOCKET",
        "HOME",
        "XDG_CONFIG_HOME",
        "XDG_STATE_HOME",
        "XDG_RUNTIME_DIR",
    ] {
        let value = std::env::var_os(name).unwrap_or_default();
        args.push(format!("{name}={}", value.to_string_lossy()));
    }
    let tearoom = std::env::current_exe()
        .context("resolve Tearoom executable for Herdr pane")?
        .to_string_lossy()
        .into_owned();
    args.extend([tearoom, "hc".into(), "attach".into(), panel.into()]);
    Ok(args)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn finds_only_the_exact_stable_workspace_label() {
        let value = serde_json::json!({
            "result":{"workspaces":[
                {"workspace_id":"w1","label":"tr-project-abc"},
                {"workspace_id":"w2","label":"tr-project-abd"}
            ]}
        });
        assert_eq!(
            workspace_entries(&value),
            [
                ("w1".into(), "tr-project-abc".into()),
                ("w2".into(), "tr-project-abd".into())
            ]
        );
    }

    #[test]
    fn reads_terminal_identity_without_confusing_panes() {
        let value = serde_json::json!({"result":{"panes":[
            {"pane_id":"w1:p1","terminal_id":"t1"},
            {"pane_id":"w1:p2","terminal_id":"t2"}
        ]}});
        assert_eq!(
            collect_panes(&value),
            [("w1:p1".into(), "t1".into()), ("w1:p2".into(), "t2".into())]
        );
    }

    #[test]
    fn reads_tab_and_root_pane_from_herdr_response() {
        let response = serde_json::json!({
            "result": {
                "root_pane": {"pane_id": "w1:p3"},
                "tab": {"tab_id": "w1:t2"}
            }
        });
        assert_eq!(
            tab_ids(&response).unwrap(),
            ("w1:t2".into(), "w1:p3".into())
        );
        assert!(tab_ids(&serde_json::json!({"result":{"tab":{}}})).is_err());
    }

    #[test]
    fn shell_arguments_preserve_quotes_and_spaces() {
        let command = shell_join(&["hc".into(), "attach".into(), "panel name' 1".into()]);
        assert_eq!(command, "'hc' 'attach' 'panel name'\\'' 1'");
    }

    #[test]
    fn herdr_attach_uses_the_current_tearoom_executable() {
        let args = attach_args("panel").unwrap();
        assert_eq!(args[args.len() - 3], "hc");
        assert_eq!(args[args.len() - 2], "attach");
        assert_eq!(args[args.len() - 1], "panel");
        assert!(std::path::Path::new(&args[args.len() - 4]).is_absolute());
    }

    #[test]
    fn requires_the_attached_hc_terminal_for_that_panel() {
        let matching = serde_json::json!({"result":{"process_info":{"foreground_processes":[
            {"argv":["/opt/bin/hc","attach","tr-abc"],"argv0":"hc","name":"herdr-core"}
        ]}}});
        let shell = serde_json::json!({"result":{"process_info":{"foreground_processes":[
            {"argv":["-zsh"],"argv0":"zsh","name":"zsh"}
        ]}}});
        assert!(has_hc_attach(&matching, "tr-abc"));
        assert!(!has_hc_attach(&matching, "tr-other"));
        assert!(!has_hc_attach(&shell, "tr-abc"));

        let embedded = serde_json::json!({"result":{"process_info":{"foreground_processes":[
            {"argv":["tearoom-hc-internal","attach","tr-abc"],"argv0":"tearoom-hc-internal","name":"tearoom"}
        ]}}});
        assert!(has_hc_attach(&embedded, "tr-abc"));
        assert!(!has_hc_attach(&embedded, "tr-other"));
    }

    #[test]
    fn shell_is_not_inferred_from_an_unrelated_foreground_process() {
        let shell = serde_json::json!({"result":{"process_info":{"foreground_processes":[
            {"argv":["-zsh"],"argv0":"zsh","name":"zsh"}
        ]}}});
        let editor = serde_json::json!({"result":{"process_info":{"foreground_processes":[
            {"argv":["vim"],"argv0":"vim","name":"vim"}
        ]}}});
        assert!(has_shell(&shell));
        assert!(!has_shell(&editor));
    }
}
