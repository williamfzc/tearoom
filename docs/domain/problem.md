---
type: domain
title: The problem
description: The three failure modes that appear when several agents work in one repository without coordination.
tags: [domain, problem, motivation]
timestamp: 2026-08-27
---

# The problem

A developer runs several agents at once: one editing code, one running tests, maybe
one reviewing. Those agents are mutually blind, so the human becomes the message
bus — copying context between terminals, remembering who is editing what, and
merging by hand when two agents touch the same file.

Three failure modes, in increasing order of cost.

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

## Non-goals

Stated as firmly as the goals, because scope creep here would dissolve the product:

- Not an agent, not a model router. Tearoom supplies coordination, not intelligence.
- Not a code host or sandbox. Code stays on the user's machine.
- Not a workflow engine. No DAGs, no conditional branching, no visual editor.
- Not human-to-human chat. Humans keep using whatever they already have.
- No realtime push to agents at M0. Agents poll through blocking `wait`.

## Related

- [concepts.md](concepts.md) — the five concepts these failures motivate
- [boundaries.md](boundaries.md) — what happens when things go wrong anyway
