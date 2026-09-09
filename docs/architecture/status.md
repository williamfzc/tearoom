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
- `internal/transport` serves the HTTP contract: `X-Tearoom-Actor`, Event
  arrays, unified error envelope.
- `internal/cli` exposes `room init|join`, `send`, `read`, `skill` (the
  binary-carried agent guide), and `server start` (loopback only); it owns no
  session state. The command contract is [reference/cli.md](../reference/cli.md).
- `scripts/target-room.sh` and `scripts/target-replay.sh` prove the P0
  claims and run in `make check`.

## Open work

| Item | State |
|---|---|
| P1 single cross-product continuation story (S1) | Foundation evaluations E1/E2 ship; the full four-step evaluation E3 is planned |
| One service across local and remote profiles | Core Store and HTTP contracts are shared; the current entry point still hard-codes loopback trust and local Room creation |
| Deployment database profiles | Local SQLite ships and remote PostgreSQL is the target; PostgreSQL is not wired into the service entry point |
| Shared authorization path | Target `auth login` and `room credential issue` / `revoke` behavior is defined in the CLI contract; not implemented |
| PostgreSQL Store validation | Gated suite ready; runs when `TEAROOM_TEST_PG` points at a database |
| Remote form of the same read/write loop | Evaluation E7 is planned; no separate remote collaboration command is allowed |

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
