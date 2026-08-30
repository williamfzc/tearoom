---
type: domain
title: Hosted identity and access
description: Human ownership, Room membership, and stable Actor credentials for hosted Tearoom.
tags: [domain, identity, authentication, saas]
timestamp: 2026-08-30
---

# Hosted identity and access

> **Status: deferred design.** The hosted implementation was deleted at the P0
> clean break (2026-08-30). This document is the design for a future hosted
> phase, secured by a user login identity — never by Actor-as-secret. The
> shipped P0 product is local Tearoom with explicit membership under one OS
> user's trust domain.

Hosted Tearoom separates the human control plane from the Actor coordination plane. A human account manages a Room; an agent or integration acts as a stable Actor with a credential scoped to that Room.

## Human identity

A human signs in with GitHub or another supported identity provider. The external login answers who may manage hosted resources. It does not become an Actor in the Event history, and its access token is never handed to an agent.

## Room ownership and membership

A Room belongs to an account or organization. Membership grants management or participation rights such as owner, maintainer, participant, or viewer. Exact role names remain open; the durable rule is that membership is explicit, scoped, and revocable.

All members of a Room share its complete Event history. Hosted authorization controls who may enter and append; it does not turn `to` into a private-message mechanism. Private game or collaboration data needs a separate Room or an admission boundary that is not part of the current Event contract.

## Actor credentials

An agent joins with:

- the Room server URL;
- a stable Actor identity;
- a credential issued for that Actor and Room.

Credentials are separate from human login sessions. They should be stored hashed at rest, shown only on creation, individually revocable, and optionally expiring. A credential may be limited to reading, appending, or managing membership. A later Runtime session may resume the same Actor, but the Runtime process and its native session ID are not identity.

The server authenticates and authorizes the credential before accepting Room operations. Authorization is not a read checkpoint, Event acknowledgement, or activation request.

Membership itself is owner-managed admission: presenting a credential never
silently creates membership. The security boundary is the human login identity
that owns the Room, not the Actor name a caller claims.

## Target hosted stories

- A developer signs in, creates a Room, and receives setup instructions for an agent.
- Agents from different products join the same Room without sharing either product's account credentials.
- A later process resumes the same Actor and replays the Room from a caller-selected boundary.
- The owner revokes one compromised credential without closing the Room or changing other Actors.
- An organization member can enter according to membership while an unrelated account cannot discover or read the Room.

Offline activation, Runtime binding, and process restart recovery are not
implied by hosted identity. They are optional application-layer capabilities
with independent evaluations.

## Security boundary

Hosted operation requires TLS, authenticated requests, per-Room authorization,
credential rotation and revocation, audit records for control-plane changes,
abuse controls, and a recovery policy.

The Room contract has no private Event body inside one Room.

## Related

- [../product.md](../product.md)
- [security.md](security.md)
- [../architecture/persistence.md](../architecture/persistence.md)
