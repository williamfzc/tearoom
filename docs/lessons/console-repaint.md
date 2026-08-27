---
type: lesson
title: A polling UI must not rebuild what did not change
description: The console replaced its entire DOM every three seconds, breaking selection, invalidating handles and leaking a callback per button per poll.
resource: cmd/tea-web/main.go
tags: [lesson, frontend, wasm, performance]
timestamp: 2026-08-27
---

# A polling UI must not rebuild what did not change

## What happened

The console polls the board every three seconds and rendered by clearing `#app` and
rebuilding it. Three consequences, in increasing severity:

- Screenshots of the page timed out, because the document was replaced mid-capture.
- Element handles went stale within one poll interval, so any interaction had to find
  and click inside the same tick.
- Every rebuild called `js.FuncOf` again for each button. Those allocations are never
  collected until explicitly released, so the page leaked one callback per button per
  poll — invisible at a desk, fatal on a wallboard left open for a week.

For a human it also meant text selection was destroyed every three seconds while
reading.

## The fix

Fingerprint the meaningful state and repaint only when it changes. The fingerprint
deliberately excludes the clock and the idle durations that tick every second;
including them would make every poll look like a change and defeat the purpose.

The timestamp is updated in place so the page still visibly proves it is live, and
callbacks are released before the DOM that owns them is discarded.

## How it was verified

Behaviour, not just tests. A marker was set on a live child node, then:

- after eight idle seconds the marker survived while the clock advanced — no rebuild
- after a real state change the marker was gone — rebuild happened when it mattered

Both directions matter. Suppressing a repaint that should have happened would be a
worse bug than the original, since the console would then show stale state.

## Related

- [../architecture/isomorphic-go.md](../architecture/isomorphic-go.md) — why the console is Go
- [../operations/serving.md](../operations/serving.md) — running the console
