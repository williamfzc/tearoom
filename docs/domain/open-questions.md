---
type: decision
title: Open questions
description: Runtime design questions left open after adopting shared Room conversation and hc-based collaboration.
tags: [domain, decision, open]
timestamp: 2026-11-07
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

## Room rename and move

Names are the addresses everyone quotes (`room#seq`). Renaming or moving a
Room would silently break quoted references; no evidence yet says the
operation is needed often enough to design around that.

## Presentation of multiple roots

A workspace may hold several independent roots. The CLI prints a forest; how
a human or Agent should navigate many concurrent roots is a reading-order
question, not a data-model one.

## Room board and collaborator discovery

The proposed Room-role binding is in
[collaboration.md](../architecture/collaboration.md). The configuration file,
CLI syntax, and precise availability reporting remain to be validated.

## Broadcast and addressed delivery

The proposed defaults and explicit targeting are in
[collaboration.md](../architecture/collaboration.md). Validate broadcast and
`@all` behavior, input escaping, and structured content encoding with real
agents before treating them as a shipped CLI contract.

## Delivery recovery

The proposed reconciliation boundary is in
[collaboration.md](../architecture/collaboration.md). Validate the durable
store, agent-specific readiness, and ambiguous spawn or send outcomes with
interruption tests. hc does not guarantee task completion or recovery.

## Wake and delivery scanning

The kernel has no notification surface: observation is a plain read, and the
dispatcher scans Room records from its durable boundary as specified in
[collaboration.md](../architecture/collaboration.md). Whether that scan and
the hc event stream together cover every interruption case remains open until
exercised.
