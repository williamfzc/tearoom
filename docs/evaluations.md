---
type: evaluations
title: Evaluation set
description: User stories, the evaluations that prove them, and the bidirectional map between the two.
tags: [evaluations, stories, testing, registry]
timestamp: 2026-08-29
---

# Evaluation set

A user story is proven only when an evaluation in `make check` exercises it. This document is the bidirectional map: every story names its proof, every evaluation names its story. [scripts/check-evals.sh](../scripts/check-evals.sh) verifies the map stays consistent, because a story marked proven without a shipped evaluation — or a suite dropped from `make check` while its story still claims proof — is the same silent rot `check-docs` catches for links.

Story statuses: `proven` (a shipped evaluation proves it), `open` (in scope, not yet proven), `deferred` (deliberately out of the current product). Evaluation statuses: `shipped`, `planned`.

## Stories

| ID | Story | Status | Defined in |
|---|---|---|---|
| S1 | Core: agents from any product exchange ordered events across sessions without a human relaying context | proven | [product.md](product.md) |
| S2 | Harness: planner, generator, and evaluator run one plan/generate/evaluate loop | proven | [domain/harness-workflow.md](domain/harness-workflow.md) |
| S3 | Game: Werewolf with private role events runs as a protocol over Events | proven | [product.md](product.md) |
| S4 | One Room across several real worktrees keeps cursor-only reads sufficient | proven | [architecture/status.md](architecture/status.md) |
| S5 | A developer signs in with GitHub and creates a Room for a repository | open | [domain/hosted-identity.md](domain/hosted-identity.md) |
| S6 | The developer connects agents from different products without sharing either product's credentials | proven | [domain/hosted-identity.md](domain/hosted-identity.md) |
| S7 | An agent process ends; a later session resumes the same Actor and cursor | proven | [domain/hosted-identity.md](domain/hosted-identity.md) |
| S8 | The owner revokes one compromised agent credential without closing the Room | proven | [domain/hosted-identity.md](domain/hosted-identity.md) |
| S9 | An organization member inspects Room history per membership; unrelated accounts cannot | deferred | [domain/hosted-identity.md](domain/hosted-identity.md) |
| S10 | A service operator rotates signing secrets without changing Room or Actor identity | deferred | [domain/hosted-identity.md](domain/hosted-identity.md) |

## Evaluations

| ID | Evaluation | Target | Script | Status |
|---|---|---|---|---|
| E1 | Unit, HTTP, and cross-process concurrency tests | `make test` | — | shipped |
| E2 | File smoke: shipped CLI, exit classes, retries, directed Events | `make smoke` | [scripts/smoke.sh](../scripts/smoke.sh) | shipped |
| E3 | SQLite smoke: the same invariants over the relational Store | `make smoke-sqlite` | [scripts/smoke-sqlite.sh](../scripts/smoke-sqlite.sh) | shipped |
| E4 | Session smoke: same-product and cross-product handoff, server-held watch | `make session-smoke` | [scripts/session-smoke.sh](../scripts/session-smoke.sh) | shipped |
| E5 | Harness demo: plan/generate/evaluate loop end to end | `make harness-demo` | [scripts/harness-demo.sh](../scripts/harness-demo.sh) | shipped |
| E6 | Game demo: one Werewolf round over directed role Events | `make game-demo` | [scripts/game-demo.sh](../scripts/game-demo.sh) | shipped |
| E7 | Worktree demo: two real git worktrees sharing one Room | `make worktree-demo` | [scripts/worktree-demo.sh](../scripts/worktree-demo.sh) | shipped |
| E8 | Hosted demo: credentials, actor binding, session resume, revocation | `make hosted-demo` | [scripts/hosted-demo.sh](../scripts/hosted-demo.sh) | shipped |

## Story → evaluation

Shipped proofs only; an `open` or `deferred` story has no row here.

| Story | Evaluations |
|---|---|
| S1 | E1, E2, E3, E4 |
| S2 | E5 |
| S3 | E6 |
| S4 | E7 |
| S6 | E8 |
| S7 | E8 |
| S8 | E8 |

## Evaluation → story

The inverse map. `check-evals` requires both directions to agree.

| Evaluation | Stories |
|---|---|
| E1 | S1 |
| E2 | S1 |
| E3 | S1 |
| E4 | S1 |
| E5 | S2 |
| E6 | S3 |
| E7 | S4 |
| E8 | S6, S7, S8 |

## Pending evidence, not stories

Engineering work the product needs that no user story names:

| Item | Evidence |
|---|---|
| PostgreSQL validation | gated suite shipped (`make pg-smoke` + the `TEAROOM_TEST_PG` unit battery); needs a running PostgreSQL to execute |
| GitHub OAuth | S5's sign-in is a dev-mode admin stub; the OAuth exchange is Phase 2 in [hosted-plan](architecture/hosted-plan.md) |
| Irrelevant-Event volume | measure volume before adding subscriptions, mute, or indexing |
