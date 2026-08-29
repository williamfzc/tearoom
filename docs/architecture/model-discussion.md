---
type: architecture
title: Model discussion — recursion, room types, and the agent-facing surface
description: Open design discussion on extending the Room model: entity/value split, recursive rooms, membership visibility, room types, and agent-friendly primitives.
resource: internal/core/core.go
tags: [architecture, discussion, open, decision]
timestamp: 2026-08-29
---

# Model discussion: recursion, room types, and the agent-facing surface

> **Status: discussion draft.** Converged during a 2026-08-29 design conversation.
> This is not a decision. Open questions are listed at the end; nothing here is
> implemented or planned until it survives usage evidence.

## Thesis

Tearoom is a coordination substrate: **the entity graph gives structure, the
value stream gives order.** Rooms and Actors are entities (identity, lifecycle,
membership, credentials); Events are values (immutable facts in an ordered
stream). All semantics — scheduling, gates, quota, evidence — live in the
protocol layer above; the kernel guarantees only order, CAS, and idempotency.

## 1. Entity vs value

The defining line is *reference semantics* vs *value semantics*:

| | Entity | Value |
|---|---|---|
| Members | Room, Actor | Event |
| Semantics | Referenced: identity, lifecycle, credentials, membership | Immutable, append-only |
| Operations | open, attach spec, authorize, watch | append and read only |

An Event has an address (`e<seq>`) and can be referenced (`caused_by`), which
makes it look like an entity — but it is an *addressable value*: it has no
lifecycle and no credentials. Only Rooms can be opened, authorized, and watched.

One consequence: an entity birth (opening a child room) is **a fact in the
parent's stream plus a new entity**, not an event that *is* an entity. The two
must stay separate in the model.

## 2. Recursion and topology: rooms like directories, never merge order

```
Room  ::= Event+                 # a room is a sequence of events
Event ::= Fact | BirthRecord     # a fact, or "a child room was born here"
BirthRecord ⟹ new Room + its own event sequence
```

- Nesting is directory nesting; `room.open` is `mkdir`. Recursion happens on
  **entities**, never inside a value stream.
- **The rule that must not break: hierarchy is structure, not order.** A parent
  room's `seq` contains only events appended to the parent (birth records,
  milestones a referee fans in). A parent never merges children's event
  streams — merging two dense sequences reintroduces partial order, vector
  clocks, and complexity that this model exists to avoid.
- The metaphor is literal in the file backend: `.tearoom/` is the room,
  `events.jsonl` is the stream, `actors.json` is membership, `children/` would
  be sub-rooms, `spec.json` would be the charter.

## 3. Visibility = membership

- **Everyone in a room sees everything that happens in the room.** The
  visibility boundary is the container, not the content.
- Today's directed filtering (`Event.VisibleTo`, `internal/core/core.go:70-80`,
  enforced in every `Read`) would be removed. The `To` field stays as **routing
  / attention metadata** (mention-style: everyone can see it, recipients treat
  it as "this concerns you"), not as a secrecy mechanism.
- Privacy is provided by **sub-rooms with membership**: private reviews, secret
  game roles — each is its own room.
- Benefits: the cursor-wedge problem disappears (no more "the room moved, I
  don't know why", `docs/domain/boundaries.md:25-27`); membership already
  exists (local: joined actors refuse unjoined reads, `boundaries.md:21`;
  hosted: credentials bound to room + actor, `docs/domain/hosted-identity.md:40`).
  This change closes the remaining gap rather than opening a new door.
- Cost: breaking behavior change — `--to` semantics, the `note.whisper`
  example, three doc files, and tests.

## 4. Event extensibility

- Today `--type` accepts any string and `Data` is arbitrary JSON
  (`internal/cli/cli.go:374,386`). **Extending event types is zero kernel
  change**; it is a protocol convention.
- Recommended next step: an envelope layer — versioned type names
  (`todo.claim@1`) or a `protocol` field — so conventions can evolve without
  breaking old clients.
- The type vocabulary should be **verbs**: an Event is "Actor did Type to
  Room/Actor". `claimed`, `reviewed`, `approved`, `opened` — matching the
  actor-verb-object structure of the model.

## 5. Room types: the collaboration method

A room can declare a `spec.json` — the room's charter — containing:

```text
type: goal@v1
allowed:  [todo.claim, todo.update, gate.*, evidence, quota.*]
gates:    gate.ask must be resolved before further work events
referee:  ref-prod-1            # who may append decision events
strict:   true                  # kernel rejects unlisted types
```

- Different room types, different event gating: `chat-room@v1` (free-form,
  no referee), `goal-room@v1` (vocabulary + gates + referee), `game-room@v1`
  (role sub-rooms, game master).
- **Plugins are template directories, not kernel code**: a room type is a
  directory containing `spec.json` (+ a README teaching the collaboration
  method); `room init --type goal@v1` copies the template. The "code" of a
  room type lives in its referee, an ordinary external actor. Hook-style
  plugins that register behavior into the kernel are the fourth entity in a
  new costume and should be avoided.
- The kernel participates only optionally: in `strict` mode it checks the
  appended type against the room's declared vocabulary ("the room declares the
  invariant, the store enforces it"). This extends — rather than contradicts —
  the existing "invariants belong in the store" lesson
  (`docs/lessons/invariants-belong-in-the-store.md`).
- Additive by design: no type = today's behavior; type without strict =
  documentation for referees; type + strict = one set-membership check at
  append.

## 6. Agent-facing primitives

Current CLI is transport-shaped, not intent-shaped. The agent pays for it:
the cursor dance (stale-write exit 2, then manual `list` to resync), raw feeds
instead of distilled answers, `watch` waking on any movement, and protocol
burden (conventions) left entirely to the agent.

Target shape, by cost-benefit order:

1. **Make stale-write failures actionable**: on exit 2, return the missed
   events ("the room moved after your last read; here is what changed"). The
   CAS must stay — it is correctness — but the refusal should be a decision
   point, not an opaque error.
2. **Add a `status` primitive**: a distilled summary (new public events,
   events directed at me, gates waiting on me, cursor lag) instead of the raw
   feed, so agents stop re-implementing parse-and-filter.
3. **Watch with filter**: `watch --type gate.ask` — block until the *relevant*
   event, replacing the wake → filter → sleep loop.
4. **No side effects on read**: `list` should not advance the saved cursor
   (`--no-advance`); cursor disappears from agent-facing help text.

The reference shape is LoopX's small vocabulary — five commands
(`quota should-run`, `todo claim`, `todo update`, `refresh-state`,
`quota spend-slot`), each answering one agent question. Primitives are the
integration contract.

## 7. Long-horizon coordination (LoopX comparison)

Tearoom provides the physical layer of coordination: order, CAS, idempotency,
membership, blocking wait. Long-horizon governance (quota, gates, evidence,
claims — the LoopX state kernel) is **a `goal@v1` room type plus a referee**,
not kernel functionality. The two genuinely missing thin layers:

- an **external timer** that appends `schedule.wake` events (the bus holds
  facts, not intent);
- a **referee actor** that reads the stream and appends decision events
  (safe as a single writer via `Expected`).

Event TTL / retention / compaction remain an open question
(`docs/domain/open-questions.md:29-31`).

## 8. Converged vs open

**Converged in this discussion:** entity/value split; recursion on entities
with never-merged order; visibility = membership; type vocabulary as verbs;
plugins as template directories + referees.

**Explicitly undecided:**

1. Tree-as-truth vs log-as-truth: does a birth record stay in the parent's
   stream, or is the directory structure itself the record? (A real
   filesystem keeps no mkdir log; append-only philosophy leans toward
   keeping one.)
2. When the room-type system lands, and how far strict enforcement goes.
3. Priority and sequencing of the primitive rework (§6).
4. Event TTL / retention policy.
5. Whether the file-system reading surface (direct `cat`/`tail -f` of the
   JSONL stream) should be promoted to a supported interface or remain an
   implementation detail.
