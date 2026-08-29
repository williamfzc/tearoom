#!/usr/bin/env bash
# Prove a Room crosses process sessions and independent client products.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TEAROOM="$ROOT/bin/tearoom"
WORK="$(mktemp -d)"
PORT="$(python3 - <<'PY'
import socket
s=socket.socket(); s.bind(('127.0.0.1',0)); print(s.getsockname()[1]); s.close()
PY
)"
BASE="http://127.0.0.1:$PORT"
SERVER_PID=""
cleanup(){ [ -z "$SERVER_PID" ] || kill "$SERVER_PID" 2>/dev/null || true; rm -rf "$WORK"; }
trap cleanup EXIT

cd "$WORK"
CLIENT_HOME="$WORK/.client"
export TEAROOM_HOME="$CLIENT_HOME"
"$TEAROOM" room init --name sessions >/dev/null
"$TEAROOM" server start --address "127.0.0.1:$PORT" --directory "$WORK" >server.log 2>&1 &
SERVER_PID=$!
for _ in $(seq 1 50); do curl -fsS "$BASE/api/room" >/dev/null && break; sleep .1; done
curl -fsS "$BASE/api/room" >/dev/null

# Remote mode keeps client progress outside the workspace and works from any directory.
REMOTE_WORK="$WORK/remote-client"
mkdir "$REMOTE_WORK"
cd "$REMOTE_WORK"

# Same product, later process: stable identity and local cursor resume automatically.
TEAROOM_SERVER="$BASE" TEAROOM_ACTOR=same "$TEAROOM" event send "session one" >/dev/null
TEAROOM_SERVER="$BASE" TEAROOM_ACTOR=same "$TEAROOM" event list |
  python3 -c 'import json,sys; d=json.load(sys.stdin); assert d["events"]==[] and d["cursor"]==1'
printf '  ok   same agent product resumes its session cursor\n'

# Another product uses raw HTTP, then the CLI reads and replies without join.
curl -fsS -X POST "$BASE/api/actors" -H 'Content-Type: application/json' -d '{"ID":"other"}' >/dev/null
curl -fsS -X POST "$BASE/api/events" -H 'Content-Type: application/json' \
  -d '{"actor":"other","expected":1,"type":"handoff","body":"created over HTTP","key":"http-1"}' >/dev/null
TEAROOM_SERVER="$BASE" TEAROOM_ACTOR=same "$TEAROOM" event list |
  python3 -c 'import json,sys; d=json.load(sys.stdin); assert d["events"][0]["body"]=="created over HTTP" and d["cursor"]==2'
TEAROOM_SERVER="$BASE" TEAROOM_ACTOR=same "$TEAROOM" event send "received by CLI" >/dev/null
curl -fsS "$BASE/api/events?actor=other&after=2" |
  python3 -c 'import json,sys; d=json.load(sys.stdin); assert d["events"][0]["body"]=="received by CLI" and d["cursor"]==3'
printf '  ok   different agent products cross sessions\n'

# A stale raw client is rejected, proving the rule remains below the CLI.
code=$(curl -sS -o "$WORK/stale.json" -w '%{http_code}' -X POST "$BASE/api/events" -H 'Content-Type: application/json' \
  -d '{"actor":"other","expected":1,"type":"message","body":"obsolete","key":"stale-http"}')
[ "$code" = 409 ]
python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); assert d["kind"]=="stale_write" and d["actual"]==3' "$WORK/stale.json"
printf '  ok   stale raw HTTP writer is rejected\n'

# Server-held watch: the waiting CLI learns about a later Event without
# polling. The append goes through the server, so the wait is broadcast.
TEAROOM_SERVER="$BASE" TEAROOM_ACTOR=same "$TEAROOM" event list >/dev/null
( sleep 1; curl -fsS -X POST "$BASE/api/events" -H 'Content-Type: application/json' \
  -d '{"actor":"other","expected":3,"type":"message","body":"wake up","key":"wake-key"}' >/dev/null ) &
CURL_PID=$!
W=$(TEAROOM_SERVER="$BASE" TEAROOM_ACTOR=same "$TEAROOM" event watch --timeout 5s)
printf '%s' "$W" |
  python3 -c 'import json,sys; d=json.load(sys.stdin); assert d["events"][0]["body"]=="wake up" and d["cursor"]==4'
printf '  ok   server-held watch returns the later Event\n'
# Wait only for the appending curl, never for the long-running server itself.
wait "$CURL_PID"

# Watch timeout still exits 3 against a server.
CODE=0
TEAROOM_SERVER="$BASE" TEAROOM_ACTOR=same "$TEAROOM" event watch --timeout 200ms >/dev/null 2>&1 || CODE=$?
[ "$CODE" = 3 ]
printf '  ok   server watch timeout exits 3\n'
