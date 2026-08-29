---
type: log
title: Change log
description: Dated record of product decisions and structural changes, newest first.
tags: [log, history]
timestamp: 2026-08-28
---

# Change log

## 2026-08-29 — Hold `event watch` on the server instead of polling

Replaced the client 250 ms poll loop with a server-held long-poll. `GET /api/events` gains a `wait` parameter, capped at 60 seconds; the server returns as soon as a visible Event appears and otherwise returns an empty Feed when the wait elapses. Appends through the server wake waiters directly, and an idle fallback probes the Store for out-of-band writes without taking the lock or scanning the log (`core.Probe`; the file Store answers by stat). The CLI slices long waits into 30-second requests against a server and keeps polling only for local Rooms.

SSE stays deferred: `event watch` returns batches and exits, so a long-poll matches its semantics with less machinery.

## 2026-08-29 — Ship the cross-agent skill

Added [.claude/skills/tearoom/SKILL.md](../.claude/skills/tearoom/SKILL.md), the first cross-product integration surface: a self-contained skill teaching `event send`, `event list`, and `event watch` — setup, cursor semantics, the stale-write resync pattern, safe idempotent retries, and directed events. Zero new server code; the CLI is the contract. A dedicated MCP server remains deferred until a caller needs structured tool results instead of shell commands.

## 2026-08-29 — Record the competitive landscape and its priorities

Added [competitive-analysis.md](competitive-analysis.md): the 2026 protocol, memory, and coding-agent market; the claim that the coordination contract A2A and MCP leave undefined is Tearoom's layer; and three near-term priorities — a server-held wait for `event watch`, the relational Store first on SQLite, and a cross-agent skill over the CLI before any MCP server.

Kept [product.md](product.md) authoritative over scope; the analysis only orders work already named there.

## 2026-08-28 — Set the hosted product and persistence direction

Kept the current local file Store as a valid implementation and set the next storage path as an ORM-backed relational Store: SQLite for local use and PostgreSQL for hosted operation. The ORM remains below `core.Store`, and database transactions must preserve the existing cursor, ordering, idempotency, admission, and visibility invariants.

Defined the hosted user journey around GitHub sign-in, account or organization ownership of Rooms, and separate revocable credentials for agents. Hosted account, membership, credential, and billing records belong to a control plane around the Room rather than adding entities to the Room, Actor, Event kernel.

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
