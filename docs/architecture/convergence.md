---
type: architecture
title: Convergence
description: The minimum Room and Event model, its intended CLI shape, and the boundary with the current implementation.
resource: internal/cli/cli.go
tags: [architecture, cli, convergence, decision]
timestamp: 2026-08-30
---

# Convergence

`model-discussion.md` is the current design baseline. It describes the smallest
honest product: a durable shared Room, membership, one ordered immutable Event
history, stable Actor identities, and optional Agent activation above that
kernel. This document records the corresponding command and layer shape.

The baseline is not yet the implementation. The executable still exposes the
older `room`, `server`, and `event` command groups, auto-registers Actors,
advances a saved local cursor on reads, filters directed Events, and requires a
fresh cursor for append. Those differences are intentional migration debt, not
facts to hide behind the target vocabulary.

## Target responsibilities

| Concern | Target surface | Responsibility |
|---|---|---|
| `room` | `init`, `info` | Local or hosted Room lifecycle and membership admission |
| `server` | `start` | Optional local service process |
| common interaction | `send`, `read` | Append and repeatable history queries |
| activation | application-owned service | Optional wake, claim, retry, and runtime binding |

The target common path is deliberately small:

```bash
tearoom send "review worktree A"
tearoom read
tearoom read --after e42 --wait 5m
```

`to` is public addressing metadata. It identifies Room members who should pay
attention; it does not change visibility and does not itself start a process.
An activation policy may observe an addressed Event, but activation remains an
optional application capability.

## Current command boundary

Until the migration is designed and implemented, the supported executable path
remains:

```text
tearoom room init|info
tearoom server start
tearoom event send|list|watch
```

The current `event list` and `event watch` semantics are implementation facts,
not the target contract: they use local session cursors and `watch` waits for a
batch. Do not describe them as a durable subscription or as proof of offline
Agent activation. The current broadcaster can wake only waiters connected to
the same server process.

## Migration boundary

The migration must be explicit and cross-layer:

1. replace implicit Actor read checkpoints with caller-supplied `--after` or an
   explicitly named consumer identity;
2. make Room membership the visibility boundary, leaving `to` as routing or
   attention metadata;
3. make ordinary append independent of a saved read cursor;
4. retain stable Actor identity while separating it from credentials, runtime
   bindings, and process executions;
5. add activation only as an application-layer experiment, with durable
   delivery state separate from Room history.

The code, evaluation scripts, and reference documents still prove the older
model. A future migration is complete only when those surfaces agree; this
document does not claim that it has happened.
