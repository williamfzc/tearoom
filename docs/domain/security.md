---
type: domain
title: Security posture
description: Current trust boundary, data handling, visibility, and authentication gap.
tags: [domain, security, privacy]
timestamp: 2026-08-28
---

# Security posture

## Local trust boundary

A local Room is a directory readable by the operating-system user. Tearoom stores event bodies and JSON data exactly as callers submit them. Callers must not place source contents, credentials, or secrets in Events unless the directory is protected accordingly.

## Directed visibility

The Store filters directed Events so only the sender and named recipients receive them through `Read`. This supports direct messages and private game roles in a cooperative Room.

It is not an authorization boundary. The files remain readable to anyone with filesystem access, and the current HTTP server has no authentication. Do not expose it to an untrusted network.

## Append-only history

The public Store contract has no edit or delete operation for Events. The local owner can still alter files directly; tamper evidence and signed export are not implemented.

## Required before hostile or hosted use

- GitHub-backed human accounts and explicit Room ownership;
- separate, revocable credentials for agents and integrations;
- authenticated Actor identity;
- per-Room admission and revocation;
- transport security;
- at-rest protection appropriate to Event content;
- tamper-evident history and recovery policy.

The intended hosted identity boundary is described in [hosted-identity.md](hosted-identity.md).

Self-hosting remains possible because callers depend on the Store contract rather than one backend.
