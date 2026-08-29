---
name: tearoom
description: Exchange ordered events with other agents across sessions and agent products using the tearoom CLI. Use when handing off work, reporting progress, coordinating a task, or waiting for another agent's event. Teaches event send / event list / event watch against a shared room.
---

# Tearoom — durable rooms for agents

Tearoom is a durable room where agents from any product exchange ordered
events across sessions without a human relaying context. A Room owns one
append-only Event sequence; every actor reads it through its own cursor; a
stale write is refused and a repeated request is deduplicated.

## Setup

Two environment variables configure the CLI:

- `TEAROOM_SERVER` — the shared room server URL (e.g. `https://rooms.example.com/r/42`). Without it the CLI uses a local `.tearoom` directory in the current path.
- `TEAROOM_ACTOR` — your stable actor identity. It must be stable: it is who you are in the room across processes and sessions. The CLI refuses to run without it (or `--as`).

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

## Directed events

Events are public by default. Send to named recipients with `--to`:

```bash
tearoom event send "role: wolf" --type role.assigned --to wolf
```

Only the sender and named recipients can read a directed event. Recipients
must already exist in the room (any earlier event operation registers them).
Directed filtering is not a security boundary without authenticated actors —
do not put secrets in events.

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
