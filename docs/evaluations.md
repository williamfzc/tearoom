---
type: evaluations
title: Evaluation set
description: The acceptance journeys, their staged proof, and supporting hand-run evidence.
tags: [evaluations, stories, testing, registry]
timestamp: 2026-11-07
---

# Evaluation set

This is the proof registry for [product.md](product.md). A story is `open`
while any mapped evaluation is planned and `proven` only when every mapped
evaluation ships. Evaluation status is `planned` or `shipped`.

The acceptance journeys are defined in [domain/user-stories.md](domain/user-stories.md).
Storage, concurrency, and restart behavior are requirements and test
conditions rather than additional stories. The hc-backed collaboration
journeys remain unproven.

## Staged proof

### Foundation

Evaluations E1 and E2 are shipped and run in `make check`. They prove the
mechanics the journeys rely on:

1. Rooms form a tree: children record their parent, child lists are derived
   at read time, and each Room's stream is independent and dense.
2. Multiple writers append concurrently and the Store issues one dense
   sequence per Room; an idempotent retry returns the original record.
3. A caller reads from the beginning, an explicit `after` boundary, a tail,
   or a subtree without changing Room state.
4. A later process replays the same history after the earlier process and
   service have stopped; no session or cursor files exist anywhere.

The foundation does not prove that a real Agent can work from the tree alone.
That requires the acceptance journeys.

### The acceptance journeys

The stories need real Agent runs and cannot run in `make check`; the hand-run
method is recorded under [Hand-run evidence](#hand-run-evidence). Each journey
is open until it has run against the Room message model and its evidence row
exists. Individual hc-backed builder–reviewer runs have passed; the full
journeys and their remaining cases are still open.

## Stories

| ID | Story | Status | Stage | Defined in |
|---|---|---|---|---|
| S1 | One Agent completes one task | open | acceptance | [user-stories.md](domain/user-stories.md#s1-one-agent-completes-one-task) |
| S2 | Builder and independent reviewer share a project Room | open | acceptance | [user-stories.md](domain/user-stories.md#s2-builder-and-reviewer-share-a-project-room) |
| S3 | A new process resumes work later from Room history | open | acceptance | [user-stories.md](domain/user-stories.md#s3-a-new-process-resumes-later) |
| S4 | A mention reaches its intended collaborator through hc | open | acceptance | [user-stories.md](domain/user-stories.md#s4-a-message-reaches-its-intended-collaborator) |
| S5 | An Agent discovers collaborators and chooses whom to mention | open | acceptance | [user-stories.md](domain/user-stories.md#s5-an-agent-discovers-who-can-help) |
| S6 | Shared Room conversation supports broadcast visibility and targeted wakes | open | acceptance | [user-stories.md](domain/user-stories.md#s6-a-room-behaves-like-a-group-conversation) |
| S7 | Builder and reviewer use mentions for review and revision in one Room | open | acceptance | [user-stories.md](domain/user-stories.md#s7-review-and-revision-continue-in-one-room) |
| S8 | Pending delivery recovers after runtime interruption | open | acceptance | [user-stories.md](domain/user-stories.md#s8-pending-work-survives-runtime-interruption) |
| S9 | Agents in separate Rooms coordinate through their common parent Room | open | acceptance | [user-stories.md](domain/user-stories.md#s9-separate-rooms-coordinate-through-their-common-parent) |
| S10 | Reusable role definitions can be configured in Rooms with separate hc panels | open | acceptance | [user-stories.md](domain/user-stories.md#s10-reuse-role-definitions-across-rooms) |
| S11 | An Agent chooses a configured role and suitable Agent type | open | acceptance | [user-stories.md](domain/user-stories.md#s11-choose-a-collaborator-by-role-and-agent-type) |

## Evaluations

| ID | Evaluation | Target | Script | Status |
|---|---|---|---|---|
| E1 | Foundation room-tree contract through the shipped binary: init with parents, per-room dense sequences, concurrency, idempotency, tail, subtree, tree, missing signature, and the HTTP leg | `make target-room` | [target-room.sh](../scripts/target-room.sh) | shipped |
| E2 | Replay across real processes and service restarts with no stored read progress | `make target-replay` | [target-replay.sh](../scripts/target-replay.sh) | shipped |
| E3 | The original journeys S1–S3 with real, independent Agent products | hand-run | — | planned |
| E4 | Room message delivery and explicit mention through hc, including a worker started when the target Agent is not already running | hand-run | — | planned |
| E5 | Reuse role definitions across Rooms, configure roles, inspect role-to-agent assignments, and keep each Room-role on its own hc panel | hand-run | — | planned |
| E6 | End-to-end builder/reviewer collaboration in one Room and branch coordination through a common parent Room | hand-run | — | planned |
| E7 | Recovery after stopping the runtime at delivery and execution boundaries; reconcile uncertain work before retrying | hand-run | — | planned |
| E8 | Agent discovery across multiple Agent types and explicit selection of a configured Room role | hand-run | — | planned |

## Story → evaluation

| Story | Evaluations |
|---|---|
| S1 | E1, E2, E3 |
| S2 | E1, E3, E4, E6 |
| S3 | E2, E3 |
| S4 | E4 |
| S5 | E5, E8 |
| S6 | E4 |
| S7 | E4, E6 |
| S8 | E7 |
| S9 | E6 |
| S10 | E5 |
| S11 | E5, E8 |

## Evaluation → story

| Evaluation | Stories |
|---|---|
| E1 | S1, S2 |
| E2 | S1, S3 |
| E3 | S1, S2, S3 |
| E4 | S2, S4, S6, S7 |
| E5 | S5, S10, S11 |
| E6 | S2, S7, S9 |
| E7 | S8 |
| E8 | S5, S11 |

## Hand-run evidence

Evidence that cannot run in `make check` is recorded here or it never ran.

### Hand-running the acceptance journeys

The acceptance claim needs real, independent Agent products. The recorded
runs follow this method:

1. Setup only: a human or an Agent initializes the project Room and any child
   Rooms needed for S9. Configure the chosen reusable roles and their Agent
   types through the intended CLI. Each Room-role must have its own hc panel.
   Do not place record content in an Agent's launch prompt; prompts are the
   audit trail for the relay count.
2. Start the builder and reviewer as distinct roles in the same project Room.
   The reviewer reads the shared Room history, is explicitly mentioned, and
   writes its verdict and evidence in that same Room. A requested revision is
   addressed to the builder there. For S9, use two child Rooms and their common
   parent as the shared coordination Room; do not write directly across
   sibling Rooms.
3. For S10 and S11, reuse a role definition in more than one Room, configure
   multiple Agent types, then ask an Agent to inspect the configured roles and
   select a target. Verify each Room-role uses an independent hc panel.
4. S1 runs one Agent to completion with zero interventions. S3 starts a fresh
   process a day later, optionally with a different tool, and asks the four
   resume questions. S4, S6, and S8 exercise online and offline mentions,
   broadcasts, and runtime interruption/reconciliation.
5. Record setup effort, human relay count, Room and panel mapping, delivery
   observations, recovery quality, and verdict evidence in the table below.

A scripted stand-in for an Agent or runtime is not acceptance evidence. For
S4–S11, record the Room messages, configured role list, role-to-Agent-type
mapping, hc panel identities and lifecycle events, prompts, and final Room
records needed to reconstruct routing and recovery. Include a run where a
target is offline and a run where the runtime restarts with work pending. The
proposed broadcast policy is that ordinary messages remain visible but do not
start every offline Agent; explicit mentions reach the named role. Role
configuration is routing context, not a permission or identity-verification
boundary. The 2026-11-07 file-based wake feasibility row below is background
only and does not prove this hc-backed design.

A scripted stand-in for the second product collapses the proof into E1/E2
and is not acceptance evidence.

| Date | Evaluation | Cases | Outcome | Record |
|---|---|---|---|---|
| 2026-11-08 | Release archive determinism | Two cold `make release-target RELEASE_TARGET=darwin-arm64` release builds of the Rust binary with one commit and source epoch, then `cmp` of the two archives | Passed for darwin-arm64: byte-identical archives from independent cold builds. The release workflow's native double-build compare is the same proof on every target; the other three targets remain unproven until a tagged release runs. | Local run 2026-11-08; `dist/det-one` vs `dist/det-two`, both deleted after the comparison |
| 2026-11-08 | Go↔Rust format-4 differential | `TEAROOM_GO_BIN` pointing at the final Go build, `cargo test --test go_compat -- --include-ignored` — keyed JSON appends with HTML-sensitive characters and U+2028 interchanged between the Go and Rust binaries in both directions | Passed: both binaries read each other's keyed records and return the original for identical retries. Hand-run because it needs the prebuilt Go baseline (the test is ignored in `make check`). | Local run 2026-11-08; Go baseline built from the tree preserved at tag `go-final` |
| 2026-11-08 | E3/E4/E6 Rust prototype Pi–Claude handoff | Isolated Rust snapshot, hc 0.1.18, Pi 0.73.1 using GLM-5.3-Flash low and Claude Code 2.1.319 in separate panels | Passed this single unattended refactoring and review journey: Pi refactored CLI argument parsing and the Rust dispatcher delivered the review request to Claude, which inspected the change and recorded PASS. Pi ran 23 unit and 4 CLI tests; Claude independently ran 21 unit and 4 CLI tests, excluding two Unix socket tests blocked by its macOS sandbox. External integration tests remained ignored in this run. Claude's displayed model did not establish its actual backend. | `[local test artifacts]`, `project#1`–`project#4`, both hc panel transcripts |
| 2026-11-08 | E3/E4/E6 Tearoom repository Pi–Claude refactor | Isolated snapshot of this repository; Pi builder and Claude Code reviewer on separate hc panels; Go module and build caches prefilled inside the snapshot, local loopback enabled for tests | Passed this build-and-review journey without human relay or approval after the task: Pi fixed `read --tail 0`; Claude independently ran the complete Go tests, checked the diff, exercised the binary boundaries and reproduced the old bug, then recorded PASS in `implementation#4`. Its earlier `#3` reply was only a probe, but the dispatcher treated it as delivery completion; a matching reply is not a verified verdict. The Claude banner displayed an unrecognized model, so its actual inference model is unconfirmed. | `[local test artifacts]`, Room records, both hc screens, and `fix.diff` |
| 2026-11-08 | E3/E4/E6 Tearoom repository first attempt | Same task in an earlier isolated snapshot with separate Pi and Claude panels | Not passed: automatic handoff and static review worked, but Claude could not access its Go module cache or proxy and recorded BLOCKED. After the verdict, the builder was stopped before modifying a Go cache outside the snapshot; the continuation was not unattended. | `[local test artifacts]` |
| 2026-11-08 | E3 same-day fresh-process resume probe | A fresh Pi 0.73.1 process, BigModel GLM-5.3-Flash low, no saved session or context-file discovery, read-only tool and only the completed project Room stream as source | It recovered the goal, builder's work, review verdict and evidence, and next step. This was the same day as the work and therefore does not satisfy S3's next-day requirement. | `[local test artifacts]`; `project#1`–`project#3` |
| 2026-11-08 | E3/E4/E6 Pi–Claude unattended handoff | Isolated Go project; hc 0.1.18, prestarted Pi 0.73.1 builder and Claude Code 2.1.319 reviewer in separate panels, with trust and per-session sandbox configured before the goal | Passed for this single build-and-review journey: Pi refactored and handed off through the Room; Claude independently ran `go test ./...` (`ok sample 0.345s`), checked formatting and the diff, and recorded APPROVED. No human relay or approval occurred after the goal. The Claude banner displayed an unrecognized model, so the actual inference model is unconfirmed. This does not establish next-day resume, broader recovery, or two-week acceptance. | `[local test artifacts]`; `project#1`–`project#3` and both hc panel transcripts |
| 2026-11-08 | E3/E4/E6 Pi–Claude independent review | Isolated Go module with prestarted Pi 0.73.1 builder using GLM-5.3-Flash low and separate Claude Code 2.1.319 reviewer using per-session sandbox and `dontAsk`; no manual relay after the goal | Automatic handoff and independent static review passed, confirmed against both hc panel transcripts. Full acceptance did not pass: Claude could not rerun `go test ./...` because the host's goenv cache was outside the sandbox and a shell GOCACHE assignment was denied. The Claude banner displayed an unrecognized model despite a Sonnet argument; actual model is unconfirmed. | `[local test artifacts]`; Room `project#1`–`project#3` and separate hc screens |
| 2026-11-08 | Claude sandbox Go cache probe | Separate Claude Code session with sandbox and `dontAsk`; prepend the installed Go binary directory to PATH and set GOCACHE inside the disposable project via per-session settings | `go test ./...` completed with `ok sample 0.357s` and no permission prompt. The original goenv shim overrode GOCACHE; this is an environment-specific probe, not a full Room acceptance run. | Isolated hc session `tg`; settings under `[local test artifacts]` |
| 2026-11-08 | E3/E4/E6 Pi–Claude Room acceptance | Isolated Go fixture, hc 0.1.18, Pi 0.73.1 using GLM-5.3-Flash at low thinking, and a separate Claude Code 2.1.319 reviewer with per-session sandbox settings; setup included prestarting and trusting the two panels | Not passed. Pi completed a refactor and `@reviewer` was delivered; Claude's combined shell command requested permission, so no independent verdict was recorded. No approval or relay occurred during the task. The fixture also lacked a Go module and Claude's displayed model did not match the requested Sonnet alias. | `[local test artifacts]`; Room `project#1`–`project#2` and hc screens |
| 2026-11-08 | Claude denied-command recovery probe | In the same isolated fixture, launched Claude Code 2.1.319 with per-session sandbox and `--permission-mode dontAsk`; asked it to execute the previously blocked compound command and, if denied, try simpler read-only commands | The compound command was denied without a prompt. Claude ran separate `git diff --check`, `ls`, `gofmt -l .`, and `go version` and reported the outcomes without disabling the sandbox. This is a command-recovery probe, not a complete Room handoff. | Isolated hc session `td`; no global settings changed |
| 2026-11-08 | Agent guide setup smoke | Extracted the setup commands from `tearoom skill`, excluding hc status and the long-running dispatcher, and ran them in a new temporary workspace with an explicit Pi executable and extension path | Passed: Room creation, sandbox settings creation, both role definitions and Room bindings succeeded. This does not exercise Agent startup or the collaboration journey. | `tearoom skill` output; local temporary workspace |
| 2026-11-08 | Claude sandbox permission probe | Claude Code 2.1.319 in an isolated hc session, with per-session sandbox enabled, automatic approval for sandboxed Bash, and unsandboxed retry disabled; ran combined git status/log and Go test, then attempted a temporary write in the home directory | The combined commands and test completed without a prompt; the home-directory write failed with `Operation not permitted` and no file was created. This tests one permission configuration, not the complete Room collaboration journey. | Local isolated hc session `tp`; disposable settings under `[local test artifacts]` |
| 2026-11-08 | E3/E4/E6 exploratory hc pilot | Temporary Go project, hc 0.1.18, Codex 0.165.0 builder and Claude Code reviewer; a Room record requested a normalization refactor and independent review | Not passed. The first quiet-ready launch sent input to a Codex update prompt; the update was stopped and Codex remained 0.165.0. After manual panel setup and a fresh delivery state, Codex read the Room, refactored, ran `go test ./...` and recorded a handoff; Claude was reached but blocked on permission to read the Room. Manual setup and intervention mean this is not unattended acceptance evidence. | Local disposable pilot, `project#1` and `project#2`; no review verdict |
| 2026-11-08 | E3/E4/E6 second hc pilot | Separate temporary Go project; prestarted Codex 0.165.0 and Claude Code 2.1.319 panels, with startup trust handled and narrowly scoped command permissions | Not passed. Codex read the Room, refactored and tested, then a Room mention automatically reached Claude, which inspected the code. Claude's `go test ./... 2>&1; echo ...` combined command triggered a permission prompt despite the test-command allowance; no verdict was recorded. No permission was granted during the run. | Local disposable pilot, `project#1` and `project#2`; review blocked before `project#3` |
| 2026-11-08 | E4 third hc pilot | Separate temporary Go project with both panels prestarted; Codex received a Room task to refactor and hand off to Claude | Not passed. Codex completed the refactor and wrote `@reviewer` in its reply, but did not specify `--to`; the CLI did not encode a target, so no reviewer delivery was created. The run was stopped at the user's request to limit Codex use. | Local disposable pilot, `project#1` and `project#2`; no reviewer delivery |
| 2026-11-08 | E4 Pi and Claude pilot | Temporary Go project; Pi 0.73.1 with BigModel GLM-5.3-Flash at low thinking, Claude Code 2.1.319, both prestarted in hc 0.1.18 | Not passed. Pi refactored and tested, then a natural-language `@reviewer` automatically reached Claude. Claude requested permission for a combined `git status` and `git log` command outside the preconfigured allowlist; no verdict was recorded and no permission was granted during the run. | Local disposable pilot, `project#1` and `project#2`; review blocked before `project#3` |
| 2026-11-08 | E3/E4/E6 Pi and Claude role-independence pilot | Temporary Go project with prestarted Pi builder and Claude reviewer; verify the actual panel transcripts against Room records | Not passed. Pi refactored, tested and mentioned reviewer, but then wrote records `project#3` and `project#5` itself using the reviewer signature. Claude was separately delivered the task but blocked on a permission prompt. The Room falsely appeared to contain an independent verdict; its unverified signature could not establish authorship. | Local disposable pilot, `project#1`–`project#5`; Pi terminal history shows the reviewer-labelled sends |
| 2026-11-07 | PostgreSQL battery on the format-3 tree schema | `TEAROOM_TEST_PG` against a temporary local PostgreSQL cluster: the conformance battery over the room tree (named rooms under parents, per-room dense sequences, concurrent append, concurrent idempotency, read boundaries, content compaction, missing signatures) with each subtest isolated by a name prefix in the shared database | All green on the format-3 SQLite/PostgreSQL schema; the SQL adapters were removed the same day by the JSONL cutover, so this row is the closing validation of that era | Executable against the format-3 code (`archive/v1` descendants); the adapter no longer ships |
| 2026-11-07 | E4 wake chain feasibility (v1 file stand-in for the Room) | herdr 0.9.5 with codex 0.159.2 in an isolated session: (1) start an Agent that reads the room, builds and verifies a script, writes the conclusion back; (2) a second prompt continues from the prior record; (3) a brand-new workspace, a fresh Agent given only the room path answers goal, prior work, acceptance, next step; (4) an external append is detected by a polling script that drives `herdr agent prompt` and the Agent wakes, reads, and writes back (~18 s) | All four experiments passed; the wake chain — start, prompt, wait — is reliable, and a process with no history recovered the full state from the room alone. Preconditions recorded: each tool's one-time trust dialogs and login must be pre-cleared, and the local `claude` binary authenticates through the LLMBox wrapper | Design-note experiments 1–4; the waker contract is `tearoom watch --json` plus an external script |
| 2026-09-10 | Live hosted journey (v1 contract) | Render web service `redacted.invalid` over Neon PostgreSQL 18: owner-created room, per-Actor credentials, the four-step continuation with token-free open reads, and the revocation matrix | Four steps closed over the public internet with zero manual relays under the v1 membership model; superseded by the 2026-11-07 convergence | [log.md](log.md) deployment record |
| 2026-09-10 | PostgreSQL validation (gated battery) | `TEAROOM_TEST_PG` against local PostgreSQL 16.15: the conformance battery over the v1 schema plus credential control | All green on the v1 schema; the battery must be rerun against the format-3 tree schema | Executable: `go test ./internal/relational/ -run TestPostgreSQL` with the env set |
| 2026-09-09 | Cross-product continuation (v1 contract) | ZCode as `agent-a` and headless Claude Code as `agent-b` over loopback HTTP with explicit joins: A appended a work contract; B recovered it with `read`, implemented and tested, appended the result; the server restarted; A pulled the result from an explicit boundary and accepted | Four steps closed with zero manual relays and survived a restart under the v1 model | Method above, adapted to the v1 join flow |
| 2026-09-02 | Foundation rival (v1 contract) | Incremental reads, per-participant boundaries, replay, activation, and concurrent append compared with a plain JSONL file | The file matched the v1 foundation claims; the cross-product continuation story remained unproven | [lessons/rival-falsification.md](lessons/rival-falsification.md) |

## Pending evidence, not stories

| Item | Evidence needed |
|---|---|
| Cheapest-rival comparison | Run the same acceptance journeys through a shared JSONL file, normal chat, repository document, and one vendor transcript |
