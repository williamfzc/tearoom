#!/usr/bin/env bash
# End-to-end check of the M0 hypothesis: two agents coordinate a handoff with no
# human relaying anything, and the conflict/gate/evidence rules actually hold.
#
# This deliberately drives the real binary rather than calling Go functions, so it
# also covers exit codes — which are contractual, since agents branch on them.
set -uo pipefail

TEA="$(cd "$(dirname "$0")/.." && pwd)/bin/tea"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
cd "$WORK"

pass=0; fail=0
ok()   { printf '  \033[32mok\033[0m   %s\n' "$1"; pass=$((pass+1)); }
bad()  { printf '  \033[31mFAIL\033[0m %s\n' "$1"; fail=$((fail+1)); }
check(){ [ "$2" = "$3" ] && ok "$1" || bad "$1 (got '$2', want '$3')"; }

# jq is not assumed present, and grepping JSON by hand proved fragile. Python is
# available everywhere we care about and parses the real structure.
jget(){ python3 -c 'import json,sys;d=json.load(sys.stdin);
p=sys.argv[1].split(".")
for k in p:
    d=d[int(k)] if k.isdigit() else d[k]
print(d)' "$1"; }

echo
echo "tearoom smoke test in $WORK"
echo

# --- setup -------------------------------------------------------------------
"$TEA" init --name smoke >/dev/null
[ -d .tearoom ] && ok "init created workspace" || bad "init did not create workspace"

# Re-init must refuse: silently resetting someone's coordination state would be
# unforgivable.
"$TEA" init --name again >/dev/null 2>&1
check "re-init refuses to clobber" "$?" "1"

export TEA_AGENT=impl
"$TEA" join --role implementer >/dev/null
TEA_AGENT=verify "$TEA" join --role verifier >/dev/null
ok "two agents joined"

# --- thread handoff ----------------------------------------------------------
TID=$("$TEA" post "fix flaky auth test" --body "fails 1 in 5 on CI" --json | jget thread.id)
check "post returned a short id" "$TID" "t1"

"$TEA" claim "src/auth/**" --ttl 5m --thread "$TID" >/dev/null
check "impl claimed the resource" "$?" "0"

# The core promise: a second agent cannot take an overlapping lease, and finds out
# with exit 2 rather than a crash or a silent success.
TEA_AGENT=verify "$TEA" claim "src/auth/login.ts" --ttl 5m >/dev/null 2>&1
check "overlapping claim rejected with exit 2" "$?" "2"

# The rejection has to be actionable, not just a code.
CONFLICT=$(TEA_AGENT=verify "$TEA" claim "src/auth/login.ts" 2>&1)
echo "$CONFLICT" | grep -q "impl" && ok "conflict names the holder" || bad "conflict does not name the holder"
echo "$CONFLICT" | grep -qE "remaining|until" && ok "conflict states when it frees up" || bad "conflict has no expiry"

# A non-overlapping path must still be claimable, or leases would be uselessly coarse.
TEA_AGENT=verify "$TEA" claim "test/**" --ttl 5m >/dev/null 2>&1
check "non-overlapping claim allowed" "$?" "0"

"$TEA" reply "$TID" --body "patched retry logic" --status claimed >/dev/null
OWNER=$("$TEA" context "$TID" --json | jget thread.owner)
check "claiming sets thread owner" "$OWNER" "impl"

# --- evidence rule -----------------------------------------------------------
# "Done" with no proof is the most common way a multi-agent run quietly goes wrong.
TEA_AGENT=verify "$TEA" reply "$TID" --body "looks fine" --status resolved >/dev/null 2>&1
check "resolve without evidence refused" "$?" "1"

echo "3 runs, 0 failures" > ci.log
TEA_AGENT=verify "$TEA" reply "$TID" --body "suite green" --status resolved --evidence ci.log >/dev/null
check "resolve with evidence accepted" "$?" "0"

STATUS=$("$TEA" context "$TID" --json | jget thread.status)
check "thread resolved" "$STATUS" "resolved"

# --- lease ownership ---------------------------------------------------------
TEA_AGENT=verify "$TEA" release "src/auth/**" >/dev/null 2>&1
check "non-holder cannot release" "$?" "1"

"$TEA" release "src/auth/**" >/dev/null
check "holder can release" "$?" "0"

TEA_AGENT=verify "$TEA" claim "src/auth/login.ts" --ttl 5m >/dev/null 2>&1
check "resource claimable after release" "$?" "0"

# --- gates -------------------------------------------------------------------
T2=$("$TEA" post "run prod migration" --json | jget thread.id)
GID=$("$TEA" gate "needs DBA sign-off" --thread "$T2" --json | jget gate.id)
[ -n "$GID" ] && ok "gate opened" || bad "gate not opened"

# Opening a gate must block the thread, so the board shows one problem rather than
# a gate plus an apparently-healthy thread.
GS=$("$TEA" context "$T2" --json | jget thread.status)
check "gate blocks its thread" "$GS" "blocked"

# wait must not return before a decision exists.
"$TEA" wait --gate "$GID" --timeout 2s --interval 300ms >/dev/null 2>&1
check "wait on pending gate times out with exit 3" "$?" "3"

"$TEA" gate approve "$GID" --note "reviewed" >/dev/null
check "gate approved" "$?" "0"

"$TEA" wait --gate "$GID" --timeout 3s --interval 300ms >/dev/null 2>&1
check "wait returns once gate is decided" "$?" "0"

"$TEA" gate approve "$GID" >/dev/null 2>&1
check "settled gate cannot be re-decided" "$?" "1"

# --- wait wakes on a real event ---------------------------------------------
T3=$("$TEA" post "async handoff" --json | jget thread.id)
( sleep 1; TEA_AGENT=verify "$TEA" reply "$T3" --body "picked it up" >/dev/null ) &
"$TEA" wait "$T3" --timeout 6s --interval 500ms >/dev/null 2>&1
check "wait wakes on a new event" "$?" "0"
wait

# --- inbox filtering ---------------------------------------------------------
# An inbox that returns everything is just ls, and costs the agent tokens.
INBOX=$("$TEA" inbox)
echo "$INBOX" | grep -q "$TID" && bad "resolved thread still in inbox" || ok "resolved thread excluded from inbox"

# --- oversight ---------------------------------------------------------------
"$TEA" status >/dev/null
check "status renders" "$?" "0"

"$TEA" status --json | python3 -c 'import json,sys; d=json.load(sys.stdin); sys.exit(0 if "attention" in d and "agents" in d and "threads" in d else 1)' \
  && ok "status --json exposes a full board" || bad "status --json missing board fields"

LEDGER=$("$TEA" ledger --tail 50 | wc -l | tr -d ' ')
[ "$LEDGER" -gt 5 ] && ok "ledger recorded activity ($LEDGER entries)" || bad "ledger too short: $LEDGER"

# --- gate must move its thread, whichever client decides it -------------------
# Regression: the gate->thread transition once lived in the CLI, so approving from
# the web console settled the gate but left the thread blocked forever.
GT=$("$TEA" post "gated work" --json | jget thread.id)
GID=$("$TEA" gate "needs sign-off" --thread "$GT" --json | jget gate.id)
[ "$("$TEA" context "$GT" --json | jget thread.status)" = blocked ] \
  && ok "opening a gate blocks its thread" || bad "gate did not block its thread"

"$TEA" gate approve "$GID" >/dev/null 2>&1
AFTER=$("$TEA" context "$GT" --json | jget thread.status)
[ "$AFTER" = claimed ] && ok "approving a gate resumes its thread" \
  || bad "after approval thread is $AFTER, want claimed (would hang forever)"
OWNER=$("$TEA" context "$GT" --json | jget thread.owner)
[ "$OWNER" != human ] && ok "approval did not reassign work to the human" \
  || bad "approval made the human the owner"

RT=$("$TEA" post "rejected work" --json | jget thread.id)
RID=$("$TEA" gate "risky" --thread "$RT" --json | jget gate.id)
"$TEA" gate reject "$RID" >/dev/null 2>&1
RSTATUS=$("$TEA" context "$RT" --json | jget thread.status)
[ "$RSTATUS" = open ] && ok "rejecting a gate reopens its thread" \
  || bad "after rejection thread is $RSTATUS, want open"

# Deciding twice would let a rejection be quietly overturned.
"$TEA" gate approve "$RID" >/dev/null 2>&1 && bad "a settled gate was decided twice" \
  || ok "a settled gate cannot be decided again"

# Audit trail must include the security-relevant actions, not just chatter. Read it
# once: querying per action with a fixed --tail window silently drops early entries.
ALL_LEDGER=$("$TEA" ledger --tail 1000)
for action in thread.create lease.claim lease.release gate.open gate.approved; do
  echo "$ALL_LEDGER" | grep -q "$action" && ok "ledger has $action" || bad "ledger missing $action"
done

# --- error handling ----------------------------------------------------------
"$TEA" context nonexistent >/dev/null 2>&1
check "unknown thread fails cleanly" "$?" "1"

( cd / && TEA_AGENT=x "$TEA" post "outside a workspace" >/dev/null 2>&1 )
check "no workspace gives exit 4" "$?" "4"

env -u TEA_AGENT "$TEA" post "no identity" >/dev/null 2>&1
check "missing identity is rejected" "$?" "1"

# Pointing at a dead server must fail loudly, never look like a no-op.
TEA_SERVER=http://127.0.0.1:1 TEA_AGENT=x "$TEA" claim "a/**" >/dev/null 2>&1
check "unreachable server gives exit 4" "$?" "4"

echo
if [ "$fail" -eq 0 ]; then
  printf '\033[32m%d passed, 0 failed\033[0m\n\n' "$pass"
  exit 0
else
  printf '\033[31m%d passed, %d FAILED\033[0m\n\n' "$pass" "$fail"
  exit 1
fi
