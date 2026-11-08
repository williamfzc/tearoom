#!/usr/bin/env python3
"""Create an SPDX 2.3 SBOM from the target-specific Cargo graphs."""

import hashlib
import json
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
        for package in metadata["packages"]:
            if package["id"] in active and package.get("source"):
                dependencies[(package["name"], package["version"])] = package
    if not dependencies:
        raise SystemExit("release: dependency graph is empty")
    root_id = package_id(root, version)
    packages = [{"SPDXID": root_id, "name": root, "versionInfo": version,
                 "downloadLocation": "NOASSERTION", "licenseConcluded": "NOASSERTION",
                 "licenseDeclared": "NOASSERTION", "filesAnalyzed": False}]
    relationships = [{"spdxElementId": "SPDXRef-DOCUMENT", "relationshipType": "DESCRIBES", "relatedSpdxElement": root_id}]
    for (name, dep_version), package in sorted(dependencies.items()):
        identifier = package_id(name, dep_version)
        packages.append({"SPDXID": identifier, "name": name, "versionInfo": dep_version,
                         "downloadLocation": "NOASSERTION", "licenseConcluded": "NOASSERTION",
                         "licenseDeclared": "NOASSERTION", "filesAnalyzed": False})
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
