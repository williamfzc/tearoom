#!/usr/bin/env bash
# The harness workflow user story, run against the shipped binary: planner,
# generator, and evaluator agents run one plan/generate/evaluate loop through
# a Room with no human relay. The loop is a protocol over Events — watch for
# handoff, caused-by for chaining, directed Events for private reviewer notes —
# not new kernel machinery. See docs/domain/harness-workflow.md.
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

"$TEAROOM" room init --name harness >/dev/null

# Planner opens the loop with a plan proposal.
P1=$(TEAROOM_ACTOR=planner "$TEAROOM" event send "plan: implement the ledger module" --type plan.proposed)
check "planner opens the loop" "$(printf '%s' "$P1" | jget seq)" "1"

# Generator waits on the Room, then reports generated code chained to the plan.
TEAROOM_ACTOR=generator "$TEAROOM" event watch --timeout 5s >/dev/null
G1=$(TEAROOM_ACTOR=generator "$TEAROOM" event send "generated: ledger.go" --type generation.reported --caused-by e1)
check "generation is chained to the plan" "$(printf '%s' "$G1" | jget caused_by)" "e1"

# Evaluator waits, then asks for a revision with structured data.
TEAROOM_ACTOR=evaluator "$TEAROOM" event watch --timeout 5s >/dev/null
E1=$(TEAROOM_ACTOR=evaluator "$TEAROOM" event send --data '{"score":0.6,"verdict":"revise"}' --type evaluation.reported --caused-by e2)
check "first evaluation is chained to the generation" "$(printf '%s' "$E1" | jget caused_by)" "e2"
check "evaluation carries structured data" "$(printf '%s' "$E1" | jget data.verdict)" "revise"

# Generator waits for the verdict and revises.
TEAROOM_ACTOR=generator "$TEAROOM" event watch --timeout 5s >/dev/null
G2=$(TEAROOM_ACTOR=generator "$TEAROOM" event send "generated: ledger.go v2" --type generation.reported --caused-by e3)
check "revision is chained to the first evaluation" "$(printf '%s' "$G2" | jget caused_by)" "e3"

# Evaluator approves, then whispers a private note only the generator may see.
TEAROOM_ACTOR=evaluator "$TEAROOM" event watch --timeout 5s >/dev/null
E2=$(TEAROOM_ACTOR=evaluator "$TEAROOM" event send --data '{"score":1.0,"verdict":"approved"}' --type evaluation.reported --caused-by e4)
check "approval is chained to the revision" "$(printf '%s' "$E2" | jget caused_by)" "e4"
TEAROOM_ACTOR=evaluator "$TEAROOM" event send "keep the two-phase commit" --type note.whisper --to generator --caused-by e5 >/dev/null

# The whisper reaches the generator (whose watch also sees the approval)...
WG=$(TEAROOM_ACTOR=generator "$TEAROOM" event watch --timeout 5s)
check "generator receives the whisper" "$(printf '%s' "$WG" | jget events.1.body)" "keep the two-phase commit"

# ...while the planner's close is refused: its cursor stopped at its own plan.
CODE=0
TEAROOM_ACTOR=planner "$TEAROOM" event send "close: task accepted" --type task.completed --caused-by e5 >/dev/null 2>&1 || CODE=$?
check "planner's stale close is refused" "$CODE" "2"

# Planner resyncs (the stale-write pattern) and closes against the real head.
PL=$(TEAROOM_ACTOR=planner "$TEAROOM" event list)
check "planner resyncs: four visible Events, the whisper hidden" "$(printf '%s' "$PL" | jlen events)" "4"
check "the last visible Event is the approval" "$(printf '%s' "$PL" | jget events.3.data.verdict)" "approved"
C=$(TEAROOM_ACTOR=planner "$TEAROOM" event send "close: task accepted" --type task.completed --caused-by e5)
check "planner closes against the real head" "$(printf '%s' "$C" | jget seq)" "7"

# The loop closes where it opened: the evaluator watches the completion.
WE=$(TEAROOM_ACTOR=evaluator "$TEAROOM" event watch --timeout 5s)
check "evaluator sees the task close" "$(printf '%s' "$WE" | jget events.0.type)" "task.completed"

echo
if [ "$fail" -eq 0 ]; then printf '%d passed, 0 failed\n' "$pass"; exit 0; fi
printf '%d passed, %d failed\n' "$pass" "$fail"; exit 1
