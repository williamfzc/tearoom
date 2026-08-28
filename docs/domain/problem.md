---
type: domain
title: The problem
description: The four failure modes that appear when several agents work in one place without coordination.
tags: [domain, problem, motivation]
timestamp: 2026-08-28
---

# The problem

A developer runs several agents at once: one editing code, one running tests, maybe
one reviewing. Those agents are mutually blind, so the human becomes the message
bus — copying context between terminals, remembering who is editing what, and
merging by hand when two agents touch the same file.

Four failure modes, in increasing order of cost. The summary table lives in
[the product definition](../product.md); this page explains each one and what answers it.

## Broken handoff

No way to notify progress, hand over an artifact, or wait on a precondition. The
workaround is human copy-paste, which means work stops the moment the human leaves.

Addressed by threads and blocking `wait`.

## Silent overwrite

Two agents edit one file; the later write wins and nothing reports it. The workaround
is human memory, which fails at scale and — worse — fails *quietly*.

Addressed by leases. The quietness is the real problem, which is why a rejected claim
names the holder and the expiry instead of just failing.

## No oversight trail

No way to know what an agent is doing, where it is stuck, or why it decided
something. The workaround is reading scrollback across N terminals, which makes
post-hoc review impossible and means an organisation cannot delegate the work at all.

Addressed by the append-only ledger and the attention-first board.

## Stale action

An agent reads the workspace, spends minutes reasoning and calling tools, then acts on a
world that has moved on. The result is a duplicate reply, a claim on work someone else
already took, or a confident statement that was true when it was formed and false when it
landed.

This is the one failure mode the product does not yet answer. Observed here: an agent
wrote *"nobody has this, I'll take it"* into a thread another agent already owned, and the
workspace accepted it. The remedy is to refuse a write whose base state has changed and
hand the delta back — see
[borrowed from consensus systems](../lessons/borrowed-from-consensus.md).

The workaround in the meantime is the human noticing, which is the thing this product
exists to stop requiring.

## Non-goals

Stated as firmly as the goals, because scope creep here would dissolve the product:

- Not an agent, not a model router. Tearoom supplies coordination, not intelligence.
- Not a code host or sandbox. Code stays on the user's machine.
- Not a workflow engine. No DAGs, no conditional branching, no visual editor.
- Not human-to-human chat. Humans keep using whatever they already have.

Realtime push is **not** on this list. It was previously recorded here as something we do
not need, on the grounds that blocking `wait` covers it. That reasoning does not hold: a
blocking wait occupies a tool call and dies with the session, so nothing in the workspace
can express "look at this again tomorrow". It is missing work, not a rejected goal —
see [agent experience](agent-experience.md).

## Related

- [../product.md](../product.md) — the authoritative scope these failures justify
- [concepts.md](concepts.md) — the five concepts these failures motivate
- [boundaries.md](boundaries.md) — what happens when things go wrong anyway
