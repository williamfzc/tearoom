# Tearoom

A long-lived collaborative workspace where people and Agents write, continue,
and deliver work inside Rooms. Rooms form a single-parent tree; whoever reads
a Room can pick up the work — across Agents, tools, and process restarts.

[`docs/product.md`](docs/product.md) is the authoritative product definition.
Start with the [documentation route](docs/index.md). Implementation state
lives in [status.md](docs/architecture/status.md); proof lives in
[evaluations.md](docs/evaluations.md).

## Minimum model

- **Room** — one span of context: a parent (or none) and one ordered record
  stream.
- **Record** — one stream entry: sequence, time, signature, content.
  Append-only, never edited.
- **Notification** — the envelope emitted when a record lands: which room,
  which sequence, which signature. It stores nothing.

Signatures are unverified labels. There is no membership, no admission, and
no credentials: the `.tearoom` directory is the trust domain. Storage is
plain files — one JSONL stream per Room — so the tree can be committed to
git and the memory travels with the checkout. This repository does exactly
that: its own development lives in its `.tearoom/` tree.

## Quick start

```bash
make build                     # builds ./bin/tearoom
tearoom_bin="$PWD/bin/tearoom"
work_dir="$(mktemp -d)"
cd "$work_dir"
"$tearoom_bin" room init --name main-line
"$tearoom_bin" room init --name implementation --parent main-line
"$tearoom_bin" room init --name review --parent main-line
TEAROOM_ACTOR=planner "$tearoom_bin" send --text "goal recorded" --room main-line
TEAROOM_ACTOR=codex "$tearoom_bin" send --text "started" --room implementation
TEAROOM_ACTOR=codex "$tearoom_bin" read --room implementation --tail 5
"$tearoom_bin" tree
```

- `room init --name <name> [--parent <room>]` creates a Room; the first one
  creates the workspace. Rooms are addressed by name.
- `send` appends one record and prints it as JSON. `--as` sets the
  signature; plain text is encoded as a JSON string, `--json` passes raw
  JSON.
- `read` prints JSONL, one record per line. `--after e<seq>` sets your own
  boundary, `--tail 5` limits each selected Room, `--subtree` reads the Room
  and its descendants. Reading is side-effect free.
- `tree` prints the skeleton: every root with its descendants.
- `watch --json` prints one notification envelope per new record; an
  external waker decides whether anyone is woken.
- `server start` serves the workspace over HTTP on `127.0.0.1:8787`
  (loopback only); set `TEAROOM_SERVER` to point other processes at it.

## Working the tree

The conventions are writing, not fields: an executor writes what it did,
what is blocked, and what the next worker should do first; a reviewer works
in its own Room and cites evidence; quoting a record is `room-name#seq` in
plain text; closing work is a sentence. Waking the next worker is external —
a script reads `tearoom watch --json` and drives a scheduler such as herdr.

## Build and verify

```bash
make check   # fmt vet test test-race target-room target-replay check-evals line-budget docs
```

See [operations/building.md](docs/operations/building.md) for the toolchain
traps (macOS test linking, the module proxy) and
[reference/agent-integration.md](docs/reference/agent-integration.md) for the
Agent-facing integration path.
