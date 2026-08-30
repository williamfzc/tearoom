---
type: decision
title: Open questions
description: Decisions deliberately left unsettled after the Room, Actor, Event convergence.
tags: [domain, decision, open]
timestamp: 2026-08-30
---

# Open questions

Each question needs usage evidence rather than a speculative abstraction.

## Room placement

Should one repository normally contain one Room, or should a Room live above several repositories and worktrees? Local discovery currently binds one Room to one ancestor directory. The cross-product HTTP mode is not directory-bound for clients.

## Protocol ownership

Which event conventions deserve reusable libraries? Worktree handoff, task claiming, approval, and Werewolf can all be expressed today, but the kernel should not own their rules without at least two independent callers.

## Explicit read and attention state

The kernel has no implicit Actor cursor, unread marker, or acknowledgement. Is
an explicit consumer/subscription index needed for any real delivery journey,
or are caller-supplied `--after`/`--since` boundaries sufficient? Measure token
cost and recovery behaviour in busy Rooms before adding durable attention state.

## Privacy composition

When a protocol needs private information, is a separate Room sufficient, or
does the application need an external authoritative secret store? The kernel
will not reintroduce per-Event private visibility to answer this question.

## Conditional append

Which real protocol, if any, requires a conditional append against an explicit
Room head or another boundary? Ordinary `send` must remain independent of read
progress unless evidence justifies a separate capability.

## Offline activation proof

Does a durable activation loop for an absent Agent provide value beyond an IM
bot, queue, or workflow engine? The technical boundary is now defined in
[activation.md](../architecture/activation.md); the open question is whether
the P2 experiment demonstrates enough recovery, latency, cost, and integration
value to justify implementation beyond the trivial responder.

## Event envelope evidence

Which protocol has evidence for `type` or `caused_by` as a shared envelope?
Keep them above the minimum `seq`/`actor`/`at`/`to`/`content` shape until two
independent callers require the same contract. Request-level append idempotency
is a transport/Store retry contract, not an Event field.

## Hosted authorization details

GitHub sign-in for humans, explicit Room ownership, and separate Agent credentials are settled directions. Remaining choices include organization mapping, role names, token scopes and expiry defaults, invitation flow, and whether self-hosted deployments may replace GitHub with another identity provider. See [hosted-identity.md](hosted-identity.md).

## Retention

The Event log grows forever. Compaction, snapshots, and export require a rule that preserves replay and audit semantics without introducing editable history.
