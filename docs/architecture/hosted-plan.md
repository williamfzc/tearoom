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

**URL shape.** Management at `/api/hosted/*` (admin bearer). The event API at `/r/<room-id>/api/*`; a middleware resolves the Bearer credential → (room, actor, capabilities) and rejects mismatches **before** delegating the stripped request to that room's own `transport.Server` instance (one server per room, so wakeups never cross rooms). Wrong-room credentials answer 401, never 403 — a token cannot probe which rooms exist.

**PostgreSQL is the hosted database.** The same models and migrations target both SQLite (local) and PostgreSQL (hosted); `relational.OpenURL` picks the dialector by DSN. Driver-specific pragmas live only in the local open path.

**Watch's wait lives in the client.** `event watch` keeps its batch-return semantics without depending on server state: the CLI polls on an interval, exactly as it already does for local Rooms, so the hosted server is a stateless read/append/register surface. Phase 1's server-held `wait` (per-room `Broadcaster`) remains in the code as an optional optimization for self-hosted LANs; the deployment shapes below do not rely on it.

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
- **Management UI**: the front end from the [deployment shape](#deployment-shapes) — a static React + Vite SPA on Cloudflare Pages (rooms, memberships, credentials, room history against `/api/hosted/*`), styled Vercel/Claude-style minimal: restrained layout, generous whitespace, precise type.

## Phase 3 — operation

- **Billing** sits behind the same control plane, per the 2026-08-28 direction: billing records belong to the control plane around the Room, never to the kernel.
- **Abuse controls**: per-account rate limits, quotas, and moderation surfaces; the coordination schema is untouched.
- **Retention and compaction**: the interplay of expiry/compaction with the append contract (dense `seq`, `caused_by` chains, cursor semantics) needs its own design before data deletion is offered.

## Deployment shapes

The server is deliberately stateless: `event watch` waits in the CLI (client polling, the same loop local mode already runs), so hosted serving is a plain read/append/register API and everything stateful lives in one shared database. The free shapes therefore close:

```
Cloudflare Pages — management UI (React/Vite SPA, static assets; $0, unlimited bandwidth)
        │ fetch /api/hosted/* (Bearer; CORS allows the Pages origin)
Render (free)    — `tearoom hosted serve`, one stateless instance (Go, unchanged)
        │ postgres://
Neon (free)      — ctl_* control plane + coordination tables (the shared database)
```

**Render's free tier is the service's home, and it is genuinely 24/7.** The 750 free instance hours per workspace exceed the longest month (31 × 24 = 744 hours), spun-down time does not consume hours, and client polling keeps the instance awake in practice — as long as any agent polls at least every 15 minutes, the instance never sleeps and never pays a cold start. A quiet night spins it down and the first morning request pays one 30–60 s cold start, which coordination tolerates. The database always lives on Neon: Render's free PostgreSQL expires 30 days after creation by design.

- **Management UI** — Cloudflare Pages, the only free static host with unlimited bandwidth and no commercial-use ban (Vercel's Hobby plan forbids commercial use; Netlify's 300 build minutes are too few; Render's own static hosting shares the workspace's 5 GB/month egress with the API). A React + Vite SPA calling `/api/hosted/*` with the admin Bearer token. Visual direction: Vercel/Claude-style minimal tech — restrained layout, generous whitespace, precise type, cool but clean; never decorative.
- **Comfort upgrade** — Fly.io shared-cpu-1x at ≈ $2/month when the free tier pinches (no cold starts, no hour accounting, no CORS).
- **Superseded** — the earlier "stateful instance behind Cloudflare" shape (per-room broadcasters, held long-polls) and the developer's own machine behind a `cloudflared` tunnel. A self-hosted LAN server may still use the server-held wait as an optimization; the hosted product does not.

## Implementation queue

The direction is settled above; this is the executable order. Each step ends with its own conventional commit (`feat:`/`docs:`/`web:`, body ending `Co-Authored-By: Claude Code <noreply@anthropic.com>`), and `make check` green before the next step starts.

1. **Client-side watch polling** (`internal/cli`). `eventWatch`'s remote branch stops using the server-held `wait`: poll with `Read` + `Sleep(interval)`, exactly like the local branch, so hosted serving is not required to hold requests. Watch's command surface, skill, and demo semantics must not change — hosted-demo still proves the handoff matrix, now carried by polls. `transport`'s `wait`/Broadcaster stays untouched (optional LAN optimization).
2. **Deployment files**. `Dockerfile` (multi-stage, `CGO_ENABLED=0` static build) + `render.yaml` (free tier web service, start command `tearoom hosted serve --database "$DATABASE_URL"`, env `TEAROOM_HOSTED_ADMIN`), plus a runbook at `docs/operations/deploy.md` linked from the operations index: Neon project + DSN, `tearoom hosted init-db`, the Render blueprint, TLS/domain notes, and the free-tier constraints recorded in the deployment shapes above (750 h math, cold start, 5 GB egress).
3. **Management UI** (Phase 2). A React + Vite static SPA on Cloudflare Pages calling `/api/hosted/*` with a Bearer token: rooms, memberships, credentials, room history. Deliberately thin — no framework beyond React + Vite, no SSR. Visual direction recorded above: Vercel/Claude-style minimal tech — restrained layout, generous whitespace, precise type, cool but clean.

## Considered: the all-Cloudflare shape

The free tiers are real (2026): Workers 100K requests/day (10 ms CPU, 1 MB body), D1 5 GB / 5M rows read / 100K rows written per day, R2 10 GB, Pages unlimited bandwidth. The watch-semantics objection is gone — client polling works against any HTTP surface. What still holds:

- **The Go server does not run on Workers.** It is a request-lifecycle runtime for JS/TS; `transport`, `relational`, `control`, and `hosted` would be rewritten in another language, and the verification chain rebuilt with them (unit batteries plus the nine demo suites in `make check`). The Phase 1 property that one schema and one transaction contract run over SQLite locally and PostgreSQL hosted would split into two implementations with drift risk.
- **The database access is not portable.** D1 is SQLite behind a worker-binding API, not a DSN GORM can open; Neon would need its serverless driver and a re-verification of the append transaction over HTTP.
- **The request budget is small.** 100K requests/day is 2–3 agents polling every 5 s; 10 ms CPU per request sits at the edge of an append transaction.

What Cloudflare contributes today, free and conflict-free: Pages for the management UI, plus DNS, TLS, CDN, and R2 for event exports. Reconsider the rewrite if the product proves itself and the server surface stays a thin CRUD — the migration list is small then, but it is a rewrite, not a configuration change.

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
| Watch's wait | client polling (CLI, as local mode already does) | coordination tolerates seconds of latency; keeps the hosted server stateless |
| Hosted service home | Render free tier + Neon | 750 instance hours ≥ the longest month's 744; polling keeps it awake; Neon holds the data |
| Management UI | static React/Vite SPA on Cloudflare Pages | the only free static host with unlimited bandwidth and no commercial-use ban |
