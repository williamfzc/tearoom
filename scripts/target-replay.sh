#!/usr/bin/env bash
# Evaluation: E2 — P0 replay across real processes, no server-owned read progress
#
# Proves: different real processes act as the same Actor identity; a later
# process replays from an explicit eN boundary after the writer exited; no
# session or cursor files exist anywhere (the room layout is exactly the five
# store files, and the client config directory stays empty); read --wait
# affects only its own invocation; Room history is consistent across process
# and server restarts.
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

# seqs_of reads JSONL on stdin and prints the event sequence numbers.
seqs_of() {
  python3 -c '
import json, sys
seqs = [json.loads(ln)["seq"] for ln in sys.stdin if ln.strip()]
print(" ".join(str(s) for s in seqs))
'
}

echo
echo "E2: one actor identity across real processes"
room="$WORK/room"
mkdir -p "$room"

# Process 1: create the room and append three events as alice, then exit.
expect "process 1: room init admits alice" "$room" "$BIN" room init --as alice
expect "process 1: alice sends event one" "$room" "$BIN" send "first" --as alice
expect "process 1: alice sends event two" "$room" "$BIN" send --json '{"plan":2}' --as alice
expect "process 1: alice sends event three" "$room" "$BIN" send "third" --as alice

# Process 2: a brand-new invocation replays from the explicit e1 boundary.
replay="$(in_dir "$room" "$BIN" read --after e1 --as alice)"
rc=0
printf '%s\n' "$replay" | python3 -c '
import json, sys
events = [json.loads(ln) for ln in sys.stdin if ln.strip()]
seqs = [e["seq"] for e in events]
sys.exit(0 if seqs == [2, 3] and all(e["actor"] == "alice" for e in events) else 1)
' || rc=$?
check "process 2 replays events 2 and 3 from the e1 boundary" "$rc"

# Process 3: the same Actor appends a fourth event and reads everything.
expect "process 3: alice appends event four" "$room" "$BIN" send "fourth" --as alice
full="$(in_dir "$room" "$BIN" read --as alice)"
rc=0
printf '%s\n' "$full" | python3 -c '
import json, sys
events = [json.loads(ln) for ln in sys.stdin if ln.strip()]
seqs = [e["seq"] for e in events]
sys.exit(0 if seqs == [1, 2, 3, 4] else 1)
' || rc=$?
check "process 3 reads the complete dense history 1..4" "$rc"

echo
echo "E2: no session or cursor files"
leftovers="$(find "$HOME" -type f | head -5)"
check "the client config directory holds no session files" "$([ -z "$leftovers" ] && echo 0 || echo 1)"
room_files="$(cd "$room" && find .tearoom -maxdepth 1 -type f -exec basename {} \; | sort | tr '\n' ' ')"
check "the room holds only the five store files ($room_files)" \
  "$(printf '%s' "$room_files" | grep -q '^actors.json events.jsonl lock room.json store.json $' && echo 0 || echo 1)"

echo
echo "E2: read --wait affects only its own invocation"
timeout_rc=0
in_dir "$room" "$BIN" read --after e4 --wait 2s --as alice >/dev/null 2>&1 || timeout_rc=$?
check "read --wait with nothing new times out with exit 3" "$([ "$timeout_rc" -eq 3 ] && echo 0 || echo 1)"
wait_full="$(in_dir "$room" "$BIN" read --as alice | seqs_of)"
check "the timed-out wait persisted no state: full read is still 1..4" \
  "$(printf '%s' "$wait_full" | grep -q '^1 2 3 4$' && echo 0 || echo 1)"
tail="$(in_dir "$room" "$BIN" read --after e2 --limit 1 --as alice)"
rc=0
printf '%s\n' "$tail" | python3 -c '
import json, sys
events = [json.loads(ln) for ln in sys.stdin if ln.strip()]
sys.exit(0 if len(events) == 1 and events[0]["seq"] == 3 else 1)
' || rc=$?
check "the next invocation still picks its own boundary (e2 -> event 3)" "$rc"

echo
echo "E2: history is consistent across restarts"
PORT="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')"
URL="http://127.0.0.1:$PORT"
start_server() {
  ( cd "$room" && exec "$BIN" server start --address "127.0.0.1:$PORT" ) >"$WORK/server.log" 2>&1 &
  SRV_PID=$!
  for _ in $(seq 1 100); do
    if TEAROOM_SERVER="$URL" "$BIN" room members >/dev/null 2>&1; then return 0; fi
    sleep 0.1
  done
  return 1
}
stop_server() {
  kill "$SRV_PID" 2>/dev/null || true
  wait "$SRV_PID" 2>/dev/null || true
}

start_server
srv_first="$(in_dir "$room" env TEAROOM_SERVER="$URL" "$BIN" read --as alice | seqs_of)"
stop_server
start_server
srv_second="$(in_dir "$room" env TEAROOM_SERVER="$URL" "$BIN" read --as alice | seqs_of)"
check "server restart serves the same history" "$([ "$srv_first" = "$srv_second" ] && [ "$srv_first" = "1 2 3 4" ] && echo 0 || echo 1)"
stop_server

expect "process 4: alice appends after the restarts" "$room" "$BIN" send "after restart" --as alice
final="$(in_dir "$room" "$BIN" read --as alice | seqs_of)"
check "history stays consistent after restarts: 1..5" \
  "$(printf '%s' "$final" | grep -q '^1 2 3 4 5$' && echo 0 || echo 1)"

echo
if [ "$fail" -eq 0 ]; then
  printf 'E2 passed: %d checks\n\n' "$pass"
else
  printf 'E2 FAILED: %d of %d checks failed\n\n' "$fail" "$((pass + fail))"
  exit 1
fi
