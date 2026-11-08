#!/usr/bin/env python3
"""Collect license texts for the target-specific Cargo dependency graph."""

import json
import os
import sys


def main():
    if len(sys.argv) != 1:
        raise SystemExit("usage: release-notices.py < cargo-metadata.json")
    metadata = json.load(sys.stdin)
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
            if name.lower().startswith(("license", "licence", "copying", "notice"))
            and os.path.isfile(os.path.join(root, name))
        )
        if package.get("license_file") and package["license_file"] not in names:
            names.append(package["license_file"])
        if not names:
            missing.append(f'{package["name"]}@{package["version"]}')
        for name in sorted(set(names)):
            entries.append((package["name"], package["version"], root, name))
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
