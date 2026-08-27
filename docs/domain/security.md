---
type: domain
title: Security posture
description: What Tearoom stores, what it refuses to store, and the properties revocation and the ledger must preserve.
tags: [domain, security, privacy, audit]
timestamp: 2026-08-27
---

# Security posture

Four commitments. Each is a constraint on what the product may become, not just a
description of today.

## Code never leaves the machine

The store holds paths, diff summaries and decision text. It does not hold file contents.

This is what makes the tool adoptable inside an organisation that would never allow its
source into a third-party service — and it is why the hosted store in the roadmap is
still viable. Breaking this would change what the product is.

## One token per agent, individually revocable

Revocation immediately invalidates that agent's leases. An identity that can be revoked
but leaves its holds behind would strand every resource it was touching.

This connects to identity coming from the token rather than the process — see
[concepts.md](concepts.md).

## The ledger is append-only, with no delete API

Export must be verifiable offline. An audit trail that the audited party can edit is not
an audit trail, and the ability to review a run after the fact is what makes delegating
to an agent defensible.

See [../reference/storage-layout.md](../reference/storage-layout.md).

## Self-hosting must stay possible

M0 does not need it. The architecture must not foreclose it — which is part of why the
`Store` contract exists and why nothing above it assumes a particular backend.

## Related

- [concepts.md](concepts.md) — agent identity and lease semantics
- [boundaries.md](boundaries.md) — behaviour when an agent dies holding a lease
- [../architecture/status.md](../architecture/status.md) — where the hosted store sits
