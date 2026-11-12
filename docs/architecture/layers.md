---
type: architecture
title: Layers and dependency direction
description: The package boundaries of the room-tree kernel and its herdr-core collaboration integration.
resource: src/
tags: [architecture, boundaries, layering, clean-break]
timestamp: 2026-11-07
---

# Layers and dependency direction

The Rust implementation has these boundaries:

| Layer | Package or owner | Owns |
|---|---|---|
| Domain | `src/core.rs` | Room and Record data, content compaction, and idempotency fingerprints |
| Persistence | `src/store.rs` and `store/` | The `.tearoom` JSONL layout, Store operations, recovery, and the cross-process lock |
| Application | `src/collab.rs` and `collab/` | Roles, delivery state, and Room-to-panel coordination |
| Outside | `hc.rs`, `herdr/` | Embedded herdr-core runtime client and Herdr command adapters |
| Outside | `src/cli.rs`, `cli/`, `cli/web*`, `main.rs` | Command parsing, output, the optional loopback browser face, and process entry |

Dependencies point inward. `core` imports no project package. Persistence
uses the domain types; the CLI translates commands. The kernel never imports a
scheduler, launcher, or Agent runtime. Credentials,
admission, and the hosted multi-room service were removed with the v1 model on
2026-11-07 (see [../log.md](../log.md)).

## Foundation boundary

`src/core.rs` defines the shared Room and Record data:

- Room identity: a name, an optional parent, and a creation time;
- Record: `seq`, `author`, `at`, `content` — the author is an unverified
  label, and message addressing remains in content/convention rather than a
  recipient field;
- content compaction and fingerprints used for idempotent appends.

The Store never checks identity: reading and writing require no admission,
and no Store operation creates membership as a side effect. What one Room
sees is bounded by the tree, not by permissions.

## Persistence boundary

`src/store.rs` and `store/` own the `.tearoom` layout: one workspace directory holds
the whole room tree as JSONL files behind a format marker, and the JSONL
Store operates under a cross-process flock. It may not:

- filter records by author or message addressing;
- validate or transform a signature beyond storing it verbatim;
- cache a parent's child list or any derived tree shape;
- host delivery claims, wake state, or process state.

One append holds the exclusive lock across room verification, idempotency
resolution, sequence assignment, and the append, so concurrent writers
receive a dense per-room sequence. The format boundary is specified in
[persistence.md](persistence.md).

## Outside boundary

`src/cli.rs` and `cli/` expose the command surface documented in the
[CLI reference](../reference/cli.md). The CLI owns formatting and
invocation-local options, not shared invariants. The
v1 commands `room join`, `room credential`, and the hosted `server` profile
are deleted; no alias keeps them alive. The 2026-11-09 concept pass removed
the watch and HTTP faces with the notification concept (see
[../log.md](../log.md)); the loopback `server start` and `TEAROOM_SERVER`
client mode died with it. The 2026-11-10 browser face is a new optional view,
not a restoration of that remote-capable server: it binds only loopback,
carries a page token, and owns no product or storage invariant; its loopback,
token, and origin rules are transport policy. It reads through the Store and
appends through the CLI's shared message composition.
The per-user local collaboration service is a new lifecycle boundary, not the
removed hosted profile or HTTP face; its contract and limits are in
[collaboration.md](collaboration.md#local-collaboration-service).

## Collaboration runtime boundary

`herdr-core` (`hc`) is compiled into Tearoom and runs as a separate background
process for Agent terminals, prompt delivery, parent-child execution, and
lifecycle events. It has no native Tearoom board or role-routing policy. An
integration outside the kernel defines reusable roles (command, working directory, and role
instructions), configures collaborators for a Room, and binds each
Room's role instance to its own hc panel. All those roles share the Room's one
record stream; reusing a role definition in another Room creates a separate
panel instance. An explicit recipient routes through hc to that Room-scoped panel. Different
Rooms can coordinate through a shared ancestor Room; build and review use
separate Agent instances and roles in the same Room.

The `tearoom hc` command exposes the embedded runtime CLI. The integration also
exposes configured collaborators and reconciles Room records with hc
snapshots/events after interruption. `tearoom hc update` is disabled so the
embedded runtime cannot replace the Tearoom executable; updates arrive through
Tearoom releases. The
collaborator roster is discovery and routing data, not admission; it does not
affect who can read or write a local Room. Role instructions are an initial message, not a native system prompt. Panel-failure handling and reliable
recovery are not existing hc guarantees. The delivery contract is in
[collaboration.md](collaboration.md); configuration is in the
[Agent integration guide](../reference/agent-integration.md).
The integration may depend on `core` and the Store; neither may depend on it.

## Clean-break dependency checks

1. `core` has no import path to persistence or CLI.
2. The current Store has one format and no parallel old/new reader or writer.
3. No Store operation validates identity or admits anyone.
4. The hc client (`hc.rs`) is one JSON-RPC vocabulary with no authorization
   paths and no Room semantics.
5. CLI, scripts, tests, and docs use only the command and record vocabulary
   above.
6. The hc integration stays outside the kernel and does not add
   recipient, membership, or process-state fields to core Records.

Automated checks cover only part of these boundaries; review dependencies and
repository searches when a change could cross them.
Rollback is a Git rollback to a matched revision and data snapshot; adding a
deprecated dependency or compatibility layer is not an allowed recovery.

## Related

- [persistence.md](persistence.md)
- [status.md](status.md)
- [../domain/concepts.md](../domain/concepts.md)
