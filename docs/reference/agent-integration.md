---
type: reference
title: Agent integration guide
description: Configure Agent roles, hc panels, readiness, and safe handoffs in a Tearoom workspace.
tags: [reference, agents, integration, cli, hc]
timestamp: 2026-11-08
---

# Agent integration guide

This guide describes the implemented local hc integration and the limits still
being validated. The binary carries a short copyable setup path in
`tearoom skill`; this page explains the agent-specific configuration. The
command contract remains in [cli.md](cli.md), and current validation state is
in [implementation status](../architecture/status.md).

## Configure a Room and its roles

Run from the project directory. The first Room creates `.tearoom`; each role is
a reusable launch definition. A Room selects which definitions participate.
This roster routes addressed messages and is not an access-control list.

```bash
(
set -eu
tearoom room init --name project

mkdir -p .tearoom
if [ -e .tearoom/claude-settings.json ]; then cat .tearoom/claude-settings.json; exit 1; fi
cat > .tearoom/claude-settings.json <<'JSON'
{"sandbox":{"enabled":true,"autoAllowBashIfSandboxed":true,"allowUnsandboxedCommands":false,"failIfUnavailable":true}}
JSON
: "${TEAROOM_SOURCE:?set to the Tearoom source checkout containing scripts/pi-hc-status.ts}"
export PI_HC_STATUS_EXTENSION="$TEAROOM_SOURCE/scripts/pi-hc-status.ts"
export PI_BIN="${PI_BIN:-pi}"
test -r "$PI_HC_STATUS_EXTENSION"
"$PI_BIN" --version

tearoom role define --name builder --description 'Implement the requested change' \
  --agent-type claude --command claude \
  --arg=--settings --arg="$PWD/.tearoom/claude-settings.json" \
  --arg=--permission-mode --arg=dontAsk \
  --instructions 'Implement and record evidence. If a command is denied, try simpler permitted commands; never disable the sandbox. Record unresolved blockers.'

tearoom role define --name reviewer --description 'Review implementation evidence' \
  --agent-type pi --command "$PI_BIN" \
  --arg=--extension --arg="$PI_HC_STATUS_EXTENSION" \
  --instructions 'Review the implementation and its evidence. Record a verdict with concrete findings.'

tearoom room roles --room project --add builder,reviewer
tearoom room roles --room project --status
tearoom collab run
)
```

The example assumes a host with `claude`, `pi`, and a configured Pi provider and
model. Set `TEAROOM_SOURCE` to the Tearoom source checkout containing the Pi
extension before running these commands. The `PI_BIN` override selects the Pi
executable; the version check confirms what it resolves to, while provider and
model availability still need local verification. On this machine, `pi`
resolves to a ZCode shim and `pi-real` is the Pi executable, so use
`PI_BIN=pi-real` here. Replace the command and repeated `--arg` values with the
local Agent CLI and its configuration.
Alternatively, define a role with `--template <hc-template>`; the hc template
must already exist on the host. Role definitions and Room bindings are stored
in the project's `.tearoom` workspace metadata. Do not put tokens or passwords
in these records or in role instructions.

The project role is not hard-coded to a specific model or tool. A builder and
reviewer can use different Agent CLIs, and roles can be reused in other Rooms.
Each Room-role binding resolves to its own hc panel, so the same role in two
Rooms has separate panel state. `tearoom room roles --room project` shows the
configured roster; adding a role after creation uses `--add`, and removing it
uses `--remove`.

## Message routing

Messages without a target are broadcast into shared Room history. They do not
start or notify offline panels. Address a configured Room role either in text
or with `--to`:

```bash
tearoom send --room project --as builder \
  --to @reviewer --reply-to project#1 \
  --text 'Implementation is ready. Please review the diff and the recorded test output.'
```

The dispatcher reads each addressed record and delivers it to that Room-role's
panel. The record remains in the same Room stream for everyone. A body mention
such as `@reviewer` is recognized outside code spans, code fences, escaped at
signs, and email addresses. Unknown roles and `@all` are rejected. For
addressed sends the message is stored as structured text, mentions, and an
optional `reply_to` reference. `--reply-to project#1` is a coordination pointer;
Tearoom does not check identity or infer that the referenced work was done.

One participant may work in the shared project Room and address the reviewer
there. When work truly needs separate Rooms, coordinate them in a shared
ancestor Room; one Room's roster does not include roles from a sibling Room.

## Claude permissions are per launch

The example passes `--settings <path>` to the Claude process started for this
role. Claude's `--settings` option loads settings for that process invocation;
it does not edit a user's global Claude configuration. The sandbox example
allows Bash commands that remain inside Claude's sandbox and refuses to fall
back when sandboxing cannot be provided. `dontAsk` denies operations requiring
approval rather than waiting for a person. The Agent may simplify its command
or record a blocker; this does not ensure every task will complete. Review the
[Claude permissions guide](https://code.claude.com/docs/en/permissions)
and [sandboxing guide](https://code.claude.com/docs/en/sandboxing) for the
current behavior of the installed Claude version.

If a Go test cannot write its build cache, set a writable `GOCACHE` in the
role's per-session settings. Some `goenv` shims overwrite `GOCACHE`; in that
case also put the intended Go binary directory before the shim in the session
`PATH`. The environment is machine-specific. The local probe is recorded in
[evaluations.md](../evaluations.md).

The settings example was probed in an isolated temporary session: a Git read
and Go test command completed without an approval prompt, and a home-directory
write was denied. This proves only that small probe, not every Claude tool or
the full Tearoom collaboration journey. Keep the settings file local, review
the policy before enabling unattended work, and resolve any initial trust or
login prompt interactively before dispatching.

## Pi lifecycle status

hc needs terminal status to distinguish idle, working, done, and blocked
panels. Pi does not currently emit hc's OSC status by default in this setup.
The optional `scripts/pi-hc-status.ts` extension maps Pi lifecycle events to
the OSC status understood by hc:

```typescript
export default function (pi: any) {
  const report = (state: string) => process.stdout.write(`\u001b]7501;state=${state}\u0007`);
  pi.on("session_start", () => report("idle"));
  pi.on("agent_start", () => report("working"));
  pi.on("agent_end", () => report("done"));
  pi.on("agent_error", () => report("error"));
}
```

Load it with Pi's `--extension <path>` option, as in the role example. A Pi
`done` event means its turn ended; it does not mean Tearoom's acceptance
criteria passed. A local smoke probe observed idle/working/done through hc for
short prompts; the extension and longer workflow remain part of integration
validation.

## Startup and observed readiness

Before `tearoom collab run`, install hc, read `hc --skill`, initialize hc, and
authenticate the Agent CLIs. Resolve one-time trust, hooks, update, and login
prompts in the actual working directory. A terminal that is blocked on a first
run dialog is not ready. Do not start unattended work until the intended
Agent's startup has been observed by a person.

For roles using hc templates, Tearoom preflights that the installed template
exists and its command matches. `ready: quiet` is rejected because an update or
permission prompt may otherwise look like a quiet ready panel. Missing or
`ready: report` values pass this preflight; the tool still needs to report
status reliably. Codex template and direct-command launches require
`--no-daemon` so the dispatcher controls the panel it observes. Run
`tearoom room roles --room project --status` to ask hc about each bound panel.
The reported value is only a snapshot: `missing`, `idle`, `working`, `blocked`,
or `unknown` does not prove task completion.

The dispatcher persists delivery intents and serializes work per Room-role.
`tearoom collab status` displays the ledger. A submitted prompt means only that
hc accepted input; a completed delivery means a matching Room reply was
observed, not that its author or the work was verified. Inspect the record and
independent panel evidence to judge the work.
Agents without a reliable lifecycle signal stay pending rather than being
assumed idle or done. The current `done` check followed by submission is not
atomic, so another sender can race in between. Exactly-once execution is not
promised.

Resolve only deliveries with an `unknown` outcome, after inspecting both hc and
the Room:

```bash
tearoom collab resolve --room project --seq 2 --role reviewer --outcome delivered
tearoom collab resolve --room project --seq 3 --role builder --outcome skip
tearoom collab resolve --room project --seq 4 --role reviewer --outcome retry --confirm
```

`delivered` stops retries while waiting for the Agent's Room receipt. `skip`
closes that delivery and releases later messages for the role. `retry --confirm`
can repeat work or external side effects. Never retry an ambiguous delivery
without first reconciling the panel and Room.

## Identity and independent review limits

`--as` is a free-form signature, including when supplied through
`TEAROOM_ACTOR`. Tearoom does not authenticate the process or prevent it from
using another role's label. A reviewer record, a separate panel, or a
`reviewer` signature therefore cannot establish that the reviewer was
independent. For meaningful review, arrange a genuinely separate Agent/tool
and inspect concrete evidence from its review. The current runtime routes work
but does not enforce reviewer identity or prove reviewer independence.

## Validation state

Room roles, hc panel routing, mention delivery, durable delivery status,
resolution commands, Claude settings plumbing, and the Pi status extension
have implementation or focused probe evidence. The complete unattended
multi-Agent journey, first-run recovery, different-tool independence, and
two-week real-work validation remain open. See
[implementation status](../architecture/status.md) and the hand-run records in
[evaluations](../evaluations.md) before treating the integration as proven.
