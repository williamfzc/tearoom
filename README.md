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
- **Role** — a reusable launch definition and a Room roster entry. It routes
  addressed messages; it is not access control.

Signatures are unverified labels. There is no admission or credential step: the
`.tearoom` directory is the trust domain, and a Room's collaborator roster is
routing configuration, not access control. The
[storage reference](docs/reference/storage-layout.md) describes portability.
This repository keeps its own `.tearoom/` workspace local (gitignored);
working on the repository does not require using it.

## Quick start

```bash
make build                     # builds ./bin/tearoom
tearoom_bin="$PWD/bin/tearoom"
work_dir="$(mktemp -d)"
cd "$work_dir"
"$tearoom_bin" room init --name main-line
"$tearoom_bin" room init --name implementation --parent main-line
TEAROOM_ACTOR=planner "$tearoom_bin" send --text "goal recorded" --room main-line
TEAROOM_ACTOR=codex "$tearoom_bin" send --text "started; review will happen here" --room implementation
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
- `role` and `collab` configure Room collaborators and run the local hc
  dispatcher that delivers addressed records to Agent panels.

This example shows Room records only; it does not start an Agent or perform a
review. For collaborator setup and Herdr visibility, see the
[integration guide](docs/reference/agent-integration.md).

## Working the tree

When handing work over, record progress, blockers, and what comes next. When
independent review is needed, a different reviewer cites evidence. Record references use
`room-name#seq` in plain text. The same-Room role and hc integration is
specified in [product.md](docs/product.md) and
[architecture/collaboration.md](docs/architecture/collaboration.md).

## Build and verify

```bash
make check   # formatting, Clippy, tests, E1/E2 journeys, docs, line budget, release checks
```

See [operations/building.md](docs/operations/building.md) for the toolchain
requirements and the ignored cross-language and hc tests, and
[reference/agent-integration.md](docs/reference/agent-integration.md) for the
Agent-facing integration path.
