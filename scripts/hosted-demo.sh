#!/usr/bin/env bash
# The hosted stories (S5-S8): a multi-tenant server, an admin who issues
# revocable credentials, agents who act only as themselves, and a read-only
# credential that reads and cannot append. Dev mode, SQLite control
# database — no external dependencies. Story S5-S8; see docs/evaluations.md.
# Evaluation: E8 (see docs/evaluations.md)
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TEAROOM="$ROOT/bin/tearoom"
WORK="$(mktemp -d)"
SERVER_PID=""
cleanup(){ [ -z "$SERVER_PID" ] || kill "$SERVER_PID" 2>/dev/null || true; rm -rf "$WORK"; }
trap cleanup EXIT
cd "$WORK"
export TEAROOM_HOME="$WORK/.client"
export TEAROOM_HOSTED_DEV=1
export TEAROOM_HOSTED_ADMIN=dev-admin-token

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
code(){ curl -s -o /dev/null -w '%{http_code}' "$@"; }

# One shared database hosts everything; the dev admin is a bearer token.
"$TEAROOM" hosted init-db --database "$WORK/hosted.db" >/dev/null
PORT="$(python3 - <<'PY'
import socket
s=socket.socket(); s.bind(('127.0.0.1',0)); print(s.getsockname()[1]); s.close()
PY
)"
"$TEAROOM" hosted serve --database "$WORK/hosted.db" --address "127.0.0.1:$PORT" >server.log 2>&1 &
SERVER_PID=$!
B="http://127.0.0.1:$PORT"
AUTH="Authorization: Bearer dev-admin-token"
for _ in $(seq 1 50); do curl -fsS -H "$AUTH" "$B/api/hosted/rooms" >/dev/null && break; sleep .1; done

# Management routes demand the admin bearer.
check "management without a token is 401" \
  "$(code -X POST -H "Content-Type: application/json" -d '{"name":"x"}' "$B/api/hosted/rooms")" "401"

# The admin creates two rooms and issues credentials: alice and bob can read
# and append, carol can only read.
R1=$(curl -fsS -X POST -H "$AUTH" -H "Content-Type: application/json" -d '{"name":"ops"}' "$B/api/hosted/rooms" | jget id)
R2=$(curl -fsS -X POST -H "$AUTH" -H "Content-Type: application/json" -d '{"name":"staging"}' "$B/api/hosted/rooms" | jget id)
check "rooms are distinct tenants" "$(test "$R1" != "$R2" && echo yes)" "yes"

ALICE=$(curl -fsS -X POST -H "$AUTH" -H "Content-Type: application/json" -d '{"actor":"alice","capabilities":"read,append"}' "$B/api/hosted/rooms/$R1/credentials" | jget token)
BOB_RESP=$(curl -fsS -X POST -H "$AUTH" -H "Content-Type: application/json" -d '{"actor":"bob","capabilities":"read,append"}' "$B/api/hosted/rooms/$R1/credentials")
BOB_ID=$(printf '%s' "$BOB_RESP" | jget id)
BOB=$(printf '%s' "$BOB_RESP" | jget token)
CAROL=$(curl -fsS -X POST -H "$AUTH" -H "Content-Type: application/json" -d '{"actor":"carol","capabilities":"read"}' "$B/api/hosted/rooms/$R1/credentials" | jget token)

# A credential is bound to one actor in one room: the request must declare
# that actor, and the token must belong to the room it is used on.
check "no credential is 401" "$(code -H "Authorization: Bearer nope" "$B/r/$R1/api/events?actor=alice")" "401"
check "a credential for another room is 401" \
  "$(code -H "Authorization: Bearer $ALICE" "$B/r/$R2/api/events?actor=alice")" "401"
check "bob's credential cannot claim alice" \
  "$(code -H "Authorization: Bearer $BOB" "$B/r/$R1/api/events?actor=alice")" "403"

# The happy path runs through the ordinary CLI: TEAROOM_SERVER points at the
# room, TEAROOM_TOKEN carries the credential, TEAROOM_ACTOR is the identity.
export TEAROOM_SERVER="$B/r/$R1"
E1=$(TEAROOM_ACTOR=alice TEAROOM_TOKEN="$ALICE" "$TEAROOM" event send "fix the demo" --type handoff.requested)
check "alice sends through the hosted API" "$(printf '%s' "$E1" | jget seq)" "1"
H=$(TEAROOM_ACTOR=bob TEAROOM_TOKEN="$BOB" "$TEAROOM" event watch --timeout 10s)
check "bob sees the handoff" "$(printf '%s' "$H" | jget events.0.body)" "fix the demo"
E2=$(TEAROOM_ACTOR=bob TEAROOM_TOKEN="$BOB" "$TEAROOM" event send "accepted" --type handoff.accepted --caused-by e1)
check "acceptance is chained" "$(printf '%s' "$E2" | jget caused_by)" "e1"

# A later alice process resumes the same actor and cursor.
H2=$(TEAROOM_ACTOR=alice TEAROOM_TOKEN="$ALICE" "$TEAROOM" event watch --timeout 10s)
check "alice resumes her session" "$(printf '%s' "$H2" | jget events.0.type)" "handoff.accepted"

# A read-only credential reads everything but cannot append.
C=$(TEAROOM_ACTOR=carol TEAROOM_TOKEN="$CAROL" "$TEAROOM" event list --cursor 0)
check "carol reads both events" "$(printf '%s' "$C" | jlen events)" "2"
if TEAROOM_ACTOR=carol TEAROOM_TOKEN="$CAROL" "$TEAROOM" event send "sneaky" >/dev/null 2>&1; then
  bad "carol appends despite a read-only credential"
else
  ok "carol's append is refused"
fi
check "read-only append over HTTP is 403" \
  "$(code -X POST -H "Authorization: Bearer $CAROL" -H "Content-Type: application/json" \
    -d '{"actor":"carol","type":"message","body":"sneaky","expected":2,"key":"k"}' "$B/r/$R1/api/events")" "403"

# Revoking bob kills bob only; alice keeps working.
check "revoking bob succeeds" "$(code -X DELETE -H "$AUTH" "$B/api/hosted/rooms/$R1/credentials/$BOB_ID")" "200"
check "revoked bob is 401" "$(code -H "Authorization: Bearer $BOB" "$B/r/$R1/api/events?actor=bob")" "401"
E3=$(TEAROOM_ACTOR=alice TEAROOM_TOKEN="$ALICE" "$TEAROOM" event send "still working" --type handoff.done --caused-by e2)
check "alice works after bob's revocation" "$(printf '%s' "$E3" | jget seq)" "3"

echo
if [ "$fail" -eq 0 ]; then printf '%d passed, 0 failed\n' "$pass"; exit 0; fi
printf '%d passed, %d failed\n' "$pass" "$fail"; exit 1
