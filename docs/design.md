# Tearoom — design

Status: M0 draft. Decisions marked **[open]** are not settled.

## 1. Problem

A developer runs several agents at once: one editing code, one running tests,
maybe one reviewing. Today those agents are mutually blind, so the human becomes
the message bus — copying context between terminals, remembering who is editing
what, and merging by hand when two agents touch the same file.

Three failure modes, increasing in cost:

| Failure | Mechanism | Current workaround and its cost |
|---|---|---|
| Broken handoff | No way to notify progress, hand over artifacts, or wait on a precondition | Human copy-paste. Work stops when the human leaves |
| Silent overwrite | Two agents edit one file; the later write wins with no error | Human memory. Fails at scale, and fails *quietly* |
| No oversight trail | No way to know what an agent is doing, where it is stuck, or why it decided something | Reading scrollback in N terminals. No post-hoc review, so orgs cannot delegate |

## 2. Non-goals

- Not an agent, not a model router. Tearoom supplies coordination, not intelligence.
- Not a code host or sandbox. Code stays on the user's machine.
- Not a workflow engine. No DAGs, no conditional branching, no visual editor.
- Not human-to-human chat. Humans keep talking in whatever they already use.
- No realtime push to agents in M0. Agents poll via blocking `wait`.

## 3. Concepts

Exactly five. A proposal requiring a sixth is rejected unless it can prove the
existing five cannot express it.

### Workspace
A project or repository, and the unit of isolation. Bound to a directory via
`.tearoom/config.json` but not strictly tied to it.
**[open]** One workspace per repo, or per branch? Affects parallel work.

### Agent
A durable identity with a role label and a heartbeat. **Identity comes from the
token, not the process** — an agent that crashes and restarts is the same agent,
which is what makes lease reclamation and stale detection meaningful.

### Thread
One goal, with an append-only event log. Fixed state machine:

```
open ──▶ claimed ──▶ resolved
 │  ▲       │  ▲
 ▼  │       ▼  │
blocked ────┘  │
 └─────────────┘
```

Transitions are free-form in both directions except that `resolved` requires at
least one event carrying evidence. No custom states — the value of a fixed
machine is that a human scanning the console does not have to learn per-project
vocabulary.

### Lease
An advisory exclusive hold on a resource — a file glob, a port, a task name —
with a mandatory TTL.

Advisory, not enforced: an agent can ignore a lease and write anyway. Enforcing
at the filesystem level would require a kernel hook or a FUSE mount, which is a
far heavier install than a single binary, and would still be bypassable. The
optional git pre-commit guard is the second line of defence.

**[open]** Default TTL. Too short interrupts long edits, too long slows recovery
after a crash. Needs measurement against real agent edit durations.

### Gate
A checkpoint requiring human approval. Blocks `wait` until resolved. **There is
no auto-approval on timeout** — auto-approval destroys the only reason gates
exist.

## 4. Architecture

```
  agent A          agent B          agent C            human
 (implement)      (verify)         (review)          (oversight)
     │                │                │                  │
     └── tea CLI ─────┴── tea CLI ─────┘                  │
              │  MCP adapter (same surface)               │
              ▼                                           ▼
     ┌──────────────────────────────────────────────────────┐
     │                    Tearoom core                      │
     │  threads │ leases │ gates │ append-only ledger       │
     └──────────────────────────────────────────────────────┘
                              │
                     ┌────────┴────────┐
                     │      store      │  ← interface
                     └────────┬────────┘
              ┌───────────────┴───────────────┐
       file store (M0)                 hosted (M3)
```

The client is a protocol translator and holds no authoritative state. This is
deliberate: the moment a client caches thread state, two machines can disagree
about who holds a lease, and there is no tiebreaker.

Everything mutating goes through a single `Store` interface. M0 implements it
over local files with `flock`; the hosted backend implements the same interface
over HTTP. The CLI surface does not change between them.

## 5. Storage layout (M0)

```
.tearoom/
  config.json          workspace id, name
  agents/<id>.json     role, last heartbeat
  threads/<id>.json    title, status, owner, events[]
  leases/<hash>.json   resource pattern, holder, expiry
  ledger.jsonl         append-only audit trail
```

Every mutation takes an exclusive `flock` on a lock file, so concurrent `claim`
attempts from separate processes serialise. Without this, two agents polling
simultaneously both read "free" and both write "held" — the exact bug this
product exists to prevent.

`ledger.jsonl` is append-only with no delete path, and is what makes an agent
run reviewable after the fact.

## 6. Context compaction

`tea context` is the token-cost lever. Naively an agent re-reads the whole thread
each turn, so cost grows linearly with conversation length. Instead it returns:

- thread metadata and current status
- active leases relevant to the thread
- a rolling summary of older events
- the last N events verbatim

Full history requires `--full`. **The default path must be the cheap one**;
completeness is the exception, not the norm.

## 7. Boundary behaviour

| Situation | Behaviour | Rationale |
|---|---|---|
| Agent crashes holding a lease | Lease becomes preemptable once heartbeat is stale; preemption is written to the ledger | Prefer progress over deadlock, but never silently |
| Store unreachable | Command fails loudly, no degraded mode | A false lease is more dangerous than a failure |
| Two agents claim the same resource | Serialised by lock; loser gets holder identity and expiry | A rejection must be actionable |
| Thread grows unboundedly | `context` compacts by default | Cheap path must be the default path |
| Human ignores a gate | Blocks indefinitely, escalates reminders | Auto-approval defeats the purpose |
| Circular waits | Detect the cycle, surface as deadlock, require human | Auto-breaking may kill the wrong side |

## 8. Security

- Code never leaves the machine. The store holds paths, diff summaries and
  decision text only.
- One token per agent, individually revocable. Revocation immediately invalidates
  that agent's leases.
- Append-only ledger, no delete API. Export must be verifiable offline.
- Self-hosting must stay possible. M0 does not need it; the architecture must not
  foreclose it.

## 9. Roadmap

| Phase | Scope | Hypothesis under test | Pass condition |
|---|---|---|---|
| M0 | CLI + local store; threads, leases, gates | Can agents hand off without a human relay? | Run a real multi-agent task with zero copy-paste |
| M1 | MCP adapter, read-only web console | Can a human grasp cluster state at a glance? | 7 of 10 users correctly answer "who is stuck" within 10s |
| M2 | Gates, the three human write actions, IM forwarding | Is the human-in-the-loop path actually used? | Gate usage and human response time land in an acceptable band |
| M3 | Hosted multi-tenant store, billing, team permissions | Will anyone pay for coordination alone? | First paid conversions with clear renewal intent |

## 10. Risks

| Risk | Assessment | Response |
|---|---|---|
| Upstream agent vendors ship this natively | The existential one | Bet on cross-vendor neutrality — vendors will only coordinate within their own ecosystem |
| "I'll just write a script" | Legitimate: the local case really is ~100 lines | Value must concentrate where scripts fail: multi-machine, teams, console, audit, crash recovery |
| Multi-agent coordination is a fake need | Plausible if single-agent capability keeps improving | M0 gates on dogfooding — if we don't want to use it, stop |
| Advisory leases are too weak | Agents can ignore them | Optional git pre-commit guard as second line; no kernel-level enforcement |

## 11. Open questions

- Workspace-to-repo binding granularity.
- Default lease TTL.
- Open-source the client? Leaning yes for the CLI, no for the server.
- Cold-start distribution: OSS traction or MCP ecosystem?

## Invariants belong under the store interface

Found the hard way. The rule "opening a gate blocks its thread, deciding it
unblocks the thread" was originally implemented in the CLI, immediately after the
store call. It passed every test, because every test drove the CLI.

Then the browser console got an Approve button. It called `DecideGate` directly, as
any client would, and the gate was marked approved while the thread stayed
`blocked` forever — the exact silent stall this project exists to prevent, shipped
by the tool meant to reveal it.

The fix was not to duplicate the rule in the frontend. It was to move the
transition inside `CreateGate` and `DecideGate`, under the lock, below the `Store`
interface. Any client that can reach the store now gets the invariant whether it
remembers to or not.

The rule this establishes: if a statement is true of the workspace rather than of a
particular UI, it belongs beneath the store interface. A second client is the test
that finds out. This applies directly to the M1 MCP layer, which will be a third
client and must not need to re-implement anything.

A related note on ownership: approving a gate resumes the thread as the agent that
asked, not the human who approved. A human pressing a button is not volunteering to
do the work, and a board that claims otherwise is lying about who is responsible.
