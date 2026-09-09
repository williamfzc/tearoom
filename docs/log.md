---
type: log
title: Change log
description: Dated record of product decisions and structural changes, newest first.
tags: [log, history]
timestamp: 2026-08-30
---

# Change log

## 2026-09-02 — Execute the S1 falsifier against a plain-file rival

Ran the comparison [evaluations.md](evaluations.md) requires for S1 and recorded
the outcome in [lessons/rival-falsification.md](lessons/rival-falsification.md).
An append-only JSONL file plus a line number matched this project on
per-participant boundaries, full-history recovery, and replay of an earlier
range. Full reads cost 22.4 ms against 20.1 ms for a single-Event incremental
read over a 2,000-Event Room, so incremental reads are not a latency feature;
their remaining value is the meaning *what is new for me*, worth about 93,000
tokens of context per full read. Concurrent appends stayed whole for sixteen
processes from 4 KB to 1 MB, so coordinated appends are not a differentiator
either; the earlier contrary measurement was invalid because the rival used
buffered shell `printf` rather than one `write()` per record. Activation reached
usable at-least-once semantics in about sixty lines of external shell over
existing primitives, which conflicts with
`architecture/activation.md` specifying it as a
product layer. That conflict is left open rather than reconciled. What survives
is convenience — admission, addressing, dense sequences, the alternate Store,
server mode, one error envelope — not a demonstrated product position.

## 2026-08-30 — Define the public binary release boundary

The canonical release contract is recorded in
[operations/releasing.md](operations/releasing.md): private source, public
binary artifacts in R2 only, immutable version prefixes, a mutable
`latest.json` pointer, SHA256 verification, credential rotation, and matched
format-2 rollback snapshots. The `Release` workflow and its actual package,
check, and upload steps are described there without conflating package checks
with the separate `make check` project gate.

## 2026-08-30 — Execute the P0 clean-break cutover

The clean break planned above shipped as one cutover branch, in three
implementation waves plus the CLI, deletion, and documentation passes:

1. **Core and file store** (`internal/core`, `internal/store`): the minimum
   Room/Actor/Event contract, `format: 2` marker gate, and the file backend on
   the new `Store` interface.
2. **Relational and transport** (`internal/relational`, `internal/transport`):
   the SQLite/PostgreSQL Store on the same contract and the HTTP server/client
   with the unified error envelope, `X-Tearoom-Actor`, `Idempotency-Key`, and
   server-side `wait`.
3. **CLI** (`internal/cli`, `cmd/tearoom`): `room init|join|members`, `send`,
   `read`, and `server start` only; exit codes 0/1/3/4; plain text encodes as a
   JSON string; `--after e<seq>`, `--limit`, `--wait`, `--idempotency-key`;
   loopback-only serving in P0. The `event` group, session files, cursor, typed
   Event flags, `TEAROOM_HOME`, and `--caused-by`/`--body`/`--data` are gone.
4. **Deletion per the manifest**: the eight shell suites, `internal/control`,
   `internal/hosted`, `internal/cli/hosted.go`, the `web/` SPA, the deployment
   files, and `operations/deploy.md` were removed, not aliased.
5. **Evaluations**: `target-room` (E1) and `target-replay` (E2) ship as
   executable scripts inside `make check`; S1 and S3 became proven; the legacy
   suites are recorded as removed in the registry.
6. **Docs**: status, reference, operations, product, and design documents were
   rewritten to the shipped surface. The hosted implementation is deleted and
   its design deferred, secured by a user login identity rather than
   Actor-as-secret. The cutover is recorded here, newest first.

## 2026-08-30 — Record the P0 cutover execution contract

The clean break is now executable from one document:
[operations/p0-room-cutover.md](operations/p0-room-cutover.md). It fixes the
target Store/CLI/HTTP contract, the Luna A/B/C wave ownership, the integration
order, the test loop, and the deletion manifest. Two decisions were locked
during review: the shell smokes (`pg-smoke.sh`, `smoke-sqlite.sh` included)
are deleted rather than rewritten — PostgreSQL validation lives in the gated Go
battery and SQLite is covered by the `target-room` evaluation — and
`architecture/status.md` itself is rewritten at cutover, because the hosted
implementation is deleted (not adapted) while the hosted product design stays
as a deferred, login-identity-based future.

## 2026-08-30 — Choose a clean-break implementation migration

The adopted Room/Event model will replace the pre-convergence implementation
without command aliases, dual Event schemas, cursor-session import, directed
visibility fallback, old HTTP request shapes, or a deprecated Store path. Each
implementation phase must cut its owned layers, callers, tests, examples, and
docs over together. Rollback uses a matched Git/deployment revision and a
pre-cutover data snapshot; it is not an in-product compatibility mode.

The normative route is now [product.md](product.md) for product facts,
[architecture/status.md](architecture/status.md) for implementation phases,
[evaluations.md](evaluations.md) for proof, and [reference/](reference/index.md)
for target interfaces. The old Agent skill was removed because executable
instructions for the old CLI would keep routing Agents into the rejected model;
a new skill is created only when the target CLI exists.

## 2026-08-30 — Adopt the minimum Room and optional activation model

Promoted the conclusions in
[architecture/model-discussion.md](architecture/model-discussion.md) into the
authoritative product design. The Room now owns one complete history visible to
every member; `to` is public addressing rather than privacy; reads have no
implicit Actor checkpoint; ordinary append has no hidden freshness precondition;
and the common target surface is `send` plus side-effect-free `read`, with
waiting as a read mode. The minimum Event is `seq`, `actor`, `at`, optional
`to`, and `content`; protocols may encode richer meaning in content without
promoting `type` or `caused_by` into the kernel.

Agent activation is an optional application above the Room. A runtime binding
and activation policy may elect an addressed Event, launch or resume the stable
Actor, and pass an Event reference so the Agent reconstructs context from the
Room. Activation delivery state is explicit operational state, not Actor read
progress or proof of task completion.

This entry records a product decision, not a shipped implementation. The v0.1
code still has saved cursors, mandatory stale-write checks, directed visibility,
typed/causal Event fields, and `event send/list/watch`; the migration and the
offline-activation proof remain open in
[architecture/status.md](architecture/status.md) and
[evaluations.md](evaluations.md).

## 2026-08-29 — Record the model discussion draft

Captured the first open design discussion as
[architecture/model-discussion.md](architecture/model-discussion.md). The file
was subsequently rewritten and adopted on 2026-08-30; this entry describes its
historical draft state rather than its current contents.

## 2026-08-29 — Take the hosted stack live: Render + Neon + Cloudflare Pages

Deployed the real thing for the first time. The service runs on **Render free** (Singapore, Docker, `[redacted deployment address]`), the database on **Neon free** (`[redacted project identifier]`, also Singapore, auto-suspend 5 min idle), and the management UI on **Cloudflare Pages** (`[redacted deployment address]`). Verified end to end against the live service: healthz, dev-admin login, room creation, credential issue, actor registration, event append + read, and the CORS matrix (preflight 204 for the Pages origin, foreign origins get 401 with no headers).

The live deployment surfaced two real gaps that the runbook had not caught:

1. **A platform service without an explicit start command cannot serve.** The dashboard-created service ran the image's bare entrypoint and exited immediately. Fixed by making the image default to `hosted serve` (`$DATABASE_URL` interpolated at container start) — `6ea8bd6`. A platform start command still overrides it.
2. **`TEAROOM_CORS_ORIGIN` was never read.** The `--cors-origin` flag defaulted to empty, so an env-driven service silently served without CORS. The flag now defaults to the env var — `2f792a5`.

Operational notes recorded: Render's API types (`web_service`), the env-vars endpoint is a full-replacement `PUT` with a bare array body, the API requires a payment method on file even for free services (dashboard action, no API bypass), and neonctl 4.x auth is `neonctl auth` (browser) or `NEON_API_KEY`. The admin token lives at `~/.tearoom-admin-token` and in the service's `TEAROOM_HOSTED_ADMIN`.

## 2026-08-29 — Complete the implementation queue: polling, deployment files, management UI

Shipped all three steps of [hosted-plan.md](architecture/hosted-plan.md)'s implementation queue, each green in `make check` before its commit:

1. **Client-side watch polling** (`refactor: poll event watch from the client instead of the server-held wait`, 4fe77c9). `eventWatch` now reads on an interval for every store, exactly like the local branch; the CLI's waiter interface is gone. Watch's command surface, skill text, and demo semantics are unchanged — hosted-demo still proves the handoff matrix, now carried by polls. `transport`'s `wait`/Broadcaster stays as the optional LAN optimization.
2. **Deployment files** (`feat: add the deployment files`, e4c10be). A multi-stage `Dockerfile` (static `CGO_ENABLED=0` build into alpine, non-root user), the `render.yaml` blueprint, and the runbook at `operations/deploy.md` (deleted at the cutover; its deployment
was never re-adopted): Neon project + pooled DSN → `tearoom hosted init-db` → admin token → Render free-tier web service → end-to-end verification → TLS/domain notes → the 2026 free-tier constraints. The server gained `GET /healthz` (no credentials) for the platform health check.
3. **Management UI** (`feat: allow a configured CORS origin`, b79e1f0; `feat: list room credentials for the management UI`, c5f2961; `web: build the hosted management UI`, fe0de64). A deliberately thin React + Vite static SPA in `web/` — rooms, credentials, room history — in the recorded Vercel/Claude-style minimal tech, built to `dist/` for Cloudflare Pages (`VITE_API_BASE` + `--cors-origin`). Supporting server work: CORS for one exact configured origin (preflight 204, foreign origins get no headers), and `GET /api/hosted/rooms/<id>/credentials` returning a hash-free `CredentialView` — the at-rest token digest never leaves the server.

## 2026-08-29 — Decide watch's wait moves to the client; settle the free deployment

Re-examined the server-held long-poll: coordination tolerates seconds of latency, so the long-poll was over-engineering. `event watch` keeps its batch semantics and its place in the skill and the demos; the wait moves to the CLI — the same `Read` + interval loop the local mode already runs. The hosted server is therefore a stateless read/append/register API; Phase 1's `wait` parameter and per-room broadcaster remain only as an optional self-hosted-LAN optimization.

With the server stateless, the deployment settles ([hosted-plan.md](architecture/hosted-plan.md)): **Render free tier** hosts the Go service — 750 free instance hours/month exceed the longest month's 744, spun-down time does not consume hours, and client polling keeps the instance awake in practice, so it is genuinely 24/7 at $0. **Neon free** holds the database (Render's free PostgreSQL expires 30 days after creation by design). **Cloudflare Pages** hosts the management UI — a React + Vite static SPA calling `/api/hosted/*` with a Bearer token, the only free static host with unlimited bandwidth and no commercial-use ban (Vercel's Hobby plan forbids commercial use; Netlify's 300 build minutes are too few). Visual direction: Vercel/Claude-style minimal tech — restrained layout, generous whitespace, precise type. An all-Cloudflare rewrite stays out of scope until the product proves itself.

## 2026-08-29 — Ship hosted Phase 1: the multi-tenant server

Implemented the first executable slice of the hosted product ([hosted-plan.md](architecture/hosted-plan.md)): a control plane and a per-room event API in front of the unchanged Room/Actor/Event kernel, all in `make check`.

`relational` became multi-tenant: the `RoomID` sentinel is now a per-store `roomID`, `New(db, roomID)` makes the Store a cheap value object over a shared `*gorm.DB`, and `OpenURL(dsn)` opens PostgreSQL by URL while local use keeps `Init`/`OpenAt`. New `internal/control` owns the control plane (`ctl_accounts`, `ctl_memberships`, `ctl_credentials`, own migration chain): credentials are opaque `tr_` tokens — 192 random bits, sha256 at rest, bound to (room, actor, capabilities, expiry), plaintext shown once, individually revocable. New `internal/hosted` serves management routes (`/api/hosted/*`, dev admin bearer) and the authenticated event API (`/r/<room-id>/api/*`): a middleware resolves the Bearer to a credential, checks room binding, actor binding, and capability, then delegates to that room's own transport server with its own long-poll broadcaster. Wrong-room tokens answer 401 so they cannot probe room existence. The CLI gained the `hosted` group (`init-db`, `create-room`, `issue-credential`, `revoke`, `serve`); clients needed no new surface — `TEAROOM_SERVER` points at `/r/<id>` and `TEAROOM_TOKEN` carries the credential.

`scripts/hosted-demo.sh` (E8, 15 checks, deleted at the cutover with the hosted
implementation) proves the hosted stories S6–S8: a handoff through credentials with session resume, and the negative matrix — no token 401, wrong-room 401, actor mismatch 403, read-only append refused (CLI exit and HTTP 403), revoked bob dead while alice works. PostgreSQL validation stays gated behind `TEAROOM_TEST_PG` (`make pg-smoke` + a `pg_test.go` battery that mirrors the SQLite suite); both skip with a notice without a DSN. The GitHub OAuth exchange is the stub `login` route; S5 stays open as pending evidence.

## 2026-08-29 — Run the harness with real agents and close the remaining stories

Ran the harness workflow with three real agents (one planner, one generator, one evaluator, each an independent agent session) on a shared Room server. Each learned the CLI from the skill, then the loop ran end to end: plan → generate → revise → generate v2 → approve → close, threaded by `caused_by`. Measured send latency 13–31 ms and event-to-delivery latency 18–20 ms over local HTTP; no stale writes, no retries needed. The agents' skill feedback (flags not in the reference, watch batch semantics, feed shape) went back into SKILL.md.

Then shipped the two remaining story scenarios. `scripts/game-demo.sh` plays one Werewolf round as a protocol over directed role Events (E6, story S3): a moderator deals three private roles, night actions reach only the moderator and their sender, the day is public — 13 checks. `scripts/worktree-demo.sh` proves one Room across two real git worktrees (E7, story S4): worktrees keep no local Room, handoffs cross them by `watch`, later processes resume cursors — 9 checks. Both ran inside `make check` until the clean break deleted them with the old contract they proved; the evaluation set then marked every story except the hosted ones proven.

## 2026-08-29 — Establish the bidirectional evaluation set

Added [evaluations.md](evaluations.md), the single registry mapping every user story to the evaluations that prove it: stories S1–S10 with explicit statuses (`proven`, `open`, `deferred`), evaluations E1–E7 (`shipped`, `planned`), and two mapping tables the new `make check-evals` verifier requires to agree with each other and with the statuses. Shipped suites now carry a machine-checkable `# Evaluation:` marker, and story documents name their registry IDs. Everything previously scattered as prose — the untested game protocol (S3), the unmeasured cross-worktree Room (S4), the deferred hosted stories (S5–S10), PostgreSQL validation — is now marked, tracked, and checkable in one place.

## 2026-08-29 — Add the harness workflow user story

Added [domain/harness-workflow.md](domain/harness-workflow.md): a planner, a generator, and an evaluator run one plan/generate/evaluate loop through a Room with no human relay. The loop is a protocol over Events — `watch` for handoff, `caused-by` for chaining, directed Events for private reviewer notes, stale-write refusal as the resync signal — not new kernel entities. `scripts/harness-demo.sh` ran the story against the shipped binary inside `make check` (deleted at the clean break with the old contract); it closed the first evidence gap of [status.md](architecture/status.md), the task protocol outside the kernel.

## 2026-08-29 — Ship the relational Store on SQLite

Added the ORM-backed relational Store behind `core.Store`, shipped on SQLite: `tearoom room init --store sqlite` creates a GORM-managed `room.db` (pure-Go `glebarez/sqlite` dialector, so release binaries stay static), the choice is recorded in `.tearoom/store.json`, and every command and the server dispatch on it. The append contract that `flock` enforces in the file Store — cursor freshness, idempotency, dense ordering, admission, recipient validation, visibility — is enforced by one append transaction: lock the Room head, validate, resolve the idempotency key, compare the cursor, insert the immutable Event, advance the head, commit. Schema changes are versioned migrations applied at open. A shipped-binary smoke suite proves the same invariants over SQLite, including through the HTTP server; PostgreSQL validation remains pending behind the same schema and migrations.

## 2026-08-29 — Hold `event watch` on the server instead of polling

Replaced the client 250 ms poll loop with a server-held long-poll. `GET /api/events` gains a `wait` parameter, capped at 60 seconds; the server returns as soon as a visible Event appears and otherwise returns an empty Feed when the wait elapses. Appends through the server wake waiters directly, and an idle fallback probes the Store for out-of-band writes without taking the lock or scanning the log (`core.Probe`; the file Store answers by stat). The CLI slices long waits into 30-second requests against a server and keeps polling only for local Rooms.

SSE stays deferred: `event watch` returns batches and exits, so a long-poll matches its semantics with less machinery.

## 2026-08-29 — Ship the cross-agent skill

Added `.claude/skills/tearoom/SKILL.md`, the first cross-product integration
surface at the time. It taught the pre-convergence `event send/list/watch`
contract and was removed by the 2026-08-30 clean break so Agents would not be
routed into the rejected model.

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
