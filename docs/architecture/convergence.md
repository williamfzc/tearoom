---
type: architecture
title: Convergence
description: The standard a command must meet to exist, and the redundancy an audit of the current surface found.
resource: internal/cli/cli.go
tags: [architecture, cli, convergence, decision]
timestamp: 2026-08-28
---

# Convergence

Raft displaced Paxos in practice not by being faster or cheaper in messages, but by
aiming for something else: that an engineer could implement it completely and
correctly. It buys that by **restricting the system's freedom** — the log flows one way,
from leader to follower, with no holes. The cost is a little latency in the worst case.
The purchase is predictable behaviour.[^1]

The user here is an agent rather than an engineer, but the conclusion is the same shape:
a capability that agents use correctly is the only kind worth having. An extra command
is not one more line of documentation. It is one more fork in the road every time the
model decides what to do.

Raft got there two ways, and both transfer directly: **decompose the problem into
independent sub-problems**, and **use one general mechanism for every case rather than a
special one per case**, which removes the cost of reasoning about special cases at all.
After that simplification its core is two RPCs.[^2]

This CLI has fourteen top-level commands. It has not done that exercise.

## The standard

A command earns its place only if all three hold:

1. **It corresponds to a decision the agent actually makes.** Not a view of data, not an
   output preference.
2. **It cannot be expressed by composing existing commands.** If it composes, it should
   not exist.
3. **Removing it breaks a user story.** Making one wordier is not a reason.

Flags are held tighter. **A flag may change how much is shown; if it changes what the
command means, it is a different command.** In the other direction, a command that
exists to save typing does not exist.

## What the audit found

### Three commands stating one fact

Everything `tea threads` and `tea leases` print is already in `tea status`. Same
workspace, three invocations:

```
tea threads →  t1  open  -  task one
tea leases  →  src/**  a  expires 15:25:47
tea status  →  both of the above, plus agent state and what needs a human
```

They answer no decision of their own. They are one fact, sliced three ways. By standard
2 they fold into `status`, with a flag to pick the slice.

### `claim` does not claim the work

The real defect, observed:

```
tea claim "src/**" --ttl 10m --thread t1
tea threads → t1  open  -  task one     ← still open, still unowned
```

The lock is held. The work still shows as **nobody's** on the board. For the board to be
right the agent has to remember a second command, `tea reply t1 --status claimed`.

Forget it and you get a specific bad state: **a resource is held while the board says no
one is responsible.** That is precisely the silent inconsistency this product exists to
surface.

The reference design treats this as one act — whoever claims successfully **becomes the
owner, and subsequent execution and reporting go to the associated thread**, while those
who fail learn there is already an owner and stop.[^3] Claiming a resource and claiming
the work are one intent. Splitting them is not the agent's job to remember.

This is not a convenience complaint. It is
[invariants belong under the store](../lessons/invariants-belong-in-the-store.md)
recurring: a rule that must hold was left to the caller to remember.

### `init` is not an agent's command

`tea init` is run once, by a human, and never again. Sitting among the other fourteen it
makes an agent evaluate "should I init?" on every read of the help text, where the answer
is permanently no.

### `inbox` and `status` blur

Both answer "what needs attention", differing only in scope — mine versus everyone's.
That is one question with two ranges, which is a flag.

## The converged shape

Writes are already irreducible:

| Command | The decision |
|---|---|
| `join` | who I am |
| `post` | there is work |
| `claim` | I am taking it (**and owning the thread**) |
| `reply` | progress, findings |
| `release` | I am done |
| `gate` | a human must decide |
| `wait` | I am waiting for change |

Reads converge to one entry point:

| Command | Notes |
|---|---|
| `status` | the workspace; `--mine` narrows it and replaces `inbox` |
| `context` | one thread, compacted by default |
| `ledger` | the audit trail — different in kind, kept |

`init` leaves the agent surface. `threads` and `leases` fold into `status`. **Fourteen
becomes ten, and no user story dies.**

## The counter-constraint

Convergence is not merging everything. `ledger` also looks like a read, but it answers
**what happened**, where `status` answers **what is**. One is an append-only history, the
other a mutable present; [they are different things](../reference/storage-layout.md), and
merging them costs auditing its own door.

`context` likewise stays out of `status`: it exists to return a **compacted** view to save
tokens, which is a different goal, not a narrower one.

## Related

- [Borrowed from consensus systems](../lessons/borrowed-from-consensus.md) — where these principles come from, and what else is worth taking
- [Concepts](../domain/concepts.md) — the objects these commands act on
- [Layers](layers.md) — which layer a converged rule belongs in

[^1]: Raft 共识算法深度原理剖析 — <https://bytedance.larkoffice.com/docx/AI0ldxD6sopO8Bx9lS1mBU9nyid>
[^2]: Raft 简介 — <https://bytedance.larkoffice.com/wiki/wikcnGFTSkT4ZLYSv8Tuno0JbDb>
[^3]: 也许还真得为 Agent 重新设计一次聊天系统 — <https://bytedance.larkoffice.com/docx/P9qndn78Vo7xK8xsR2ScPmeWnwd>
