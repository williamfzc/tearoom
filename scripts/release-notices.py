#!/usr/bin/env python3
"""Collect license texts for the target-specific Cargo dependency graph."""

import json
import os
import subprocess
import sys


def main():
    if len(sys.argv) != 2:
        raise SystemExit("usage: release-notices.py CARGO_METADATA")
    metadata_path = sys.argv[1]
    with open(metadata_path, encoding="utf-8") as stream:
        metadata = json.load(stream)
    core = next((p for p in metadata["packages"] if p["name"] == "herdr-core"), None)
    if core:
        workspace = os.path.normpath(os.path.join(os.path.dirname(core["manifest_path"]), "../.."))
        ghostty = os.path.join(workspace, "vendor/ghostty")
        if not os.path.isfile(os.path.join(ghostty, "LICENSE")):
            raise SystemExit("release: hc's pinned Ghostty license is missing")
        commit = subprocess.check_output(
            ["git", "-C", workspace, "rev-parse", "HEAD:vendor/ghostty"], text=True
        ).strip()
        metadata["tearoomReleaseSources"] = {
            "ghostty": {
                "version": commit,
                "license": "MIT",
                "downloadLocation": f"https://github.com/ghostty-org/ghostty/tree/{commit}",
            }
        }
        with open(metadata_path, "w", encoding="utf-8") as stream:
            json.dump(metadata, stream, separators=(",", ":"))
            stream.write("\n")
    active = {node["id"] for node in metadata["resolve"]["nodes"]}
    packages = sorted(
        (p for p in metadata["packages"] if p["id"] in active and p.get("source")),
        key=lambda p: (p["name"], p["version"]),
    )
    entries, missing = [], []
    for package in packages:
        root = os.path.dirname(package["manifest_path"])
        names = sorted(
            name for name in os.listdir(root)
            if name.lower().startswith(("license", "licence", "copying", "notice", "third-party-notices"))
            and os.path.isfile(os.path.join(root, name))
        )
        files = [(root, name) for name in names]
        if (
            package["name"] in {"herdr-core", "ghostty-vt"}
            and not any(name.lower().startswith(("license", "licence", "copying")) for name in names)
        ):
            parent = os.path.dirname(root)
            while parent != root:
                candidates = sorted(
                    name for name in os.listdir(parent)
                    if name.lower().startswith(("license", "licence", "copying"))
                    and os.path.isfile(os.path.join(parent, name))
                )
                if candidates:
                    files.extend((parent, name) for name in candidates)
                    break
                root, parent = parent, os.path.dirname(parent)
        if package.get("license_file") and package["license_file"] not in names:
            files.append((os.path.dirname(package["manifest_path"]), package["license_file"]))
        if not files:
            missing.append(f'{package["name"]}@{package["version"]}')
        for file_root, name in sorted(set(files)):
            entries.append((package["name"], package["version"], file_root, name))
        if package["name"] == "herdr-core":
            commit = metadata["tearoomReleaseSources"]["ghostty"]["version"]
            ghostty = os.path.join(
                os.path.normpath(os.path.join(os.path.dirname(package["manifest_path"]), "../..")),
                "vendor/ghostty",
            )
            entries.append(("ghostty", commit, ghostty, "LICENSE"))
    if missing:
        raise SystemExit("release: missing dependency license texts: " + ", ".join(missing))
    out = sys.stdout.buffer
    out.write(b"Tearoom third-party notices\n================================\n\n")
    for name, version, root, filename in entries:
        out.write(f"Crate: {name}@{version}\nFile: {filename}\n--------------------------------\n".encode())
        with open(os.path.join(root, filename), "rb") as stream:
            out.write(stream.read())
        out.write(b"\n\n")
    print(f"release: third-party notices: crates={len(packages)}, license-files={len(entries)}", file=sys.stderr)


if __name__ == "__main__":
    main()
