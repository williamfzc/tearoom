#!/usr/bin/env bash
# The game protocol user story (S3): one Werewolf round over directed role
# Events. A moderator deals private roles, night actions stay invisible to
# everyone except their sender and the moderator, and the day is public.
# Story S3; see docs/evaluations.md. Everything is a protocol over Events —
# no game entity exists in the kernel.
# Evaluation: E6 (see docs/evaluations.md)
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
jlen(){ python3 -c 'import json,sys; d=json.load(sys.stdin)
for k in sys.argv[1].split("."): d=d[int(k)] if k.isdigit() else d[k]
print(len(d))' "$1"; }

"$TEAROOM" room init --name werewolf >/dev/null

# Players register before roles are dealt: a directed Event requires every
# recipient to already exist in the Room.
for player in wolf villager seer; do
  TEAROOM_ACTOR=$player "$TEAROOM" event watch --timeout 1s >/dev/null 2>&1 || true
done

# Every send follows the product's stale-write pattern: if the room moved
# past the actor's cursor the append is refused (exit 2), so resync with
# list and retry. Who goes first does not matter; the store decides.
resync_send(){ local actor=$1; shift
  local out
  for _ in 1 2 3; do
    out=$(TEAROOM_ACTOR=$actor "$TEAROOM" event send "$@" 2>/dev/null) && { printf '%s' "$out"; return 0; }
    TEAROOM_ACTOR=$actor "$TEAROOM" event list >/dev/null 2>&1 || true
  done
  return 1
}

# Night zero: the moderator deals three private roles. Each player sees only
# its own role Event.
R1=$(resync_send moderator wolf --type role.assigned --to wolf)
R2=$(resync_send moderator villager --type role.assigned --to villager)
R3=$(resync_send moderator seer --type role.assigned --to seer)
check "roles are dealt in order" "$(printf '%s' "$R3" | jget seq)" "3"

# Night actions: the seer checks, the wolf kills. Both are directed to the
# moderator only, so the other player must never see them.
resync_send seer --data '{"target":"wolf"}' --type seer.check --to moderator >/dev/null
resync_send wolf "kill villager" --type wolf.kill --to moderator >/dev/null

# Dawn: the moderator announces publicly, the villager votes, the game ends.
resync_send moderator "night 1: villager died" --type day.break >/dev/null
resync_send villager "vote wolf" --type vote.cast --caused-by e6 --to moderator >/dev/null
G=$(resync_send moderator "wolves win" --type game.over --caused-by e7)
check "the game ends chained to the vote" "$(printf '%s' "$G" | jget caused_by)" "e7"

# Visibility, read from the start of the room: what each player's full feed
# should be. The moderator sees the whole game.
M=$(TEAROOM_ACTOR=moderator "$TEAROOM" event list --cursor 0)
check "moderator sees all eight Events" "$(printf '%s' "$M" | jlen events)" "8"
check "the game closes publicly" "$(printf '%s' "$M" | jget events.7.type)" "game.over"

# The wolf sees its own role and kill, the public day, and nothing else.
W=$(TEAROOM_ACTOR=wolf "$TEAROOM" event list --cursor 0)
check "wolf sees four Events" "$(printf '%s' "$W" | jlen events)" "4"
check "wolf keeps its own role" "$(printf '%s' "$W" | jget events.0.body)" "wolf"
check "wolf's kill reaches its sender" "$(printf '%s' "$W" | jget events.1.body)" "kill villager"

# The villager never learns who the wolf is, nor the seer's check.
V=$(TEAROOM_ACTOR=villager "$TEAROOM" event list --cursor 0)
check "villager sees four Events" "$(printf '%s' "$V" | jlen events)" "4"
check "villager knows only its own role" "$(printf '%s' "$V" | jget events.0.body)" "villager"
printf '%s' "$V" | grep -q '"type": "wolf.kill"' && bad "wolf.kill leaks to the villager" || ok "wolf.kill stays hidden from the villager"
printf '%s' "$V" | grep -q '"type": "seer.check"' && bad "seer.check leaks to the villager" || ok "seer.check stays hidden from the villager"

# The seer sees its own check but not the wolf's kill.
S=$(TEAROOM_ACTOR=seer "$TEAROOM" event list --cursor 0)
check "seer sees its own check" "$(printf '%s' "$S" | jget events.1.type)" "seer.check"
printf '%s' "$S" | grep -q '"type": "wolf.kill"' && bad "wolf.kill leaks to the seer" || ok "wolf.kill stays hidden from the seer"

echo
if [ "$fail" -eq 0 ]; then printf '%d passed, 0 failed\n' "$pass"; exit 0; fi
printf '%d passed, %d failed\n' "$pass" "$fail"; exit 1
