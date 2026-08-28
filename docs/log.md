---
type: log
title: Change log
description: Dated record of product decisions and structural changes, newest first.
tags: [log, history]
timestamp: 2026-08-28
---

# Change log

## 2026-08-28 — Shorten the common Event path

Separated workspace lifecycle (`room`) from the service process (`server`) and kept normal interaction under `event`. Removed the explicit Actor command: Event operations register the named Actor automatically. Added per-Room, per-Actor client cursor state so ordinary use is `event send`, `event list`, and `event watch` without protocol bookkeeping; explicit `--cursor` and `--id` overrides remain for advanced clients. Real Agent trials showed that callers naturally guessed `--body` and `--id`, so those names replaced the less direct request-id spelling.

## 2026-08-28 — Group the CLI by product resources

The intermediate design replaced the flat command list with `room`, `actor`, and `event` resource groups. It established nested help and JSON defaults, but exposed domain nouns more directly than users needed; the later command-surface decision above supersedes it.

## 2026-08-28 — Replace the task model with Room, Actor, Event

Replaced Workspace, Agent, Thread, Lease, Gate, and the separate ledger with three core entities: Room, Actor, and Event. Removed the derived board packages instead of retaining a parallel compatibility model.

Introduced Room, Actor, and Event as the core model. Task coordination, worktree handoff, approval, direct messages, and games became Event protocols rather than kernel entities.

Every append still carries an expected cursor and idempotency key. The CLI supplies them from local session state on the common path, while explicit protocol clients may provide them directly. The Store rejects stale views, assigns one dense Room sequence under `flock`, returns the original Event for an identical retry, and filters directed Events by Actor.

Replaced the previous task-specific tests with direct Store, HTTP, real-binary, cross-process, cross-session, and cross-product checks.

## 2026-08-28 — One binary, no browser interface

Removed the WebAssembly frontend and separate server binary. `tearoom server start` now exposes an existing local Room from the same executable used by clients.

## 2026-08-27 — Establish repository boundaries

Adopted the repository working rules, separated domain contracts from file persistence and outside transports, and organized durable documentation as an indexed Markdown bundle.
