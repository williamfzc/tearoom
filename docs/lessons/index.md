---
type: index
title: Lessons
description: Mistakes that cost something to find, with the evidence that produced them and the rule each one established.
tags: [index, lessons]
timestamp: 2026-08-27
---

# Lessons

Each of these was found by using the tool, not by reading it. They are recorded
because the rule each one established is easy to violate again.

| Document | Rule it established |
|---|---|
| [invariants-belong-in-the-store.md](invariants-belong-in-the-store.md) | A rule every client must obey belongs below the store interface |
| [cross-process-locking.md](cross-process-locking.md) | Test concurrency with processes, and verify the test can fail |
| [console-repaint.md](console-repaint.md) | A polling UI must not rebuild state that did not change |
| [flag-parsing.md](flag-parsing.md) | Accept arguments in the order callers actually write them |

## Why these are documented and most things are not

A lesson earns a place here when the cost of relearning it is high and the mistake
looks reasonable at the time. Routine bugs do not qualify — the fix is the record.

Two of these four were found by a second client appearing (the browser console), and
one by deliberately breaking a passing test to see whether it noticed. Both are
techniques worth reusing, not just anecdotes.
