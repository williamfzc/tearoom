---
type: architecture
title: Implementation status
description: What the code ships today and what remains open — the single implementation-gap source.
tags: [architecture, status]
timestamp: 2026-09-10
---

# Implementation status

This page records what the shipped code implements against the target in
[../product.md](../product.md). History and rationale live in
[../log.md](../log.md); package ownership and dependency direction in
[layers.md](layers.md); the storage boundary in
[persistence.md](persistence.md); executable proof in
[../evaluations.md](../evaluations.md).

## Shipped boundary

- `internal/core/core.go` defines `Room`, `Actor`, `Event` (`seq`, `actor`,
  `at`, optional `to`, `content`), and the five-method `Store` seam; the
  conformance battery in `contract.go` runs against every Store
  implementation.
- `internal/store` owns the `.tearoom` layout over SQLite: explicit admission,
  dense append, caller-owned reads, per-request idempotency records.
- `internal/relational` implements the same contract on SQLite (format-2
  gate); a PostgreSQL suite exists behind `TEAROOM_TEST_PG` and is pending
  validation.
- `internal/control` owns credential policy: issue for admitted actors,
  bearer verification, expiry, and immediate revocation; tokens are stored
  only as sha-256 hashes.
- `internal/transport` serves the HTTP contract: `X-Tearoom-Actor`, Event
  arrays, unified error envelope; the exposed profile keeps reads open and
  gates writes on bearer credentials.
- `internal/hosted` serves many rooms of one deployment database behind
  `/r/<id>` URLs, with owner-only room creation and migrations on start.
- `internal/cli` exposes `room init` (local or `--server`), `room join`,
  `room credential issue|revoke`, `send`, `read`, `skill` (the
  binary-carried agent guide), and `server start` in local and hosted
  profiles; it owns no session state. The command contract is
  [reference/cli.md](../reference/cli.md).
- `scripts/target-room.sh` and `scripts/target-replay.sh` prove the P0
  claims and run in `make check`.

## Open work

| Item | State |
|---|---|
| P1 single cross-product continuation story (S1) | Foundation evaluations E1/E2 ship; the full four-step evaluation E3 is planned |
| `auth login` | Reserved issuance ceremony (adjudicated 2026-09-10); the operator-issued owner token ships instead |
| PostgreSQL Store validation | Validated 2026-09-10: the contract battery and credential control pass against PostgreSQL 16.15 (gated; rerun with `TEAROOM_TEST_PG`) |

Delivery to an absent Agent is out of product scope (adjudicated 2026-09-09);
see [../product.md](../product.md) and
[lessons/rival-falsification.md](../lessons/rival-falsification.md).

## Clean-break policy

Releases carry no compatibility layer: no old command aliases, no dual Event
schemas, no old HTTP shapes, no deprecated Store paths. Rollback is an
operator action on the Git revision and, where storage formats changed, its
pre-cutover data snapshot — never an in-product compatibility mode. The
completed P0 cutover and its deletion manifest are recorded in
[../log.md](../log.md).

`make check` (unit, race, E1/E2 target evaluations, evaluation registry,
line budget, docs bundle) is the completion proof for every change.
