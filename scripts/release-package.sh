#!/bin/sh
# Package and verify Tearoom release artifacts; Makefile owns local entry points.
set -eu

usage() {
    echo "usage: $0 {build-target|assemble|check} --tag TAG --output DIR [options]" >&2
    exit 2
}

command=${1:-}
case "$command" in
    build-target|assemble|check) shift ;;
    *) usage ;;
esac

tag=
output=
cargo_bin=${CARGO:-cargo}
target=
commit=${RELEASE_COMMIT:-unknown}
source_date_epoch=${SOURCE_DATE_EPOCH:-}

if [ -z "$commit" ] || [ "$commit" = unknown ]; then
    commit=$(git rev-parse HEAD 2>/dev/null || printf 'unknown')
fi
if [ -z "$source_date_epoch" ]; then
    source_date_epoch=$(git log -1 --format=%ct 2>/dev/null || date +%s)
fi

while [ "$#" -gt 0 ]; do
    case "$1" in
        --tag) [ "$#" -ge 2 ] || usage; tag=$2; shift 2 ;;
        --output) [ "$#" -ge 2 ] || usage; output=$2; shift 2 ;;
        --cargo) [ "$#" -ge 2 ] || usage; cargo_bin=$2; shift 2 ;;
        --target) [ "$#" -ge 2 ] || usage; target=$2; shift 2 ;;
        --commit) [ "$#" -ge 2 ] || usage; commit=$2; shift 2 ;;
        --source-date-epoch) [ "$#" -ge 2 ] || usage; source_date_epoch=$2; shift 2 ;;
        *) echo "release: unknown option: $1" >&2; usage ;;
    esac
done

[ -n "$tag" ] || { echo "release: --tag is required" >&2; exit 2; }
[ -n "$output" ] || { echo "release: --output is required" >&2; exit 2; }

validate_tag() {
    python3 - "$1" <<'PY'
import re, sys
tag = sys.argv[1]
identifier = r"(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)"
pattern = rf"^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)" \
          rf"(?:-(?:{identifier})(?:\.(?:{identifier}))*)?" \
          rf"(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$"
if not re.fullmatch(pattern, tag):
    raise SystemExit(1)
PY
}

validate_tag "$tag" || { echo "release: tag must be strict semver vMAJOR.MINOR.PATCH[-prerelease][+build]: $tag" >&2; exit 2; }

archive_name() { printf 'tearoom_%s_%s_%s.tar.gz' "$tag" "$1" "$2"; }
sbom_name() { printf 'tearoom_%s_sbom.spdx.json' "$tag"; }

checksum_tool=sha256sum
if ! command -v "$checksum_tool" >/dev/null 2>&1; then checksum_tool=shasum; fi
check_artifacts() {
    [ -d "$output" ] || { echo "release: artifact directory does not exist: $output" >&2; return 1; }
    sums="$output/SHA256SUMS"
    [ -f "$sums" ] || { echo "release: missing $sums" >&2; return 1; }
python3 - "$output" "$tag" <<'PY' || return 1
import hashlib, os, re, sys
output, tag = sys.argv[1:]
expected = {f"tearoom_{tag}_{os_name}_{arch}.tar.gz" for os_name in ("darwin", "linux") for arch in ("amd64", "arm64")}
expected.add(f"tearoom_{tag}_sbom.spdx.json")
entries = {}
with open(os.path.join(output, "SHA256SUMS"), encoding="utf-8") as stream:
    for line in stream:
        match = re.fullmatch(r"([0-9a-f]{64})  ([^/\\\r\n]+)\n", line)
        if not match or match.group(2) in entries:
            raise SystemExit("release: malformed or duplicate checksum entry")
        entries[match.group(2)] = match.group(1)
if set(entries) != expected:
    raise SystemExit("release: checksum file must cover every archive and SBOM exactly once")
for name, digest in entries.items():
    with open(os.path.join(output, name), "rb") as stream:
        if hashlib.sha256(stream.read()).hexdigest() != digest:
            raise SystemExit(f"release: checksum mismatch for {name}")
PY
    (cd "$output" && if [ "$checksum_tool" = shasum ]; then shasum -a 256 -c SHA256SUMS; else sha256sum -c SHA256SUMS; fi) || return 1
    for target in darwin-amd64 darwin-arm64 linux-amd64 linux-arm64; do
        os=${target%-*}; arch=${target#*-}; archive=$(archive_name "$os" "$arch")
        [ -f "$output/$archive" ] || { echo "release: missing $archive" >&2; return 1; }
        entries=$(tar -tzf "$output/$archive" | sed 's#/$##' | LC_ALL=C sort)
        expected=$(printf 'LICENSE\nTHIRD_PARTY_NOTICES\ntearoom\n')
        [ "$entries" = "$expected" ] || { echo "release: $archive must contain exactly LICENSE, THIRD_PARTY_NOTICES, and tearoom" >&2; return 1; }
        python3 - "$output/$archive" "$target" <<'PY' || return 1
import struct, sys, tarfile
path, target = sys.argv[1:]
with tarfile.open(path, "r:gz") as archive:
    binary = archive.extractfile("tearoom").read()
head = binary[:64]
if target.startswith("darwin-"):
    expected = {"darwin-amd64": 0x01000007, "darwin-arm64": 0x0100000c}[target]
    ok = len(head) >= 8 and head[:4] == b"\xcf\xfa\xed\xfe" and struct.unpack("<I", head[4:8])[0] == expected
else:
    expected = {"linux-amd64": 62, "linux-arm64": 183}[target]
    ok = len(head) >= 64 and head[:6] == b"\x7fELF\x02\x01" and struct.unpack("<H", head[18:20])[0] == expected
    if ok:
        offset = struct.unpack("<Q", head[32:40])[0]
        size, count = struct.unpack("<HH", head[54:58])
        ok = count > 0 and size >= 56 and offset + size * count <= len(binary)
        if ok:
            types = [struct.unpack("<I", binary[offset + size * i:offset + size * i + 4])[0] for i in range(count)]
            ok = 1 in types and 3 not in types
if not ok:
    raise SystemExit(f"release: {path} contains the wrong binary architecture")
PY
    done
    sbom="$output/$(sbom_name)"
    [ -f "$sbom" ] || { echo "release: missing $sbom" >&2; return 1; }
python3 - "$sbom" "$tag" "$output" <<'PYDOC'
import json, os, sys
path, tag, output = sys.argv[1:]
with open(path, encoding="utf-8") as stream:
    doc = json.load(stream)
if doc.get("spdxVersion") != "SPDX-2.3" or doc.get("SPDXID") != "SPDXRef-DOCUMENT":
    raise SystemExit("release: SBOM is not SPDX 2.3")
if not doc.get("packages") or not doc.get("relationships"):
    raise SystemExit("release: SBOM has no package or relationship data")
expected = {f"tearoom_{tag}_{os_name}_{arch}.tar.gz" for os_name in ("darwin", "linux") for arch in ("amd64", "arm64")}
expected.update({f"tearoom_{tag}_sbom.spdx.json", "SHA256SUMS"})
if set(os.listdir(output)) != expected:
    raise SystemExit("release: artifact directory contains unexpected files")
PYDOC
}

target_triple() {
    case "$1" in
        darwin-amd64) printf '%s' x86_64-apple-darwin ;;
        darwin-arm64) printf '%s' aarch64-apple-darwin ;;
        linux-amd64) printf '%s' x86_64-unknown-linux-musl ;;
        linux-arm64) printf '%s' aarch64-unknown-linux-musl ;;
        *) echo "release: unknown target: $1" >&2; return 1 ;;
    esac
}

build_target() {
    [ -n "$target" ] || { echo "release: build-target requires --target" >&2; return 2; }
    triple=$(target_triple "$target")
    case "$(uname -s)-$(uname -m)" in
        Darwin-x86_64) native=darwin-amd64 ;;
        Darwin-arm64) native=darwin-arm64 ;;
        Linux-x86_64) native=linux-amd64 ;;
        Linux-aarch64) native=linux-arm64 ;;
        *) echo "release: unsupported build host" >&2; return 1 ;;
    esac
    [ "$target" = "$native" ] || { echo "release: $target requires a native runner (host is $native)" >&2; return 1; }
    os=${target%-*}; arch=${target#*-}; archive=$(archive_name "$os" "$arch")
    mkdir -p "$output"
    [ ! -e "$output/$archive" ] || { echo "release: refusing to overwrite $output/$archive" >&2; return 1; }
    [ ! -e "$output/metadata-$target.json" ] || { echo "release: refusing to overwrite metadata for $target" >&2; return 1; }
    work=$(mktemp -d "${TMPDIR:-/tmp}/tearoom-release.XXXXXX")
    trap 'rm -rf "$work"' EXIT HUP INT TERM
    "$cargo_bin" metadata --locked --format-version 1 --filter-platform "$triple" --manifest-path Cargo.toml > "$work/metadata.json"
    python3 scripts/release-notices.py "$work/metadata.json" > "$work/THIRD_PARTY_NOTICES"
    echo "release: building $target ($triple)"
    TEAROOM_BUILD_VERSION="$tag" TEAROOM_BUILD_COMMIT="$commit" \
        "$cargo_bin" build --locked --release --target "$triple" --target-dir "$work/target" --manifest-path Cargo.toml
    mkdir -p "$work/stage"
    cp "$work/target/$triple/release/tearoom" "$work/stage/tearoom"
    reported=$("$work/stage/tearoom" --version)
    [ "$reported" = "tearoom version=$tag commit=$commit store-format=4" ] || {
        echo "release: version proof failed: $reported" >&2; return 1;
    }
    cp LICENSE "$work/stage/LICENSE"
    cp "$work/THIRD_PARTY_NOTICES" "$work/stage/THIRD_PARTY_NOTICES"
    python3 scripts/release-archive.py "$output/$archive" "$work/stage" "$source_date_epoch"
    cp "$work/metadata.json" "$output/metadata-$target.json"
    trap - EXIT HUP INT TERM
    rm -rf "$work"
}

assemble() {
    for name in "$(sbom_name)" SHA256SUMS; do
        [ ! -e "$output/$name" ] || { echo "release: refusing to overwrite $output/$name" >&2; return 1; }
    done
    for item in darwin-amd64 darwin-arm64 linux-amd64 linux-arm64; do
        os=${item%-*}; arch=${item#*-}
        [ -f "$output/$(archive_name "$os" "$arch")" ] || { echo "release: missing archive for $item" >&2; return 1; }
        [ -f "$output/metadata-$item.json" ] || { echo "release: missing Cargo metadata for $item" >&2; return 1; }
    done
    python3 scripts/release-sbom.py "$tag" "$source_date_epoch" \
        "$output/metadata-darwin-amd64.json" "$output/metadata-darwin-arm64.json" \
        "$output/metadata-linux-amd64.json" "$output/metadata-linux-arm64.json" > "$output/$(sbom_name)"
    rm "$output"/metadata-darwin-amd64.json "$output"/metadata-darwin-arm64.json \
       "$output"/metadata-linux-amd64.json "$output"/metadata-linux-arm64.json
    if [ "$checksum_tool" = shasum ]; then
        (cd "$output" && find . -type f \( -name '*.tar.gz' -o -name '*.spdx.json' \) -print | sed 's#^./##' | sort | xargs shasum -a 256 > SHA256SUMS)
    else
        (cd "$output" && find . -type f \( -name '*.tar.gz' -o -name '*.spdx.json' \) -print | sed 's#^./##' | sort | xargs sha256sum > SHA256SUMS)
    fi
    check_artifacts
}


case "$command" in
    build-target) build_target ;;
    assemble) assemble ;;
    check) check_artifacts ;;
esac
