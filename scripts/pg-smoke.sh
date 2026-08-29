#!/usr/bin/env bash
# The relational Store and the hosted control plane against a real
# PostgreSQL, through the shipped binary. Gated by TEAROOM_TEST_PG (a
# postgres:// DSN): without it this suite skips with a notice, so `make
# check` runs anywhere and the suite runs wherever a database exists. The
# same invariants run in-process in internal/relational/pg_test.go.
# Evaluation: see docs/evaluations.md
set -uo pipefail

DSN="${TEAROOM_TEST_PG:-}"
if [ -z "$DSN" ]; then
  echo "TEAROOM_TEST_PG not set; skipping the PostgreSQL suite"
  exit 0
fi

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TEAROOM="$ROOT/bin/tearoom"
WORK="$(mktemp -d)"
SERVER_PID=""
cleanup(){ [ -z "$SERVER_PID" ] || kill "$SERVER_PID" 2>/dev/null || true; rm -rf "$WORK"; }
trap cleanup EXIT
cd "$WORK"
export TEAROOM_HOME="$WORK/.client"
export TEAROOM_HOSTED_DEV=1 TEAROOM_HOSTED_ADMIN=dev-admin-token

pass=0; fail=0
ok(){ printf '  ok   %s\n' "$1"; pass=$((pass+1)); }
bad(){ printf '  FAIL %s\n' "$1"; fail=$((fail+1)); }
check(){ [ "$2" = "$3" ] && ok "$1" || bad "$1 (got '$2', want '$3')"; }
jget(){ python3 -c 'import json,sys; d=json.load(sys.stdin)
for k in sys.argv[1].split("."): d=d[int(k)] if k.isdigit() else d[k]
print(d)' "$1"; }
jlen(){ python3 -c 'import json,sys; d=json.load(sys.stdin)
for k in sys.argv[1].split("."): d=d[int(k)] if k.isdigit() else d[k]
print(len(d))' "$1"; }

# The hosted CLI drives the control plane directly on the shared database.
"$TEAROOM" hosted init-db --database "$DSN" >/dev/null
RID=$("$TEAROOM" hosted create-room --database "$DSN" --name pg-ops | jget id)
check "create-room against PostgreSQL returns an id" "$(test -n "$RID" && echo yes)" "yes"

TOKEN=$("$TEAROOM" hosted issue-credential --database "$DSN" --room "$RID" \
  --actor alice --capabilities read,append | jget token)
check "issue-credential returns a token" "$(test -n "$TOKEN" && echo yes)" "yes"

# Serve the hosted event API over PostgreSQL.
PORT="$(python3 - <<'PY'
import socket
s=socket.socket(); s.bind(('127.0.0.1',0)); print(s.getsockname()[1]); s.close()
PY
)"
"$TEAROOM" hosted serve --database "$DSN" --address "127.0.0.1:$PORT" >server.log 2>&1 &
SERVER_PID=$!
export TEAROOM_SERVER="http://127.0.0.1:$PORT/r/$RID"
for _ in $(seq 1 50); do
  TEAROOM_ACTOR=alice TEAROOM_TOKEN="$TOKEN" "$TEAROOM" room info >/dev/null 2>&1 && break
  sleep .2
done

E1=$(TEAROOM_ACTOR=alice TEAROOM_TOKEN="$TOKEN" "$TEAROOM" event send "over postgres" --type pg.first)
check "the first event lands on PostgreSQL" "$(printf '%s' "$E1" | jget seq)" "1"

# Twelve writers race the same cursor over PostgreSQL: the store serializes
# them, the losers resync and retry, and all twelve land in order.
for i in $(seq 1 12); do
  TEAROOM_ACTOR="w$i" TEAROOM_TOKEN="$TOKEN" "$TEAROOM" event watch --timeout 1s >/dev/null 2>&1 || true
done
for i in $(seq 1 12); do
  TEAROOM_ACTOR="w$i" TEAROOM_TOKEN="$TOKEN" "$TEAROOM" event send "writer $i" --type pg.write >/dev/null 2>&1 &
done
wait
sleep .5
F=$(TEAROOM_ACTOR=alice TEAROOM_TOKEN="$TOKEN" "$TEAROOM" event list --cursor 0)
check "all thirteen events land over PostgreSQL" "$(printf '%s' "$F" | jlen events)" "13"
check "the room head is dense" "$(printf '%s' "$F" | jget cursor)" "13"

echo
if [ "$fail" -eq 0 ]; then printf '%d passed, 0 failed\n' "$pass"; exit 0; fi
printf '%d passed, %d failed\n' "$pass" "$fail"; exit 1
