---
type: reference
title: Agent integration guide
description: Configure Agent roles, hc panels, readiness, and safe handoffs in a Tearoom workspace.
tags: [reference, agents, integration, cli, hc]
timestamp: 2026-11-08
---

# Agent integration guide

This guide covers local Agent setup, hc panel visibility, and recovery limits.
The binary carries a short setup path in `tearoom skill`. Command names, flags,
and behavior are owned by [cli.md](cli.md); validation evidence is in
[evaluations](../evaluations.md) and implementation gaps are in
[status](../architecture/status.md).

The everyday path is to define reusable roles, create a Room and select its
roles, give the Room a goal, then let the worker and reviewer use its shared
history. Roles can be assigned or changed later. Herdr shows the running
panels when available. The setup below is a machine-specific example, not a
required choice of Agents; it includes readiness and recovery details for
operators.

## Example: configure a Room and its roles

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
tearoom room roles --room project --observe
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
Role definitions and Room bindings are stored
in the project's `.tearoom` workspace metadata. Do not put tokens or passwords
in these records or in role instructions.

Role definitions can launch different Agent CLIs and can be reused across
Rooms. Each Room-role binding has its own hc panel. A roster routes addressed
messages; it does not grant access to Room history or writes. See the
[CLI contract](cli.md#room-lifecycle) for roster changes and definition updates.
Changes to a running process's executable, agent type, or
working directory take effect when a new process starts.

## Herdr session per Room

Each Room has its own named local Herdr session, with its role panels in tabs.
Run `tearoom room open --room <name>` from the workspace to open it. Room
creation prepares the session when Herdr is available; if that fails, Room
history remains usable and `tearoom room herdr ensure --room <name>` retries.
A role panel appears when an addressed message starts it; its displayed state
comes from hc. Existing panels in the former shared session are not closed.

Herdr 0.9.5 also remembers a separate selected machine for its interface.
If it opens on a previously selected remote machine, select Local in the
Herdr sidebar to view the Room session. Tearoom does not change the user's
saved machine selection or disable machines.

If Herdr restarts and a pane becomes a shell, Tearoom marks the state unknown
and does not type into that pane. Inspect the pane and the panel name from
`tearoom collab deliveries`, then reconnect it with `hc attach <panel>` in that
pane, using the same hc session as the dispatcher. If a pane creation outcome
is uncertain, Tearoom refuses to create another automatically. Inspect the
workspace and the local `.tearoom/herdr.json` binding; no recovery command for
that ambiguous case is currently provided.
When the dispatcher or Herdr is unavailable, the last displayed status can be
stale. A visible status or successful attachment does not prove task completion.

## Message routing

Address a configured Room role in message text or with `--to`, then run the
dispatcher. Exact mention, message, and reply-reference behavior is in the
[CLI contract](cli.md#append-and-read):

```bash
tearoom send --room project --as builder \
  --to @reviewer --reply-to project#1 \
  --text 'Implementation is ready. Please review the diff and the recorded test output.'
```

Addressed records remain in the shared Room stream. An unaddressed record
does not start or notify an offline panel. A reply reference is a coordination
pointer; it does not verify identity or prove that the referenced work was
done.

One participant may work in the shared project Room and address the reviewer
there. When work needs separate Rooms, a shared ancestor Room can carry their
coordination; one Room's roster does not include roles from a sibling Room.

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

The Claude Code CLI probed here did not emit hc's OSC lifecycle status, so a
direct launch stays pending for the dispatcher. Run it as a human-supervised
panel, or use an Agent that reports status, before relying on unattended
delivery.

## Pi lifecycle status

hc needs terminal status to distinguish idle, working, done, and blocked
panels. Pi does not currently emit hc's OSC status by default in this setup.
The optional [`scripts/pi-hc-status.ts`](../../scripts/pi-hc-status.ts)
extension reports Pi lifecycle events to hc. A Pi `done` event means its turn
ended; it does not mean Tearoom's acceptance criteria passed. Probe results
and remaining validation are recorded in [evaluations](../evaluations.md).

## Startup and observed readiness

Before `tearoom collab run`, install hc, read `hc --skill`, initialize hc, and
authenticate the Agent CLIs. Resolve one-time trust, hooks, update, and login
prompts in the actual working directory. A terminal that is blocked on a first
run dialog is not ready. The Agent setting up the Room checks readiness and
handles authorized trust prompts; unresolved login or approval needs a person.

Codex direct launches require `--no-daemon` so the dispatcher controls the
panel it observes. Use `--observe` as defined in the [CLI
contract](cli.md#room-lifecycle) to ask hc about bound panels. The result is a
snapshot, not proof of task completion.

The dispatcher persists delivery intents and serializes work per Room-role.
`tearoom collab deliveries` displays the ledger. Submitted means hc accepted
input; completed means a matching Room reply was observed. Inspect the record
and independent panel evidence to judge the work.
Agents without a reliable lifecycle signal stay pending rather than being
assumed idle or done. The current `done` check followed by submission is not
atomic, so another sender can race in between. Exactly-once execution is not
promised.

Resolve only an `unknown` delivery after inspecting both hc and the Room. The
available actions and effects are in the [CLI contract](cli.md#delivery-boundary):

```bash
tearoom collab resolve --room project --seq 2 --role reviewer --outcome delivered
tearoom collab resolve --room project --seq 3 --role builder --outcome skip
tearoom collab resolve --room project --seq 4 --role reviewer --outcome retry --confirm
```

Retrying can repeat work or external side effects. Never retry an ambiguous
delivery without first reconciling the panel and Room.

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
multi-Agent journey, first-run recovery, and different-tool independence
remain unproven. See
[implementation status](../architecture/status.md) and the hand-run records in
[evaluations](../evaluations.md) before treating the integration as proven.
