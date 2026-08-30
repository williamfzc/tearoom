#!/usr/bin/env python3
"""Turn `go list -deps -json ./cmd/tearoom` package records into SPDX 2.3."""

import hashlib
import json
import sys
from datetime import datetime, timezone


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


def package_id(name, version):
    digest = hashlib.sha256(f"{name}@{version}".encode()).hexdigest()[:16]
    return f"SPDXRef-Package-{digest}"


def main():
    if len(sys.argv) != 3:
        raise SystemExit("usage: release-sbom.py VERSION SOURCE_DATE_EPOCH")
    version, epoch_text = sys.argv[1:]
    epoch = int(epoch_text)
    modules_by_key = {}
    root = None
    for package in read_modules():
        module = package.get("Module")
        if not module:
            continue
        if module.get("Main"):
            root = module.get("Path")
            continue
        key = (module.get("Path", ""), module.get("Version", "UNKNOWN"))
        modules_by_key[key] = module
    if not root:
        raise SystemExit("release: dependency graph has no main module")
    modules = [modules_by_key[key] for key in sorted(modules_by_key)]
    root_id = package_id(root, version)
    packages = [{
        "SPDXID": root_id,
        "name": root,
        "versionInfo": version,
        "downloadLocation": "NOASSERTION",
        "licenseConcluded": "NOASSERTION",
        "licenseDeclared": "NOASSERTION",
        "filesAnalyzed": False,
    }]
    relationships = [{
        "spdxElementId": "SPDXRef-DOCUMENT",
        "relationshipType": "DESCRIBES",
        "relatedSpdxElement": root_id,
    }]
    for module in modules:
        name = module.get("Path")
        if not name or name == root:
            continue
        module_version = module.get("Version") or "UNKNOWN"
        module_id = package_id(name, module_version)
        packages.append({
            "SPDXID": module_id,
            "name": name,
            "versionInfo": module_version,
            "downloadLocation": "NOASSERTION",
            "licenseConcluded": "NOASSERTION",
            "licenseDeclared": "NOASSERTION",
            "filesAnalyzed": False,
        })
        relationships.append({
            "spdxElementId": root_id,
            "relationshipType": "DEPENDS_ON",
            "relatedSpdxElement": module_id,
        })
    document = {
        "spdxVersion": "SPDX-2.3",
        "dataLicense": "CC0-1.0",
        "SPDXID": "SPDXRef-DOCUMENT",
        "name": f"tearoom-{version}",
        "documentNamespace": f"https://github.com/williamfzc/tearoom/sbom/{version}",
        "creationInfo": {
            "created": datetime.fromtimestamp(epoch, timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z"),
            "creators": ["Tool: tearoom-release"],
        },
        "packages": packages,
        "relationships": relationships,
    }
    json.dump(document, sys.stdout, indent=2, sort_keys=True)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
