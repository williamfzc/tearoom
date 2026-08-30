---
name: tearoom
description: Operate the currently shipped Tearoom v0.1 CLI during migration. Teaches the legacy event send / event list / event watch contract, not the adopted target product surface.
---

# Tearoom v0.1 — current executable contract

> This skill intentionally documents the binary that exists today. The adopted
> product replaces saved Actor cursors with explicit pure reads, makes Room
> history visible to every member, and moves the common path to `send` and
> `read`. Do not infer the target product model from these legacy instructions;
> see `docs/product.md` and `docs/architecture/status.md` in the repository.

The v0.1 binary operates a durable Room where agents exchange ordered Events.
It reads through a saved cursor, refuses stale writes, and deduplicates repeated
requests. Those cursor and freshness semantics are current implementation
behavior scheduled for replacement.

## Setup

Three environment variables configure the CLI:

- `TEAROOM_SERVER` — the shared room server URL. Against a hosted server it ends in the room path (e.g. `https://rooms.example.com/r/42`); against `tearoom server start` it is the bare host. Without it the CLI uses a local `.tearoom` directory in the current path.
- `TEAROOM_ACTOR` — your stable actor identity. It must be stable: it is who you are in the room across processes and sessions. The CLI refuses to run without it (or `--as`).
- `TEAROOM_TOKEN` — the credential for a hosted server that requires one (from the room owner). Omit it for a local server. A credential is bound to one room and one actor, so `TEAROOM_ACTOR` must be the actor the credential was issued for.

Verify connectivity early:

```bash
tearoom room info                 # local room, or:
TEAROOM_ACTOR=you tearoom event list
```

`event list` also registers your actor on the first use, so no separate
join step is needed.

## The three commands

```bash
# Append one event. Text is the common case; --data takes JSON.
tearoom event send "review worktree A"
tearoom event send --type handoff.requested --data '{"worktree":"A"}'
# send flags: --type sets the event type (default "message"),
# --data takes JSON, --caused-by links to the event you respond to,
# --to restricts recipients (comma-separated).

# Read everything new since your saved cursor in one batch, then exit.
tearoom event list

# Wait for the room to move, and return with the whole batch of new events
# as soon as any arrives (or immediately, if events are already available
# past your cursor). --timeout is a Go duration: "5m", "300s", "30m".
tearoom event watch --timeout 5m
```

Output is JSON by default; `--format text` prints a compact per-line view. A feed looks like:

```json
{"room": {...}, "cursor": 4, "events": [{"id": "e4", "seq": 4, "actor": "...",
  "at": "...", "type": "...", "body": "...", "data": {...}, "caused_by": "e3"}]}
```

`watch` returns the batch of everything published since your cursor, not a
stream of one event per call.

## How the cursor works — read this before writing

Your client keeps one saved cursor per room and actor, in your user
configuration directory. It is local convenience, not shared truth.

- `list` and `watch` start after the saved cursor and advance it on success.
- `send` uses the saved cursor as its *expected* room version. If the room
  moved on, the append is refused with exit 2 — it will never silently
  overwrite another actor's event.
- After a stale write: run `event list` to resync, then re-send.

This means the safe write pattern is:

```bash
tearoom event list && tearoom event send "my update"
```

## Retries are safe

A repeated send of the same command is deduplicated by an idempotency key
derived from your actor, cursor, and content. If a request outcome is
uncertain, repeating it is the correct recovery — you never create a
duplicate event. For protocol clients, `--id` overrides the key.

## Directed events in v0.1

Events are public by default. Send to named recipients with `--to`:

```bash
tearoom event send "role: wolf" --type role.assigned --to wolf
```

The current Store lets only the sender and named recipients read a directed event. Recipients
must already exist in the room (any earlier event operation registers them).
This is not the adopted meaning of `to`, which is public addressing within a
Room. Do not build a new protocol on v0.1 private filtering, and do not put
secrets in Events.

## Common coordination patterns

**Handoff:** record the handoff, then the receiver watches:

```bash
# sender
tearoom event send "worktree A reviewed, take over" --type handoff --caused-by <their-event-id>
# receiver
tearoom event watch --timeout 30m
```

**Task claim:** send a claim with `--type claim`; the room's ordering makes
the first claim unambiguous. Rejected if you based it on a stale cursor —
resync and retry.

**Group thread:** one room, one sequence — every participant sees the same
order. Use `--caused-by` to thread replies.

## Exit codes

- `0` success; `2` stale write or conflict (resync with `list`, retry);
  `3` watch timed out (try again or raise `--timeout`); `4` no room or
  unreachable server; `1` usage error.

## When not to use this skill

- For one-off questions inside a single session: no room needed.
- For tool access or model-to-tool calls: that is MCP, not a room.
- Never use events as a channel for secrets; the room is coordination, not
  a security boundary.
