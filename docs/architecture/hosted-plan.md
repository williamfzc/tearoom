---
type: architecture
title: Hosted Tearoom plan
description: How the multi-tenant service is built: architecture decisions, the Phase 1 implementation, and the phased roadmap.
tags: [architecture, hosted, saas, plan, roadmap]
timestamp: 2026-08-29
---

# Hosted Tearoom plan

[hosted-identity.md](../domain/hosted-identity.md) decides the product shape: a human control plane (accounts, GitHub, membership, credentials) around the Room/Actor/Event kernel. This document records how that gets built — the architecture, what Phase 1 shipped, and the roadmap after it.

## The architecture

**The coordination plane stays untouched.** Rooms, actors, and events keep the same schema and invariants; `core.Store` does not gain authentication. Everything here is a layer in front of it.

**One shared database, `room_id` as the tenant key.** The relational models already carry `room_id` on every row, so a `relational.Store` is now a cheap value object over a shared `*gorm.DB`: `relational.New(db, roomID)`. Many stores, one database, no cross-room table collisions. The append transaction's `UPDATE rooms ... WHERE id = ?` row lock serializes writers per room, exactly as it did for one room.

**The control plane is its own schema.** `internal/control` owns `ctl_accounts`, `ctl_memberships`, `ctl_credentials`, and its own versioned migration table (`ctl_schema_versions`), so the coordination schema never grows control rows and vice versa. Two migration chains, one database, both idempotent on open.

**Credentials are opaque, bound, hashed tokens.** A credential is `tr_` + 192 random bits, bound to (room, actor, capabilities, expiry), stored only as its sha256 hash, plaintext shown once at issue, individually revocable. Capabilities: `read`, `append`, `manage`. A credential is one agent's identity — the server requires every request to declare that exact actor, so a credential can never be used as a room-level key.

**URL shape.** Management at `/api/hosted/*` (admin bearer). The event API at `/r/<room-id>/api/*`; a middleware resolves the Bearer credential → (room, actor, capabilities) and rejects mismatches **before** delegating the stripped request to that room's own `transport.Server` instance. Each instance carries its own long-poll `Broadcaster`, so wakeups never cross rooms. Wrong-room credentials answer 401, never 403 — a token cannot probe which rooms exist.

**PostgreSQL is the hosted database.** The same models and migrations target both SQLite (local) and PostgreSQL (hosted); `relational.OpenURL` picks the dialector by DSN. Driver-specific pragmas live only in the local open path.

## Phase 1 — shipped 2026-08-29

The executable slice: dev-mode administration, credentials, the authenticated event API, and the full negative matrix — all in `make check`, no external dependencies.

- `internal/relational`: `RoomID` sentinel became a per-store `roomID`; `New(db, roomID)` and `OpenURL(dsn)` added; local `Init`/`OpenAt` unchanged.
- `internal/control`: the control plane — accounts, memberships, credentials (issue/verify/revoke, sha256 at rest, capability parse), room creation with owner membership, dev admin.
- `internal/hosted`: the multi-tenant server — `/api/hosted/*` management routes, `/r/<id>/api/*` with Bearer auth, actor binding, capability enforcement, per-room broadcaster registry.
- CLI: the `hosted` group — `init-db`, `create-room`, `issue-credential`, `revoke`, `serve`. Client side needed no new surface: `TEAROOM_SERVER` points at `/r/<room-id>`, `TEAROOM_TOKEN` carries the credential.
- Dev mode: `TEAROOM_HOSTED_DEV=1` + `TEAROOM_HOSTED_ADMIN=<token>` gates management routes; the login endpoint answers the dev account. The GitHub OAuth exchange is stubbed by design.
- Proof: E8 ([hosted-demo](../../scripts/hosted-demo.sh), 15 checks: happy-path handoff through credentials, session resume, and the 401/403/revocation matrix) plus unit suites for `control` and `hosted`. S6/S7/S8 are now proven in the [evaluation set](../evaluations.md). PostgreSQL keeps its gated suite (`make pg-smoke`, `TEAROOM_TEST_PG`).

Run it:

```bash
TEAROOM_HOSTED_DEV=1 TEAROOM_HOSTED_ADMIN=dev-admin-1 tearoom hosted serve --database hosted.db
# admin (Bearer dev-admin-1):
curl -X POST -H "Authorization: Bearer dev-admin-1" -H "Content-Type: application/json" \
  -d '{"name":"ops"}' http://localhost:8787/api/hosted/rooms
curl -X POST -H "Authorization: Bearer dev-admin-1" -H "Content-Type: application/json" \
  -d '{"actor":"alice","capabilities":"read,append"}' http://localhost:8787/api/hosted/rooms/<id>/credentials
# agent (the issued token):
TEAROOM_SERVER=http://localhost:8787/r/<id> TEAROOM_TOKEN=tr_... TEAROOM_ACTOR=alice tearoom event send "..."
```

## Phase 2 — real identity

- **GitHub OAuth** replaces the dev admin: the `/api/hosted/login` exchange issues a signed session, sessions map to accounts, and management routes authorize by account membership instead of a shared token. S5 closes; the OAuth exchange becomes pending evidence with its own gated suite.
- **TLS and deployment**: hosted operation's security boundary ([security.md](../domain/security.md)) requires TLS termination, rate limiting, and audit records for control-plane changes.
- **Organizations (S9)**: membership scoping — a member inspects only their rooms; unrelated accounts cannot discover or read a room. The `Membership` model and `MembershipOf` already scope management; orgs add a tenant layer above accounts.
- **Credential rotation (S10)**: expiry is already per-credential; rotation adds an operator workflow (issue → migrate → revoke) without touching Room or Actor identity.
- **Invitation flow**: issue credentials to named actors with a TTL, mirroring the existing API.

## Phase 3 — operation

- **Billing** sits behind the same control plane, per the 2026-08-28 direction: billing records belong to the control plane around the Room, never to the kernel.
- **Abuse controls**: per-account rate limits, quotas, and moderation surfaces; the coordination schema is untouched.
- **Retention and compaction**: the interplay of expiry/compaction with the append contract (dense `seq`, `caused_by` chains, cursor semantics) needs its own design before data deletion is offered.

## Deployment shapes

Phase 1 code runs as one process; the shape follows what is stateful:

```
Cloudflare — DNS · TLS · CDN · DDoS · static admin UI · R2   (stateless, free)
        │ reverse proxy
tearoom hosted serve  — per-room broadcaster registry,      (stateful, one instance)
                        60s-held watch long-polls, GORM pool
        │ postgres://
Neon (free tier) — ctl_* control plane + coordination tables (the shared database)
```

The service itself is deliberately stateful in memory: the per-room `Broadcaster` and held watch connections are the product's handoff mechanism. Nothing stops a second instance behind a sticky load balancer, but the memory state is real and single-instance is the default. Cheapest early shape: the developer's own machine behind a `cloudflared` tunnel (Cloudflare gives DNS, TLS, CDN for free), Neon as the database; a VPS later. The gateway layers — DNS, TLS, CDN, rate limiting, the static management UI, R2 for exports — are the genuinely stateless parts and belong to Cloudflare in any shape.

## Considered: the all-Cloudflare shape

The free tiers are real (2026): Workers 100K requests/day (10 ms CPU, 1 MB body), D1 5 GB / 5M rows read / 100K rows written per day, R2 10 GB. It is rejected for the current product, not because of quotas:

- **Watch semantics.** `event watch` is the agent handoff mechanism: a server-held long-poll capped at 60 s, woken by a per-room in-process `Broadcaster`. Workers is a request-lifecycle model — a request cannot hold a 60-second wait on an in-process channel, and the broadcaster registry has no home in a stateless runtime. Porting watch to Queues/Durable Objects (whose free-tier availability is itself uncertain) is a rewrite of half the transport layer.
- **The database.** D1 is SQLite behind a worker-binding API, not a DSN GORM can open. Switching to it throws away the Phase 1 property that one schema and one transaction contract run over SQLite locally and PostgreSQL hosted; and 100K rows written/day is a daily limit for an active coordination room, not a headroom figure.

What Cloudflare contributes today, free and conflict-free: DNS, TLS, CDN, DDoS protection, the static management UI, and R2 for event exports. Reconsider the all-Cloudflare shape if watch ever degrades to polling (the CLI's local mode already polls) or if a serverless event API becomes acceptable.

## Decisions recorded

| Decision | Choice | Why |
|---|---|---|
| Tenant key | `room_id` on every coordination row | the models already carry it; no schema change |
| Control schema | `ctl_` tables + own migration chain | control rows never mix with coordination rows |
| Credential storage | sha256 hash, no KDF | 192 random bits need no stretching; leak never yields tokens |
| Capability model | `read` / `append` / `manage` | matches the kernel's read-append surface |
| URL shape | `/r/<id>/api/*` | keeps the path self-describing and one segment per tenant |
| Wrong-room tokens | 401 | a token must not probe room existence |
| Registration | any live credential may register its own actor | identity, not content; a read-only agent can still join |
| Dev mode | env-gated admin token | real OAuth needs a browser flow; Phase 2 replaces it |
