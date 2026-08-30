---
type: domain
title: Security posture
description: Current trust boundary, target membership visibility, data handling, and authentication gap.
tags: [domain, security, privacy]
timestamp: 2026-08-30
---

# Security posture

## Local trust boundary

A local Room is a directory readable by the operating-system user. Tearoom stores event bodies and JSON data exactly as callers submit them. Callers must not place source contents, credentials, or secrets in Events unless the directory is protected accordingly.

## Room visibility and addressing

The target model makes Room membership the visibility boundary: every member
can read the complete Event history. `to` names addressees for attention or
routing, but remains visible to every member and does not provide secrecy,
permission, or process activation. Private messages or game roles require a
separate Room with separate admission.

The current Store still filters directed Events and the local path still
auto-registers Actors. Those are compatibility behaviours and do not satisfy
the target membership model. The files remain readable to anyone with
filesystem access, and any unauthenticated HTTP path must not be exposed to an
untrusted network.

## Append-only history

The public Store contract has no edit or delete operation for Events. The local owner can still alter files directly; tamper evidence and signed export are not implemented.

## Optional activation boundary

An Event addressed to an Agent does not itself grant execution authority or
request a process wake. If offline activation is introduced, it must live in an
application/control-plane layer with operator-owned runtime bindings, sender
and budget policy, explicit credentials, a durable delivery ledger, bounded
leases, and duplicate-safe retries. Delivery state must not be presented as
Actor read, comprehension, or task completion. No such activation service is
currently implemented.

## Hosted boundary

Phase 1 of the hosted server ([hosted-plan.md](../architecture/hosted-plan.md))
implements the agent-facing half of the boundary. The legacy hosted demo
exercises these implementation facts, but no target evaluation has shipped:

- separate, revocable credentials for agents and integrations — **implemented**: opaque `tr_` tokens, sha256 at rest, per-credential expiry, individual revocation;
- authenticated Actor identity — **implemented**: every event-API request must declare the credential's own Actor, so a credential is an identity, not a room-level key;
- per-Room admission — **implemented**: credentials are bound to one Room, and the server checks the binding before the coordination layer sees the request. Wrong-room tokens answer 401 so a token cannot probe room existence;
- explicit Room ownership — **implemented for the dev admin**: `create-room` binds an owner membership; the GitHub exchange will bind accounts;
- GitHub-backed human accounts — **pending**: dev-mode admin token until Phase 2.

## Required before hostile or hosted use

- GitHub-backed human accounts and explicit Room ownership (pending; see above);
- transport security — **pending**: the hosted server assumes TLS termination in front of it;
- at-rest protection appropriate to Event content;
- rate limiting and abuse controls;
- audit records for control-plane changes;
- tamper-evident history and recovery policy.

The intended hosted identity boundary is described in [hosted-identity.md](hosted-identity.md), and the phase plan in [hosted-plan.md](../architecture/hosted-plan.md).

Self-hosting remains possible because callers depend on the Store contract rather than one backend.
