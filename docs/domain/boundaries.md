---
type: domain
title: Boundary behaviour
description: Tearoom behaviour under stale views, retries, contention, crashes, and unavailable stores.
tags: [domain, failure-modes, invariants]
timestamp: 2026-08-28
---

# Boundary behaviour

Tearoom prefers a loud refusal to a quiet false belief.

| Situation | Behaviour |
|---|---|
| Room changed after an actor read | Append fails with exit 2 and reports expected and current cursor |
| Identical request is retried | Original Event is returned; no duplicate is appended |
| One key is reused for different content | Append fails as a conflict |
| Writers race on one cursor | One wins that sequence; the others receive stale-write conflicts |
| Writer crashes while holding the file lock | The kernel releases `flock` |
| Store or server is unavailable | Command fails with exit 4; there is no cached success |
| Reader is not a joined Actor | Read is refused |
| Directed Event is read by an unrelated Actor | Cursor advances, body is omitted from that Actor's feed |
| Reader requests a limited page | Cursor stops at the last inspected Event so the next read resumes safely |

## Why hidden events advance the cursor

A reader must be able to move past an Event it may not see. Otherwise one private Event would permanently wedge every unrelated actor at the preceding cursor. The reader learns only that the Room advanced, not the hidden content.

## Windows

The current cross-process lock is implemented for Unix-like systems. Windows builds do not yet provide a real lock and are not safe for concurrent use.
