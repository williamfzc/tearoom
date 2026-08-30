---
type: domain
title: Agent experience
description: How callers observe a Room and how optional activation differs from read progress.
tags: [domain, attention, context, roadmap]
timestamp: 2026-08-30
---

# Agent experience

The target contract treats a Room as shared history, not as a per-Actor inbox. Every member can read the complete Event sequence. A caller chooses how much history to inspect and where to begin; Tearoom does not record an implicit unread position.

## Target observation model

- `read` is a pure query. Its `after` boundary belongs to the caller and is not saved by the Room.
- A caller may read from the beginning, from a known sequence, or from the boundary it keeps in its own session.
- `read --wait` is an invocation-scoped wait for later Room movement. It is a mode of reading, not a separate delivery or acknowledgement protocol.
- Every member sees the same complete history. `to`, when present, is public attention metadata; it is not a visibility filter or a process-control request.
- A read boundary is not proof that an Actor read, understood, or completed an Event.

The minimum useful observation is therefore an Event reference and enough ordered history for the caller to reconstruct context. A caller may maintain a local boundary for convenience, but that state is not shared truth.

## Optional activation

Activation is above the Room. An activation service may observe an addressed Event, elect it according to policy, and deliver an Event reference to a launcher. The launcher may start or resume a Runtime. The Runtime reads the Room and appends its own contribution.

Activation delivery state is operational consumer state. It must not be
represented as an Actor read cursor or as task completion. The canonical state,
recovery, lease, retry, and duplicate-safety contracts are in
[activation.md](../architecture/activation.md); their proof remains optional.

## What the target does not promise

- server-owned unread state or read acknowledgements;
- mute, subscription, or mention tiers;
- scheduled wake-ups as part of the Room kernel;
- automatic summaries or long-term Actor memory;
- private Event bodies inside one Room;
- exactly-once Runtime execution.

These may be application protocols or control-plane capabilities. None adds a fourth core entity to Room, Actor, and Event.

## Design rule

The cheapest observation must stay useful. A feature that forces every Actor to read more content or run a process must prove that the extra context or execution is necessary.
