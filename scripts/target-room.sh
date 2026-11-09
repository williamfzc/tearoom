#!/usr/bin/env bash
# Evaluation: E1 — the room-tree contract through the shipped binary
#
# Proves: named rooms with parents (duplicate names refused, unknown parents
# refused); per-room dense sequences that start at 1 in a child; concurrent
# append yields one dense sequence; a missing signature is stored as an empty
# author; an idempotent retry returns the original record and a key conflict
# is an error; tails, subtrees, and the skeleton print what the tree holds.
set -euo pipefail
cd "$(dirname "$0")/.."
BIN="$PWD/bin/tearoom"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
# This is a contract run, not a user journey: never touch a developer's Herdr.
export TEAROOM_HERDR_BIN="$WORK/missing-herdr"

pass=0
fail=0
check() {
  if [ "$2" -eq 0 ]; then pass=$((pass + 1)); echo "  ok   $1"
  else fail=$((fail + 1)); echo "  FAIL $1"; fi
}

# in_dir runs a command with the given working directory.
in_dir() { local dir="$1"; shift; ( cd "$dir" && "$@" ); }

# expect runs a command and checks that it exits 0.
expect() { local name="$1"; shift; local rc=0; in_dir "$@" >/dev/null 2>&1 || rc=$?; check "$name" "$rc"; }

# reject runs a command and checks that it exits nonzero.
reject() { local name="$1"; shift; local rc=0; in_dir "$@" >/dev/null 2>&1 || rc=$?; [ "$rc" -ne 0 ] && rc=0 || rc=1; check "$name" "$rc"; }

# seqs_of reads JSONL on stdin and prints the record sequence numbers.
seqs_of() {
  python3 -c '
import json, sys
seqs = [json.loads(ln)["seq"] for ln in sys.stdin if ln.strip()]
print(" ".join(str(s) for s in seqs))
'
}

# rooms_of reads JSONL on stdin and prints the room names in order.
rooms_of() {
  python3 -c '
import json, sys
print(",".join(json.loads(ln)["room"] for ln in sys.stdin if ln.strip()))
'
}

# dense checks that the given sequence string is exactly 1..N.
dense() {
  python3 - "$1" <<'PY'
import sys
seqs = [int(x) for x in sys.argv[1].split()]
sys.exit(0 if seqs == list(range(1, len(seqs) + 1)) else 1)
PY
}

echo
echo "E1: the room tree"
local_room="$WORK/local-room"
mkdir -p "$local_room"

expect "room init creates the named root" "$local_room" "$BIN" room init --name main-line
expect "room init creates a child under its parent" "$local_room" "$BIN" room init --name implementation --parent main-line
expect "a second root joins the same workspace" "$local_room" "$BIN" room init --name review --parent main-line
reject "a duplicate room name is refused" "$local_room" "$BIN" room init --name implementation --parent main-line
reject "an unknown parent is refused" "$local_room" "$BIN" room init --name orphan --parent missing
reject "a blank name is refused" "$local_room" "$BIN" room init --name "  "

# Each room owns its own stream; the child's first record is seq 1.
for text in one two three; do
  expect "main-line records $text" "$local_room" "$BIN" send "$text" --room main-line --as planner
done
expect "implementation records its first note" "$local_room" "$BIN" send "started" --room implementation --as codex
main_seqs="$(in_dir "$local_room" "$BIN" read --room main-line | seqs_of)"
impl_seqs="$(in_dir "$local_room" "$BIN" read --room implementation | seqs_of)"
check "main-line holds the dense 1..3 ($main_seqs)" "$(dense "$main_seqs" && echo 0 || echo 1)"
check "implementation starts its own stream at 1 ($impl_seqs)" "$([ "$impl_seqs" = "1" ] && echo 0 || echo 1)"

# The skeleton is derived from the rooms listing.
tree_out="$(in_dir "$local_room" "$BIN" tree)"
check "tree prints the skeleton" "$([ "$tree_out" = "main-line
├── implementation
└── review" ] && echo 0 || echo 1)"

# A missing signature is stored as an empty author.
anon="$(in_dir "$local_room" env -u TEAROOM_ACTOR "$BIN" send "anonymous" --room review)"
check "a record without a signature carries an empty author" \
  "$(printf '%s' "$anon" | python3 -c 'import json,sys; sys.exit(0 if json.load(sys.stdin)["author"] == "" else 1)' && echo 0 || echo 1)"

# Reads slice: a tail keeps the last records; a subtree walks the branch.
expect "implementation grows a child room" "$local_room" "$BIN" room init --name login-page --parent implementation
expect "login-page records a note" "$local_room" "$BIN" send "note" --room login-page
tail_seqs="$(in_dir "$local_room" "$BIN" read --room main-line --tail 2 | seqs_of)"
check "--tail 2 keeps the last two records ($tail_seqs)" "$([ "$tail_seqs" = "2 3" ] && echo 0 || echo 1)"
subtree_rooms="$(in_dir "$local_room" "$BIN" read --room implementation --subtree | rooms_of)"
check "--subtree walks implementation and its descendants ($subtree_rooms)" "$([ "$subtree_rooms" = "implementation,login-page" ] && echo 0 || echo 1)"
reject "an unknown room read is refused" "$local_room" "$BIN" read --room missing

# Idempotent retry returns the original record; a key conflict is an error.
first="$(in_dir "$local_room" "$BIN" send --json '{"n":1}' --idempotency-key local-k1 --room main-line --as planner)"
seq1="$(printf '%s' "$first" | python3 -c 'import json,sys; print(json.load(sys.stdin)["seq"])')"
retry="$(in_dir "$local_room" "$BIN" send --json '{"n": 1}' --idempotency-key local-k1 --room main-line --as planner)"
seq2="$(printf '%s' "$retry" | python3 -c 'import json,sys; print(json.load(sys.stdin)["seq"])')"
check "idempotent retry returns the original record (seq $seq1)" "$([ "$seq1" = "$seq2" ] && echo 0 || echo 1)"
reject "same key with different content conflicts" "$local_room" "$BIN" send --json '{"n":2}' --idempotency-key local-k1 --room main-line --as planner
check "the retry created no new record" "$(printf '%s' "$(in_dir "$local_room" "$BIN" read --room main-line | seqs_of)" | grep -q '^1 2 3 4$' && echo 0 || echo 1)"

# Six writers append concurrently to one room; the store issues one dense sequence.
expect "a fresh room hosts the concurrency run" "$local_room" "$BIN" room init --name busy
pids=()
for id in erin frank grace hank iris joy; do
  in_dir "$local_room" "$BIN" send "from $id" --room busy --as "$id" >/dev/null 2>&1 &
  pids+=("$!")
done
concurrent_rc=0
for p in "${pids[@]}"; do wait "$p" || concurrent_rc=1; done
check "all six concurrent appends succeed" "$concurrent_rc"
busy_seqs="$(in_dir "$local_room" "$BIN" read --room busy | seqs_of)"
rc=0; dense "$busy_seqs" || rc=$?
check "concurrent appends yield the dense sequence 1..6 ($busy_seqs)" "$rc"

echo
if [ "$fail" -eq 0 ]; then
  printf 'E1 passed: %d checks\n\n' "$pass"
else
  printf 'E1 FAILED: %d of %d checks failed\n\n' "$fail" "$((pass + fail))"
  exit 1
fi
