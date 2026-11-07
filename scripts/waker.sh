#!/usr/bin/env bash
# The external waker: consume `tearoom watch --json`, follow the handoff
# convention, and drive herdr.
#
# Tearoom only says "there is something new". This script owns the wake
# decision: it reads each appended record, and when that record carries a
# next field naming a worker, it prompts that agent through herdr and waits
# for the turn to settle.
#
# Handoff convention (a writing convention, not a kernel field): a record
# that wants to wake its next worker is a JSON object carrying
# {"next": "<agent name>"}. Records without one notify no one.
#
# Prerequisites, measured 2026-11-07 (see docs/operations/serving.md):
# - each agent tool's one-time trust dialogs and login are pre-cleared;
# - the agent already runs inside the herdr session (herdr agent start);
# - herdr does not watch files itself: this loop is the watch layer.
#
# Usage: scripts/waker.sh            (run inside the workspace directory)
#   TEAROOM_BIN    override the tearoom binary (default bin/tearoom)
#   WAKER_TIMEOUT_MS  override the herdr prompt timeout (default 240000)
set -euo pipefail
cd "$(dirname "$0")/.."
BIN="${TEAROOM_BIN:-$PWD/bin/tearoom}"
TIMEOUT_MS="${WAKER_TIMEOUT_MS:-240000}"

# next_worker prints the next field of the record identified by room and seq.
next_worker() {
  local room="$1" seq="$2"
  "$BIN" read --room "$room" --after "e$((seq - 1))" 2>/dev/null | python3 -c '
import json, sys
try:
    record = json.loads(sys.stdin.readline())
    content = record.get("content")
    if isinstance(content, dict):
        print(content.get("next", ""))
except Exception:
    pass
'
}

# wake_one delivers the room handoff to the named agent and waits.
wake_one() {
  local room="$1" seq="$2"
  local next
  next="$(next_worker "$room" "$seq")"
  if [ -z "$next" ]; then
    echo "waker: $room #$seq asks for no one" >&2
    return 0
  fi
  echo "waker: $room #$seq wakes $next" >&2
  herdr agent prompt "$next" \
    "Read the tearoom room '$room' in the workspace at $PWD ($BIN read --room '$room'), continue the work, and write your handoff back into that room." \
    --wait --timeout "$TIMEOUT_MS"
}

"$BIN" watch --json | while IFS= read -r line; do
  [ -z "$line" ] && continue
  envelope="$(printf '%s' "$line" | python3 -c '
import json, sys
envelope = json.loads(sys.stdin.readline())
print(envelope["room"], envelope["seq"])
')" || continue
  room="${envelope% *}"
  seq="${envelope#* }"
  wake_one "$room" "$seq" || true
done
