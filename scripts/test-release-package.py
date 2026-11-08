#!/usr/bin/env python3
"""Exercise release assembly and corruption checks without publishing."""

import io
import hashlib
import json
import pathlib
import struct
import subprocess
import tarfile
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parent.parent
SCRIPT = ROOT / "scripts/release-package.sh"
TARGETS = ("darwin-amd64", "darwin-arm64", "linux-amd64", "linux-arm64")


def archive(directory, target, payload_target=None):
    os_name, arch = (payload_target or target).split("-")
    header = bytearray(120)
    if os_name == "darwin":
        struct.pack_into("<II", header, 0, 0xFEEDFACF, 0x01000007 if arch == "amd64" else 0x0100000C)
    else:
        header[:6] = b"\x7fELF\x02\x01"
        struct.pack_into("<H", header, 18, 62 if arch == "amd64" else 183)
        struct.pack_into("<Q", header, 32, 64)
        struct.pack_into("<HH", header, 54, 56, 1)
        struct.pack_into("<I", header, 64, 1)
    name = f"tearoom_v1.2.3_{target.replace('-', '_')}.tar.gz"
    with tarfile.open(directory / name, "w:gz") as stream:
        for member, content in (("tearoom", bytes(header)), ("LICENSE", b"test"), ("THIRD_PARTY_NOTICES", b"test")):
            info = tarfile.TarInfo(member)
            info.size = len(content)
            stream.addfile(info, io.BytesIO(content))
    package = {"id": "dep-id", "name": "dep", "version": "1.0.0", "source": "registry+test"}
    metadata = {"packages": [package], "resolve": {"nodes": [{"id": "dep-id"}]}}
    (directory / f"metadata-{target}.json").write_text(json.dumps(metadata), encoding="utf-8")


def invoke(command, directory):
    return subprocess.run(
        [str(SCRIPT), command, "--tag", "v1.2.3", "--commit", "abc123", "--source-date-epoch", "0", "--output", str(directory)],
        cwd=ROOT, capture_output=True, text=True, check=False,
    )


class ReleaseAssemblyTests(unittest.TestCase):
    def test_assembly_and_checksum_validation(self):
        with tempfile.TemporaryDirectory() as temp:
            directory = pathlib.Path(temp)
            for target in TARGETS:
                archive(directory, target)
            self.assertEqual(invoke("assemble", directory).returncode, 0)
            manifest = json.loads((directory / "latest.json").read_text(encoding="utf-8"))
            self.assertEqual(manifest["storeFormat"], 4)
            self.assertEqual(invoke("check", directory).returncode, 0)
            manifest_bytes = (directory / "latest.json").read_bytes()
            self.assertNotEqual(invoke("assemble", directory).returncode, 0)
            self.assertEqual((directory / "latest.json").read_bytes(), manifest_bytes)
            (directory / "tearoom_v1.2.3_linux_arm64.tar.gz").write_bytes(b"corrupt")
            self.assertNotEqual(invoke("check", directory).returncode, 0)

    def test_wrong_architecture_is_refused(self):
        with tempfile.TemporaryDirectory() as temp:
            directory = pathlib.Path(temp)
            for target in TARGETS:
                archive(directory, target, "darwin-arm64" if target == "darwin-amd64" else None)
            result = invoke("assemble", directory)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("wrong binary architecture", result.stderr)

    def test_missing_target_is_refused(self):
        with tempfile.TemporaryDirectory() as temp:
            result = invoke("assemble", pathlib.Path(temp))
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("missing archive", result.stderr)

    def test_incomplete_or_duplicate_checksum_set_is_refused(self):
        for mutation in ("missing", "duplicate"):
            with self.subTest(mutation=mutation), tempfile.TemporaryDirectory() as temp:
                directory = pathlib.Path(temp)
                for target in TARGETS:
                    archive(directory, target)
                self.assertEqual(invoke("assemble", directory).returncode, 0)
                sums = directory / "SHA256SUMS"
                lines = sums.read_text(encoding="utf-8").splitlines(keepends=True)
                sums.write_text("".join(lines[1:] if mutation == "missing" else lines + lines[:1]), encoding="utf-8")
                manifest = json.loads((directory / "latest.json").read_text(encoding="utf-8"))
                next(item for item in manifest["artifacts"] if item["name"] == "SHA256SUMS")["sha256"] = hashlib.sha256(sums.read_bytes()).hexdigest()
                (directory / "latest.json").write_text(json.dumps(manifest), encoding="utf-8")
                self.assertNotEqual(invoke("check", directory).returncode, 0)

    def test_manifest_tampering_and_extra_archive_are_refused(self):
        for mutation in ("digest", "key", "duplicate", "extra"):
            with self.subTest(mutation=mutation), tempfile.TemporaryDirectory() as temp:
                directory = pathlib.Path(temp)
                for target in TARGETS:
                    archive(directory, target)
                self.assertEqual(invoke("assemble", directory).returncode, 0)
                path = directory / "latest.json"
                manifest = json.loads(path.read_text(encoding="utf-8"))
                if mutation == "digest":
                    manifest["artifacts"][0]["sha256"] = "0" * 64
                elif mutation == "key":
                    manifest["artifacts"][0]["key"] = "releases/wrong"
                elif mutation == "duplicate":
                    manifest["artifacts"].append(manifest["artifacts"][0])
                else:
                    extra = directory / "unexpected.tar.gz"
                    extra.write_bytes(b"extra")
                    manifest["artifacts"].append({"name": extra.name, "sha256": hashlib.sha256(extra.read_bytes()).hexdigest(), "key": f"releases/v1.2.3/{extra.name}"})
                path.write_text(json.dumps(manifest), encoding="utf-8")
                self.assertNotEqual(invoke("check", directory).returncode, 0)


if __name__ == "__main__":
    unittest.main()
