---
type: lesson
title: A rival built from a plain file falsified four claimed advantages
description: Measured comparison against an append-only JSONL file removed activation, incremental reads, per-participant boundaries, and concurrent-append safety as reasons for this project to exist.
resource: docs/evaluations.md
tags: [lesson, falsification, scope, measurement]
timestamp: 2026-09-02
---

# A rival built from a plain file falsified four claimed advantages

## Why this record exists

[../evaluations.md](../evaluations.md) states the falsifier for S1: if an
ordinary group chat or a shared file delivers comparable replay and integration
cost, the story does not hold. That falsifier was never executed. This document
records the session that executed it, because the result narrows scope rather
than widening it.

Each claim below was tested by building the cheapest rival that could satisfy
it — an append-only JSONL file plus a line number — and measuring both sides.

## Claim 1: activation needs to be a product layer

Falsified. A poll, read-context, launch script reached usable semantics in
about sixty lines of shell, using only three existing primitives: reads with no
server-side cursor, an explicit `--after` boundary, and public `to` addressing.

The naive thirty-line version failed three ways, and each fix was small:

| Failure | Fix |
|---|---|
| Work lost silently when the waker died mid-run | Claim before running, settle after; retry an unsettled claim, bounded |
| One task executed twice under two wakers | Exclusive lock per target Actor via atomic `mkdir` |
| Two Actors woke each other without end | Never wake an Actor for its own contribution |

Two bugs in the fix itself only appeared under a real kill. A lock removed by
an `EXIT` trap survives `SIGKILL` and deadlocks every later waker, so liveness
of the recorded holder — not existence of the directory — must decide
ownership. A deadlock is worse than the duplicate it prevents. Separately, a pid
recorded from inside a subshell is not the pid a supervisor kills, so the
orphan looks alive and the lock is never reclaimed.

What the script does not provide: multi-host coordination, lease takeover on
timeout, and fencing of a preempted run. Those are absent from most
single-operator use.

Consequence: the product definition then specified activation as a layer
inside the product. That conflict was adjudicated on 2026-09-09: activation
is out of product scope, and delivery is a paste, a context link, or an
external script. The refusal and its reopening condition live in
[../product.md](../product.md); the superseded design is in Git history.

## Claim 2: incremental reads are needed for cost

Falsified at small and moderate scale. Measured against one Room of 2,000
Events on the file Store:

| Operation | Median latency | Payload |
|---|---|---|
| Full read, 2,000 Events | 22.4 ms | 363 KB |
| Incremental read, 1 Event | 20.1 ms | 0.2 KB |

Latency differs by 1.1x; process startup dominates. Reading everything is not
slower in any way a caller notices.

The real cost is context, not disk. 363 KB is roughly 93,000 tokens: about 46%
of a 200k window for a single read, and a Room near 4,300 Events fills a whole
window once.

A second effect matters more than cost. Reading everything removes the
distinction between new and already-handled work: an Actor woken three times
against one directed Event responded three times, because nothing in the
payload told it which part was new.

So `--after` is not a performance feature. It carries the meaning *what is new
for me*, which is why it stays.

## Claim 3: per-participant progress lines are distinctive

Falsified. The rival reproduced independent boundaries at the cost of one
integer per participant: each reader keeps a line number, reads from it, and
advances it. Divergent boundaries, late joiners recovering full history, and
replay of an earlier range all worked.

## Claim 4: concurrent appends need a coordinated Store

Falsified, and the earlier evidence for it was invalid.

An initial comparison showed 38 of 60 records torn at 8 KB under six concurrent
writers, against zero for this project. That measurement was wrong. The rival
was written with shell `printf`, whose stdio buffering splits an 8 KB record
into several `write()` calls; interleaving then tears it. The comparison was
between a defective script and a correct program, not between two designs.

Rewritten so one record is exactly one `write()` with `O_APPEND`, the rival
holds. Sixteen concurrent processes, forty records each:

| Payload | Expected | Parsed | Corrupted |
|---|---|---|---|
| 4 KB | 640 | 640 | 0 |
| 8 KB | 640 | 640 | 0 |
| 64 KB | 640 | 640 | 0 |
| 256 KB | 640 | 640 | 0 |
| 1 MB | 640 | 640 | 0 |

A single append write stays whole well past the `PIPE_BUF` boundary that the
original reasoning assumed. Concurrent-append safety is not a differentiator.

The transferable rule: a rival built to lose proves nothing. When a comparison
favors the local design, suspect the rival's implementation before believing
the result.

## What survives

Not capability. What remains are conveniences a caller would otherwise
rebuild: membership admission, `to` addressing, dense stable sequence numbers,
an alternate SQLite Store, the server mode, and one error envelope.

Convenience is a legitimate reason for a tool to exist. It is not evidence of a
defensible product position, and this record should not be read as providing
one.

## Related

- [../evaluations.md](../evaluations.md)
- [../product.md](../product.md)
- [cross-process-locking.md](cross-process-locking.md)
- [../reference/agent-integration.md](../reference/agent-integration.md)
