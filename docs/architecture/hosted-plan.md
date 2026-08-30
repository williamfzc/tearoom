---
type: architecture
title: Hosted Tearoom plan
description: The hosted control plane, its current implementation boundary, and the target Room/Event contract.
tags: [architecture, hosted, saas, plan, roadmap]
timestamp: 2026-08-30
---

# Hosted Tearoom plan

[hosted-identity.md](../domain/hosted-identity.md) defines the intended human
control plane around the Room/Actor/Event kernel. This document separates what
Phase 1 currently implements from the minimum Room/Event model in
[model-discussion.md](model-discussion.md). The distinction matters: hosted
authentication is shipped, but the Room migration and offline Agent activation
are not.

## Stable hosted boundary

The control plane remains outside coordination. Accounts, human sessions,
organizations, credentials, and billing authorize access to a Room; they do
not become Room or Event fields. The coordination Store remains responsible for
Room and Actor identity, membership, ordered immutable history, and append/read
semantics.

The target hosted contract is:

- membership is real admission and the Room visibility boundary;
- every admitted member can read the complete Room history;
- `to` is public addressing or attention metadata, not an Event-level secrecy
  filter and not a process-start command;
- reads take an explicit boundary and do not silently persist an Actor cursor;
- an optional activation service may observe addressed Events, but its runtime
  bindings, delivery ledger, leases, and launcher remain above the Store.

The hosted service is still pre-cutover. Its exact implementation gap belongs
in [status.md](status.md). The clean break updates the authenticated API, both
Store implementations, CLI, tests, and docs together; it keeps no legacy
request path or data reader.

## Current Phase 1 architecture — shipped 2026-08-29

**One shared database, `room_id` as tenant key.** Relational models carry
`room_id` on every row. `relational.New(db, roomID)` creates a per-Room Store
over one shared `*gorm.DB`; the Room-head lock serializes writers per Room.

**Separate control schema.** `internal/control` owns `ctl_accounts`,
`ctl_memberships`, `ctl_credentials`, and `ctl_schema_versions`. Control rows
do not mix with coordination rows, and both migration chains are idempotent on
open.

**Bound credentials.** A credential is a one-time shown `tr_` token containing
192 random bits, stored only as a SHA-256 hash, bound to `(room, actor,
capabilities, expiry)`, and individually revocable. Capabilities are `read`,
`append`, and `manage`. The server requires every request to declare the exact
Actor bound to the credential.

**URL and isolation.** Management uses `/api/hosted/*`; Room events use
`/r/<room-id>/api/*`. Middleware resolves the Bearer credential before
delegating to that Room's Store. A wrong-room credential returns 401, so a
token cannot probe whether another Room exists.

**Stateless serving.** Hosted `event watch` uses client polling. The server-held
wait/Broadcaster remains an optional same-process or LAN optimization; hosted
correctness does not depend on it.

The shipped executable slice includes hosted administration, credential issue,
revocation, authenticated event access, and the negative authorization matrix.
`scripts/hosted-demo.sh` is legacy implementation evidence recorded in
[evaluations.md](../evaluations.md), not target-model proof. PostgreSQL remains
a gated validation path until a DSN is supplied.

## Optional activation boundary — not shipped

Offline Agent activation is an application capability, not a hosted Store
feature. The intended path is:

```text
addressed Event -> activation policy -> durable claim/lease
                -> configured launcher -> ephemeral Agent Runtime
                -> Runtime reads Room and appends a contribution
```

The trigger carries a Room/Event reference rather than a copied transcript.
The durable activation ledger is separate from Room history and Actor read
state. It can provide at-least-once attempts, bounded leases, retry, budget,
and duplicate coalescing; it cannot claim that an Agent understood or completed
an Event. Addressing an Actor never grants authority to spend resources or
start a process without policy.

The first proof should be one local launcher: a member with no running process
receives an addressed Event, resumes after daemon restart, reads the Room, and
uses request-level append idempotency to avoid duplicate response Events. Until
that exists, the hosted service must not advertise offline wakeup. The complete
technical contract is [activation.md](activation.md).

## Deployment shape

The current hosted shape is deliberately stateless:

```text
Cloudflare Pages — optional management UI
        |
Render — `tearoom hosted serve` (Go service)
        |
Neon — control and coordination PostgreSQL
```

The service may later move to another host without changing the coordination
contract. TLS termination, rate limiting, audit records, and secret handling
remain deployment obligations; the current hosted server assumes TLS in front
of it and does not yet provide rate limiting.

## Roadmap

1. **Identity:** replace the dev admin token with the GitHub OAuth exchange and
   human sessions; preserve Room/Actor identity and credential binding.
2. **Admission:** make hosted membership explicit before claiming Room-wide
   visibility as a security boundary; remove silent registration from the
   target API.
3. **Room cutover:** replace implicit cursors and directed visibility with
   explicit read boundaries and membership visibility in one matched release;
   reset pre-adoption coordination data instead of adding compatibility code.
4. **Activation evidence:** implement one local durable activation ledger and
   launcher before adding hosted wake services or a general scheduler.
5. **Operations:** add organizations, credential rotation, rate limits,
   retention, and compaction only after their data and authorization contracts
   are independently specified.

## Decisions recorded

| Decision | Choice | Boundary |
|---|---|---|
| Tenant key | `room_id` on coordination rows | Current and target |
| Control schema | `ctl_` tables with a separate migration chain | Current |
| Credential storage | Hash opaque, bound, revocable tokens | Current |
| Capability model | `read` / `append` / `manage` | Current; target may refine admission |
| Room URL | `/r/<id>/api/*` | Current |
| Hosted wait | Client polling | Current hosted behavior |
| Event visibility | Membership-wide in target; directed filtering still in code | Migration required |
| Read progress | Explicit boundary in target; saved Actor cursor still in code | Migration required |
| Activation | Optional application service, never kernel behavior | Not shipped |

## Related

- [model-discussion.md](model-discussion.md)
- [layers.md](layers.md)
- [persistence.md](persistence.md)
- [../domain/hosted-identity.md](../domain/hosted-identity.md)
- [../operations/deploy.md](../operations/deploy.md)
