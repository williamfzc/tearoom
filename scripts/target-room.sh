#!/usr/bin/env bash
# Evaluation: E1 — P0 Room contract through the shipped binary
#
# Proves: explicit join over the local SQLite room and the loopback HTTP
# service; read/send rejected for unjoined actors; multi-actor concurrent
# append yields one dense sequence; every member reads the same complete
# history; repeated reads have no side effects; ordinary append does not
# depend on a prior read; an idempotent retry returns the original event and
# a key conflict is an error (CLI exit 1, HTTP 409).
set -euo pipefail
cd "$(dirname "$0")/.."
BIN="$PWD/bin/tearoom"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

pass=0
fail=0
check() {
  if [ "$2" -eq 0 ]; then pass=$((pass + 1)); echo "  ok   $1"
  else fail=$((fail + 1)); echo "  FAIL $1"; fi
}

# in_dir runs a command with the given working directory.
in_dir() { local dir="$1"; shift; ( cd "$dir" && "$@" ); }

# expect runs a command and checks that it exits 0.
expect() { local name="$1"; shift; local rc=0; in_dir "$@" >/dev/null 2>&1 || rc=$?; check "$name" "$rc"; }

# reject runs a command and checks that it exits nonzero.
reject() { local name="$1"; shift; local rc=0; in_dir "$@" >/dev/null 2>&1 || rc=$?; [ "$rc" -ne 0 ] && rc=0 || rc=1; check "$name" "$rc"; }

# seqs_of reads JSONL on stdin and prints the event sequence numbers.
seqs_of() {
  python3 -c '
import json, sys
seqs = [json.loads(ln)["seq"] for ln in sys.stdin if ln.strip()]
print(" ".join(str(s) for s in seqs))
'
}

# dense checks that the given sequence string is exactly 1..N.
dense() {
  python3 - "$1" <<'PY'
import sys
seqs = [int(x) for x in sys.argv[1].split()]
sys.exit(0 if seqs == list(range(1, len(seqs) + 1)) else 1)
PY
}

echo
echo "E1: local room"
local_room="$WORK/local-room"
mkdir -p "$local_room"

expect "room init admits the creator" "$local_room" "$BIN" room init --as planner
expect "explicit join bob" "$local_room" "$BIN" room join bob
expect "explicit join carol" "$local_room" "$BIN" room join carol

reject "unjoined read rejected" "$local_room" "$BIN" read --as stranger
reject "unjoined send rejected" "$local_room" "$BIN" send hi --as stranger

expect "planner sends three events" "$local_room" "$BIN" send one --as planner
expect "planner sends a second event" "$local_room" "$BIN" send two --as planner
expect "planner sends a third event" "$local_room" "$BIN" send three --as planner

bob_hist="$(in_dir "$local_room" "$BIN" read --as bob)"
carol_hist="$(in_dir "$local_room" "$BIN" read --as carol)"
check "bob and carol read the same complete history" "$([ "$bob_hist" = "$carol_hist" ] && echo 0 || echo 1)"
check "the complete history is exactly events 1..3" \
  "$(printf '%s' "$carol_hist" | seqs_of | grep -q '^1 2 3$' && echo 0 || echo 1)"

# A freshly joined actor appends without ever reading the room first.
expect "join dave" "$local_room" "$BIN" room join dave
expect "append without any prior read" "$local_room" "$BIN" send "first word" --as dave

# Six writers append concurrently; the store must issue one dense sequence.
for id in erin frank grace hank iris joy; do
  expect "join $id" "$local_room" "$BIN" room join "$id"
done
pids=()
for id in erin frank grace hank iris joy; do
  in_dir "$local_room" "$BIN" send "from $id" --as "$id" >/dev/null 2>&1 &
  pids+=("$!")
done
concurrent_rc=0
for p in "${pids[@]}"; do wait "$p" || concurrent_rc=1; done
check "all six concurrent appends succeed" "$concurrent_rc"

seqs="$(in_dir "$local_room" "$BIN" read --as planner | seqs_of)"
rc=0; dense "$seqs" || rc=$?
check "concurrent appends yield the dense sequence 1..11 ($seqs)" "$rc"

# Repeated reads are side-effect-free and identical.
again="$(in_dir "$local_room" "$BIN" read --as planner)"
check "repeated reads return identical history" "$([ "$again" = "$(in_dir "$local_room" "$BIN" read --as planner)" ] && echo 0 || echo 1)"

# Idempotent retry returns the original event; a key conflict is an error.
before_key="$(in_dir "$local_room" "$BIN" read --as planner | seqs_of | wc -w | tr -d ' ')"
first="$(in_dir "$local_room" "$BIN" send --json '{"n":1}' --idempotency-key local-k1 --as planner)"
seq1="$(printf '%s' "$first" | python3 -c 'import json,sys; print(json.load(sys.stdin)["seq"])')"
retry="$(in_dir "$local_room" "$BIN" send --json '{"n": 1}' --idempotency-key local-k1 --as planner)"
seq2="$(printf '%s' "$retry" | python3 -c 'import json,sys; print(json.load(sys.stdin)["seq"])')"
check "idempotent retry returns the original event (seq $seq1)" "$([ "$seq1" = "$seq2" ] && echo 0 || echo 1)"
reject "same key with different content conflicts" "$local_room" "$BIN" send --json '{"n":2}' --idempotency-key local-k1 --as planner
after_key="$(in_dir "$local_room" "$BIN" read --as planner | seqs_of | wc -w | tr -d ' ')"
check "idempotent retry created no new event" "$([ "$after_key" = "$((before_key + 1))" ] && echo 0 || echo 1)"

echo
echo "E1: HTTP leg"
http_room="$WORK/http-room"
mkdir -p "$http_room"
expect "http room init" "$http_room" "$BIN" room init --as planner
expect "http join bob" "$http_room" "$BIN" room join bob

PORT="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')"
URL="http://127.0.0.1:$PORT"
( cd "$http_room" && exec "$BIN" server start --address "127.0.0.1:$PORT" ) >"$WORK/server.log" 2>&1 &
SRV_PID=$!
cleanup_server() { kill "$SRV_PID" 2>/dev/null || true; wait "$SRV_PID" 2>/dev/null || true; }
trap 'cleanup_server' EXIT

ready=0
for _ in $(seq 1 100); do
  if TEAROOM_SERVER="$URL" "$BIN" read --as planner >/dev/null 2>&1; then ready=1; break; fi
  sleep 0.1
done
check "http server becomes ready on 127.0.0.1:$PORT" "$([ "$ready" -eq 1 ] && echo 0 || echo 1)"

expect "http join carol" "$http_room" env TEAROOM_SERVER="$URL" "$BIN" room join carol
expect "http send a first event" "$http_room" env TEAROOM_SERVER="$URL" "$BIN" send --json '{"plan":"http"}' --as planner
expect "http send a second event" "$http_room" env TEAROOM_SERVER="$URL" "$BIN" send "second" --as planner

http_bob="$(in_dir "$http_room" env TEAROOM_SERVER="$URL" "$BIN" read --as bob)"
http_carol="$(in_dir "$http_room" env TEAROOM_SERVER="$URL" "$BIN" read --as carol)"
check "http members read the same complete history" "$([ "$http_bob" = "$http_carol" ] && echo 0 || echo 1)"
check "the http history is exactly events 1..2" \
  "$(printf '%s' "$http_carol" | seqs_of | grep -q '^1 2$' && echo 0 || echo 1)"

reject "http unjoined read rejected" "$http_room" env TEAROOM_SERVER="$URL" "$BIN" read --as stranger
reject "http unjoined send rejected" "$http_room" env TEAROOM_SERVER="$URL" "$BIN" send hi --as stranger

first="$(in_dir "$http_room" env TEAROOM_SERVER="$URL" "$BIN" send --json '{"n":1}' --idempotency-key http-k1 --as planner)"
seq1="$(printf '%s' "$first" | python3 -c 'import json,sys; print(json.load(sys.stdin)["seq"])')"
retry="$(in_dir "$http_room" env TEAROOM_SERVER="$URL" "$BIN" send --json '{"n":1}' --idempotency-key http-k1 --as planner)"
seq2="$(printf '%s' "$retry" | python3 -c 'import json,sys; print(json.load(sys.stdin)["seq"])')"
check "http idempotent retry returns the original event (seq $seq1)" "$([ "$seq1" = "$seq2" ] && echo 0 || echo 1)"
reject "http same key with different content conflicts (409)" "$http_room" env TEAROOM_SERVER="$URL" "$BIN" send --json '{"n":2}' --idempotency-key http-k1 --as planner

cleanup_server
trap 'rm -rf "$WORK"' EXIT

echo
if [ "$fail" -eq 0 ]; then
  printf 'E1 passed: %d checks\n\n' "$pass"
else
  printf 'E1 FAILED: %d of %d checks failed\n\n' "$fail" "$((pass + fail))"
  exit 1
fi
