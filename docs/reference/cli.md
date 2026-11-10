---
type: reference
title: CLI reference
description: Authoritative command contract for the tearoom binary over the room tree.
resource: rust/src/cli.rs
tags: [reference, cli, contract]
timestamp: 2026-11-10
---

# CLI reference

The CLI is Tearoom's top-level product interface. This page is its only command
contract. Implementation gaps belong in [status.md](../architecture/status.md),
and complete user journeys belong in [evaluations.md](../evaluations.md).

## Command contract

```text
tearoom room init --name <name> [--parent <room>] [--role <role,...>]
tearoom room delete --room <name> [--confirm]
tearoom room open --room <name>
tearoom room move --room <name> [--parent <room>]
tearoom room roles --room <name> [--add <role,...>] [--remove <role,...>] [--observe]
tearoom room herdr ensure --room <name>
tearoom role init --name <name> --agent-type <type> --command <program> [--arg <arg>] [--cwd <dir>] [--description <text>] [--instructions <text>]
tearoom role update --name <name> [--agent-type <type>] [--command <program> [--arg <arg>]] [--cwd <dir>] [--description <text>] [--instructions <text>]
tearoom role delete --name <name>
tearoom role list
tearoom role show --name <name>
tearoom collab run
tearoom collab deliveries
tearoom collab resolve --room <room> --seq <seq> --role <role> --outcome <delivered|skip|retry> [--confirm]
tearoom send [--room <room>] <text> [--as <signature>] [--to @role,...] [--reply-to <room>#<seq>] [--idempotency-key <key>]
tearoom send [--room <room>] --text <text> [--as <signature>] [--to @role,...] [--reply-to <room>#<seq>] [--idempotency-key <key>]
tearoom send [--room <room>] --json <json> [--as <signature>] [--idempotency-key <key>]
tearoom read [--room <room>] [--after e<seq>] [--tail <n>] [--subtree]
tearoom tree
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
- `room delete --room <name>` removes a leaf Room: its directory and record
  stream, its roster binding, its dispatch intents and cursor, and its Herdr
  binding go with it, and its Herdr session is stopped best-effort. Role
  definitions survive. Running Agent panels are not stopped; the command
  names each panel and its `hc kill` command. `reply_to` references from
  surviving Rooms' records become dangling pointers.
  The command refuses while the Room has child Rooms or unresolved
  deliveries, and while a dispatcher holds the workspace lock. Without
  `--confirm` it refuses and states what would be removed; the removal is
  irreversible.
- `--role` binds existing reusable role definitions to the new Room.
- Room creation attempts to prepare an independent local Herdr session.
  `room open --room <name>` opens it in the current terminal. Failure leaves
  the Room usable and is reported on stderr; `room herdr ensure` retries
  the association. See the
  [integration guide](agent-integration.md#herdr-session-per-room).
- `room roles` lists configured roles or adds/removes bindings. `--observe`
  explicitly queries hc for each panel's observed availability; failures are
  shown as unknown. The roster does not grant or restrict read/write access.
  Removing a binding does not stop its running process or cancel an existing
  delivery intent.
- A Role is a reusable launch definition. A Room's roster binds selected Role
  definitions for message routing; it is not an access-control list. A given
  Room and Role pair has its own hc panel.
- `role init` creates a reusable launch definition with an agent type and
  an executable with repeatable `--arg` values; it refuses to overwrite an
  existing name. Use `role update` with the same definition fields to change
  selected values; fields not supplied remain unchanged. Changing the agent
  type, command, or working directory affects only newly started role
  processes. Running processes keep their
  current process and settings, and the CLI reports this on stderr. `--cwd`
  defaults to the workspace root; a relative directory is resolved from that
  root when the panel starts. `--instructions` supplies role text. `role list`
  and `role show` print one JSON object each (one per line for `role list`).
- `role delete` removes a definition only after every Room has removed its
  binding and all deliveries for that role are resolved, including messages
  the dispatcher has not yet scanned. It does not stop an already-running
  Agent or change Room history. A running dispatcher may briefly hold the
  delivery lock; retry if the lock is busy.
- Success prints one JSON object:

```json
{"name":"implementation","parent":"main-line","created":"2026-11-07T10:00:00Z"}
```

Identifiers and timestamps above are examples, not fixed values.

### Room organization

The current `room move` command changes a Room's parent; omit `--parent` to
make it a root. Its name, history, role bindings and descendants stay intact.
A Room cannot become its own ancestor. Subtree reads use the new location.
Its place in a simpler command surface is under review; see
[open questions](../domain/open-questions.md#room-organization).

### Append and read

- `send` appends one Record to the target Room and prints it as one JSON
  object. Plain text becomes a JSON string; `--json` accepts one valid JSON
  value. `--as` sets the signature — an unverified label; omitting it stores
  an empty signature.
- An explicit `--to @reviewer` addresses only the listed roles, even if the
  body mentions other roles. Without `--to`, body mentions address configured
  Room roles. Addressed text is stored as structured JSON with `text` and
  `mentions`. Escaped @ signs, email addresses, and Markdown code spans and
  fences do not address roles. Unknown selected roles and selected `@all` are
  rejected.
  An empty or whitespace-only `--to` is rejected rather than falling back to
  body mentions.
  Recipients live in the record's structured content, never in the kernel
  Record. Addressed `--json` content is not supported; raw JSON that parses
  as a message with nonempty `mentions` is rejected. Other raw JSON keeps its
  original shape.
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

`send` and `read` target Rooms by name:

- With `--room <name>` the named Room is used; an unknown name is an error.
- Without `--room`, a workspace containing exactly one Room uses it; a
  workspace with several Rooms refuses and names the rule.

### Skeleton

`tree` prints the workspace skeleton: every root with its descendants,
depth-first by name. It derives the shape at read time and caches nothing.

```text
main-line
├── implementation
│   └── login-page
└── review
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
source. In particular, an empty `read` succeeds, a missing workspace is an
availability failure (exit 4), and an unknown room name or an idempotency
conflict is a request failure (exit 1).

## Delivery boundary

The collaboration dispatcher
reads the addressed Record, resolves the Room-role binding, and uses hc to
start or notify that role's panel. Role and Room roster commands configure
routing; `collab run` starts the dispatcher and `collab deliveries` prints its
durable delivery intents. They do not wake Agents by themselves. See
[collaboration delivery](../architecture/collaboration.md) for the delivery
contract and [status.md](../architecture/status.md) for what ships.

Codex roles must use `--no-daemon`. Agents that do not report a reliable hc
status remain pending after their first turn; the dispatcher does not infer
readiness from terminal text.

`collab resolve` changes only an `unknown` delivery and takes the same
workspace lock as the dispatcher. `delivered` stops retries while awaiting an
Agent's Room receipt; `skip` closes the delivery and releases later messages
for that role. `retry --confirm` requeues the message and may repeat work or
external side effects. Inspect `collab deliveries` before resolving; unknown
outcomes are never retried automatically.
