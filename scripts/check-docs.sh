#!/usr/bin/env bash
# Validate the docs bundle: every document carries frontmatter with a type, every
# directory has an index, and every internal link resolves.
#
# This exists because documentation rot is silent. A moved file leaves a link that
# still looks fine in a diff, and a document without frontmatter is invisible to
# anything that queries the tree. Both were real: migrating docs/ into concern
# directories broke two README links immediately.
set -uo pipefail
cd "$(dirname "$0")/.."

fail=0
note() { printf '  \033[31mFAIL\033[0m %s\n' "$1"; fail=$((fail + 1)); }
good() { printf '  \033[32mok\033[0m   %s\n' "$1"; }

echo
echo "checking docs bundle"
echo

# --- frontmatter -------------------------------------------------------------
missing_fm=0
missing_type=0
while IFS= read -r f; do
  if [ "$(head -1 "$f")" != "---" ]; then
    note "no frontmatter: $f"
    missing_fm=$((missing_fm + 1))
    continue
  fi
  # `type` is the one field AGENTS.md requires; the rest are optional by design.
  if ! awk 'NR>1 && /^---$/{exit} NR>1 && /^type:/{found=1} END{exit !found}' "$f"; then
    note "frontmatter without a type: $f"
    missing_type=$((missing_type + 1))
  fi
done < <(find docs -name '*.md')
[ "$missing_fm" -eq 0 ] && good "every document opens with frontmatter"
[ "$missing_type" -eq 0 ] && good "every document declares a type"

# --- indexes ----------------------------------------------------------------
missing_index=0
while IFS= read -r d; do
  if [ ! -f "$d/index.md" ]; then
    note "directory without an index: $d"
    missing_index=$((missing_index + 1))
  fi
done < <(find docs -type d)
[ "$missing_index" -eq 0 ] && good "every directory has an index"

# --- links ------------------------------------------------------------------
# Resolved with python because relative link resolution in shell is error-prone,
# and a wrong answer here would defeat the purpose of the check.
if python3 - <<'PY'
import pathlib, re, sys
bad = []
for f in list(pathlib.Path('docs').rglob('*.md')) + [pathlib.Path('README.md')]:
    for m in re.finditer(r'\[([^\]]+)\]\(([^)]+)\)', f.read_text(encoding='utf-8')):
        target = m.group(2)
        if target.startswith(('http://', 'https://', '#')):
            continue
        path = target.split('#')[0]
        if path and not (f.parent / path).resolve().exists():
            bad.append(f"{f}: [{m.group(1)}] -> {target}")
for b in bad:
    print(b)
sys.exit(1 if bad else 0)
PY
then
  good "every internal link resolves"
else
  note "broken links above"
fi

echo
if [ "$fail" -eq 0 ]; then
  printf '\033[32mdocs bundle is consistent\033[0m\n\n'
else
  printf '\033[31m%d problem(s)\033[0m\n\n' "$fail"
  exit 1
fi
