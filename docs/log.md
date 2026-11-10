---
type: log
title: Change log
description: Dated record of product decisions and structural changes, newest first.
tags: [log, history]
timestamp: 2026-09-10
---

# Change log

## 2026-11-10 — Put the Rust package at the repository root

Tearoom has one Rust package. Its manifest, source and tests now live at the
repository root, using the usual Cargo layout. Build, release and documentation
references follow the move; Room data and behavior are unchanged.

## 2026-11-10 — Deliver Room updates without directing Agent work

Explicit recipients now wake roles; a name in message text alone does not.
Wakes carry new Room records, while role instructions are sent on the first
wake and after explicit replacement. A finished turn releases the next queued
delivery without requiring a prescribed reply. A missing previously used panel
requires reconciliation instead of silently creating a new conversation. See
[collaboration delivery](architecture/collaboration.md).

## 2026-11-10 — Keep Agent context and decisions with the Agent

Tearoom owns Room routing, delivery and role lifecycle, not an Agent's context
management or work decisions. Wakes should carry new Room messages directly;
the Agent can read more history when needed. The current prompt and queue gaps
are recorded in [status](architecture/status.md#open-work), and the boundary is
in [product](product.md#conversation-and-runtime-policy).

## 2026-11-10 — Keep dispatch running during a short lock collision

Manual delivery operations and dispatcher scans share a state lock. A scan
previously treated a busy lock as fatal and stopped the dispatcher, leaving
later handoffs undelivered. A busy scan now waits for its next iteration;
other errors still surface. A separate lifetime lock refuses a second
dispatcher, since the short-lived state lock alone did not establish a single
running process. The contracts are in [collaboration delivery](architecture/collaboration.md#delivery-and-recovery)
and [storage layout](reference/storage-layout.md#the-files).

## 2026-11-10 — Refuse unproven conversation recovery

A static continuation command was implemented and then removed. It cannot name
the per-instance session that `--resume <id>`-style Agents require, and every
"continue the most recent conversation" flag (`claude -c`, `codex resume
--last`, `pi -c`, bare `dsh tui --resume`) selects by recency in the working
directory. Several Room-roles share one workspace, so a restarted role could
attach to another role's conversation while appearing to continue its own.
That is worse than a fresh start, which at least has the Room records available.
At that point Tearoom started the configured command after a lost panel and
claimed no continuity. The later delivery change above instead leaves a
previously used missing panel for explicit reconciliation.
Reopen when an Agent-independent way to bind and verify a conversation
identity exists and is proven with two roles in one workspace; per-Agent
session flags alone do not cover every Agent used here. The contract is in
[collaboration delivery](architecture/collaboration.md#conversation-continuity-and-replacement)
and [cli.md](reference/cli.md#delivery-boundary); evidence is in
[evaluations](evaluations.md).

## 2026-11-10 — Replace a Room-role panel explicitly

`tearoom collab restart` stops one Room-role panel and starts its configured
command again, refusing while the panel is working and refusing, without
`--confirm`, while a submitted or uncertain delivery still needs
reconciliation. It is also how a changed role command reaches a running role.
Tearoom still stores no conversation identifier and resumes nothing: a
replacement starts the configured command as a new conversation, and a new
Agent rebuilds context from Room records. This refuses a hidden panel swap
during unreconciled work, and it refuses to promise continuity Tearoom cannot
observe.
A terminal UI can report `idle` before its own setup ends, so the first
delivery to a replaced panel may be uncertain; that limit is recorded rather
than papered over. The contract is in
[collaboration delivery](architecture/collaboration.md#conversation-continuity-and-replacement)
and [cli.md](reference/cli.md#delivery-boundary); evidence is in
[evaluations](evaluations.md).

## 2026-11-10 — Continue a Room-role conversation across wakes

The owner chose a continuing Agent conversation per Room-role instance: the
first wake creates it, later wakes deliver to it, and busy work queues. A panel
is not the conversation itself. Replacing a panel must not silently replace
the conversation; an intentional fresh conversation is explicit and follows
reconciliation of pending work. This refuses treating every wake or missing
panel as a fresh conversation. Reopen if a concrete Agent cannot resume or a
replacement workflow needs a different recovery contract. The intended behavior
and current gap are in [collaboration delivery](architecture/collaboration.md#conversation-continuity-and-replacement)
and [status](architecture/status.md#open-work); this decision adds no product entity.

## 2026-11-10 — Add a loopback browser face, not a hosted service

A person asked for a small local browser interface over the existing Room
tree: navigate Rooms, read their history, and send messages with explicit
`@role` addresses. `tearoom web` now serves that view from the same binary and
workspace. The page reads through the Store seam and appends through the same
message composition as `send`, so address resolution, content encoding, and
record ownership keep one implementation. It adds no product entity, stored read
cursor, or write path.

This does not restore the retired hosted HTTP face or reopen the hosted
control-plane refusal. The listener binds only `127.0.0.1`; requests with a
non-loopback `Host` are refused, the page carries a per-process random token
for API calls, and cross-origin writes are refused. The page has a restrictive
Content Security Policy and no CORS header. The contract is in
[reference/cli.md](reference/cli.md#local-browser-interface), and the black-box
`cli_web` test in `make test` proves the room state, history, addressed append,
and rejected host/origin/token cases. The visual surface is deliberately
plain: a neutral canvas, system UI type, a room rail, grouped messages, and a
compact composer. Decorative variants were rejected; conversation remains the
main surface.

## 2026-11-10 — Delete a leaf Room, and say "init" when creating

`room delete --room <name> --confirm` removes a leaf Room with its record
stream, roster, and Herdr binding, refusing children and unresolved
deliveries so irreversible loss is always a confirmed act. Creation verbs are
now uniform: `role define` became `role init` with no alias, per the
clean-break policy. The contract is in
[reference/cli.md](reference/cli.md#room-lifecycle).

## 2026-11-09 — Give each Room its own Herdr session

Each Tearoom Room should open in its own named Herdr session, with that Room's
role panels as tabs inside it. A shared session becomes cluttered when it
contains the workspaces for many Rooms, and asking people to navigate opaque
workspace and tab IDs exposes Herdr's layout instead of the Room they want.
This is now implemented with separate local sessions. Implementation status and the current contract are
in [agent-integration.md](reference/agent-integration.md#herdr-session-per-room).
Revisit the mapping only if real use shows that room-level sessions make
opening, resuming, or cross-Room work harder.

## 2026-11-09 — Retire unused role definitions safely

Role definitions can be removed after Room assignments and unfinished
deliveries are resolved. Deleting a definition neither stops an existing Agent
nor removes Room records. The command and its checks are in
[reference/cli.md](reference/cli.md).

## 2026-11-09 — Explicit recipients take precedence

When a message supplies explicit recipients, only those roles receive it;
otherwise body mentions choose recipients. This replaces the earlier union,
which could wake a person mentioned only as context. The current contract is
in [reference/cli.md](reference/cli.md). Existing records retain their stored
recipients; changing the rule does not rewrite history.

## 2026-11-09 — Keep the everyday journey small

The main journey is to prepare reusable roles as needed, create a Room, and
work and review in its shared conversation. Preparation remains editable.
Room moves and other lower-frequency operations are not prerequisites for
that journey; their existing implementation remains documented in the CLI
reference while their place in a simpler command surface is reconsidered.
Revisit the choice when a concrete reorganization need is demonstrated.

## 2026-11-09 — Repository work does not require self-hosting

Developing Tearoom no longer requires its own Room workspace. The repository
and documentation carry the working context; any local workspace is preserved
but is not a prerequisite. Revisit self-hosting when it has a concrete benefit
and its workspace can be used safely with the current release.

## 2026-11-09 — Status is not a CLI concept

Three surfaces used "status" for different things: a Room overview, a roster
flag, and a delivery ledger. The overview command is gone; `room roles
--observe` asks hc for observed panel state, and `collab deliveries` prints the
durable delivery ledger. Observation is an explicit flag; a ledger is a noun.
The command contract is in [reference/cli.md](reference/cli.md).

## 2026-11-09 — Remove the template launch concept

Roles are Tearoom's entity; the optional hc-template field imported a second
launch concept and duplicated role instructions. A role now launches only its
`command`; hc's interactive-start-and-wait-for-status path remains. The earlier
template-based acceptance evidence is withdrawn, and the direct-command
cross-product rerun is pending.

## 2026-11-09 — Move Rooms without deleting their history

The owner clarified that reorganizing work means moving a Room, not deleting
it. A move changes only its parent; names, records, descendants, role bindings,
and quoted `room#seq` references stay intact. Subtree reads follow the new
position. This settles the earlier open question about moves; renaming remains
open because it would change quoted addresses. The command contract is in
[reference/cli.md](reference/cli.md).

## 2026-11-09 — Keep the project's own workspace local

The project's own `.tearoom/` workspace is no longer committed: the owner
asked for the unpublished history to be rewritten without it and the
directory gitignored. This supersedes the 2026-11-07 decision to carry the
room tree in git; the layout capability stands (a workspace can still be
committed and travel with a checkout), but this repository keeps its working
state on the machine. The unpushed commit range was filtered to remove every
`.tearoom` path, the three record-only commits became empty and were pruned,
and `go-final` was re-tagged on the rewritten commit. Reopening condition:
when the room memory must travel with clones again, the owner re-decides.

## 2026-11-09 — Two concepts: remove the notification and HTTP faces

The kernel is now exactly two concepts: Room and Record. The third concept —
the notification envelope — had no consumer: the collaboration dispatcher
scans Room records from its durable boundary instead, `watch` had no test, no
gate coverage, and no caller besides the retired herdr waker, and the
loopback HTTP face served every recorded Agent journey zero times while the
same journeys ran through the CLI in one checkout. The 2026-11-07 design made
"notify externally, wake elsewhere" the wake contract; the 2026-11-08 hc
integration became the real wake path, and what remains of that old contract
— "observe by reading" — is now stated plainly: later records are observed by
reading again from a remembered boundary.

Removed: the `watch` command and its envelope, the `server start` command,
the `TEAROOM_SERVER` client mode, the transport layer (`rust/src/transport.rs`
and its dependencies), `scripts/waker.sh`, and the serving and HTTP reference
pages. E1 and E2 lost their HTTP legs — every other check stands, and E2's
restart leg became a plain cross-process leg. The reopening condition is a
real integration that cannot live as a caller-side read loop: if a second
consumer of record streams needs a served or pushed interface, the case goes
through product.md first.

## 2026-11-08 — Port the kernel to Rust and retire the Go implementation

The shipped binary is now built from `rust/` with Cargo. The format-4
room-tree contract is unchanged — E1 and E2 run through the shipped Rust
binary, and a hand-run differential proved keyed JSON interoperable in both
directions against the final Go build ([evaluations.md](evaluations.md)).
The move retires the Go toolchain traps (macOS test linking, the module
proxy), lets each release target build on its native runner, and drops the
pure-Go SQLite driver entirely.

Two refusals come with it. First, the automatic format-2/3 → 4 migration is
gone: the Rust store refuses legacy markers on open without touching the
database, because an automatic migration could run while an old process is
still writing; migrating such a workspace is a one-time operator step with
the final Go release, preserved at tag `go-final`. Second, the Go
implementation leaves the tree rather than staying as an unbuilt parallel:
it remains reachable at `go-final` and serves as the prebuilt baseline for
the ignored `go_compat` differential test. The reopening condition for both
is a real workspace that must cross the boundary — the Go-era release
performs the migration; nothing here blocks it.

## 2026-11-08 — Retain unverified role signatures

The user chose not to restrict signatures after the role-independence pilot
showed an Agent can write under another role's label. Signatures remain
unverified; independent review therefore requires evidence beyond the label.
The failed pilot is recorded in [evaluations.md](evaluations.md).

## 2026-11-08 — Propose a recoverable hc delivery boundary

Recorded the local role-to-panel and directed delivery proposal in
[architecture/collaboration.md](architecture/collaboration.md). Room history
remains the source of messages; hc provides terminal execution, while a local
integration scans and reconciles delivery. The design refuses to treat a
terminal send as task completion or promise exactly-once execution. Encoding,
agent readiness and interrupted delivery still need real validation.

## 2026-11-08 — Put collaborators together in their Room

A Room is the shared working conversation, not a role-specific lane. Its
configured collaborators may include implementers and independent reviewers;
they exchange records and directed mentions in that Room. Reusable role
definitions are separate from Room participation, and each Room-role binding
has its own hc panel. Coordination between distinct Rooms takes place in a
common ancestor Room. This supersedes the earlier requirement that review occur in a
separate Room, while retaining review by a different Agent. The product
contract and outstanding runtime details live in [product.md](product.md) and
[domain/open-questions.md](domain/open-questions.md).

## 2026-11-07 — Design Room collaboration as group chat

The product direction now includes discoverable Room collaborators, broadcast
and directed messages, and hc as the Agent runtime and notification delivery
infrastructure. This supersedes the earlier refusal of @-addressing; it does
not restore admission or turn a signature into verified identity. A logical
Room collaboration surface is distinct from durable Room history. Delivery,
wake defaults, routing and recovery remain design questions, not shipped
capabilities. The canonical direction and proof obligations are linked from
[product.md](product.md) and [evaluations.md](evaluations.md).

## 2026-11-07 — Store the room tree as JSONL files, and let the project live in its own tree

The SQLite backend lived for one day and one purpose: prove the tree
contract on a transactional store. It did, and then the storage requirement
changed shape — the memory must be able to travel with git. One workspace is
now one `.tearoom` directory of plain files: `rooms/<name>/room.json`
(identity and the single parent link, omitted for a root),
`records.jsonl` (the stream, one JSON record per line, the same shape the
CLI prints), and `idempotency.jsonl` (the per-(author, key) retry ledger),
under a `store.json` marker at format 4. Appends serialize through one
flock on `.tearoom/lock` across room verification, idempotency resolution,
sequence assignment, and the append — the v0 file Store's proven pattern,
now carrying the tree. Reads take the shared lock and stay pure.

The boundary that follows is stated in the storage layout: sequence
guarantees hold within one checkout. Two clones that both append reconcile
like any other merged text.

`internal/relational`, the PostgreSQL adapter, and the gated battery were
removed with the SQL era (their green runs are recorded in
[evaluations.md](evaluations.md)); gorm and pgx left go.mod, and the
pure-Go SQLite driver survives only inside the one-time format-2/3 → 4
migrations, which read the retired databases once, write the JSONL layout,
delete the database, and rewrite the marker.

The same decision turns the tool on itself: the repository carries its own
workspace in `.tearoom/` — root Room for goal and acceptance, children for
implementation and review — committed to git, and AGENTS.md now points
contributors at the tree before they work. The `.gitignore` rule that
banished `.tearoom/` from version control was written for the old model and
is gone with it.

## 2026-11-07 — Converge the kernel: one room tree, one record stream, one signature

Retired the v1 model — Room + Actor + membership + credentials + hosted
service — and adopted the converged kernel: a single-parent room tree, one
append-only record stream per Room, and an unverified signature label. The
trigger was a design audit that found the coordination story living outside
the data model: agents collaborated through prompts and transcripts while the
Room only stored text, and every addition (admission, recipients,
credentials, a hosted control plane) guarded a boundary nobody had crossed.

What replaced what: membership/admission died (the workspace directory is the
trust domain); the `to` recipient field died (no @-addressing); credentials
and the hosted multi-room profile died (serving beyond one machine is a
future need); actor identity became a plain `author` label; status, closure,
and handoff became writing conventions. The tree arrives: each Room records
its parent, child lists are derived at read time, and each Room owns one
dense stream — a child's first record is seq 1.

Storage moves to format 3: one workspace database holds the whole tree
(`rooms.name/parent/head`, `events.room_id/seq/author/at/content`, the
per-(room, signature, key) idempotency ledger). The format-2 → 3 migration is
one-time on open: the old single room becomes a root named by its old
identifier and every author is kept verbatim; there is no compatibility
layer after it. The v1 code is frozen at tag `v1-final` on branch
`archive/v1`; E7 and the Render/Neon deployment files were removed with the
hosted profile; the PostgreSQL battery stays and must be rerun against the
format-3 schema.

Feasibility for the external half was measured the same day (herdr 0.9.5 +
codex 0.159.2, four experiments, all green — see
[evaluations.md](evaluations.md) hand-run evidence): start, prompt, and wait
are reliable; a fresh process recovered full state from a room alone. The
wake contract is `tearoom watch --json` plus an external waker; unattended
waking requires each tool's one-time trust dialogs and login to be
pre-cleared on the host.

## 2026-09-10 — Deploy the hosted profile live on Render + Neon

The hosted profile is live on the internet: the old `tearoom` Render service
was re-adopted for the current contract (branch main, env vars replaced with
the Neon pooled DSN and a fresh owner token — the stale hosted-admin secret
died with the old contract), and the first rollout surfaced a real
operational bug: the service still carried `healthCheckPath: /healthz` while
the new binary no longer served it, so the platform probe failed and Render
kept the rejected-contract container answering. GET /healthz was re-adopted
(precedent: the pre-cutover platform check) and the rollout went live.

The live E7 journey then ran against `redacted.invalid` over Neon
PostgreSQL 18: room creation, per-Actor credentials, the four-step
continuation journey with token-free open reads, and the revocation matrix —
all green, zero manual relays. The transcript is recorded in
[evaluations.md](evaluations.md) hand-run evidence. Operational facts that
cost debugging time are in the serving runbook (removed 2026-11-09 with the
HTTP face):
the Render API requires a payment method on file even for free-service
creation (existing services are updated freely), and macOS ignores
SSL_CERT_FILE, which is why the client carries TEAROOM_CA_FILE.

## 2026-09-10 — Ship E7: remote readiness proven in make check

E7 shipped as `make target-remote`: the script starts a temporary PostgreSQL
cluster, fronts the hosted service with a standard-library TLS terminator,
and deterministically executes remote readiness — owner room creation over
https, per-Actor credentials, the four-step continuation journey through
ordinary read/send with token-free open reads, and the revocation matrix
(unauthenticated write, wrong-actor token, revoke, untouched history). The
expired token joined the negative matrix at the HTTP boundary, the gated
PostgreSQL battery and credential control ran green against PostgreSQL 16.15,
and the client gained TEAROOM_CA_FILE for private-CA TLS fronts. Remote
readiness is now a deployment gate that runs on every `make check`; putting
the hosted profile on a cloud host is operational work, not another proof.

## 2026-09-10 — Ship the remote profile: credentials, hosted service, and the served contract

Implemented the adjudicated authentication shape end to end. `internal/control`
issues write credentials for admitted actors — 192-bit opaque tokens shown
once, sha-256 at rest, room and actor binding, expiry, immediate idempotent
revocation — and the credentials table joined the format-2 schema additively.
`internal/transport` gained the exposed profile: reads stay open, writes
require a bearer whose actor is authoritative, and joins plus credential
management are owner operations; the local loopback profile keeps
self-asserted actors. `internal/hosted` serves many rooms of one deployment
database (postgres:// by DSN, SQLite for tests) behind /r/<id> URLs with
migrations on start and owner-only room creation. The CLI ships
`room credential issue|revoke`, `room init --server`, and TEAROOM_TOKEN; the
URL rule follows TLS reality (plaintext http stays on loopback). `auth login`
moved from target to reserved. The negative matrix — unauthenticated write,
wrong-actor token, revoked token, cross-room token, open read — is proven at
the control, transport, hosted, and CLI layers, and the CLI-level remote
journey runs the full loop against a hosted service.

What remains open is not code: running the gated PostgreSQL battery against a
real database, deploying the hosted service behind TLS, and executing live E7
— the four-step journey with revocation — whose transcript belongs in
[evaluations.md](evaluations.md).

## 2026-09-10 — Cut the surface and settle the authentication shape

Shrank the product to the concepts the continuation story actually uses. The
CLI is now `room init|join`, `send` (`--as`, `--json`, `--idempotency-key`),
`read` (`--after`), `server start`, and `skill`. Removed: `room members`,
the `--to` flag (the `to` Event field remains domain and HTTP vocabulary with
no CLI writer), `--limit` (callers truncate — that is what caller-owned
boundaries means), `--wait` with exit code 3 and the invocation-wait transport
path (polling is a caller loop), and the `--store` flag together with the file
backend itself. `.tearoom` is one SQLite database in WAL mode behind the
format-2 marker; a pre-cut directory is an incompatible store and rollback is
the previous release with its data snapshot. Production Go shrank by roughly
600 lines and `cli.go`'s ratchet baseline dropped 567 to 456.

Adjudicated the authentication shape for the remote profile: one authorization
model everywhere — reads are open and the Room URL is the read capability;
credentials gate writes; Room creation and credential issue/revoke stay owner
operations. A credential is an opaque random token, hashed at rest, printed
once, and distributed over the operator-controlled channel; revocation blocks
further writes without touching history. There are no credential scopes, no
read authorization, and no local login state. GitHub OAuth device flow is
reserved as a future issuance ceremony in front of the same credential model,
reconsidered only when a deployment needs identities the operator channel
cannot name. TLS remains a deployment requirement because bearer tokens ride
the write path.

E1 was rewritten over the local SQLite room plus the loopback HTTP service and
E2 over pure boundaries and restarts; both ship in `make check`.

## 2026-09-09 — Reduce the product to one CLI continuation story

Adopted one user story: Actor A writes, Actor B in another process or Agent
product pulls the Room through the CLI, B writes back, and A or Actor C pulls
the new result. Human participation and process restart are conditions of that
same loop, not separate stories. The foundation contract, remote deployment,
identity, credentials, and database parity are supporting requirements.

Rejected the share-page and context-link direction adopted earlier that day.
There is no browser replay, snapshot publication, link command, implicit Room
fork, or continuation Room. An external operator or Agent product supplies the
Room connection and starts the receiver; the receiver uses ordinary `read`
and `send` with JSON/JSONL. Local and remote profiles keep that same interface,
with remote adding only connection security and identity.

Removed `competitive-analysis.md` because its current positioning depended on
the rejected context-link thesis. The measured plain-file falsification remains
in [lessons/rival-falsification.md](lessons/rival-falsification.md); the removed
analysis and superseded decisions remain available in Git history.

## 2026-09-09 — Make the CLI the explicit top-level product contract

Separated the CLI reference into shipped and target surfaces. The shipped
section now records complete command, identity, output, wait, retry, and failure
behavior and corrects Actor selection: `room join` takes its Actor positionally,
while `room members` needs no Actor identity. The target section fixes one
command vocabulary for human login, remote Room creation, per-Agent credentials,
snapshot links, and continuation Rooms while marking every unimplemented
surface plainly.

The evaluation registry now classifies core stories, contract and stress
scenarios, distribution, remote ownership, and deferred policy. Each active
story has an expected CLI journey; individual command semantics remain owned
only by the CLI reference. This makes the command line the top-level acceptance
boundary without creating separate local and remote products.

## 2026-09-09 — Make local and remote one deployment-shaped product

Adopted one service across local and remote deployment. SQLite is the target
local database and PostgreSQL the remote database; network, identity, object
storage, and process supervision are deployment adapters. Room, Actor, Event,
commands, HTTP behavior, authorization decisions, migrations, errors, and
acceptance journeys remain shared. Local setup may bootstrap an owner but does
not define a second trust model after bootstrap.

The evaluation sequence was compacted after the retired wake gate: current
remote hardening is P2. Older entries keep their historical gate labels.

Rejected separate Local and Hosted editions, separate server entry points, and
remote-only product behavior. The current file backend remains a portable
implementation while it satisfies the same Store contract; its long-term
retention requires evidence that SQLite cannot serve that use case. Reopen the
single-service decision only if a required infrastructure difference cannot be
contained behind the existing boundaries without changing the Room contract.

## 2026-09-09 — Retire history and deferred design; consolidate the domain page

The docs tree had grown to 39 documents for a three-entity kernel: 38% of it
described things that do not exist or no longer govern, and the core
addressing invariant was restated in 15 files. Deleted, with successors:

- `architecture/activation.md` (427-line superseded design) — the refusal and
  its reopening condition live in [product.md](product.md); the design is in
  Git history.
- `architecture/model-discussion.md`, `architecture/convergence.md`,
  `operations/p0-room-cutover.md` — completed rationale and migration process;
  their surviving decisions were already in this log, and the command surface
  is owned by [reference/cli.md](reference/cli.md).
- `architecture/hosted-plan.md`, `domain/hosted-identity.md` — deferred-phase
  design; the adopted remote-facing slice and journey live in
  [product.md](product.md).
- `domain/problem.md`, `domain/boundaries.md`, `domain/security.md`,
  `domain/agent-experience.md` — merged into
  [domain/concepts.md](domain/concepts.md), which now states the addressing
  invariant once and links to it.

`architecture/status.md` shrank to current implementation state plus open
work; migration stages moved to this log. Inbound links were fixed rather
than left dangling; historical log entries that name deleted files use plain
text, not links.

## 2026-09-09 — Adopt engineering-management mechanics: budgets, adjudication, evidence

Imported four mechanics from the sibling SKILLS workspace's governance design,
adapted rather than copied. AGENTS.md now states the budget clause (every
addition names what it replaces; unsure means no change), adjudication with
reopening conditions for open conflicts, and the hand-run evidence rule: a
suite never recorded has never run. [evaluations.md](evaluations.md) gains the
hand-run evidence table, first row the 2026-09-02 falsification. New
`make line-budget` ratchet (`scripts/check-line-budget.sh` +
`scripts/line-baseline.txt`): production Go files over 300 lines fail only
when new or grown past their recorded count; the four pinned files are
`cli.go`, `contract.go`, `relational/store.go`, and `store/file.go`, and test
batteries stay review-governed. `check-docs.sh` was hardened to parse
frontmatter as real YAML, enforce type vocabularies per directory, require
index registration for every page, and reject invisible characters — it
immediately caught a genuinely broken (unquoted-colon) frontmatter in
`operations/p0-room-cutover.md`, fixed in the same change.

## 2026-09-09 — Adjudicate delivery as paste; define the context-link slice

Settled the open conflict from 2026-09-02: delivery of an Event to an absent
Agent is a paste, a context link, or an operator-owned external script, and
the in-product wake control plane is out of product scope. Story S4 moved to
`deferred`, evaluation E5 was removed, and gate P2 was retired. At that point
`architecture/activation.md` remained as a reference design; it
was later removed during the 2026-09-09 documentation consolidation recorded
at the top of this log. The replacement delivery slice is the **context link**
defined in [product.md](product.md): a
content-addressed, immutable snapshot URL with a human replay face and an
agent-readable face, growing through fork chains and continuation rooms
before any account machinery. A chat thread carrying links is a transport,
never a Room; publish is an explicit act outside the membership boundary.
`competitive-analysis.md` recorded the share-link
landscape — vendor-bound share surfaces, A2A's 2026-08-17 entry into the
Agentic AI Foundation alongside MCP — and two new falsifiers (handoffs
staying inside one product; receivers who only view). S9 and E8 enter the
registry as the link journey; [evaluations.md](evaluations.md) and
[status.md](architecture/status.md) carry the gate changes.

## 2026-09-02 — Execute the S1 falsifier against a plain-file rival

Ran the comparison [evaluations.md](evaluations.md) requires for S1 and recorded
the outcome in [lessons/rival-falsification.md](lessons/rival-falsification.md).
An append-only JSONL file plus a line number matched this project on
per-participant boundaries, full-history recovery, and replay of an earlier
range. Full reads cost 22.4 ms against 20.1 ms for a single-Event incremental
read over a 2,000-Event Room, so incremental reads are not a latency feature;
their remaining value is the meaning *what is new for me*, worth about 93,000
tokens of context per full read. Concurrent appends stayed whole for sixteen
processes from 4 KB to 1 MB, so coordinated appends are not a differentiator
either; the earlier contrary measurement was invalid because the rival used
buffered shell `printf` rather than one `write()` per record. Activation reached
usable at-least-once semantics in about sixty lines of external shell over
existing primitives, which conflicts with
`architecture/activation.md` specifying it as a
product layer. That conflict is left open rather than reconciled. What survives
is convenience — admission, addressing, dense sequences, the alternate Store,
server mode, one error envelope — not a demonstrated product position.

## 2026-08-30 — Define the public binary release boundary

The canonical release contract is recorded in
[operations/releasing.md](operations/releasing.md): private source, public
binary artifacts in R2 only, immutable version prefixes, a mutable
`latest.json` pointer, SHA256 verification, credential rotation, and matched
format-2 rollback snapshots. The `Release` workflow and its actual package,
check, and upload steps are described there without conflating package checks
with the separate `make check` project gate.

## 2026-08-30 — Execute the P0 clean-break cutover

The clean break planned above shipped as one cutover branch, in three
implementation waves plus the CLI, deletion, and documentation passes:

1. **Core and file store** (`internal/core`, `internal/store`): the minimum
   Room/Actor/Event contract, `format: 2` marker gate, and the file backend on
   the new `Store` interface.
2. **Relational and transport** (`internal/relational`, `internal/transport`):
   the SQLite/PostgreSQL Store on the same contract and the HTTP server/client
   with the unified error envelope, `X-Tearoom-Actor`, `Idempotency-Key`, and
   server-side `wait`.
3. **CLI** (`internal/cli`, `cmd/tearoom`): `room init|join|members`, `send`,
   `read`, and `server start` only; exit codes 0/1/3/4; plain text encodes as a
   JSON string; `--after e<seq>`, `--limit`, `--wait`, `--idempotency-key`;
   loopback-only serving in P0. The `event` group, session files, cursor, typed
   Event flags, `TEAROOM_HOME`, and `--caused-by`/`--body`/`--data` are gone.
4. **Deletion per the manifest**: the eight shell suites, `internal/control`,
   `internal/hosted`, `internal/cli/hosted.go`, the `web/` SPA, the deployment
   files, and `operations/deploy.md` were removed, not aliased.
5. **Evaluations**: `target-room` (E1) and `target-replay` (E2) ship as
   executable scripts inside `make check`; S1 and S3 became proven; the legacy
   suites are recorded as removed in the registry.
6. **Docs**: status, reference, operations, product, and design documents were
   rewritten to the shipped surface. The hosted implementation is deleted and
   its design deferred, secured by a user login identity rather than
   Actor-as-secret. The cutover is recorded here, newest first.

## 2026-08-30 — Record the P0 cutover execution contract

The clean break is now executable from one document:
`operations/p0-room-cutover.md`. It fixes the
target Store/CLI/HTTP contract, the Luna A/B/C wave ownership, the integration
order, the test loop, and the deletion manifest. Two decisions were locked
during review: the shell smokes (`pg-smoke.sh`, `smoke-sqlite.sh` included)
are deleted rather than rewritten — PostgreSQL validation lives in the gated Go
battery and SQLite is covered by the `target-room` evaluation — and
`architecture/status.md` itself is rewritten at cutover, because the hosted
implementation is deleted (not adapted) while the hosted product design stays
as a deferred, login-identity-based future.

## 2026-08-30 — Choose a clean-break implementation migration

The adopted Room/Event model will replace the pre-convergence implementation
without command aliases, dual Event schemas, cursor-session import, directed
visibility fallback, old HTTP request shapes, or a deprecated Store path. Each
implementation phase must cut its owned layers, callers, tests, examples, and
docs over together. Rollback uses a matched Git/deployment revision and a
pre-cutover data snapshot; it is not an in-product compatibility mode.

The normative route is now [product.md](product.md) for product facts,
[architecture/status.md](architecture/status.md) for implementation phases,
[evaluations.md](evaluations.md) for proof, and [reference/](reference/index.md)
for target interfaces. The old Agent skill was removed because executable
instructions for the old CLI would keep routing Agents into the rejected model;
a new skill is created only when the target CLI exists.

## 2026-08-30 — Adopt the minimum Room and optional activation model

Promoted the conclusions in
`architecture/model-discussion.md` into the
authoritative product design. The Room now owns one complete history visible to
every member; `to` is public addressing rather than privacy; reads have no
implicit Actor checkpoint; ordinary append has no hidden freshness precondition;
and the common target surface is `send` plus side-effect-free `read`, with
waiting as a read mode. The minimum Event is `seq`, `actor`, `at`, optional
`to`, and `content`; protocols may encode richer meaning in content without
promoting `type` or `caused_by` into the kernel.

Agent activation is an optional application above the Room. A runtime binding
and activation policy may elect an addressed Event, launch or resume the stable
Actor, and pass an Event reference so the Agent reconstructs context from the
Room. Activation delivery state is explicit operational state, not Actor read
progress or proof of task completion.

This entry records a product decision, not a shipped implementation. The v0.1
code still has saved cursors, mandatory stale-write checks, directed visibility,
typed/causal Event fields, and `event send/list/watch`; the migration and the
offline-activation proof remain open in
[architecture/status.md](architecture/status.md) and
[evaluations.md](evaluations.md).

## 2026-08-29 — Record the model discussion draft

Captured the first open design discussion as
`architecture/model-discussion.md`. The file
was subsequently rewritten and adopted on 2026-08-30; this entry describes its
historical draft state rather than its current contents.

## 2026-08-29 — Take the hosted stack live: Render + Neon + Cloudflare Pages

Deployed the real thing for the first time. The service runs on **Render free** (Singapore, Docker, `[redacted deployment address]`), the database on **Neon free** (`[redacted project identifier]`, also Singapore, auto-suspend 5 min idle), and the management UI on **Cloudflare Pages** (`[redacted deployment address]`). Verified end to end against the live service: healthz, dev-admin login, room creation, credential issue, actor registration, event append + read, and the CORS matrix (preflight 204 for the Pages origin, foreign origins get 401 with no headers).

The live deployment surfaced two real gaps that the runbook had not caught:

1. **A platform service without an explicit start command cannot serve.** The dashboard-created service ran the image's bare entrypoint and exited immediately. Fixed by making the image default to `hosted serve` (`$DATABASE_URL` interpolated at container start) — `6ea8bd6`. A platform start command still overrides it.
2. **`TEAROOM_CORS_ORIGIN` was never read.** The `--cors-origin` flag defaulted to empty, so an env-driven service silently served without CORS. The flag now defaults to the env var — `2f792a5`.

Operational notes recorded: Render's API types (`web_service`), the env-vars endpoint is a full-replacement `PUT` with a bare array body, the API requires a payment method on file even for free services (dashboard action, no API bypass), and neonctl 4.x auth is `neonctl auth` (browser) or `NEON_API_KEY`. The admin token lives at `~/.tearoom-admin-token` and in the service's `TEAROOM_HOSTED_ADMIN`.

## 2026-08-29 — Complete the implementation queue: polling, deployment files, management UI

Shipped all three steps of `architecture/hosted-plan.md`'s implementation queue, each green in `make check` before its commit:

1. **Client-side watch polling** (`refactor: poll event watch from the client instead of the server-held wait`, 4fe77c9). `eventWatch` now reads on an interval for every store, exactly like the local branch; the CLI's waiter interface is gone. Watch's command surface, skill text, and demo semantics are unchanged — hosted-demo still proves the handoff matrix, now carried by polls. `transport`'s `wait`/Broadcaster stays as the optional LAN optimization.
2. **Deployment files** (`feat: add the deployment files`, e4c10be). A multi-stage `Dockerfile` (static `CGO_ENABLED=0` build into alpine, non-root user), the `render.yaml` blueprint, and the runbook at `operations/deploy.md` (deleted at the cutover; its deployment
was never re-adopted): Neon project + pooled DSN → `tearoom hosted init-db` → admin token → Render free-tier web service → end-to-end verification → TLS/domain notes → the 2026 free-tier constraints. The server gained `GET /healthz` (no credentials) for the platform health check.
3. **Management UI** (`feat: allow a configured CORS origin`, b79e1f0; `feat: list room credentials for the management UI`, c5f2961; `web: build the hosted management UI`, fe0de64). A deliberately thin React + Vite static SPA in `web/` — rooms, credentials, room history — in the recorded Vercel/Claude-style minimal tech, built to `dist/` for Cloudflare Pages (`VITE_API_BASE` + `--cors-origin`). Supporting server work: CORS for one exact configured origin (preflight 204, foreign origins get no headers), and `GET /api/hosted/rooms/<id>/credentials` returning a hash-free `CredentialView` — the at-rest token digest never leaves the server.

## 2026-08-29 — Decide watch's wait moves to the client; settle the free deployment

Re-examined the server-held long-poll: coordination tolerates seconds of latency, so the long-poll was over-engineering. `event watch` keeps its batch semantics and its place in the skill and the demos; the wait moves to the CLI — the same `Read` + interval loop the local mode already runs. The hosted server is therefore a stateless read/append/register API; Phase 1's `wait` parameter and per-room broadcaster remain only as an optional self-hosted-LAN optimization.

With the server stateless, the deployment settles (`architecture/hosted-plan.md`): **Render free tier** hosts the Go service — 750 free instance hours/month exceed the longest month's 744, spun-down time does not consume hours, and client polling keeps the instance awake in practice, so it is genuinely 24/7 at $0. **Neon free** holds the database (Render's free PostgreSQL expires 30 days after creation by design). **Cloudflare Pages** hosts the management UI — a React + Vite static SPA calling `/api/hosted/*` with a Bearer token, the only free static host with unlimited bandwidth and no commercial-use ban (Vercel's Hobby plan forbids commercial use; Netlify's 300 build minutes are too few). Visual direction: Vercel/Claude-style minimal tech — restrained layout, generous whitespace, precise type. An all-Cloudflare rewrite stays out of scope until the product proves itself.

## 2026-08-29 — Ship hosted Phase 1: the multi-tenant server

Implemented the first executable slice of the hosted product (`architecture/hosted-plan.md`): a control plane and a per-room event API in front of the unchanged Room/Actor/Event kernel, all in `make check`.

`relational` became multi-tenant: the `RoomID` sentinel is now a per-store `roomID`, `New(db, roomID)` makes the Store a cheap value object over a shared `*gorm.DB`, and `OpenURL(dsn)` opens PostgreSQL by URL while local use keeps `Init`/`OpenAt`. New `internal/control` owns the control plane (`ctl_accounts`, `ctl_memberships`, `ctl_credentials`, own migration chain): credentials are opaque `tr_` tokens — 192 random bits, sha256 at rest, bound to (room, actor, capabilities, expiry), plaintext shown once, individually revocable. New `internal/hosted` serves management routes (`/api/hosted/*`, dev admin bearer) and the authenticated event API (`/r/<room-id>/api/*`): a middleware resolves the Bearer to a credential, checks room binding, actor binding, and capability, then delegates to that room's own transport server with its own long-poll broadcaster. Wrong-room tokens answer 401 so they cannot probe room existence. The CLI gained the `hosted` group (`init-db`, `create-room`, `issue-credential`, `revoke`, `serve`); clients needed no new surface — `TEAROOM_SERVER` points at `/r/<id>` and `TEAROOM_TOKEN` carries the credential.

`scripts/hosted-demo.sh` (E8, 15 checks, deleted at the cutover with the hosted
implementation) proves the hosted stories S6–S8: a handoff through credentials with session resume, and the negative matrix — no token 401, wrong-room 401, actor mismatch 403, read-only append refused (CLI exit and HTTP 403), revoked bob dead while alice works. PostgreSQL validation stays gated behind `TEAROOM_TEST_PG` (`make pg-smoke` + a `pg_test.go` battery that mirrors the SQLite suite); both skip with a notice without a DSN. The GitHub OAuth exchange is the stub `login` route; S5 stays open as pending evidence.

## 2026-08-29 — Run the harness with real agents and close the remaining stories

Ran the harness workflow with three real agents (one planner, one generator, one evaluator, each an independent agent session) on a shared Room server. Each learned the CLI from the skill, then the loop ran end to end: plan → generate → revise → generate v2 → approve → close, threaded by `caused_by`. Measured send latency 13–31 ms and event-to-delivery latency 18–20 ms over local HTTP; no stale writes, no retries needed. The agents' skill feedback (flags not in the reference, watch batch semantics, feed shape) went back into SKILL.md.

Then shipped the two remaining story scenarios. `scripts/game-demo.sh` plays one Werewolf round as a protocol over directed role Events (E6, story S3): a moderator deals three private roles, night actions reach only the moderator and their sender, the day is public — 13 checks. `scripts/worktree-demo.sh` proves one Room across two real git worktrees (E7, story S4): worktrees keep no local Room, handoffs cross them by `watch`, later processes resume cursors — 9 checks. Both ran inside `make check` until the clean break deleted them with the old contract they proved; the evaluation set then marked every story except the hosted ones proven.

## 2026-08-29 — Establish the bidirectional evaluation set

Added [evaluations.md](evaluations.md), the single registry mapping every user story to the evaluations that prove it: stories S1–S10 with explicit statuses (`proven`, `open`, `deferred`), evaluations E1–E7 (`shipped`, `planned`), and two mapping tables the new `make check-evals` verifier requires to agree with each other and with the statuses. Shipped suites now carry a machine-checkable `# Evaluation:` marker, and story documents name their registry IDs. Everything previously scattered as prose — the untested game protocol (S3), the unmeasured cross-worktree Room (S4), the deferred hosted stories (S5–S10), PostgreSQL validation — is now marked, tracked, and checkable in one place.

## 2026-08-29 — Add the harness workflow user story

Added the harness workflow page (later removed with the v1 model): a planner, a generator, and an evaluator run one plan/generate/evaluate loop through a Room with no human relay. The loop is a protocol over Events — `watch` for handoff, `caused-by` for chaining, directed Events for private reviewer notes, stale-write refusal as the resync signal — not new kernel entities. `scripts/harness-demo.sh` ran the story against the shipped binary inside `make check` (deleted at the clean break with the old contract); it closed the first evidence gap of [status.md](architecture/status.md), the task protocol outside the kernel.

## 2026-08-29 — Ship the relational Store on SQLite

Added the ORM-backed relational Store behind `core.Store`, shipped on SQLite: `tearoom room init --store sqlite` creates a GORM-managed `room.db` (pure-Go `glebarez/sqlite` dialector, so release binaries stay static), the choice is recorded in `.tearoom/store.json`, and every command and the server dispatch on it. The append contract that `flock` enforces in the file Store — cursor freshness, idempotency, dense ordering, admission, recipient validation, visibility — is enforced by one append transaction: lock the Room head, validate, resolve the idempotency key, compare the cursor, insert the immutable Event, advance the head, commit. Schema changes are versioned migrations applied at open. A shipped-binary smoke suite proves the same invariants over SQLite, including through the HTTP server; PostgreSQL validation remains pending behind the same schema and migrations.

## 2026-08-29 — Hold `event watch` on the server instead of polling

Replaced the client 250 ms poll loop with a server-held long-poll. `GET /api/events` gains a `wait` parameter, capped at 60 seconds; the server returns as soon as a visible Event appears and otherwise returns an empty Feed when the wait elapses. Appends through the server wake waiters directly, and an idle fallback probes the Store for out-of-band writes without taking the lock or scanning the log (`core.Probe`; the file Store answers by stat). The CLI slices long waits into 30-second requests against a server and keeps polling only for local Rooms.

SSE stays deferred: `event watch` returns batches and exits, so a long-poll matches its semantics with less machinery.

## 2026-08-29 — Ship the cross-agent skill

Added `.claude/skills/tearoom/SKILL.md`, the first cross-product integration
surface at the time. It taught the pre-convergence `event send/list/watch`
contract and was removed by the 2026-08-30 clean break so Agents would not be
routed into the rejected model.

## 2026-08-29 — Record the competitive landscape and its priorities

Added `competitive-analysis.md` (later removed): the 2026 protocol, memory, and coding-agent market; the claim that the coordination contract A2A and MCP leave undefined is Tearoom's layer; and three near-term priorities — a server-held wait for `event watch`, the relational Store first on SQLite, and a cross-agent skill over the CLI before any MCP server.

Kept [product.md](product.md) authoritative over scope; the analysis only orders work already named there.

## 2026-08-28 — Set the hosted product and persistence direction

Kept the current local file Store as a valid implementation and set the next storage path as an ORM-backed relational Store: SQLite for local use and PostgreSQL for hosted operation. The ORM remains below `core.Store`, and database transactions must preserve the existing cursor, ordering, idempotency, admission, and visibility invariants.

Defined the hosted user journey around GitHub sign-in, account or organization ownership of Rooms, and separate revocable credentials for agents. Hosted account, membership, credential, and billing records belong to a control plane around the Room rather than adding entities to the Room, Actor, Event kernel.

## 2026-08-28 — Shorten the common Event path

Separated workspace lifecycle (`room`) from the service process (`server`) and kept normal interaction under `event`. Removed the explicit Actor command: Event operations register the named Actor automatically. Added per-Room, per-Actor client cursor state so ordinary use is `event send`, `event list`, and `event watch` without protocol bookkeeping; explicit `--cursor` and `--id` overrides remain for advanced clients. Real Agent trials showed that callers naturally guessed `--body` and `--id`, so those names replaced the less direct request-id spelling.

## 2026-08-28 — Group the CLI by product resources

The intermediate design replaced the flat command list with `room`, `actor`, and `event` resource groups. It established nested help and JSON defaults, but exposed domain nouns more directly than users needed; the later command-surface decision above supersedes it.

## 2026-08-28 — Replace the task model with Room, Actor, Event

Replaced Workspace, Agent, Thread, Lease, Gate, and the separate ledger with three core entities: Room, Actor, and Event. Removed the derived board packages instead of retaining a parallel compatibility model.

Introduced Room, Actor, and Event as the core model. Task coordination, worktree handoff, approval, direct messages, and games became Event protocols rather than kernel entities.

Every append still carries an expected cursor and idempotency key. The CLI supplies them from local session state on the common path, while explicit protocol clients may provide them directly. The Store rejects stale views, assigns one dense Room sequence under `flock`, returns the original Event for an identical retry, and filters directed Events by Actor.

Replaced the previous task-specific tests with direct Store, HTTP, real-binary, cross-process, cross-session, and cross-product checks.

## 2026-08-28 — One binary, no browser interface

Removed the WebAssembly frontend and separate server binary. `tearoom server start` now exposes an existing local Room from the same executable used by clients.

## 2026-08-27 — Establish repository boundaries

Adopted the repository working rules, separated domain contracts from file persistence and outside transports, and organized durable documentation as an indexed Markdown bundle.
