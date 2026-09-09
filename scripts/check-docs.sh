#!/usr/bin/env bash
# Validate the docs bundle: frontmatter structure and types, per-directory
# index registration, invisible characters, and link integrity.
#
# This exists because documentation rot is silent. A moved file leaves a link
# that still looks fine in a diff, a document without frontmatter is invisible
# to anything that queries the tree, and a page missing from its index is a
# page an agent will never be routed to. All three were real: migrating docs/
# into concern directories broke two README links immediately.
#
# A new `type` or a page outside its directory's vocabulary is an admission
# decision: extend the mapping in this script in review, not silently.
set -uo pipefail
cd "$(dirname "$0")/.."

fail=0
note() { printf '  \033[31mFAIL\033[0m %s\n' "$1"; fail=$((fail + 1)); }
good() { printf '  \033[32mok\033[0m   %s\n' "$1"; }

echo
echo "checking docs bundle"
echo

if python3 - <<'PY'
import pathlib
import re
import sys

docs = pathlib.Path("docs")

# Allowed frontmatter types per location. An index.md is always `index`;
# top-level pages carry their own standing types. Admitting a new type or
# directory mapping is a reviewed change to this table, not a side effect.
ROOT_TYPES = {"product", "evaluations", "log"}
DIR_TYPES = {
    "architecture": {"architecture"},
    "domain": {"domain", "decision"},
    "lessons": {"lesson"},
    "operations": {"runbook", "operations"},
    "reference": {"reference"},
}

INVISIBLE = re.compile("[\u200b\u200c\u200d\u200e\u200f\u2066\u2067\u2068\u2069\ufeff]")
NBSP = "\u00a0"
MD_LINK = re.compile(r"\[[^\]]*\]\(([^)]+)\)")

try:
    import yaml
except ImportError:
    yaml = None

bad = []
files = sorted(docs.rglob("*.md"))

frontmatter_ok = 0
for path in files:
    rel = path.as_posix()
    text = path.read_text(encoding="utf-8")

    if INVISIBLE.search(text):
        bad.append(f"{rel}: contains zero-width, bidirectional, or byte-order control characters")
    if NBSP in text:
        bad.append(f"{rel}: contains a non-breaking space")

    if not text.startswith("---"):
        bad.append(f"{rel}: no frontmatter")
        continue
    parts = text.split("---", 2)
    if len(parts) < 3:
        bad.append(f"{rel}: frontmatter is never closed (missing second ---)")
        continue
    fm_text = parts[1]
    frontmatter_ok += 1

    data = None
    if yaml is not None:
        try:
            data = yaml.safe_load(fm_text)
        except yaml.YAMLError as exc:
            detail = str(exc).replace("\n", " ")[:150]
            bad.append(f"{rel}: frontmatter is invalid YAML: {detail}")
            continue
        if not isinstance(data, dict):
            bad.append(f"{rel}: frontmatter is not a mapping")
            continue
        doc_type = data.get("type")
    else:
        m = re.search(r"^type:\s*(\S+)", fm_text, re.M)
        doc_type = m.group(1) if m else None

    if not doc_type:
        bad.append(f"{rel}: frontmatter without a type")
        continue

    if path.name == "index.md":
        if doc_type != "index":
            bad.append(f"{rel}: index.md must be type 'index', found '{doc_type!r}'")
    elif path.parent == docs:
        if doc_type not in ROOT_TYPES:
            allowed = ", ".join(sorted(ROOT_TYPES))
            bad.append(f"{rel}: top-level type '{doc_type}' not in {allowed}")
    else:
        allowed = DIR_TYPES.get(path.parent.name)
        if allowed is None:
            bad.append(f"{rel}: unknown docs directory {path.parent.name}/; extend DIR_TYPES in review")
        elif doc_type not in allowed:
            want = ", ".join(sorted(allowed))
            bad.append(f"{rel}: type '{doc_type}' not allowed in {path.parent.name}/ (one of: {want})")

# --- index registration ------------------------------------------------------
for path in files:
    if path.name == "index.md":
        continue
    index = path.parent / "index.md"
    if not index.exists():
        bad.append(f"{path.parent}/: has {path.name} but no index.md")
        continue
    if path.name not in index.read_text(encoding="utf-8"):
        bad.append(f"{index.as_posix()}: does not register {path.name}")

# --- links -------------------------------------------------------------------
for path in files + [pathlib.Path("README.md")]:
    rel = path.as_posix()
    text = path.read_text(encoding="utf-8")
    for m in MD_LINK.finditer(text):
        target = m.group(1)
        if target.startswith(("http://", "https://", "#", "mailto:")):
            continue
        resolved = (path.parent / target.split("#")[0]).resolve()
        if target.split("#")[0] and not resolved.exists():
            bad.append(f"{rel}: broken link -> {target}")

if yaml is None:
    print("  WARN PyYAML not installed; frontmatter checked structurally, not as YAML")

for b in bad:
    print(b)

plural = "document" if frontmatter_ok == 1 else "documents"
print(f"  checked {len(files)} {plural} for frontmatter, types, index registration, and links")
sys.exit(1 if bad else 0)
PY
then
  good "every document opens with typed frontmatter and is registered and linked"
else
  note "docs bundle problems above"
fi

echo
if [ "$fail" -eq 0 ]; then
  printf '\033[32mdocs bundle is consistent\033[0m\n\n'
else
  printf '\033[31m%d problem(s)\033[0m\n\n' "$fail"
  exit 1
fi
