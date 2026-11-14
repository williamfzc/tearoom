---
type: reference
title: CLI reference
description: Authoritative command contract for the tearoom binary over the room tree.
resource: src/cli.rs
tags: [reference, cli, contract]
timestamp: 2026-11-10
---

# CLI reference

The CLI is Tearoom's top-level automation interface. This page is its only command
contract. Implementation gaps belong in [status.md](../architecture/status.md),
and complete user journeys belong in [evaluations.md](../evaluations.md).

## Command contract

```text
tearoom room init --name <name> [--parent <room>] [--role <role,...>] [--description <text>]
tearoom room update --room <room> [--name <new-name>] [--description <text>]
tearoom room show --room <room>
tearoom room delete --room <room> [--confirm]
tearoom room open --room <room>
tearoom room move --room <room> [--parent <room>]
tearoom room roles --room <room> [--add <role,...>] [--remove <role,...>] [--observe]
tearoom room herdr ensure --room <room>
tearoom role init --name <name> --agent-type <type> --command <program> [--arg <arg>] [--cwd <dir>] [--description <text>] [--instructions <text>]
tearoom role update --name <name> [--agent-type <type>] [--command <program> [--arg <arg>]] [--cwd <dir>] [--description <text>] [--instructions <text>]
tearoom role delete --name <name>
tearoom role list
tearoom role show --name <name>
tearoom collab run
tearoom collab deliveries
tearoom collab resolve --room <room> --seq <seq> --role <role> --outcome <delivered|skip|retry> [--confirm]
tearoom collab restart --room <room> --role <role> [--confirm]
tearoom service status
tearoom service stop
tearoom service forget <workspace-root> --confirm
tearoom send [--room <room>] <text> [--as <signature>] [--to @role,...] [--reply-to <room>#<seq>] [--idempotency-key <key>]
tearoom send [--room <room>] --text <text> [--as <signature>] [--to @role,...] [--reply-to <room>#<seq>] [--idempotency-key <key>]
tearoom send [--room <room>] --json <json> [--as <signature>] [--idempotency-key <key>]
tearoom read [--room <room>] [--after e<seq>] [--tail <n>] [--subtree]
tearoom tree
tearoom web [--port <port>]
tearoom skill [--pi-extension]
tearoom --version
```

### Room lifecycle

- `room init --name <name>` creates a Room. Without `--parent` the Room is a
  root; with `--parent <room>` it records that existing Room as its parent.
  Room names are unique within the workspace and never collide with any
  Room's id. Every Room has an immutable id — its birth directory name; see
  [Room selection](#room-selection).
- The first `room init` in a directory creates the workspace (`.tearoom`);
  later inits add Rooms to the same tree. There is no join, no admission, and
  no credential step.
- `room update --room <room> [--name <new-name>] [--description <text>]`
  edits the Room's info card: its display name and description. The id,
  records, parent pointers, rosters, delivery state, and Herdr bindings stay
  untouched, and every stored `room#seq` reference keeps resolving. Names
  must stay unique and disjoint from every Room's id — a Room's birth name
  is its id and stays reserved for as long as the Room lives.
- `room show --room <room>` prints one Room's info card: its id, current
  name, parent's current name, description, creation time, and bound roles.
- `room delete --room <name>` removes a leaf Room: its directory and record
  stream, its roster binding, its dispatch intents and cursor, and its Herdr
  binding go with it, and its Herdr session is stopped best-effort. Role
  definitions survive. Running Agent panels are not stopped; the command
  names each panel and its `tearoom hc kill` command. `reply_to` references from
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
- Success prints one JSON object; `description` appears only when set:

```json
{"name":"implementation","id":"implementation","parent":"main-line","created":"2026-11-07T10:00:00Z"}
```

Identifiers and timestamps above are examples, not fixed values.

### Room organization

The current `room move` command changes a Room's parent; omit `--parent` to
make it a root. Its id, name, history, role bindings and descendants stay
intact. A Room cannot become its own ancestor. Subtree reads use the new
location. Its place in a simpler command surface is under review; see
[open questions](../domain/open-questions.md#room-organization). Label edits
belong to `room update` above.

### Append and read

- `send` appends one Record to the target Room and prints it as one JSON
  object. Plain text becomes a JSON string; `--json` accepts one valid JSON
  value. `--as` sets the signature — an unverified label; omitting it stores
  an empty signature.
- Only explicit `--to @reviewer` targets address roles; mentions in message
  text are ordinary shared Room content. Addressed text is stored as structured
  JSON with `text` and `mentions`. Unknown selected roles and selected `@all`
  are rejected. An empty or whitespace-only `--to` is rejected.
  Recipients live in the record's structured content, never in the kernel
  Record. Addressed `--json` content is not supported; raw JSON that parses
  as a message with nonempty `mentions` is rejected. Other raw JSON keeps its
  original shape.
- `--reply-to <room>#<seq>` writes a structured `reply_to` reference alongside
  the message; the reference stores the Room's id, so a later label edit never
  dangles it. Combine it with `--to` to acknowledge the delivery and direct
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

Every `--room <room>` value addresses a Room by its immutable id or by its
current display name:

- The id is the directory name the Room was born with and never changes.
  Stored references (`room#seq`), parent pointers, rosters, delivery state,
  and Herdr bindings anchor to it; the `room` field on each printed Record is
  the id.
- The display name is editable through `room update`. A reference matching an
  id wins over a name match.
- With `--room <room>` the referenced Room is used; an unknown reference is
  an error.
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

### Local browser interface

`tearoom web [--port <port>]` starts a browser interface for the current
workspace and prints one JSON object with its `url`, for example
`{"url":"http://127.0.0.1:8787/"}`. The default port is `0`, so the operating
system chooses an unused port. The command runs until interrupted.

The page has three jobs: navigate the Room tree, read a Room's ordered record
stream, and append text messages with optional `@role` addresses. It reads
through the same Store seam and appends through the same message composition
as `send`; it does not edit records, move Rooms, or persist a read cursor. The
JSON endpoints under `/api/` are an implementation detail of this local face,
not a second public contract.

The listener binds only `127.0.0.1`. Every request must carry a loopback
`Host`; the page carries a per-process random token in a meta tag, and API
requests must present that token in `X-Tearoom-Token`. Writes also refuse a
cross-origin `Origin` or `Sec-Fetch-Site` value. The page is served with a
restrictive Content Security Policy and cannot be framed. There is no CORS
header and no remote mode; this is not a hosted control plane. The token is a
browser CSRF boundary, not admission: it does not authenticate a process or a
signature. Messages remain shared Room history, and the signature remains an
unverified label.

### Agent guide

`tearoom skill` prints the orientation guide carried by the binary: the room
journey, the environment conventions, and the rules that hold at every
command. `tearoom skill --pi-extension` prints the bundled Pi lifecycle
extension instead. Neither form reads a Room or writes files.

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

The collaboration dispatcher reads the addressed Record, resolves the
Room-role binding, and uses hc to start or notify that role's panel. An
addressed CLI send or web append starts the background service after saving the
Room record. If CLI activation fails, the error says the record was saved. A
message without recipients does not activate collaboration. `read` and
`room init` never start the service. `collab run` is a short-lived manual
command that registers the current workspace and ensures the service is
running. `collab deliveries` prints durable delivery intents; it does not wake
Agents by itself.

`service status` prints whether the service is running, whether it is stopping,
and its registered workspaces with any paused-workspace errors. `service stop`
stops the service and attempts to stop the Agent terminals managed for its
registered workspaces. It records stopped panels, reports cleanup failures,+preserves Room history, and leaves the shared hc service and unrelated
registered workspaces. It records stopped panels, reports cleanup failures,
preserves Room history, and leaves the shared hc service and unrelated
terminals running. If cleanup fails, dispatch stays stopped and `service stop`
must be retried. After a successful stop, a later addressed send or `collab
run` can start collaboration again. The first activation selects the hc socket
for the user service; activation from a different hc session is refused until
the service is stopped. See
[collaboration delivery](../architecture/collaboration.md#local-collaboration-service)
for service ownership and recovery limits, and [status.md](../architecture/status.md)
for verification evidence.

The first service terminal uses the hc session and environment selected at its
startup. Later CLI commands do not refresh its environment; changes to `PATH`
or Agent credentials in a later shell are not guaranteed to reach the running
service. The current interface has no environment-refresh operation.

`service forget <workspace-root> --confirm` removes a paused registration when
the recorded workspace identity no longer matches or its marker is missing.
Use the absolute path shown by `service status`. It does not stop Agent
terminals. Inspect and clean any remaining terminals before forgetting a moved,
replaced, or missing workspace. The service uses an identity-derived owner
label for cleanup; older terminals labeled only by their path are reported but
never killed automatically.

The service pumps registered workspaces sequentially, then waits 500 ms before
the next pass. A slow dispatch in one workspace delays work and control requests
for the others. Ordinary control requests wait up to 10 seconds; `service stop`
waits up to 300 seconds. Delivery-state locks shared with manual operations
serialize scans; a busy scan is retried on a later pass. When the service starts
after an interruption, previously submitted deliveries are marked unknown.
Inspect the Room and delivery record before resolving them; the service does
not automatically replay them.

Normal stop attempts to end terminals identified by the recorded identity-bound
workspace label. Agent terminals are children of the Tearoom service terminal.
See [evaluations](../evaluations.md) for what the scripted tests establish.
Linux, hc-server failure, real Agent processes, detached descendants, and the
race between identity checks and workspace replacement remain unverified.

`collab restart --room <room> --role <role>` replaces one Room-role panel: it
stops the live panel, starts the role's configured command again, and prints
the observed status. It refuses while the panel is working, and it refuses
while that Room-role has a submitted or uncertain delivery unless `--confirm`
is given; inspect those records first. It is also how a changed role command
reaches an already-running role.

A replaced panel is a new Agent conversation, not a continuation: Tearoom
stores no session identifier and does not guess one from the Agent's
"most recent session" flag, because several roles can share a working
directory. The next wake includes Room history. A newly started terminal UI may also show its own sign-in,
setup guide, or session list while hc still reports `idle`; hc then does not
start a turn and the delivery becomes uncertain. Open the panel and leave it
at its prompt before relying on the next delivery. See the
[continuity and replacement contract](../architecture/collaboration.md#conversation-continuity-and-replacement).

Codex roles must use `--no-daemon`. Agents that do not report a reliable hc
status remain pending after their first turn; the dispatcher does not infer
readiness from terminal text.

`collab resolve` changes only an `unknown` delivery and takes the same
workspace lock as the dispatcher. `delivered` stops retries while awaiting an
Agent's Room receipt; `skip` closes the delivery and releases later messages
for that role. `retry --confirm` requeues the message and may repeat work or
external side effects. Inspect `collab deliveries` before resolving; unknown
outcomes are never retried automatically.
