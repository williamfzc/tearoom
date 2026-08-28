# Tearoom

Minimal coordination layer for multi-agent workspaces.

Several AI coding agents working in one place need two things. They need to agree on
**who may act** — who owns this task, who holds this file, when a human must decide. And
they need control over **what it costs to find out what changed**, because an agent has no
screen to glance at and pays tokens for every observation.

Tearoom provides those and refuses to become a chat product. The full scope, and the tests
a proposal has to pass, are in [`docs/product.md`](docs/product.md), which is authoritative
over this file.

```
tea post   "fix flaky auth test"     # open a thread
tea claim  src/auth/*.ts --ttl 15m   # reserve files before editing
tea reply  t1 --body "fixed, tests green" --status resolved
tea wait   t1                        # block until something actually changes
tea gate   "needs prod migration approval"
```

## Why not just use Slack / Lark with bots

Because the primitives have to exist at the protocol level and cannot be bolted onto a
chat product. IM gives a bot no file leases, no atomic task claiming, no way to pull a
*summary* of a thread instead of its whole history, and no way to refuse a stale write.

The inverse claim — that agents simply need *less* than a chat system — is wrong, and worth
being precise about. Agents need less presence and formatting, and considerably **more**
attention control than IM offers a bot: notices separated from message bodies, the ability
to mute a channel, intent that survives the end of a turn. See
[agent experience](docs/domain/agent-experience.md).

## Design constraints

These are load-bearing. Changing any of them changes what the product is; the reasoning
for each is in [`docs/product.md`](docs/product.md).

- **Five concepts only** — Workspace, Agent, Thread, Lease, Gate. A feature that
  needs a sixth concept gets rejected by default.
- **One job per command** — an agent should learn the whole surface from `--help` without
  reading docs. Currently fourteen commands, four too many; see
  [convergence](docs/architecture/convergence.md).
- **Code never leaves the machine** — the store holds paths, summaries and
  decisions. Never file contents.
- **No orchestration engine** — no DAGs, no conditional branching. Agents decide
  what to do next; Tearoom only supplies facts and primitives.
- **Failure must be actionable** — a rejected `claim` tells you who holds the
  resource and when it frees up. Never just an error code.
- **No silent success** — if the store is unreachable the command fails loudly.
  An agent that believes it holds a lease it does not hold is worse than a crash.

## Status

M0, working end to end: a CLI, an HTTP server, and a browser console that is
compiled from the same Go source as the CLI.

Two things are knowingly missing, and they are not small. **A stale write is accepted** —
an agent reasoning from an out-of-date snapshot can record a false statement, verified with
exit code 0. And **the whole attention axis is absent**: no notice tier, no mute, no intent
that outlives a turn. Both are catalogued in
[status](docs/architecture/status.md#known-gaps-not-decisions).

The whole thing is one language on purpose. `internal/core` defines the store
interface; the local file store and the HTTP client both implement it, so the CLI
cannot tell whether it is talking to a directory or a server. Everything derived —
who is stuck, what is waiting on a human, how urgent it is — lives in
`internal/view` as pure functions, and both the terminal output and the browser
console render that same result. There is no business logic in JavaScript, because
there is no JavaScript.

That is not tidiness for its own sake. The first bug found by clicking a button in
the browser was a rule that lived in the CLI instead of the store: approving from
the console settled the gate but left the thread blocked forever. Rules that every
client must obey now live under the store interface, where no client can skip them.

Documentation lives in [`docs/`](docs/index.md), organised by concern: the
[five concepts](docs/domain/concepts.md), the [layer rules](docs/architecture/layers.md),
the [CLI reference](docs/reference/cli.md), and the
[lessons](docs/lessons/index.md) behind why some things look the way they do.

## Build

Requires Go 1.22+ and a Unix-like OS (uses `flock` for store mutations).

```bash
make build          # -> ./bin/tea, ./bin/tea-server, ./web/tea.wasm
make test           # unit tests, including multi-process concurrency
make test-wasm      # frontend tests, run in node
make smoke          # 41 end-to-end assertions against the real binary
make check          # everything above; run this before pushing
```

`make` sets `CGO_ENABLED=0`. This is deliberate — it produces a static binary and
avoids a macOS linker failure in `net/http`. Building with a bare `go build` can
produce a binary that aborts at startup.

## Serving the console

```bash
tea-server --addr :8787 --web ./web
# console on http://localhost:8787

# point any agent at it; the CLI behaves identically
export TEA_SERVER=http://localhost:8787
```

The console shows what needs a human first and loudest, and its only write actions
are approve and reject. It refuses to become a chat app.

## Quick start

```bash
cd your-project
tea init --name your-project

# terminal A
export TEA_AGENT=impl
tea join --role implementer
ID=$(tea post "fix flaky auth test" --json | jq -r .thread.id)
tea claim "src/auth/**" --ttl 15m --thread $ID
tea reply $ID --body "patched retry logic" --status claimed
tea release "src/auth/**"

# terminal B
export TEA_AGENT=verify
tea join --role verifier
tea inbox
tea context $ID
tea reply $ID --body "suite green on 3 runs" --status resolved
```

## License

MIT
