---
type: decision
title: Open questions
description: Decisions deliberately left unsettled after the room-tree convergence.
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

## Notification carriers

`watch` prints to standard output, and that is the only shipped carrier. A
socket, a file tail, or a push channel may be justified by a real waker that
outgrows a script; until one exists, stdout stays the whole contract.
