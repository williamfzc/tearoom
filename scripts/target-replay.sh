#!/usr/bin/env bash
# Evaluation: E2 — replay across real processes, no stored read progress
#
# Proves: different real processes act on the same workspace; a later process
# replays from an explicit eN boundary after the writer exited; no session or
# cursor files exist anywhere (the client config directory stays empty); every
# invocation picks its own boundary; workspace history is consistent across
# processes.
set -euo pipefail
cd "$(dirname "$0")/.."
BIN="$PWD/bin/tearoom"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
export HOME="$WORK/home"
# macOS's python3 writes .pyc caches under $HOME/Library/Caches; the tearoom
# binary must be the only process allowed to touch the client state directory.
export PYTHONDONTWRITEBYTECODE=1
mkdir -p "$HOME"

pass=0
fail=0
check() {
  if [ "$2" -eq 0 ]; then pass=$((pass + 1)); echo "  ok   $1"
  else fail=$((fail + 1)); echo "  FAIL $1"; fi
}

in_dir() { local dir="$1"; shift; ( cd "$dir" && "$@" ); }
expect() { local name="$1"; shift; local rc=0; in_dir "$@" || rc=$?; check "$name" "$rc"; }

# seqs_of reads JSONL on stdin and prints the record sequence numbers.
seqs_of() {
  python3 -c '
import json, sys
seqs = [json.loads(ln)["seq"] for ln in sys.stdin if ln.strip()]
print(" ".join(str(s) for s in seqs))
'
}

echo
echo "E2: one workspace across real processes"
room="$WORK/room"
mkdir -p "$room"

# Process 1: create the workspace and append three records, then exit.
expect "process 1: room init creates the workspace" "$room" "$BIN" room init --name main-line
expect "process 1: records event one" "$room" "$BIN" send "first" --as alice
expect "process 1: records event two" "$room" "$BIN" send --json '{"plan":2}' --as alice
expect "process 1: records event three" "$room" "$BIN" send "third" --as alice

# Process 2: a brand-new invocation replays from the explicit e1 boundary.
replay="$(in_dir "$room" "$BIN" read --after e1)"
rc=0
printf '%s\n' "$replay" | python3 -c '
import json, sys
records = [json.loads(ln) for ln in sys.stdin if ln.strip()]
seqs = [r["seq"] for r in records]
sys.exit(0 if seqs == [2, 3] and all(r["author"] == "alice" for r in records) else 1)
' || rc=$?
check "process 2 replays records 2 and 3 from the e1 boundary" "$rc"

# Process 3: the same signature appends a fourth record and reads everything.
expect "process 3: alice appends record four" "$room" "$BIN" send "fourth" --as alice
full="$(in_dir "$room" "$BIN" read)"
rc=0
printf '%s\n' "$full" | python3 -c '
import json, sys
records = [json.loads(ln) for ln in sys.stdin if ln.strip()]
seqs = [r["seq"] for r in records]
sys.exit(0 if seqs == [1, 2, 3, 4] else 1)
' || rc=$?
check "process 3 reads the complete dense history 1..4" "$rc"

echo
echo "E2: no session or cursor files"
leftovers="$(find "$HOME" -type f | head -5)"
check "the client config directory holds no session files" "$([ -z "$leftovers" ] && echo 0 || echo 1)"
# Every invocation owns its boundary: reading from e1 twice returns the same
# records and leaves the workspace untouched for the next caller.
one="$(in_dir "$room" "$BIN" read --after e1 | seqs_of)"
two="$(in_dir "$room" "$BIN" read --after e1 | seqs_of)"
check "independent invocations replay the same boundary (e1 -> 2 3 4)" \
  "$([ "$one" = "$two" ] && [ "$one" = "2 3 4" ] && echo 0 || echo 1)"

echo
echo "E2: history is consistent across processes"
expect "process 4: alice appends in a fresh process" "$room" "$BIN" send "fifth record" --as alice
final="$(in_dir "$room" "$BIN" read | seqs_of)"
check "history stays consistent across processes: 1..5" \
  "$(printf '%s' "$final" | grep -q '^1 2 3 4 5$' && echo 0 || echo 1)"

echo
if [ "$fail" -eq 0 ]; then
  printf 'E2 passed: %d checks\n\n' "$pass"
else
  printf 'E2 FAILED: %d of %d checks failed\n\n' "$fail" "$((pass + fail))"
  exit 1
fi
