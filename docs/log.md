---
type: log
title: Change log
description: Dated record of decisions and structural changes, newest first.
tags: [log, history]
timestamp: 2026-08-27
---

# Change log

Newest first. Records decisions and structural moves, not routine commits.

## 2026-08-28 — Settle the product definition, give it authority

The product was defined in three places that had drifted apart. Consolidated into
[product.md](product.md), which is now **authoritative over every other document**,
including `README.md`.

**The drift was measurable.** Four documents claimed a ten-command surface. The binary has
fourteen. Nobody revisited the constraint when commands were added, so the written scope
and the shipped scope had quietly diverged. All four corrected, with ten restated as a
target and [convergence.md](architecture/convergence.md) as the route.

**The one-sentence definition changed.** Previously: three primitives, and agents need
almost nothing else. Now: Tearoom decides **who may act** and controls **what an agent
spends to learn what changed**. The second clause is new, unbuilt, and load-bearing.

**A framing in the README was wrong and is retracted.** It held that IM features —
mentions, unread state, notifications — are human decoration and therefore noise for an
agent. Agents need less presence and formatting but considerably more attention control
than IM gives a bot. The refusal now reads *not a chat product for humans*, which no
longer extends to the attention machinery.

**A fourth failure mode was added**: stale action. The first three motivated the original
build; this one has no answer yet and is demonstrated by our own binary accepting a false
ownership claim.

**Realtime push stopped being a non-goal.** It was listed as unnecessary because blocking
`wait` covers it. It does not: a blocking wait occupies a tool call and dies with the
session. Reclassified from rejected goal to missing work.

Added five ordered tests a proposal must pass, and four conditions that define the product
as working. Two of the four hold today; the other two are the honest gap.

## 2026-08-28 — Study consensus systems, find the surface is half a product

Read how Raft, `etcd-raft` and a Slock-derived agent chat system solve coordination,
then checked `bin/tea` against them. Three documents came out of it, no code yet.

**The command surface is not converged.** Fourteen top-level commands, audited against a
three-part standard. `threads` and `leases` print nothing `status` does not already
contain; `init` is a human's command living on the agent surface; `inbox` is `status`
with a narrower scope. Ten commands cover every user story. Recorded in
[architecture/convergence.md](architecture/convergence.md).

**`claim` does not claim the work.** Observed: claiming a resource with `--thread` leaves
the thread `open` and unowned, so the board says nobody is responsible while a lease is
held. The agent has to remember a second command. This is the invariant-in-the-caller
mistake again.

**Nothing rejects a stale write.** Observed: a second agent, reasoning from a snapshot
taken before a claim, wrote "nobody has this, I'll take it" into a thread already owned —
accepted with exit 0. The reference design refuses the commit and hands back the delta so
the agent can revise. Recorded with two related gaps in
[lessons/borrowed-from-consensus.md](lessons/borrowed-from-consensus.md).

**The bigger finding: this product has built one axis of two.** Everything so far governs
who may change the world. Nothing governs what an agent spends to learn what changed —
no notice tier, no mute, no deferred intent, no durable session. `wait` is the blocking
sleep that the reference design exists to avoid. Recorded in
[domain/agent-experience.md](domain/agent-experience.md), which also reframes the pitch:
agents do not need less than a chat system, they need different things from one.

## 2026-08-27 — Adopt AGENTS.md, extract the assembly layer

Adopted `AGENTS.md` as the working standard for this repository, then audited the
existing code against it. Three violations found and fixed:

- **One fact stated twice.** The five-read sequence that assembles a board existed in
  both `internal/cli` and `internal/transport`, byte-for-byte. Extracted to
  `internal/resolve`, which now owns assembly and nothing else. The same duplication in
  thread context (live-lease and pending-gate filtering) went with it.
- **A file without a header.** `internal/transport/server.go` had no opening comment
  stating what it is or where it stands.
- **An unnamed constant.** The compact-event count was an inline literal in two places.

Added nine contract tests for `internal/resolve` covering the promises it makes to the
layer above: expired leases and settled gates never pass through, and a partial read
fails rather than producing a half-true board.

Rebuilt `docs/` as an OKF bundle: frontmatter on every document, one concept per file,
per-directory indexes, and links to canonical sources instead of restatement. The
previous `design.md` and `cli.md` were split by concern into `architecture/`, `domain/`,
`reference/`, `operations/` and `lessons/`.

## 2026-08-27 — M0 complete

CLI, HTTP server and WebAssembly console working end to end. Four lessons recorded
under [lessons/](lessons/index.md), each found by using the tool rather than reading it.
