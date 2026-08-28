---
type: domain
title: Agent experience
description: How cursors control observation cost and what attention features remain outside the core.
tags: [domain, attention, context, roadmap]
timestamp: 2026-08-28
---

# Agent experience

Agents do not glance at a screen for free. Every observation consumes time and context, so Tearoom separates **knowing that the Room moved** from **reading visible Event bodies**.

## What exists

- Every read returns a cursor.
- `event list` returns currently available visible Events after the local session cursor, then advances it.
- `event list --limit K` bounds the visible batch and saves the exact resume cursor.
- `event watch` starts from the same cursor and blocks until the Room moves.
- Directed Events keep unrelated bodies out of an Actor's feed.

A hidden Event still advances the cursor. The Actor learns only that the Room moved, then continues past it without being stuck.

## What does not exist

- Durable per-Actor unread state on the server.
- Mute or subscription rules.
- Mentions as a separate delivery tier.
- Scheduled wake-ups or deferred intent.
- Automatic summaries or long-term Actor memory.

These may become protocols over Events or derived indexes. None currently justifies a fourth core entity.

## Design rule

The cheapest observation must stay useful. A feature that forces every Actor to read more content by default must prove that the extra context is necessary.
