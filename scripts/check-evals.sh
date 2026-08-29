#!/usr/bin/env bash
# Validate the evaluation set (docs/evaluations.md): the story<->evaluation
# mapping is bidirectional, statuses agree with the mapping, and every shipped
# evaluation exists and is wired into `make check`.
#
# Like check-docs.sh, this exists because a registry that drifts is worse than
# no registry: a story marked proven without a shipped evaluation, or a suite
# dropped from make check while its story still claims proof, is the same
# silent rot a broken link is.
set -uo pipefail
cd "$(dirname "$0")/.."

fail=0
note() { printf '  \033[31mFAIL\033[0m %s\n' "$1"; fail=$((fail + 1)); }
good() { printf '  \033[32mok\033[0m   %s\n' "$1"; }

echo
echo "checking evaluation set"
echo

if python3 - <<'PY'
import pathlib, re, sys

bad = []

def section(text, header):
    """Rows of the markdown table under `## <header>`, header and
    separator rows dropped. A malformed table is caught by whoever consumes
    the rows (or by the rows themselves not surviving to it)."""
    m = re.search(r'^## ' + re.escape(header) + r'\s*$', text, re.M)
    if not m:
        bad.append(f"missing section: ## {header}")
        return []
    rows = []
    for ln in text[m.end():].splitlines():
        if ln.startswith('#'):
            break
        if not ln.strip().startswith('|'):
            continue
        cells = [c.strip() for c in ln.strip().strip('|').split('|')]
        if all(re.fullmatch(r'-{2,}', c) for c in cells):
            continue
        rows.append(cells)
    return rows[1:]  # drop the column header row

text = pathlib.Path('docs/evaluations.md').read_text(encoding='utf-8')
makefile = pathlib.Path('Makefile').read_text(encoding='utf-8')

# --- stories ----------------------------------------------------------------
stories = {}
for row in section(text, 'Stories'):
    if len(row) < 3:
        bad.append(f"malformed story row: {' | '.join(row)}")
        continue
    sid, status = row[0], row[2]
    if sid in stories:
        bad.append(f"duplicate story id: {sid}")
    if status not in ('proven', 'open', 'deferred'):
        bad.append(f"story {sid}: unknown status {status!r}")
    stories[sid] = status

# --- evaluations ------------------------------------------------------------
evals = {}
for row in section(text, 'Evaluations'):
    if len(row) < 5:
        bad.append(f"malformed evaluation row: {' | '.join(row)}")
        continue
    eid, target, script, status = row[0], row[2], row[3], row[4]
    if eid in evals:
        bad.append(f"duplicate evaluation id: {eid}")
    if status not in ('shipped', 'planned'):
        bad.append(f"evaluation {eid}: unknown status {status!r}")
    evals[eid] = {'target': target, 'script': script, 'status': status}

# --- mappings ---------------------------------------------------------------
def ids(cell):
    return [x for x in (c.strip() for c in cell.split(',')) if x]

story_map = {}
for row in section(text, 'Story → evaluation'):
    if len(row) < 2:
        bad.append(f"malformed story->eval row: {' | '.join(row)}")
        continue
    story_map[row[0]] = set(ids(row[1]))

eval_map = {}
for row in section(text, 'Evaluation → story'):
    if len(row) < 2:
        bad.append(f"malformed eval->story row: {' | '.join(row)}")
        continue
    eval_map[row[0]] = set(ids(row[1]))

pairs_story = {(s, e) for s, es in story_map.items() for e in es}
pairs_eval = {(s, e) for e, ss in eval_map.items() for s in ss}
if pairs_story != pairs_eval:
    only_s = sorted(pairs_story - pairs_eval)
    only_e = sorted(pairs_eval - pairs_story)
    bad.append(
        f"mapping is not bidirectional"
        + (f"; only in story->eval: {only_s}" if only_s else "")
        + (f"; only in eval->story: {only_e}" if only_e else "")
    )

for sid, es in story_map.items():
    if sid not in stories:
        bad.append(f"story->eval references unknown story {sid}")
    for e in es:
        if e not in evals:
            bad.append(f"story {sid} references unknown evaluation {e}")
for eid, ss in eval_map.items():
    if eid not in evals:
        bad.append(f"eval->story references unknown evaluation {eid}")
    for s in ss:
        if s not in stories:
            bad.append(f"evaluation {eid} references unknown story {s}")

# --- status consistency -------------------------------------------------------
for sid, status in stories.items():
    in_map = sid in story_map
    if status == 'proven' and not in_map:
        bad.append(f"story {sid} is proven but has no evaluation mapping")
    if status != 'proven' and in_map:
        bad.append(f"story {sid} is {status} but appears in the story->eval mapping")

# --- shipped evaluations exist and are wired ---------------------------------
for eid, ev in evals.items():
    if ev['status'] == 'planned':
        if eid in eval_map:
            bad.append(f"planned evaluation {eid} appears in the mapping")
        continue
    m = re.search(r'`make ([a-z0-9-]+)`', ev['target'])
    if not m:
        bad.append(f"evaluation {eid}: no `make <target>` in {ev['target']!r}")
        continue
    target = m.group(1)
    if not re.search(r'^' + re.escape(target) + r':', makefile, re.M):
        bad.append(f"evaluation {eid}: no make target {target}")
    elif not re.search(r'^check:.*(^| )' + re.escape(target) + r'( |$)', makefile, re.M):
        bad.append(f"evaluation {eid}: target {target} is missing from the check chain")
    ms = re.search(r'\]\(([^)]+\.sh)\)', ev['script'])
    if ms:
        # Links in the registry are relative to docs/evaluations.md.
        sp = (pathlib.Path('docs') / ms.group(1)).resolve()
        if not sp.exists():
            bad.append(f"evaluation {eid}: script missing: {sp}")
        elif f'# Evaluation: {eid}' not in sp.read_text(encoding='utf-8'):
            bad.append(f"evaluation {eid}: script {sp} lacks the '# Evaluation: {eid}' marker")

for b in bad:
    print(b)
sys.exit(1 if bad else 0)
PY
then
  good "story<->evaluation mapping is bidirectional and consistent"
else
  note "evaluation set problems above"
fi

echo
if [ "$fail" -eq 0 ]; then
  printf '\033[32mevaluation set is consistent\033[0m\n\n'
else
  printf '\033[31m%d problem(s)\033[0m\n\n' "$fail"
  exit 1
fi
