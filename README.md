# Tearoom

A durable shared Room where people, Agents, and tools contribute to one ordered,
replayable history. The planned context link will carry that history anywhere:
a human opens the replay, and an absent Agent reads it and continues the
collaboration.

[`docs/product.md`](docs/product.md) is the authoritative product definition.
Start with the [documentation route](docs/index.md). Implementation state
lives in [status.md](docs/architecture/status.md); proof lives in
[evaluations.md](docs/evaluations.md).

## Minimum model

- **Room** — the membership and history boundary.
- **Actor** — a stable participant identity, independent of a process or Agent
  session.
- **Event** — one immutable contribution ordered within its Room.

Every Room member sees the complete Room history. `to` is optional public
addressing, not secrecy and not a command to start a process. Privacy requires
a different Room with different membership.

## Quick start

```bash
make build                 # builds ./bin/tearoom
tearoom_bin="$PWD/bin/tearoom"
room_dir="$(mktemp -d)"
cd "$room_dir"
export TEAROOM_ACTOR=planner
"$tearoom_bin" room init
"$tearoom_bin" room join developer
"$tearoom_bin" send "review this" --to developer
TEAROOM_ACTOR=developer "$tearoom_bin" read
```

- `room init` creates a Room and admits the creator; `room join <actor>` admits other
  members explicitly. A Room is the `.tearoom` directory beside your work;
  `--store sqlite` selects the SQLite backend.
- `send` prints the appended Event as JSON. Plain text is encoded as a JSON
  string; `--json` passes raw JSON.
- `read` prints JSONL, one Event per line. Reading is side-effect free: no
  cursor is saved. `--after e<seq>` sets the caller's boundary; `--wait` holds
  only the current invocation and exits 3 on timeout.
- `server start` serves the nearest Room over HTTP on `127.0.0.1:8787`
  (loopback only in P0); set `TEAROOM_SERVER` to point other processes at it.

To see the timeout contract without a long pause, run this optional check:

```bash
if TEAROOM_ACTOR=developer "$tearoom_bin" read --after e1 --wait 1s; then
  :
else
  test "$?" -eq 3
fi
```

## Delivery beyond the process

A Room works without an Agent runtime or wake service. When an addressed Agent
is absent, delivery happens outside the product: a human forwards the Event
reference or a context link, or an operator runs their own script against the
Room contract. The planned context link (the first remote-facing slice of the
same product, see
[docs/product.md](docs/product.md)) turns a Room range into one pasteable URL
that resolves as a human replay and as agent-readable text. A wake control
plane — daemon, ledger, launcher — was measured against a sixty-line external
rival and adjudicated out of scope
([lessons/rival-falsification.md](docs/lessons/rival-falsification.md)).

## Build and verify

Requires Go 1.22+ and a Unix-like OS.

```bash
make check
```

`make check` runs fmt, vet, the unit and race suites, the P0 evaluations
(`target-room`, `target-replay`), the evaluation-set consistency check, and
the docs-bundle check. See [docs/operations/building.md](docs/operations/building.md)
for the macOS linker and module-proxy requirements of this machine.

## Binary releases

Source stays private while compiled binaries are public artifacts. Cloudflare
R2 is the single public artifact source: pin an immutable version URL, download
its `SHA256SUMS`, and verify SHA256 before installation. `latest.json` is
only a mutable convenience pointer. Tagged releases run repository checks,
deterministic package checks, and public readback. See the [release runbook](docs/operations/releasing.md)
for supported-platform policy, R2 credential rotation, publication, and
format-2 rollback. Release archives carry both `LICENSE` and generated
`THIRD_PARTY_NOTICES`; a released binary can be reverse-engineered and contains
no secrets by design.

## License

MIT
