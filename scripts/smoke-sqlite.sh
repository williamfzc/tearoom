#!/usr/bin/env bash
# Drive the shipped binary against the SQLite backend: same invariants as
# smoke.sh, proving the relational Store answers the same contract end to end.
# Evaluation: E3 (see docs/evaluations.md)
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TEAROOM="$ROOT/bin/tearoom"
WORK="$(mktemp -d)"
SERVER_PID=""
cleanup(){ [ -z "$SERVER_PID" ] || kill "$SERVER_PID" 2>/dev/null || true; rm -rf "$WORK"; }
trap cleanup EXIT
cd "$WORK"
export TEAROOM_HOME="$WORK/.client"

pass=0; fail=0
ok(){ printf '  ok   %s\n' "$1"; pass=$((pass+1)); }
bad(){ printf '  FAIL %s\n' "$1"; fail=$((fail+1)); }
check(){ [ "$2" = "$3" ] && ok "$1" || bad "$1 (got '$2', want '$3')"; }
jget(){ python3 -c 'import json,sys; d=json.load(sys.stdin)
for k in sys.argv[1].split("."): d=d[int(k)] if k.isdigit() else d[k]
print(d)' "$1"; }

ROOM=$("$TEAROOM" room init --name sqlite-smoke --store sqlite)
check "sqlite init returns the Room" "$(printf '%s' "$ROOM" | jget name)" "sqlite-smoke"
check "sqlite backend marker is written" "$(test -f "$WORK/.tearoom/store.json" && echo yes)" "yes"
check "sqlite database is created" "$(test -f "$WORK/.tearoom/room.db" && echo yes)" "yes"

# The same short path and invariants as the file backend.
E1=$(TEAROOM_ACTOR=alice "$TEAROOM" event send "please review worktree A")
check "sqlite send creates first Event" "$(printf '%s' "$E1" | jget seq)" "1"
B=$(TEAROOM_ACTOR=bob "$TEAROOM" event list)
check "sqlite list sees the Event" "$(printf '%s' "$B" | jget events.0.body)" "please review worktree A"
TEAROOM_ACTOR=bob "$TEAROOM" event send "review complete" --type reply --caused-by e1 >/dev/null

TEAROOM_ACTOR=alice "$TEAROOM" event send "stale assumption" >/dev/null 2>&1
check "sqlite stale write still exits 2" "$?" "2"
TEAROOM_ACTOR=alice "$TEAROOM" event list >/dev/null
TEAROOM_ACTOR=alice "$TEAROOM" event send wolf --type role.assigned --to bob >/dev/null

R1=$(TEAROOM_ACTOR=alice "$TEAROOM" event send --body manual --cursor 3 --id manual-1)
R2=$(TEAROOM_ACTOR=alice "$TEAROOM" event send --body manual --cursor 4 --id manual-1)
check "sqlite explicit id makes retry idempotent" "$(printf '%s' "$R2" | jget id)" "$(printf '%s' "$R1" | jget id)"

# A second process cannot reach the room from outside the workspace.
( cd / && TEAROOM_HOME="$WORK/.client2" TEAROOM_ACTOR=bob "$TEAROOM" event send "from elsewhere" >/dev/null 2>&1 )
check "sqlite room not found outside the workspace" "$?" "4"

# Serve the same sqlite room over HTTP: raw clients keep the Store contract.
PORT="$(python3 - <<'PY'
import socket
s=socket.socket(); s.bind(('127.0.0.1',0)); print(s.getsockname()[1]); s.close()
PY
)"
BASE="http://127.0.0.1:$PORT"
"$TEAROOM" server start --address "127.0.0.1:$PORT" --directory "$WORK" >server.log 2>&1 &
SERVER_PID=$!
for _ in $(seq 1 50); do curl -fsS "$BASE/api/room" >/dev/null && break; sleep .1; done
curl -fsS "$BASE/api/room" >/dev/null

curl -fsS -X POST "$BASE/api/events" -H 'Content-Type: application/json' \
  -d '{"actor":"bob","expected":4,"type":"message","body":"over HTTP","key":"http-1"}' >/dev/null
F=$(curl -fsS "$BASE/api/events?actor=alice&after=4")
check "sqlite HTTP feed sees the raw append" "$(printf '%s' "$F" | jget events.0.body)" "over HTTP"
code=$(curl -sS -o "$WORK/stale.json" -w '%{http_code}' -X POST "$BASE/api/events" -H 'Content-Type: application/json' \
  -d '{"actor":"bob","expected":1,"type":"message","body":"obsolete","key":"stale-http"}')
check "sqlite stale raw HTTP writer is rejected" "$code" "409"
printf '%s' "$(cat "$WORK/stale.json")" | grep -q '"kind":"stale_write"'
check "sqlite stale error is typed" "$?" "0"

echo
if [ "$fail" -eq 0 ]; then printf '%d passed, 0 failed\n' "$pass"; exit 0; fi
printf '%d passed, %d failed\n' "$pass" "$fail"; exit 1
