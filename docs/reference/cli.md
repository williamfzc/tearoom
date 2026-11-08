---
type: reference
title: CLI reference
description: Authoritative command contract for the tearoom binary over the room tree.
resource: rust/src/cli.rs
tags: [reference, cli, contract]
timestamp: 2026-11-07
---

# CLI reference

The CLI is Tearoom's top-level product interface. This page is its only command
contract. Implementation gaps belong in [status.md](../architecture/status.md),
and complete user journeys belong in [evaluations.md](../evaluations.md).

## Command contract

```text
tearoom room init --name <name> [--parent <room>]
tearoom room init --name <name> [--parent <room>] [--role <role,...>]
tearoom room roles --room <name> [--add <role,...>] [--remove <role,...>]
tearoom role define --name <name> --agent-type <type> (--template <name> | --command <program>)
tearoom role list
tearoom role show --name <name>
tearoom collab run
tearoom collab status
tearoom collab resolve --room <room> --seq <seq> --role <role> --outcome <delivered|skip|retry> [--confirm]
tearoom send [--room <room>] --text <text> [--to @role,...] [--reply-to <room>#<seq>] [--as <signature>] [--idempotency-key <key>]
tearoom send [--room <room>] --json <json> [--as <signature>] [--idempotency-key <key>]
tearoom read [--room <room>] [--after e<seq>] [--tail <n>] [--subtree]
tearoom tree
tearoom watch [--room <room>] [--json]
tearoom server start [--address 127.0.0.1:8787]
tearoom skill
tearoom --version
```

### Room lifecycle

- `room init --name <name>` creates a Room. Without `--parent` the Room is a
  root; with `--parent <room>` it records that existing Room as its parent.
  Room names are unique within the workspace.
- The first `room init` in a directory creates the workspace (`.tearoom`);
  later inits add Rooms to the same tree. There is no join, no admission, and
  no credential step.
- `--role` binds existing reusable role definitions to the new Room.
- `room roles` lists configured roles or adds/removes bindings. `--status`
  explicitly queries hc for each panel's observed availability; failures are
  shown as unknown. The roster does not grant or restrict read/write access.
- `role define` creates a reusable launch definition with an agent type and
  either an installed hc template or an executable with repeatable `--arg`
  values. `--cwd` defaults to the workspace root; a relative directory is
  resolved from that root when the panel starts. `--instructions` supplies role
  text. `role list` and `role show` print JSONL definitions.
- Success prints one JSON object:

```json
{"name":"implementation","parent":"main-line","created":"2026-11-07T10:00:00Z"}
```

Identifiers and timestamps above are examples, not fixed values.

### Append and read

- `send` appends one Record to the target Room and prints it as one JSON
  object. Plain text becomes a JSON string; `--json` accepts one valid JSON
  value. `--as` sets the signature — an unverified label; omitting it stores
  an empty signature.
- An explicit `@reviewer` in text, `--to @reviewer`, or
  `--to @builder,@reviewer` addresses configured Room roles. Escaped @ signs,
  email addresses, and Markdown code spans and fences do not address roles.
  Unknown roles and `@all` are rejected. The appended content is structured
  JSON with `text` and `mentions`;
  recipients remain outside Record and Notification fields. Addressed `--json`
  content is not supported. Without `--to`, existing text and raw JSON shapes
  are unchanged.
- `--reply-to <room>#<seq>` writes a structured `reply_to` reference alongside
  the message. Combine it with `--to` to acknowledge the delivery and direct
  the next step in one append. The signature remains unverified; this reference
  is a coordination convention, not proof of who completed the work.
- `--idempotency-key` is optional and scoped per (room, signature, key).
  Repeating the same key with the same content returns the original Record;
  reusing it with different content fails.
- `read` prints JSONL, one Record per line, no envelope, no saved cursor. An
  empty result prints nothing and exits 0.
  - `--after e42` starts after that exclusive boundary.
  - `--tail 5` prints only the last 5 records of each selected Room.
    Explicit `--tail 0` prints no records; omitting the flag prints all records.
  - `--subtree` selects the Room and all its descendants (depth-first by
    name); each Record line carries its `room` field.
- A Record has this output shape:

```json
{"room":"implementation","seq":43,"author":"codex","at":"2026-11-07T10:01:00Z","content":{"plan":"review"}}
```

### Room selection

`send`, `read`, and `watch` target Rooms by name:

- With `--room <name>` the named Room is used; an unknown name is an error.
- Without `--room`, a workspace containing exactly one Room uses it; a
  workspace with several Rooms refuses and names the rule. `watch` without
  `--room` watches every Room in the workspace instead of refusing.

### Skeleton

`tree` prints the workspace skeleton: every root with its descendants,
depth-first by name. It derives the shape at read time and caches nothing.

```text
main-line
├── implementation
│   └── login-page
└── review
```

### Notifications

`watch` follows appends and prints one line per new Record. `--json` prints
the notification envelope, one JSON object per line:

```json
{"room":"implementation","seq":7,"author":"codex","at":"2026-11-07T10:02:00Z"}
```

The envelope carries no content and requests nothing; an external waker
decides what to start. `watch` runs until interrupted.

### Server

`server start` serves the nearest workspace over HTTP on the loopback
interface only — it has no credentials, and everything on loopback belongs to
one OS user, the same trust domain as the `.tearoom` directory. The HTTP
contract is [http-api.md](http-api.md). When `TEAROOM_SERVER` names a server
URL, `send`, `read`, and `watch` use it instead of the nearest local
workspace; room selection still follows the rules above.

```bash
# Local profile
tearoom server start
export TEAROOM_SERVER=http://127.0.0.1:8787
tearoom send --text "ready" --as codex
tearoom read --tail 5
```

### Agent guide

`tearoom skill` prints the orientation guide carried by the binary: the room
journey, the environment conventions, and the rules that hold at every
command. It takes no flags and reads no Room.

## Signature selection

`send` takes `--as` for the signature, falling back to `TEAROOM_ACTOR`. A
missing signature is not an error: the record simply carries an empty author.
Nothing validates the value, and `read` needs no signature at all — reading
requires no identity.

## Failure contract

All diagnostics go to standard error; successful machine-readable output goes
to standard output. [exit-codes.md](exit-codes.md) is the only exit-code
source. In particular, an empty `read` succeeds, and a missing workspace or
unreachable configured server is an availability failure (exit 4), while an
unknown room name or an idempotency conflict is a request failure (exit 1).

## Delivery boundary

`watch` emits recipient-free notifications. The collaboration dispatcher
reads the addressed Record, resolves the Room-role binding, and uses hc to
start or notify that role's panel. Role and Room roster commands configure
routing; `collab run` starts the dispatcher and `collab status` prints its
observed delivery intents. They do not wake Agents by themselves. See
[collaboration delivery](../architecture/collaboration.md) for the delivery
contract and [status.md](../architecture/status.md) for what ships.

Automated template launches reject `ready: quiet`: a quiet startup can be an
update or permission prompt. Codex roles must use `--no-daemon`. Agents that
do not report a reliable hc status remain pending after their first turn;
the dispatcher does not infer readiness from terminal text.

`collab resolve` changes only an `unknown` delivery and takes the same
workspace lock as the dispatcher. `delivered` stops retries while awaiting an
Agent's Room receipt; `skip` closes the delivery and releases later messages
for that role. `retry --confirm` requeues the message and may repeat work or
external side effects. Inspect `collab status` before resolving; unknown
outcomes are never retried automatically.
