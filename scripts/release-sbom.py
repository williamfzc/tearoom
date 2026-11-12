#!/usr/bin/env python3
"""Create an SPDX 2.3 SBOM from the target-specific Cargo graphs."""

import hashlib
import json
import re
import sys
from datetime import datetime, timezone


def package_id(name, version):
    digest = hashlib.sha256(f"{name}@{version}".encode()).hexdigest()[:16]
    return f"SPDXRef-Package-{digest}"


def main():
    if len(sys.argv) < 4:
        raise SystemExit("usage: release-sbom.py VERSION SOURCE_DATE_EPOCH CARGO_METADATA...")
    version, epoch_text, *paths = sys.argv[1:]
    dependencies = {}
    root = "tearoom"
    for path in paths:
        with open(path, encoding="utf-8") as stream:
            metadata = json.load(stream)
        active = {node["id"] for node in metadata["resolve"]["nodes"]}
        has_hc = False
        for package in metadata["packages"]:
            if package["id"] in active and package.get("source"):
                dependencies[(package["name"], package["version"])] = package
                has_hc |= package["name"] == "herdr-core"
        ghostty = metadata.get("tearoomReleaseSources", {}).get("ghostty")
        if has_hc and not ghostty:
            raise SystemExit("release: Cargo metadata is missing hc's pinned Ghostty revision")
        if ghostty:
            commit = ghostty.get("version", "")
            if not re.fullmatch(r"[0-9a-f]{40}", commit) or ghostty.get("license") != "MIT":
                raise SystemExit("release: invalid pinned Ghostty revision in Cargo metadata")
            dependencies[("ghostty", commit)] = ghostty
    if not dependencies:
        raise SystemExit("release: dependency graph is empty")
    root_id = package_id(root, version)
    packages = [{"SPDXID": root_id, "name": root, "versionInfo": version,
                 "downloadLocation": "NOASSERTION", "licenseConcluded": "NOASSERTION",
                 "licenseDeclared": "NOASSERTION", "filesAnalyzed": False}]
    relationships = [{"spdxElementId": "SPDXRef-DOCUMENT", "relationshipType": "DESCRIBES", "relatedSpdxElement": root_id}]
    for (name, dep_version), package in sorted(dependencies.items()):
        identifier = package_id(name, dep_version)
        download = package.get("downloadLocation", "NOASSERTION")
        license_declared = package.get("license", "NOASSERTION")
        packages.append({"SPDXID": identifier, "name": name, "versionInfo": dep_version,
                         "downloadLocation": download, "licenseConcluded": "NOASSERTION",
                         "licenseDeclared": license_declared, "filesAnalyzed": False})
        relationships.append({"spdxElementId": root_id, "relationshipType": "DEPENDS_ON", "relatedSpdxElement": identifier})
    document = {"spdxVersion": "SPDX-2.3", "dataLicense": "CC0-1.0", "SPDXID": "SPDXRef-DOCUMENT",
                "name": f"tearoom-{version}",
                "documentNamespace": f"https://github.com/williamfzc/tearoom/sbom/{version}",
                "creationInfo": {"created": datetime.fromtimestamp(int(epoch_text), timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z"), "creators": ["Tool: tearoom-release"]},
                "packages": packages, "relationships": relationships}
    json.dump(document, sys.stdout, indent=2, sort_keys=True)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
