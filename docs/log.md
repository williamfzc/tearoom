---
type: log
title: Change log
description: Dated record of product decisions and structural changes, newest first.
tags: [log, history]
timestamp: 2026-08-28
---

# Change log

## 2026-08-29 — Decide watch's wait moves to the client; settle the free deployment

Re-examined the server-held long-poll: coordination tolerates seconds of latency, so the long-poll was over-engineering. `event watch` keeps its batch semantics and its place in the skill and the demos; the wait moves to the CLI — the same `Read` + interval loop the local mode already runs. The hosted server is therefore a stateless read/append/register API; Phase 1's `wait` parameter and per-room broadcaster remain only as an optional self-hosted-LAN optimization.

With the server stateless, the deployment settles ([hosted-plan.md](architecture/hosted-plan.md)): **Render free tier** hosts the Go service — 750 free instance hours/month exceed the longest month's 744, spun-down time does not consume hours, and client polling keeps the instance awake in practice, so it is genuinely 24/7 at $0. **Neon free** holds the database (Render's free PostgreSQL expires 30 days after creation by design). **Cloudflare Pages** hosts the management UI — a React + Vite static SPA calling `/api/hosted/*` with a Bearer token, the only free static host with unlimited bandwidth and no commercial-use ban (Vercel's Hobby plan forbids commercial use; Netlify's 300 build minutes are too few). Visual direction: Vercel/Claude-style minimal tech — restrained layout, generous whitespace, precise type. An all-Cloudflare rewrite stays out of scope until the product proves itself.

## 2026-08-29 — Ship hosted Phase 1: the multi-tenant server

Implemented the first executable slice of the hosted product ([hosted-plan.md](architecture/hosted-plan.md)): a control plane and a per-room event API in front of the unchanged Room/Actor/Event kernel, all in `make check`.

`relational` became multi-tenant: the `RoomID` sentinel is now a per-store `roomID`, `New(db, roomID)` makes the Store a cheap value object over a shared `*gorm.DB`, and `OpenURL(dsn)` opens PostgreSQL by URL while local use keeps `Init`/`OpenAt`. New `internal/control` owns the control plane (`ctl_accounts`, `ctl_memberships`, `ctl_credentials`, own migration chain): credentials are opaque `tr_` tokens — 192 random bits, sha256 at rest, bound to (room, actor, capabilities, expiry), plaintext shown once, individually revocable. New `internal/hosted` serves management routes (`/api/hosted/*`, dev admin bearer) and the authenticated event API (`/r/<room-id>/api/*`): a middleware resolves the Bearer to a credential, checks room binding, actor binding, and capability, then delegates to that room's own transport server with its own long-poll broadcaster. Wrong-room tokens answer 401 so they cannot probe room existence. The CLI gained the `hosted` group (`init-db`, `create-room`, `issue-credential`, `revoke`, `serve`); clients needed no new surface — `TEAROOM_SERVER` points at `/r/<id>` and `TEAROOM_TOKEN` carries the credential.

[scripts/hosted-demo.sh](../scripts/hosted-demo.sh) (E8, 15 checks) proves the hosted stories S6–S8: a handoff through credentials with session resume, and the negative matrix — no token 401, wrong-room 401, actor mismatch 403, read-only append refused (CLI exit and HTTP 403), revoked bob dead while alice works. PostgreSQL validation stays gated behind `TEAROOM_TEST_PG` (`make pg-smoke` + a `pg_test.go` battery that mirrors the SQLite suite); both skip with a notice without a DSN. The GitHub OAuth exchange is the stub `login` route; S5 stays open as pending evidence.

## 2026-08-29 — Run the harness with real agents and close the remaining stories

Ran the harness workflow with three real agents (one planner, one generator, one evaluator, each an independent agent session) on a shared Room server. Each learned the CLI from the skill, then the loop ran end to end: plan → generate → revise → generate v2 → approve → close, threaded by `caused_by`. Measured send latency 13–31 ms and event-to-delivery latency 18–20 ms over local HTTP; no stale writes, no retries needed. The agents' skill feedback (flags not in the reference, watch batch semantics, feed shape) went back into SKILL.md.

Then shipped the two remaining story scenarios. [scripts/game-demo.sh](../scripts/game-demo.sh) plays one Werewolf round as a protocol over directed role Events (E6, story S3): a moderator deals three private roles, night actions reach only the moderator and their sender, the day is public — 13 checks. [scripts/worktree-demo.sh](../scripts/worktree-demo.sh) proves one Room across two real git worktrees (E7, story S4): worktrees keep no local Room, handoffs cross them by `watch`, later processes resume cursors — 9 checks. Both run inside `make check`; the evaluation set now marks every story except the hosted ones proven.

## 2026-08-29 — Establish the bidirectional evaluation set

Added [evaluations.md](evaluations.md), the single registry mapping every user story to the evaluations that prove it: stories S1–S10 with explicit statuses (`proven`, `open`, `deferred`), evaluations E1–E7 (`shipped`, `planned`), and two mapping tables the new `make check-evals` verifier requires to agree with each other and with the statuses. Shipped suites now carry a machine-checkable `# Evaluation:` marker, and story documents name their registry IDs. Everything previously scattered as prose — the untested game protocol (S3), the unmeasured cross-worktree Room (S4), the deferred hosted stories (S5–S10), PostgreSQL validation — is now marked, tracked, and checkable in one place.

## 2026-08-29 — Add the harness workflow user story

Added [domain/harness-workflow.md](domain/harness-workflow.md): a planner, a generator, and an evaluator run one plan/generate/evaluate loop through a Room with no human relay. The loop is a protocol over Events — `watch` for handoff, `caused-by` for chaining, directed Events for private reviewer notes, stale-write refusal as the resync signal — not new kernel entities. [scripts/harness-demo.sh](../scripts/harness-demo.sh) runs the story against the shipped binary inside `make check`; it closes the first evidence gap of [status.md](architecture/status.md), the task protocol outside the kernel.

## 2026-08-29 — Ship the relational Store on SQLite

Added the ORM-backed relational Store behind `core.Store`, shipped on SQLite: `tearoom room init --store sqlite` creates a GORM-managed `room.db` (pure-Go `glebarez/sqlite` dialector, so release binaries stay static), the choice is recorded in `.tearoom/store.json`, and every command and the server dispatch on it. The append contract that `flock` enforces in the file Store — cursor freshness, idempotency, dense ordering, admission, recipient validation, visibility — is enforced by one append transaction: lock the Room head, validate, resolve the idempotency key, compare the cursor, insert the immutable Event, advance the head, commit. Schema changes are versioned migrations applied at open. A shipped-binary smoke suite proves the same invariants over SQLite, including through the HTTP server; PostgreSQL validation remains pending behind the same schema and migrations.

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
