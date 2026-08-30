---
type: architecture
title: Hosted Tearoom plan
description: The deferred hosted control plane design around the Room/Event kernel.
tags: [architecture, hosted, saas, plan, roadmap, deferred]
timestamp: 2026-08-30
---

# Hosted Tearoom plan

> **Status: deferred.** The hosted implementation was deleted at the P0 clean
> break (2026-08-30); this document is the product design for a future hosted
> phase. Nothing here is shipped code. The P0 release is local Tearoom:
> explicit membership under one OS user's trust domain, one Room, and the
> loopback server. A future hosted phase is secured by a **user login
> identity**, not by Actor-as-secret.

[hosted-identity.md](../domain/hosted-identity.md) defines the intended human
control plane around the Room/Actor/Event kernel. This document records the
design so a future phase can start from it; it does not describe current
implementation state (see [status.md](status.md)).

## Stable hosted boundary

The control plane remains outside coordination. Accounts, human sessions,
organizations, credentials, and billing authorize access to a Room; they do
not become Room or Event fields. The coordination Store remains responsible
for Room and Actor identity, membership, ordered immutable history, and
append/read semantics.

The hosted contract is:

- membership is real admission and the Room visibility boundary;
- every admitted member can read the complete Room history;
- `to` is public addressing or attention metadata, not an Event-level secrecy
  filter and not a process-start command;
- reads take an explicit boundary and do not silently persist an Actor cursor;
- an optional activation service may observe addressed Events, but its runtime
  bindings, delivery ledger, leases, and launcher remain above the Store.

## Identity and admission

The future hosted phase is secured by a user login identity: a human signs in
with GitHub (or another identity provider), owns Rooms, and issues scoped,
revocable credentials to stable Actors. The human login is never handed to an
agent, and an Actor credential is never a secret that by itself admits the
Actor — membership is admitted by the Room owner. Actor-as-secret is rejected
as a security model.

The intended path:

```text
addressed Event -> activation policy -> durable claim/lease
                -> configured launcher -> ephemeral Agent Runtime
                -> Runtime reads Room and appends a contribution
```

The trigger carries a Room/Event reference rather than a copied transcript.
The durable activation ledger is separate from Room history and Actor read
state. It can provide at-least-once attempts, bounded leases, retry, budget,
and duplicate coalescing; it cannot claim that an Agent understood or
completed an Event. Addressing an Actor never grants authority to spend
resources or start a process without policy.

The first proof should be one local launcher: a member with no running process
receives an addressed Event, resumes after daemon restart, reads the Room, and
uses request-level append idempotency to avoid duplicate response Events. The
complete technical contract is [activation.md](activation.md).

## Deployment shape

The prior hosted shape was deliberately stateless:

```text
Cloudflare Pages — optional management UI
        |
Render — `tearoom hosted serve` (Go service)
        |
Neon — control and coordination PostgreSQL
```

A future phase may move to another host without changing the coordination
contract. TLS termination, rate limiting, audit records, and secret handling
remain deployment obligations.

## Roadmap

1. **Identity:** implement the GitHub OAuth exchange and human sessions;
   preserve Room/Actor identity and credential binding.
2. **Admission:** implement hosted membership as explicit, owner-managed
   admission before claiming Room-wide visibility as a security boundary.
3. **Activation evidence:** implement one local durable activation ledger and
   launcher before adding hosted wake services or a general scheduler.
4. **Operations:** add organizations, credential rotation, rate limits,
   retention, and compaction only after their data and authorization contracts
   are independently specified.

## Decisions recorded

| Decision | Choice | Status |
|---|---|---|
| Tenant key | `room_id` on coordination rows | Deferred design |
| Control schema | `ctl_` tables with a separate migration chain | Deferred design |
| Credential storage | Hash opaque, bound, revocable tokens | Deferred design |
| Identity | Human login identity, not Actor-as-secret | Adopted for the future phase |
| Capability model | `read` / `append` / `manage` | Deferred design; target may refine admission |
| Event visibility | Membership-wide; `to` is public addressing | Shipped in the P0 kernel |
| Read progress | Explicit boundary; no saved Actor cursor | Shipped in the P0 kernel |
| Activation | Optional application service, never kernel behavior | Not shipped |

## Related

- [model-discussion.md](model-discussion.md)
- [layers.md](layers.md)
- [persistence.md](persistence.md)
- [../domain/hosted-identity.md](../domain/hosted-identity.md)
- [../domain/security.md](../domain/security.md)
- [status.md](status.md)
