---
type: domain
title: Security posture
description: Authoritative trust, membership visibility, authentication, and activation boundaries.
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

The files remain readable to anyone with filesystem access, and an
unauthenticated HTTP path must never be exposed to an untrusted network.

## Append-only history

The public Store contract has no edit or delete operation for Events. The local owner can still alter files directly; tamper evidence and signed export are not implemented.

## Optional activation boundary

An Event addressed to an Agent does not itself grant execution authority or
request a process wake. If offline activation is introduced, it must live in an
application/control-plane layer with operator-owned runtime bindings, sender
and budget policy, explicit credentials, a durable delivery ledger, bounded
leases, and duplicate-safe retries. Delivery state must not be presented as
Actor read, comprehension, or task completion.

## Hosted boundary

The hosted boundary requires:

- separate, revocable credentials for Agents and integrations;
- authenticated Actor identity rather than a shared Room key;
- explicit per-Room admission before Store access;
- explicit Room ownership and management authority;
- human account authentication separated from Agent credentials.

Implementation status and deployment limitations belong in
[hosted-plan.md](../architecture/hosted-plan.md) and
[status.md](../architecture/status.md).

## Required before hostile or hosted use

- GitHub-backed human accounts and explicit Room ownership (pending; see above);
- transport security — **pending**: the hosted server assumes TLS termination in front of it;
- at-rest protection appropriate to Event content;
- rate limiting and abuse controls;
- audit records for control-plane changes;
- tamper-evident history and recovery policy.

The intended hosted identity boundary is described in [hosted-identity.md](hosted-identity.md), and the phase plan in [hosted-plan.md](../architecture/hosted-plan.md).

Self-hosting remains possible because callers depend on the Store contract rather than one backend.
