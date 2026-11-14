---
type: decision
title: Open questions
description: Runtime design questions left open after adopting shared Room conversation and hc-based collaboration.
tags: [domain, decision, open]
timestamp: 2026-11-10
---

# Open questions

Each question needs usage evidence rather than a speculative abstraction.

## Retention

The record streams grow forever. Compaction, snapshots, and export require a
rule that preserves replay and audit semantics without introducing editable
history. Measure real workspace growth before designing anything.

## Serving beyond one machine

The shipped profile is one workspace on one machine. If a multi-host or
remote need returns, it must reuse the same `core.Store` contract and the
same room tree — the v1 hosted control plane, credentials, and owner tokens
were removed on 2026-11-07 and must not return in that shape. Reopen only
with a concrete deployment whose trust model the signature-label design can
honestly carry.

## Room organization

The CLI can move a Room without changing its name or history, and delete a
leaf Room under `--confirm`, refusing children and unresolved deliveries (see
the [CLI reference](../reference/cli.md#room-lifecycle)). Moving is not part of
the main journey, and its place in a simpler command surface needs a real
reorganization case. Renaming settled on 2026-11-14 as label editing: a
Room's id (its birth directory name) is immutable while its name and
description are edited through `room update`. Whether reads such as `tree`,
`collab deliveries`, and record output should resolve ids to current names
for display or print ids verbatim stays open until real renamed-Room use
says which reads better.

## Presentation of multiple roots

A workspace may hold several independent roots. The CLI prints a forest; how
a human or Agent should navigate many concurrent roots is a reading-order
question, not a data-model one.

## Room board and collaborator discovery

Room-role configuration and commands exist; see the
[CLI reference](../reference/cli.md). How reliably availability is reported
across real Agent runtimes remains to be validated.

## Seeing a shared Room conversation

Role terminals in the same Room occupy separate Herdr tabs. They use one
shared record history, but the interface does not present that conversation
alongside their work; a reader must use the CLI to see it. Determine from real
use whether a shared conversation view or a different panel layout makes the
collaboration clear, without treating role terminals as separate Rooms.

## Broadcast and addressed delivery

The targeting contract is in the [CLI reference](../reference/cli.md).
Validate broadcast and the resulting delivery behavior with real Agents
before claiming the group conversation journey is proven.

## Delivery recovery

The reconciliation boundary is in
[collaboration.md](../architecture/collaboration.md). Validate the durable
store, agent-specific readiness, and ambiguous spawn or send outcomes with
interruption tests. hc does not guarantee task completion or recovery.

## Wake and delivery scanning

The kernel has no notification surface: observation is a plain read, and the
dispatcher scans Room records from its durable boundary as specified in
[collaboration.md](../architecture/collaboration.md). Whether that scan and
the hc event stream together cover every interruption case remains open until
exercised.
