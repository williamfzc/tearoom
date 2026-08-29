#!/usr/bin/env bash
# Drive the shipped binary through the short event path and advanced overrides.
# Evaluation: E2 (see docs/evaluations.md)
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TEAROOM="$ROOT/bin/tearoom"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
cd "$WORK"
export TEAROOM_HOME="$WORK/.client"

pass=0; fail=0
ok(){ printf '  ok   %s\n' "$1"; pass=$((pass+1)); }
bad(){ printf '  FAIL %s\n' "$1"; fail=$((fail+1)); }
check(){ [ "$2" = "$3" ] && ok "$1" || bad "$1 (got '$2', want '$3')"; }
jget(){ python3 -c 'import json,sys; d=json.load(sys.stdin)
for k in sys.argv[1].split("."): d=d[int(k)] if k.isdigit() else d[k]
print(d)' "$1"; }

HELP=$("$TEAROOM" --help)
printf '%s' "$HELP" | grep -q '^  room'
ROOM_HELP=$?
printf '%s' "$HELP" | grep -q '^  server'
SERVER_HELP=$?
printf '%s' "$HELP" | grep -q '^  event'
EVENT_HELP=$?
check "root help exposes three concerns" "$((ROOM_HELP + SERVER_HELP + EVENT_HELP))" "0"
"$TEAROOM" event send --help | grep -q 'tearoom event send "review worktree A"'
check "send help leads with the short path" "$?" "0"
"$TEAROOM" actor >/dev/null 2>&1
check "actor is not a top-level command" "$?" "1"

ROOM=$("$TEAROOM" room init --name smoke)
check "room init defaults to JSON" "$(printf '%s' "$ROOM" | jget name)" "smoke"
check "room info returns current room" "$("$TEAROOM" room info | jget name)" "smoke"

# Ordinary send auto-registers the actor, derives request id, and advances state.
E1=$(TEAROOM_ACTOR=alice "$TEAROOM" event send "please review worktree A")
check "short send creates first Event" "$(printf '%s' "$E1" | jget seq)" "1"
check "short send defaults to message" "$(printf '%s' "$E1" | jget type)" "message"

# Bob starts at cursor zero and list advances Bob's own local cursor.
B=$(TEAROOM_ACTOR=bob "$TEAROOM" event list)
check "list sees currently available Event" "$(printf '%s' "$B" | jget events.0.body)" "please review worktree A"
check "list advances Bob cursor" "$(printf '%s' "$B" | jget cursor)" "1"
EMPTY=$(TEAROOM_ACTOR=bob "$TEAROOM" event list)
check "second list starts after saved cursor" "$(printf '%s' "$EMPTY" | jget cursor)" "1"
check "second list is empty" "$(printf '%s' "$EMPTY" | jget events)" "[]"

E2=$(TEAROOM_ACTOR=bob "$TEAROOM" event send "review complete" --type reply --caused-by e1)
check "advanced fields remain optional" "$(printf '%s' "$E2" | jget seq)" "2"

# Alice is still based on cursor one, so another actor's Event makes her write stale.
TEAROOM_ACTOR=alice "$TEAROOM" event send "stale assumption" >/dev/null 2>&1
check "saved cursor still protects stale writes" "$?" "2"
TEAROOM_ACTOR=alice "$TEAROOM" event list >/dev/null
E3=$(TEAROOM_ACTOR=alice "$TEAROOM" event send wolf --type role.assigned --to bob)
check "list then send succeeds" "$(printf '%s' "$E3" | jget seq)" "3"

# An explicit cursor and id support protocol clients and safe retries.
R1=$(TEAROOM_ACTOR=alice "$TEAROOM" event send --body manual --cursor 3 --id manual-1)
R2=$(TEAROOM_ACTOR=alice "$TEAROOM" event send --body manual --cursor 4 --id manual-1)
check "explicit id makes retry idempotent" "$(printf '%s' "$R2" | jget id)" "$(printf '%s' "$R1" | jget id)"

# Watch uses saved state and advances it when another Actor writes.
TEAROOM_ACTOR=bob "$TEAROOM" event list >/dev/null
( sleep 1; TEAROOM_ACTOR=alice "$TEAROOM" event send wake >/dev/null ) &
W=$(TEAROOM_ACTOR=bob "$TEAROOM" event watch --timeout 5s)
check "watch returns the later Event" "$(printf '%s' "$W" | jget events.0.body)" "wake"
wait

TEAROOM_ACTOR=bob "$TEAROOM" event watch --timeout 200ms --poll-interval 50ms >/dev/null 2>&1
check "watch timeout uses exit 3" "$?" "3"

( cd / && TEAROOM_ACTOR=alice "$TEAROOM" event list >/dev/null 2>&1 )
check "missing local room uses exit 4" "$?" "4"
TEAROOM_SERVER=http://127.0.0.1:1 TEAROOM_ACTOR=alice "$TEAROOM" event list >/dev/null 2>&1
check "unreachable server uses exit 4" "$?" "4"

echo
if [ "$fail" -eq 0 ]; then printf '%d passed, 0 failed\n' "$pass"; exit 0; fi
printf '%d passed, %d failed\n' "$pass" "$fail"; exit 1
