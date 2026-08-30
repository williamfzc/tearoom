#!/bin/sh
# Package, verify, and publish Tearoom release artifacts; Makefile owns local entry points.
set -eu

usage() {
    echo "usage: $0 {package|check|upload} --tag TAG --output DIR [options]" >&2
    exit 2
}

command=${1:-}
case "$command" in
    package|check|upload) shift ;;
    *) usage ;;
esac

tag=
output=
go_bin=${GO:-go}
version_ldflags=${VERSION_LDFLAGS:-}
endpoint=${R2_ENDPOINT:-}
bucket=${R2_BUCKET:-}
public_base_url=${R2_PUBLIC_BASE_URL:-}
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
        --go) [ "$#" -ge 2 ] || usage; go_bin=$2; shift 2 ;;
        --version-ldflags) [ "$#" -ge 2 ] || usage; version_ldflags=$2; shift 2 ;;
        --endpoint) [ "$#" -ge 2 ] || usage; endpoint=$2; shift 2 ;;
        --bucket) [ "$#" -ge 2 ] || usage; bucket=$2; shift 2 ;;
        --public-base-url) [ "$#" -ge 2 ] || usage; public_base_url=$2; shift 2 ;;
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
sha256() {
    if [ "$checksum_tool" = shasum ]; then shasum -a 256 "$1" | awk '{print $1}'; else sha256sum "$1" | awk '{print $1}'; fi
}

check_artifacts() {
    [ -d "$output" ] || { echo "release: artifact directory does not exist: $output" >&2; return 1; }
    sums="$output/SHA256SUMS"
    [ -f "$sums" ] || { echo "release: missing $sums" >&2; return 1; }
    (cd "$output" && if [ "$checksum_tool" = shasum ]; then shasum -a 256 -c SHA256SUMS; else sha256sum -c SHA256SUMS; fi) || return 1
    for target in darwin-amd64 darwin-arm64 linux-amd64 linux-arm64; do
        os=${target%-*}; arch=${target#*-}; archive=$(archive_name "$os" "$arch")
        [ -f "$output/$archive" ] || { echo "release: missing $archive" >&2; return 1; }
        entries=$(tar -tzf "$output/$archive" | sed 's#/$##' | sort)
        expected=$(printf 'LICENSE\nTHIRD_PARTY_NOTICES\ntearoom\n')
        [ "$entries" = "$expected" ] || { echo "release: $archive must contain exactly LICENSE, THIRD_PARTY_NOTICES, and tearoom" >&2; return 1; }
    done
    sbom="$output/$(sbom_name)"
    [ -f "$sbom" ] || { echo "release: missing $sbom" >&2; return 1; }
python3 - "$sbom" "$tag" "$commit" "$output" <<'PY'
import json, os, sys
path, tag, commit, output = sys.argv[1:]
with open(path, encoding="utf-8") as stream:
    doc = json.load(stream)
if doc.get("spdxVersion") != "SPDX-2.3" or doc.get("SPDXID") != "SPDXRef-DOCUMENT":
    raise SystemExit("release: SBOM is not SPDX 2.3")
if not doc.get("packages") or not doc.get("relationships"):
    raise SystemExit("release: SBOM has no package or relationship data")
with open(output + "/latest.json", encoding="utf-8") as stream:
    manifest = json.load(stream)
if (manifest.get("tag") != tag or manifest.get("commit") != commit or
        manifest.get("storeFormat") != 2 or manifest.get("checksums") != "SHA256SUMS"):
    raise SystemExit("release: latest.json has the wrong tag or checksum file")
names = {item["name"] for item in manifest.get("artifacts", [])}
expected = {name for name in os.listdir(output) if name.endswith(".tar.gz") or name.endswith(".spdx.json") or name == "SHA256SUMS"}
if names != expected:
    raise SystemExit("release: latest.json artifact list is incomplete or contains extra files")
if set(os.listdir(output)) != expected | {"latest.json"}:
    raise SystemExit("release: artifact directory contains unexpected files")
PY
}

write_manifest() {
    python3 - "$output" "$tag" "$commit" <<'PY'
import hashlib, json, os, sys
output, tag, commit = sys.argv[1:]
names = sorted(name for name in os.listdir(output) if name.endswith(".tar.gz") or name.endswith(".spdx.json") or name == "SHA256SUMS")
artifacts = []
for name in names:
    with open(os.path.join(output, name), "rb") as stream:
        artifacts.append({"name": name, "sha256": hashlib.sha256(stream.read()).hexdigest(), "key": f"releases/{tag}/{name}"})
manifest = {"schemaVersion": 1, "tag": tag, "commit": commit, "storeFormat": 2, "checksums": "SHA256SUMS", "artifacts": artifacts}
with open(os.path.join(output, "latest.json"), "w", encoding="utf-8") as stream:
    json.dump(manifest, stream, indent=2, sort_keys=True)
    stream.write("\n")
PY
}

package() {
    if [ -e "$output" ] && [ -n "$(find "$output" -mindepth 1 -print -quit 2>/dev/null)" ]; then
        echo "release: refusing to overwrite existing artifact directory: $output" >&2
        exit 1
    fi
    mkdir -p "$output"
    work=$(mktemp -d "${TMPDIR:-/tmp}/tearoom-release.XXXXXX")
    trap 'rm -rf "$work"' EXIT HUP INT TERM
    "$go_bin" list -deps -json ./cmd/tearoom | python3 scripts/release-notices.py > "$work/THIRD_PARTY_NOTICES"
    for target in darwin-amd64 darwin-arm64 linux-amd64 linux-arm64; do
        os=${target%-*}; arch=${target#*-}; archive=$(archive_name "$os" "$arch")
        mkdir -p "$work/$target"
        echo "release: building $target"
        CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" "$go_bin" build -trimpath -ldflags "$version_ldflags" -o "$work/$target/tearoom" ./cmd/tearoom
        cp LICENSE "$work/$target/LICENSE"
        cp "$work/THIRD_PARTY_NOTICES" "$work/$target/THIRD_PARTY_NOTICES"
        python3 scripts/release-archive.py "$output/$archive" "$work/$target" "$source_date_epoch"
    done
    "$go_bin" list -deps -json ./cmd/tearoom | python3 scripts/release-sbom.py "$tag" "$source_date_epoch" > "$output/$(sbom_name)"
    if [ "$checksum_tool" = shasum ]; then
        (cd "$output" && find . -type f \( -name '*.tar.gz' -o -name '*.spdx.json' \) -print | sed 's#^./##' | sort | xargs shasum -a 256 > SHA256SUMS)
    else
        (cd "$output" && find . -type f \( -name '*.tar.gz' -o -name '*.spdx.json' \) -print | sed 's#^./##' | sort | xargs sha256sum > SHA256SUMS)
    fi
    write_manifest
    check_artifacts
    trap - EXIT HUP INT TERM
    rm -rf "$work"
}

upload() {
    command -v aws >/dev/null 2>&1 || { echo "release: upload requires AWS CLI (aws)" >&2; exit 1; }
    command -v curl >/dev/null 2>&1 || { echo "release: upload requires curl for public readback" >&2; exit 1; }
    [ -n "$endpoint" ] || { echo "release: upload requires R2_ENDPOINT" >&2; exit 1; }
    [ -n "$bucket" ] || { echo "release: upload requires R2_BUCKET" >&2; exit 1; }
    [ -n "$public_base_url" ] || { echo "release: upload requires R2_PUBLIC_BASE_URL" >&2; exit 1; }
    [ -n "${AWS_ACCESS_KEY_ID:-}" ] || { echo "release: upload requires AWS_ACCESS_KEY_ID (R2 access key)" >&2; exit 1; }
    [ -n "${AWS_SECRET_ACCESS_KEY:-}" ] || { echo "release: upload requires AWS_SECRET_ACCESS_KEY (R2 secret key)" >&2; exit 1; }
    check_artifacts
    for file in "$output"/*.tar.gz "$output"/*.spdx.json "$output/SHA256SUMS"; do
        [ -f "$file" ] || continue
        name=${file##*/}; key="releases/$tag/$name"; digest=$(sha256 "$file")
        remote_state=0
        remote_sha=$(mktemp "${TMPDIR:-/tmp}/tearoom-r2-meta.XXXXXX")
        remote_error=$(mktemp "${TMPDIR:-/tmp}/tearoom-r2-error.XXXXXX")
        aws s3api head-object --bucket "$bucket" --key "$key" --endpoint-url "$endpoint" --query Metadata.sha256 --output text >"$remote_sha" 2>"$remote_error" || remote_state=$?
        if [ "$remote_state" -eq 0 ]; then
            remote_value=$(tr -d '\r\n' <"$remote_sha")
            if [ "$remote_value" = "$digest" ]; then
                echo "release: existing object matches, skipping $key"
                rm -f "$remote_sha" "$remote_error"
                continue
            fi
            cat "$remote_error" >&2
            rm -f "$remote_sha" "$remote_error"
            echo "release: existing R2 object has a different sha256: $key" >&2
            exit 1
        fi
        if ! grep -Eiq '404|not.?found|nosuchkey' "$remote_error"; then
            cat "$remote_error" >&2
            rm -f "$remote_sha" "$remote_error"
            echo "release: cannot establish immutability for $key" >&2
            exit 1
        fi
        rm -f "$remote_sha" "$remote_error"
        put_state=0
        aws s3api put-object --bucket "$bucket" --key "$key" --body "$file" --endpoint-url "$endpoint" --metadata "sha256=$digest" --if-none-match '*' >/dev/null || put_state=$?
        if [ "$put_state" -ne 0 ]; then
            # A concurrent publisher may have won the conditional put. Re-read
            # metadata and accept only the exact same bytes.
            remote_sha=$(mktemp "${TMPDIR:-/tmp}/tearoom-r2-meta.XXXXXX")
            remote_error=$(mktemp "${TMPDIR:-/tmp}/tearoom-r2-error.XXXXXX")
            remote_state=0
            aws s3api head-object --bucket "$bucket" --key "$key" --endpoint-url "$endpoint" --query Metadata.sha256 --output text >"$remote_sha" 2>"$remote_error" || remote_state=$?
            remote_value=$(tr -d '\r\n' <"$remote_sha")
            rm -f "$remote_sha" "$remote_error"
            if [ "$remote_state" -eq 0 ] && [ "$remote_value" = "$digest" ]; then
                echo "release: concurrent upload matches, skipping $key"
            else
                echo "release: conditional upload failed for $key; remote bytes were not proven identical" >&2
                exit "$put_state"
            fi
        fi
    done
    aws s3api put-object --bucket "$bucket" --key releases/latest.json --body "$output/latest.json" --endpoint-url "$endpoint" --content-type application/json --cache-control no-cache >/dev/null
    verify_public
    echo "release: uploaded releases/$tag/, updated releases/latest.json, and verified public readback"
}

verify_public() {
    verify_dir=$(mktemp -d "${TMPDIR:-/tmp}/tearoom-r2-public.XXXXXX")
    trap 'rm -rf "$verify_dir"' EXIT HUP INT TERM
    linux_archive=$(archive_name linux amd64)
    curl --fail --location --retry 3 --output "$verify_dir/latest.json" "${public_base_url%/}/releases/latest.json"
    curl --fail --location --retry 3 --output "$verify_dir/SHA256SUMS" "${public_base_url%/}/releases/$tag/SHA256SUMS"
    curl --fail --location --retry 3 --output "$verify_dir/$linux_archive" "${public_base_url%/}/releases/$tag/$linux_archive"
    python3 - "$verify_dir/latest.json" "$tag" "$commit" <<'PY'
import json, sys
path, tag, commit = sys.argv[1:]
with open(path, encoding="utf-8") as stream:
    manifest = json.load(stream)
if (manifest.get("tag") != tag or manifest.get("commit") != commit or
        manifest.get("storeFormat") != 2):
    raise SystemExit("release: public latest.json version proof failed")
PY
    (cd "$verify_dir" && grep "  $linux_archive$" SHA256SUMS | if [ "$checksum_tool" = shasum ]; then shasum -a 256 --check -; else sha256sum --check -; fi)
    tar -xzf "$verify_dir/$linux_archive" -C "$verify_dir"
    reported=$("$verify_dir/tearoom" --version)
    case "$reported" in
        *"version=$tag"*"commit=$commit"*"store-format=2"*) ;;
        *) echo "release: public linux-amd64 version proof failed: $reported" >&2; exit 1 ;;
    esac
    trap - EXIT HUP INT TERM
    rm -rf "$verify_dir"
}

case "$command" in
    package) package ;;
    check) check_artifacts ;;
    upload) upload ;;
esac
