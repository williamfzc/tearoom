#!/usr/bin/env bash
# The one-Room-across-worktrees user story (S4): two real git worktrees share
# one Room served from the repository root. Each worktree's agent keeps its
# own cursor; handoffs cross worktrees through watch; a later process in the
# same worktree resumes the same Actor and cursor. Story S4; see
# docs/evaluations.md.
# Evaluation: E7 (see docs/evaluations.md)
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
jlen(){ python3 -c 'import json,sys; d=json.load(sys.stdin)
for k in sys.argv[1].split("."): d=d[int(k)] if k.isdigit() else d[k]
print(len(d))' "$1"; }

# A real repository with two worktrees.
git init -q repo
git -C repo -c user.email=t@t -c user.name=t commit -q --allow-empty -m seed
git -C repo worktree add -q ../wt-a
git -C repo worktree add -q ../wt-b
check "two real worktrees exist" "$(git -C repo worktree list | wc -l | tr -d ' ')" "3"

# One Room lives at the repository root and is served; the worktrees have no
# local .tearoom of their own.
cd "$WORK/repo"
"$TEAROOM" room init --name shared >/dev/null
check "no local Room inside a worktree" "$(test ! -e "$WORK/wt-a/.tearoom" && echo yes)" "yes"
PORT="$(python3 - <<'PY'
import socket
s=socket.socket(); s.bind(('127.0.0.1',0)); print(s.getsockname()[1]); s.close()
PY
)"
"$TEAROOM" server start --address "127.0.0.1:$PORT" --directory "$WORK/repo" >server.log 2>&1 &
SERVER_PID=$!
for _ in $(seq 1 50); do curl -fsS "http://127.0.0.1:$PORT/api/room" >/dev/null && break; sleep .1; done
export TEAROOM_SERVER="http://127.0.0.1:$PORT"

# Alice in wt-a requests a handoff; Bob in wt-b picks it up by watching.
E1=$(cd "$WORK/wt-a" && TEAROOM_ACTOR=alice "$TEAROOM" event send "handoff: fix tests" --type handoff.requested)
H1=$(cd "$WORK/wt-b" && TEAROOM_ACTOR=bob "$TEAROOM" event watch --timeout 30s)
check "Bob sees the handoff across worktrees" "$(printf '%s' "$H1" | jget events.0.body)" "handoff: fix tests"
E2=$(cd "$WORK/wt-b" && TEAROOM_ACTOR=bob "$TEAROOM" event send "accepted, working" --type handoff.accepted --caused-by e1)
check "acceptance is chained across worktrees" "$(printf '%s' "$E2" | jget caused_by)" "e1"

# A later Alice process in wt-a resumes the same Actor and cursor.
H2=$(cd "$WORK/wt-a" && TEAROOM_ACTOR=alice "$TEAROOM" event watch --timeout 30s)
check "Alice resumes and sees the acceptance" "$(printf '%s' "$H2" | jget events.0.type)" "handoff.accepted"
E3=$(cd "$WORK/wt-a" && TEAROOM_ACTOR=alice "$TEAROOM" event send "done" --type handoff.done --caused-by e2)
check "completion is chained to the acceptance" "$(printf '%s' "$E3" | jget caused_by)" "e2"

# Bob resumes in wt-b and sees the completion.
H3=$(cd "$WORK/wt-b" && TEAROOM_ACTOR=bob "$TEAROOM" event watch --timeout 30s)
check "Bob resumes and sees the completion" "$(printf '%s' "$H3" | jget events.0.type)" "handoff.done"

# A fresh Actor in either worktree reads the full trace through one Room.
C=$(cd "$WORK/wt-a" && TEAROOM_ACTOR=charlie "$TEAROOM" event list)
check "a fresh Actor sees all three Events" "$(printf '%s' "$C" | jlen events)" "3"
check "the trace is chained end to end" "$(printf '%s' "$C" | jget events.2.caused_by)" "e2"

echo
if [ "$fail" -eq 0 ]; then printf '%d passed, 0 failed\n' "$pass"; exit 0; fi
printf '%d passed, %d failed\n' "$pass" "$fail"; exit 1
