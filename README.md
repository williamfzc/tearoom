# Tearoom

A durable shared Room where one participant writes work, another person or
Agent product pulls the same ordered history through the CLI, continues, and
writes back for the next participant.

[`docs/product.md`](docs/product.md) is the authoritative product definition.
Start with the [documentation route](docs/index.md). Implementation state
lives in [status.md](docs/architecture/status.md); proof lives in
[evaluations.md](docs/evaluations.md).

## Minimum model

- **Room** — the membership and history boundary.
- **Actor** — a stable participant identity, independent of a process or Agent
  session.
- **Event** — one immutable contribution ordered within its Room.

Every Room member sees the complete Room history. Optional `to` addressing
carried on an Event is public, not secrecy and not a command to start a
process. Privacy requires a different Room with different membership.

## Quick start

```bash
make build                 # builds ./bin/tearoom
tearoom_bin="$PWD/bin/tearoom"
room_dir="$(mktemp -d)"
cd "$room_dir"
export TEAROOM_ACTOR=planner
"$tearoom_bin" room init
"$tearoom_bin" room join developer
"$tearoom_bin" send "review this"
TEAROOM_ACTOR=developer "$tearoom_bin" read
```

- `room init` creates a Room and admits the creator; `room join <actor>` admits other
  members explicitly. A Room is the `.tearoom` directory beside your work —
  one SQLite database, no backend choices.
- `send` prints the appended Event as JSON. Plain text is encoded as a JSON
  string; `--json` passes raw JSON.
- `read` prints JSONL, one Event per line. Reading is side-effect free: no
  cursor is saved. `--after e<seq>` sets the caller's boundary; truncating and
  polling belong to the caller.
- `server start` serves the nearest Room over HTTP on `127.0.0.1:8787`
  (loopback only); set `TEAROOM_SERVER` to point other processes at it.

## Delivery beyond the process

A Room works without an Agent runtime or wake service. When an addressed Agent
is absent, an external operator or Agent product starts and configures it with
the Room connection. The Agent then uses the same `read` and `send` commands
to pull JSONL, continue, and write back. Remote deployment adds HTTPS and
credentials, not a second collaboration interface. A wake control plane —
daemon, ledger, launcher — was measured against a sixty-line external rival
and adjudicated out of scope
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
