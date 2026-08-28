---
type: domain
title: Agent experience
description: The second axis of the problem — attention, time and context as budgeted resources — which this workspace currently does not address at all.
tags: [domain, ax, context, attention, roadmap]
timestamp: 2026-08-28
---

# Agent experience

Everything built so far answers one question: **who gets to change the world.** Leases,
gates and the ledger are all about that.

There is a second question, and it is not a smaller one: **what does an agent spend to
find out what is going on.** A human stares at a screen, sees a badge, and glances at a
channel for free. An agent only looks when woken, and **every glance is deducted from a
context budget**. The Slock-derived system names this axis AX, agent experience, and treats
it as a first-class design surface.[^1]

By that measure this project has built one axis of two.

## Why it is not a nicety

The failure it prevents is concrete. An agent pulled into a busy workspace whose traffic
is mostly irrelevant will have its context filled with other people's chatter, and the
task it was actually doing gets interrupted repeatedly.[^1] The workspace then degrades
the agents it was built to coordinate.

The reverse policy fails too: waking an agent only on direct mention costs it the ability
to notice anything on its own.[^1]

## Three mechanisms, and what we have instead

### Notice separated from body

The move is to split "something changed" from "read the content". A notice says which
channel moved, how many unread, whether you were mentioned — then **the agent decides**
whether to pull the body in.[^1] The cost asymmetry is the whole point: a check costs
tens of tokens, a read costs thousands.

An agent must also be able to **mute** a channel, staying dormant until mentioned
directly.[^1] The observation worth sitting with: when we hand agents an API, almost
nobody thinks to expose mute.[^1]

What we have: `tea status` and `tea context`. Both return content immediately. There is no
notice tier, no unread count, no mention flag, and no way for an agent to say *this thread
is not mine, stop showing it to me*. `context` compacts to a fixed five trailing events —
a constant, not a budget the agent controls.

### Future intent as an object, not a sleep

Agents constantly need to wait: for a deploy, an approval, a human. Writing that as sleep
**holds the current tool call hostage**, and after a long enough wait the agent can barely
explain why it is still doing this.[^1] The alternative is to persist the intent — owner,
trigger time, the originating thread — as an object that can be snoozed, updated or
cancelled, and to leave the turn entirely.[^1]

What we have: `tea wait` polls every two seconds and blocks up to five minutes. That is
exactly the sleep being criticised. It occupies the call, and it cannot outlive the
session. Nothing in the workspace can express "check this again tomorrow".

The internal Harness PRD reached the same conclusion from the other direction and chose
to model delay as **self-posted events** rather than sleeping.[^2]

### Identity that survives the task

The choice here is deliberate and non-obvious: the session follows **the agent**, not the
channel or the task. One continuing session sees group threads, direct messages and task
threads, replying into each. Claiming a task changes who owns it and where results are
written — it **does not open a fresh context**.[^1] So an agent does not re-learn the
project and its collaborators every time it picks up work. The admitted cost is that old
tasks and unrelated rooms keep competing for attention, which is why mute and notices are
required alongside it.[^1]

Three tiers, worth reproducing because it is a storage decision as much as a UX one:

| Tier | Holds | Used how |
|---|---|---|
| Current session | recent messages, tool calls, work in flight | appended continuously, bounded by the window |
| Workspace memory | what the agent chose to keep | survives turns, needs curation |
| Thread | original discussion, state, artefacts | re-read on demand, not resident in the prompt |

What we have: the third tier only. An `Agent` record here is a row with a role and a
heartbeat — there is no session, and nothing an agent can deliberately keep. This is the
same hole as the
[cross-session handoff gap](open-questions.md): a thread id is a coordinate inside one
workspace, not a resumable identity.

## What this changes about the product

The pitch so far has been that a workspace can be simpler than Lark or Slack. The
sharper version, given the above: **it is not that agents need less than a chat system —
they need a different set of things.** Fewer surfaces for presence and prettiness; more
for attention control, deferred intent and durable identity. Mute matters more than
formatting. A notice tier matters more than a message list.

That reframing also explains why the external field looks crowded but shallow: the
projects converging on this space are almost all lease managers.[^3] Locking is the part
that resembles familiar infrastructure. The attention and context half is the part nobody
has taken, and it is the half agents actually spend their budget on.

## Not decided

- Whether notice/read is two commands or one command with a flag, given
  [convergence](../architecture/convergence.md) pressure.
- Whether mute is per-agent-per-thread state, and therefore whether the ledger records it.
- Whether deferred intent requires a daemon. A file-backed workspace with no running
  process cannot wake anybody; `tea-server` could, which would make it load-bearing rather
  than optional.

## Related

- [Borrowed from consensus systems](../lessons/borrowed-from-consensus.md) — the first axis, and its gaps
- [Open questions](open-questions.md) — the handoff gap this compounds
- [Concepts](concepts.md) — what an agent record currently holds

[^1]: 也许还真得为 Agent 重新设计一次聊天系统, 叶思宇 — <https://bytedance.larkoffice.com/docx/P9qndn78Vo7xK8xsR2ScPmeWnwd>
[^2]: 自演化事件状态机 Harness · PRD, appendix VI — <https://bytedance.larkoffice.com/docx/FD1hdewbgoQ7P6xI3lbc7RHWnsd>
[^3]: agentlocks — <https://github.com/simke9445/agentlocks>; agent-sync — <https://github.com/denfry/agent-sync>; coordinaut — <https://github.com/LevDomasnih/coordinaut>
