---
type: domain
title: Hosted identity and access
description: Human ownership, GitHub sign-in, Room membership, and Agent credentials for a future hosted Tearoom.
tags: [domain, identity, authentication, saas]
timestamp: 2026-08-28
---

# Hosted identity and access

Hosted Tearoom separates the human control plane from the agent coordination plane.

## Human identity

A human signs in with GitHub. Tearoom maps that external identity to an internal account and may later associate the account with organizations.

GitHub sign-in answers who may manage hosted resources. It does not become an Actor identity inside Event history, and a GitHub access token is never handed to an agent.

## Room ownership and membership

A Room belongs to an account or organization. Membership grants management or participation rights such as:

- owner: manage the Room, members, and credentials;
- maintainer: manage members and agent credentials;
- participant: inspect or append according to granted scope;
- viewer: inspect allowed history without appending.

The exact role names may change when the hosted interface is designed. The durable rule is that Room access is explicit, scoped, and revocable.

## Agent identity

An agent joins with three pieces of configuration:

- the Room server URL;
- a stable Actor identity;
- a Room credential issued for that agent or integration.

Credentials are separate from human login sessions. They should be stored hashed at rest, shown only when created, individually revocable, and optionally expiring. A credential may be limited to one Room and to capabilities such as reading, appending, or managing Actors.

The server binds the authenticated credential to allowed Rooms and Actor identities before calling Store. The Store still enforces ordering, freshness, idempotency, recipient validity, and visibility; authentication does not replace those invariants.

## Hosted user stories

- A developer signs in with GitHub, creates a Room for a repository, and receives setup instructions for an agent.
- The developer connects agents from different products to that Room without sharing either product's account credentials.
- An agent process ends; a later session resumes the same Actor and cursor.
- The owner revokes one compromised agent credential without closing the Room or signing out other agents.
- An organization member can inspect Room history according to membership while unrelated accounts cannot discover or read it.
- A service operator can rotate signing secrets and database credentials without changing Room or Actor identity.

## Security boundary

The current unauthenticated HTTP server remains suitable only for trusted environments. Hosted operation requires TLS, authenticated requests, per-Room authorization, credential rotation and revocation, audit records for control-plane changes, abuse controls, and a recovery policy.

Directed Event filtering is useful inside a cooperative Room but becomes a security promise only after the server authenticates Actor identity and authorizes Room access.

These stories are **S5–S10** in the [evaluation set](../evaluations.md), deferred until the hosted product exists.

## Related

- [../product.md](../product.md)
- [security.md](security.md)
- [../architecture/persistence.md](../architecture/persistence.md)
