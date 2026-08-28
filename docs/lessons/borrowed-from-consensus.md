---
type: lesson
title: Borrowed from consensus systems
description: What Raft, etcd-raft and the Slock/Raft chat system get right that this workspace does not, verified against the current binary.
resource: internal/store/file.go
tags: [lesson, concurrency, consensus, cli, design]
timestamp: 2026-08-28
---

# Borrowed from consensus systems

Coordinating several agents in one workspace is the consistency problem in different
clothing, so the systems that solved it for machines are the right place to look. Three
sources below: the Raft algorithm itself, the `etcd-raft` implementation, and a
Slock-derived chat system built specifically for agents.[^1][^2][^3]

Each item below was checked against `bin/tea` as it stands. Two are already right by
accident of good layering. Three are real gaps, and one of them is severe.

## Already right

### Consensus logic that does no I/O

`etcd-raft`'s core **does no I/O at all** — no network, no disk, no clock. Callers drive
it, and it returns everything it wants done in one batch. The result is logic that is
**deterministic and replayable**, so testing it needs no real network. Most consensus bugs
live exactly where consensus logic and I/O are tangled together, in the callback that both
mutates state and sends a packet, leaving an inconsistent middle state on crash.[^2]

This project arrived at the same split: `internal/view` is pure derivation, and
`resolve` takes its clock by injection rather than calling `time.Now`. Worth stating
explicitly as the reason, because it is what makes the console and the CLI provably agree.

### A lock with a lease, not a lock held for the task

Holding a lock for a multi-minute task blocks everyone else for too long; the standard
answer is a lease with an expiry, so **a crashed agent's damage is bounded by one TTL**
rather than lasting forever. Leases are also what make the coordination stream itself
auditable — every acquire, denial and expiry is a recorded outcome.[^4]

`tea claim --ttl` already works this way, and `internal/resolve` treats an expired lease
as absent. This is the piece the external field has converged on too.[^5]

## Real gaps

### 1. Nothing rejects a stale write

The severe one. An agent reads state S0, spends minutes reasoning, and commits into a
world that is now S1 — so its reply is **duplicated, obsolete, or answering a question
nobody is asking any more**.[^3]

Verified here:

```
tea claim "src/**" --thread t1        (as agent a) → ok, a owns it
tea reply t1 --body "nobody has this, I'll take it"   (as agent b) → exit 0
```

Agent b's stale conclusion lands in the thread unchallenged. The ledger now contains a
confident false statement about ownership.

The mechanism for this is **Freshness Hold**: the agent generates its draft against S0,
and the server compares state at submit time. Unchanged, it commits; moved to S1, it is
**refused, and the delta is handed back to the agent** so it can revise, resend, or stay
silent. A semantic rebase rather than a hard failure.[^3]

Shape it would take here: `tea reply --since <cursor>`, refused with exit 2 and the
intervening events on stdout. The cursor is already available — `status` and `context`
both read a consistent snapshot.

### 2. Conflict tells the agent what happened, not what to do

`tea claim` on a held resource currently returns:

```
conflict: "src/**" is held by a until 2026-08-28T07:25:47Z (6m30s remaining)
```

Two defects. **`--json` is ignored on this path**, so the one client that must parse
machine-readably gets prose. And it states the obstacle without naming an action, where
the reference system pairs the refusal with an explicit recovery instruction —
*the message is already queued, do not rerun, send a corrected follow-up only if
needed*.[^3]

An agent that is told only "denied" retries. Retrying a claim it can never win is the
starvation signal in the coordination literature.[^4]

### 3. Errors do not separate what took effect from what did not

**The most dangerous return value an agent tool can produce is a blanket "failed" that
hides side effects which already happened.** One call may carry several effects; the
failure of a later one does not make the earlier ones safe to repeat. The canonical case:
message body is queued, the `@` mention fails, and the tool reports "send failed" — so the
agent resends and the body appears twice.[^3]

The fix is a per-effect status plus an explicit recovery directive.[^3] This is the point
worth adopting most, and it was already flagged as such in the internal Harness PRD, which
noted that requiring structured errors is not enough unless they can express **which steps
took effect and which did not**.[^6]

Here, `tea claim --thread` performs two writes — the lease and the thread association.
Nothing in the current exit-code contract can express "lease taken, association failed".

## The rule this yields

Every one of these gaps has the same author: **a rule that must hold was left to the
caller to remember.** Freshness is remembered by whoever thinks to re-read; recovery is
remembered by whoever reads the prose; idempotence is remembered by whoever knows which
half of the command already ran.

That is the same finding as
[invariants belong under the store](invariants-belong-in-the-store.md), and the same
remedy: push it beneath the `Store` interface, where a second client cannot get it wrong.

Raft's own lesson is the compression of all of this: **restrict the caller's freedom to
buy predictable behaviour**.[^1] An agent given fewer ways to be wrong is wrong less
often.

## Not adopted

Multi-Raft resource pooling, pipelined replication and learner replicas[^7] solve
throughput across many replicated groups. This workspace is one directory with a file
lock. Recording them here only so the decision is visible rather than accidental.

## Related

- [Convergence](../architecture/convergence.md) — the command surface these findings reshape
- [Agent experience](../domain/agent-experience.md) — the second axis these sources open up
- [Cross-process locking](cross-process-locking.md) — how the lease is actually enforced

[^1]: Raft 共识算法深度原理剖析 — <https://bytedance.larkoffice.com/docx/AI0ldxD6sopO8Bx9lS1mBU9nyid>
[^2]: same document, section 9.1, on etcd-raft as a pure state machine
[^3]: 也许还真得为 Agent 重新设计一次聊天系统, 叶思宇 — <https://bytedance.larkoffice.com/docx/P9qndn78Vo7xK8xsR2ScPmeWnwd>
[^4]: Before the Pull Request: Mining Multi-Agent Coordination — <https://arxiv.org/pdf/2606.19616>
[^5]: agentlocks — <https://github.com/simke9445/agentlocks>; agent-sync — <https://github.com/denfry/agent-sync>
[^6]: 自演化事件状态机 Harness · PRD, appendix VI — <https://bytedance.larkoffice.com/docx/FD1hdewbgoQ7P6xI3lbc7RHWnsd>
[^7]: ByteRaft — <https://bytedance.larkoffice.com/wiki/wikcnWUg5nUkVpNYXpi2mX7wKLb>
