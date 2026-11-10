#!/usr/bin/env bash
# Enforce the production source line budget as a ratchet.
#
# A hard cap on a codebase that already carries stock fails on day one for
# debt nobody just wrote, and a gate whose first act is two hundred failures
# gets disabled. So this gate counts only what moves in the wrong direction:
# scripts/line-baseline.txt records the existing over-budget files with their
# current line counts, and the check fails only when a file exceeds the
# budget with no baseline line, grows past its recorded count, or leaves a
# stale baseline line behind after its debt was paid.
#
# The baseline only ever shrinks. Deleting a line, in the same commit as the
# split that made it true, is how the debt is paid back; adding a line needs
# the same review as a rule change, because it is one.
#
# Test batteries grow with the surface they prove and are governed by review.
# The browser face under rust/src/cli/web is production source too; count its
# assets so a growing page cannot slip past the same budget.
set -uo pipefail
cd "$(dirname "$0")/.."

fail=0
note() { printf '  \033[31mFAIL\033[0m %s\n' "$1"; fail=$((fail + 1)); }
good() { printf '  \033[32mok\033[0m   %s\n' "$1"; }

echo
echo "checking production line budget"
echo

if python3 - <<'PY'
import pathlib
import sys

BUDGET = 300
baseline_path = pathlib.Path("scripts/line-baseline.txt")

bad = []

over = {}
FRONTEND = {".css", ".html", ".js", ".svg"}
for p in sorted(pathlib.Path("rust/src").rglob("*")):
    if p.suffix != ".rs" and p.suffix not in FRONTEND:
        continue
    lines = p.read_text(encoding="utf-8").splitlines()
    n = len(lines)
    if p.suffix == ".rs":
        # Inline test modules are production-adjacent proof, not production source.
        n = next((i for i, line in enumerate(lines) if line.strip() == "#[cfg(test)]"), len(lines))
    if n > BUDGET:
        over[p.as_posix()] = n

base = {}
if not baseline_path.exists():
    bad.append(f"missing baseline: {baseline_path}")
else:
    for i, raw in enumerate(baseline_path.read_text(encoding="utf-8").splitlines(), 1):
        s = raw.strip()
        if not s or s.startswith("#"):
            continue
        parts = s.rsplit(None, 1)
        if len(parts) != 2 or not parts[1].isdigit():
            bad.append(f"{baseline_path}:{i}: malformed line: {s}")
            continue
        name, n = parts[0], int(parts[1])
        if name in base:
            bad.append(f"{baseline_path}:{i}: duplicate entry for {name}")
            continue
        base[name] = n

for name, n in sorted(over.items()):
    if name not in base:
        bad.append(f"{name} is {n} lines (budget {BUDGET}) and has no baseline line; split it or justify a baseline line in review")
    elif n > base[name]:
        bad.append(f"{name} grew to {n} lines, past its recorded {base[name]}")

for name, n in sorted(base.items()):
    p = pathlib.Path(name)
    if name in over:
        continue
    if not p.exists():
        bad.append(f"{baseline_path}: {name} no longer exists; delete its line")
    else:
        actual = len(p.read_text(encoding="utf-8").splitlines())
        bad.append(f"{baseline_path}: {name} is now {actual} lines, under budget; delete its line")

for b in bad:
    print(b)
sys.exit(1 if bad else 0)
PY
then
  good "every production file is within budget or pinned by the baseline"
else
  note "line budget problems above"
fi

echo
if [ "$fail" -eq 0 ]; then
  printf '\033[32mline budget holds\033[0m\n\n'
else
  printf '\033[31m%d problem(s)\033[0m\n\n' "$fail"
  exit 1
fi
