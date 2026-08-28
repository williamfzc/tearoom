---
type: product
title: Product definition
description: What Tearoom is, who it is for, what it refuses, and the tests that decide whether a proposal belongs. Authoritative over all other documents.
tags: [product, scope, decision, authority]
timestamp: 2026-08-28
---

# Product definition

This document decides scope. Where any other document disagrees with it, this one wins
and the other is wrong. It exists because the product was previously described in three
places — `README.md`, `domain/problem.md`, `architecture/status.md` — which drifted apart
until four documents claimed a ten-command surface that had grown to fourteen.

## One sentence

**Tearoom is the coordination layer several AI agents share when they work in one
place: it decides who may act, and it controls what an agent spends to learn what
changed.**

Both halves are load-bearing. The first is built. The second is not, and until it is
this product is incomplete rather than minimal — see
[agent experience](domain/agent-experience.md).

## Who it is for

In priority order. A feature that serves a later party at the expense of an earlier one
is rejected.

1. **An agent acting in the workspace.** The primary user. It reads via a CLI, has no
   screen, and pays tokens for every observation.
2. **The person responsible for the outcome.** Needs to see who is stuck and what is
   waiting on them, and to answer when asked. Does not want a chat client.
3. **The team that has to review afterwards.** Needs to reconstruct what happened and why
   from a record, not from scrollback.

Nobody else. In particular Tearoom is not for humans to talk to each other.

## What is true of it

These are the invariants. Breaking one changes what the product is, so each requires an
explicit decision recorded in [the log](log.md).

- **A small fixed vocabulary.** Workspace, agent, thread, lease, gate. A proposal needing
  a sixth concept must first show the five cannot express it.
- **A converged command surface.** Every command maps to a decision an agent makes; no
  command exists to save typing, and no two commands state the same fact. The current
  fourteen violate this, and [convergence](architecture/convergence.md) is the correction.
- **Rules live under the store.** Anything that must be true of the workspace is enforced
  beneath the `Store` interface, not in a client. This was learned the hard way once
  already: [invariants belong under the store](lessons/invariants-belong-in-the-store.md).
- **Code never leaves the machine.** The store holds paths, summaries and decisions.
  Never file contents.
- **Failure is actionable, and never silent.** A refusal names the obstacle *and* the
  remedy. An agent that believes it holds a lease it does not hold is worse than a crash.
- **Facts, not orchestration.** No DAGs, no branching, no scheduler. Tearoom supplies
  facts and primitives; agents decide what to do next.

## What it refuses

Stated as firmly as the goals, because scope creep here dissolves the product.

- Not an agent and not a model router. Coordination, not intelligence.
- Not a code host or sandbox.
- Not a workflow engine.
- Not human-to-human chat.
- Not a general IM platform. **But see the correction below — this refusal has been
  applied too broadly.**

### The correction

The earlier framing said agents need *fewer* things than a chat system, and treated
mentions, unread state and notifications as human decoration to be stripped out. That was
wrong, and it is the one place where the original pitch has to change.

An agent does need a different set of things — but the set is not smaller. It needs
**less** presence, formatting and prettiness, and **more** attention control than any IM
platform offers a bot: a notice tier separate from message bodies, the ability to mute a
channel it does not care about, intent that survives the end of a turn, and an identity
that survives the end of a task. Mute matters more than rich text.

So the refusal stands as *not a chat product for humans*. It does not extend to the
attention machinery, which is required. Detail and sources in
[agent experience](domain/agent-experience.md).

## The failures it exists to prevent

Four now, in increasing order of cost. The first three were the original motivation; the
fourth was found by [studying consensus systems](lessons/borrowed-from-consensus.md) and
confirmed against our own binary.

| Failure | What it looks like | Answered by |
|---|---|---|
| **Broken handoff** | No way to report progress or wait on a precondition, so a human relays context by hand and work stops when they leave | threads, `wait` |
| **Silent overwrite** | Two agents edit one file, the later write wins, nothing reports it | leases |
| **No oversight trail** | No way to know what an agent did or why, so review is impossible and the work cannot be delegated | the ledger, the attention-first board |
| **Stale action** | An agent reasons from a snapshot, the world moves, and it acts on a world that no longer exists — writing a duplicate, an obsolete claim, or a confident falsehood | **nothing yet** |

The fourth is not hypothetical here. Observed: an agent wrote *"nobody has this, I'll take
it"* into a thread already owned by another agent, and the workspace accepted it with exit
code 0.

## Tests a proposal must pass

Applied in order. First failure rejects.

1. **Does it prevent one of the four failures?** If not, it is somebody else's feature.
2. **Can the five concepts express it?** If it needs a sixth, it must justify the sixth
   first.
3. **Does it correspond to a decision an agent makes?** Views, formats and conveniences
   are flags at most.
4. **Can the rule live under the store?** If it can only work by trusting the caller to
   remember it, it is not finished.
5. **Does it cost the agent tokens it did not choose to spend?** Anything that enlarges
   what an agent must read by default needs a reason.

## How this is decided to be working

Not "the tests pass" — that is a build property. The product works when:

- Two agents complete a real task in one repository with **no human relay** and **no
  overwritten work**.
- A responsible human answers "who is stuck, and what needs me" from the board in
  **under ten seconds**.
- An action taken on a stale view is **refused rather than recorded**.
- An agent in a busy workspace can stay on its own task without its context filling with
  other agents' traffic.

The first two are met at M0. The third and fourth are not, which is the honest current
state of the product.

## Where the rest of the documentation stands

| Document | Role relative to this one |
|---|---|
| [domain/](domain/index.md) | The vocabulary and rules this definition assumes |
| [architecture/](architecture/index.md) | How the code is arranged to keep these promises |
| [reference/](reference/index.md) | What the surface currently *is*, including where it is not yet converged |
| [lessons/](lessons/index.md) | Why some of these rules exist, with the evidence |
| [log.md](log.md) | Every decision that changed this document |

`README.md` is the entry point for a newcomer and must not restate this file. If the two
disagree, `README.md` is wrong.
