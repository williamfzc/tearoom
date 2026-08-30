# Tearoom

A durable shared Room where people, Agents, and tools contribute to one ordered,
replayable history. An optional activation layer may wake an absent Agent so it
can recover context from the Room and continue the collaboration.

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

## Optional Agent activation

A Room works without an Agent runtime or activation service. When activation is
enabled, an operator-owned policy may elect an addressed Event, start or resume
the target Actor's runtime, and pass a Room and Event reference. The Agent then
reads the Room and appends its contribution. Activation delivery state belongs
to that optional control plane; it is not Actor unread state.

## Build and verify

Requires Go 1.22+ and a Unix-like OS.

```bash
make check
```

`make check` runs fmt, vet, the unit and race suites, the P0 evaluations
(`target-room`, `target-replay`), the evaluation-set consistency check, and
the docs-bundle check. See [docs/operations/building.md](docs/operations/building.md)
for the macOS linker and module-proxy requirements of this machine.

## License

MIT
