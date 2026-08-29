---
type: product
title: Competitive analysis
description: The 2026 agent-coordination market, Tearoom's differentiation, and the near-term priorities that follow.
tags: [product, market, priorities]
timestamp: 2026-08-29
---

# Competitive analysis

This document records what the 2026 agent-coordination market shows and what it justifies. [product.md](product.md) remains authoritative over scope; this document only orders work that is already named there.

## The landscape

Four groups of players surround the Room, Actor, Event kernel:

**Protocols.** The market has converged on two standards: MCP for model-to-tool access and A2A for agent-to-agent tasks. IBM's ACP merged into A2A in 2025-08, A2A reached v1.0 in 2026-03 under Linux Foundation governance, and both now sit under AAIF. Neither standardizes a durable coordination contract: A2A is task request/response with no ordered event history, replay, cursors, or idempotency. The industry's own summary is that A2A defines the transport and MCP the tool interface, while the memory contract between agents remains undefined.

**Memory products.** Mem0, Letta, and Zep are per-agent, retrieval-shaped memory: they answer what one agent knows, not what happened in what order across agents. Cross-agent shared memory is repeatedly named as the remaining gap, and conflict resolution in production systems is still last-write-wins or orchestrator arbitration.

**Coding-agent tooling.** The ecosystem hands off work through static files (AGENTS.md, handoff documents) and SQLite message buses with no ordering or concurrency guarantees. Claude Code's native cross-session messaging (2026-08) is ephemeral and text-only: a platform risk, not yet a durable log.

**Direct overlaps.** A dozen small projects implement one aspect of an agent event log — Provara (per-actor hash chains), AgentLog (JSONL topics with offsets), ActiveGraph, statefold, Eventloom, and others. None combines dense ordering, per-actor cursors, stale-write rejection, idempotency, and directed visibility.

## Differentiation

The combination above is Tearoom's substantive edge: a stale write is refused at the Store boundary rather than reconciled, a retry is deduplicated, and every actor reads a consistent ordered history through its own cursor. The combination is copyable, so distribution speed and protocol experience in real use matter more than the semantics themselves.

## Near-term priorities

The analysis justifies three priorities before any new surface:

1. **Replace the watch poll loop with a server-held wait.** `event watch` currently polls every 250 ms ([internal/cli/cli.go](../internal/cli/cli.go)). Every poll takes the store lock and rescans the log, so a quiet Room with several waiting actors still burns constant lock and scan work, and a waiting actor sees a new Event up to one interval late. Long-poll or SSE makes watch idle-cheap and immediate. **Shipped 2026-08-29 as long-poll** ([reference/http-api.md](reference/http-api.md)): the server wakes on appends it serves and checks an idle probe between, so a quiet Room costs nothing; the client slices long waits into 30-second requests. SSE remains deferred until a caller needs a stream instead of batches.
2. **Add the relational Store, SQLite first.** This is the existing direction in [architecture/persistence.md](architecture/persistence.md); the analysis only confirms the sequencing. The file Store's full scan under one lock degrades as Rooms grow busy.
3. **Ship one cross-agent skill over the CLI.** A skill is a folder of instructions every major agent product reads in 2026, so a skill that teaches `event send`, `event list`, and `event watch` is the cross-product integration surface with zero new server code. A dedicated MCP server waits for a caller that needs structured tool results instead of shell commands. **Shipped 2026-08-29 as [.claude/skills/tearoom/SKILL.md](../.claude/skills/tearoom/SKILL.md)**, a self-contained SKILL.md that teaches setup, cursor semantics, safe retries, and directed events; other products install it by copying the folder.

## Risks

- Claude Code's native cross-session messaging could grow durable history.
- An A2A working group could add an event-log specification.
- Both make distribution and real-use protocol experience the only durable advantages.

## Sources

- [Agent Communication Is Solved — Agent Memory Is Not](https://blog.clawpilot.ai/posts/ai-ops-trend-2026-agent-communication-is-solved-agent-memory-is-not)
- [A2A Agent State and Data Consistency (CockroachDB)](https://www.cockroachlabs.com/blog/a2a-agent-state-data-layer/)
- [The Missing Layer in Agent Memory](https://mnemoverse.com/docs/library/agent-memory-interop-gap)
- [Google A2A Protocol in 2026: Adoption, Hype, and Reality](https://dev.to/rosgluk/google-a2a-protocol-in-2026-adoption-hype-and-reality-51d6)
- [A2A joins AAIF (Axios, 2026-08)](https://www.axios.com/2026/08/17/a2a-agentic-ai-foundation-open-ai-standards)
- [A2A surpasses 150 organizations (Linux Foundation)](https://www.linuxfoundation.org/press/a2a-protocol-surpasses-150-organizations-lands-in-major-cloud-platforms-and-sees-enterprise-production-use-in-first-year)
- [Agent memory architecture showdown: Letta vs Zep vs Mem0](https://agentmarketcap.ai/blog/2026/04/06/agent-memory-architecture-showdown-letta-zep-mem0-in-context-postgresql)
- [AI agents as CRDT peers (ElectricSQL, 2026-04)](https://electric.ax/blog/2026/04/08/ai-agents-as-crdt-peers-with-yjs.md)
- [Claude Code issue #87023: cross-session amnesia](https://github.com/anthropics/claude-code/issues/87023)
- [ActiveGraph: The Log is the Agent](https://arxiv.org/html/2605.21997)
- [Provara Protocol](https://github.com/provara-protocol/provara)
- [AgentLog](https://github.com/sumant1122/agentlog)
- [Agent Room](https://github.com/agent-room-alkl/agent-room)
- [Deepgram AgentRooms](https://github.com/deepgram/AgentRooms)
- [open-agent-room](https://github.com/cch123/open-agent-room)
