#!/usr/bin/env python3
"""Collect locked module license texts into a deterministic notice file."""

import json
import os
import sys


def read_modules():
    decoder = json.JSONDecoder()
    text = sys.stdin.read()
    offset = 0
    modules = []
    while offset < len(text):
        while offset < len(text) and text[offset].isspace():
            offset += 1
        if offset == len(text):
            break
        module, end = decoder.raw_decode(text, offset)
        modules.append(module)
        offset = end
    return modules


def license_files(root):
    if not root or not os.path.isdir(root):
        return []
    return sorted(
        name for name in os.listdir(root)
        if name.lower().startswith(("license", "copying", "notice"))
        and os.path.isfile(os.path.join(root, name))
    )


def main():
    if len(sys.argv) != 1:
        raise SystemExit("usage: release-notices.py")
    modules = {}
    for package in read_modules():
        module = package.get("Module")
        if module and not module.get("Main"):
            key = (module.get("Path", ""), module.get("Version", "UNKNOWN"))
            modules[key] = module
    modules = [modules[key] for key in sorted(modules)]
    entries = []
    missing = []
    for module in modules:
        path = module.get("Path", "")
        version = module.get("Version", "UNKNOWN")
        root = module.get("Dir")
        files = license_files(root)
        if not files:
            missing.append(f"{path}@{version} (module directory unavailable or no license text)")
            continue
        for name in files:
            entries.append((path, version, root, name))
    if missing:
        print(f"release: third-party notices missing license text for {len(missing)} module(s):", file=sys.stderr)
        for item in missing:
            print(f"  {item}", file=sys.stderr)
        raise SystemExit(1)
    entries.sort(key=lambda item: (item[0], item[1], item[3].lower(), item[3]))
    out = sys.stdout.buffer
    out.write(b"Tearoom third-party notices\n")
    out.write(b"================================\n\n")
    for path, version, root, name in entries:
        out.write(f"Module: {path}@{version}\nFile: {name}\n".encode())
        out.write(b"--------------------------------\n")
        with open(os.path.join(root, name), "rb") as stream:
            out.write(stream.read())
        out.write(b"\n\n")
    print(f"release: third-party notices: modules={len(modules)}, license-files={len(entries)}, missing=0", file=sys.stderr)


if __name__ == "__main__":
    main()
