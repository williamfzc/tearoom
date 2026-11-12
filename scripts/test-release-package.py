#!/usr/bin/env python3
"""Exercise release assembly and corruption checks without publishing."""

import io
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
    def test_hc_notices_include_its_third_party_text_and_pinned_ghostty(self):
        with tempfile.TemporaryDirectory() as temp:
            workspace = pathlib.Path(temp)
            core = workspace / "crates/herdr-core"
            ghostty = workspace / "vendor/ghostty"
            core.mkdir(parents=True)
            ghostty.mkdir(parents=True)
            (workspace / "LICENSE").write_text("Apache license\n", encoding="utf-8")
            (core / "THIRD-PARTY-NOTICES").write_text("hc and bundled notices\n", encoding="utf-8")
            (ghostty / "LICENSE").write_text("Ghostty MIT license\n", encoding="utf-8")
            subprocess.run(["git", "init", "--quiet"], cwd=ghostty, check=True)
            subprocess.run(["git", "config", "user.name", "Release test"], cwd=ghostty, check=True)
            subprocess.run(["git", "config", "user.email", "release-test@example.invalid"], cwd=ghostty, check=True)
            subprocess.run(["git", "add", "LICENSE"], cwd=ghostty, check=True)
            subprocess.run(["git", "commit", "--quiet", "-m", "fixture"], cwd=ghostty, check=True)
            ghostty_commit = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ghostty, text=True).strip()
            subprocess.run(["git", "init", "--quiet"], cwd=workspace, check=True)
            subprocess.run(["git", "config", "user.name", "Release test"], cwd=workspace, check=True)
            subprocess.run(["git", "config", "user.email", "release-test@example.invalid"], cwd=workspace, check=True)
            subprocess.run(["git", "add", "LICENSE", "crates"], cwd=workspace, check=True)
            subprocess.run(
                ["git", "update-index", "--add", "--cacheinfo", f"160000,{ghostty_commit},vendor/ghostty"],
                cwd=workspace, check=True,
            )
            subprocess.run(["git", "commit", "--quiet", "-m", "fixture"], cwd=workspace, check=True)
            commit = subprocess.check_output(["git", "rev-parse", "HEAD:vendor/ghostty"], cwd=workspace, text=True).strip()
            package = {
                "id": "herdr-core-id", "name": "herdr-core", "version": "0.1.20",
                "source": "git+https://github.com/herdrdev/herdr-core.git#fixture",
                "license": "Apache-2.0", "license_file": None,
                "manifest_path": str(core / "Cargo.toml"),
            }
            metadata = {"packages": [package], "resolve": {"nodes": [{"id": package["id"]}]}}
            metadata_path = workspace / "metadata.json"
            metadata_path.write_text(json.dumps(metadata), encoding="utf-8")
            notices = subprocess.run(
                ["python3", str(ROOT / "scripts/release-notices.py"), str(metadata_path)],
                capture_output=True, text=True, check=True,
            ).stdout
            self.assertIn("hc and bundled notices", notices)
            self.assertIn("Ghostty MIT license", notices)
            metadata["tearoomReleaseSources"] = json.loads(metadata_path.read_text(encoding="utf-8"))["tearoomReleaseSources"]
            package["manifest_path"] = "/unavailable/build/runner/crates/herdr-core/Cargo.toml"
            metadata_path.write_text(json.dumps(metadata), encoding="utf-8")

            sbom = subprocess.run(
                ["python3", str(ROOT / "scripts/release-sbom.py"), "v1.2.3", "0", str(metadata_path)],
                capture_output=True, text=True, check=True,
            )
            packages = json.loads(sbom.stdout)["packages"]
            ghostty_package = next(item for item in packages if item["name"] == "ghostty")
            self.assertEqual(ghostty_package["versionInfo"], commit)
            self.assertEqual(ghostty_package["licenseDeclared"], "MIT")
            metadata.pop("tearoomReleaseSources")
            metadata_path.write_text(json.dumps(metadata), encoding="utf-8")
            missing = subprocess.run(
                ["python3", str(ROOT / "scripts/release-sbom.py"), "v1.2.3", "0", str(metadata_path)],
                capture_output=True, text=True, check=False,
            )
            self.assertNotEqual(missing.returncode, 0)
            self.assertIn("missing hc's pinned Ghostty revision", missing.stderr)

    def test_assembly_and_checksum_validation(self):
        with tempfile.TemporaryDirectory() as temp:
            directory = pathlib.Path(temp)
            for target in TARGETS:
                archive(directory, target)
            self.assertEqual(invoke("assemble", directory).returncode, 0)
            self.assertEqual(invoke("check", directory).returncode, 0)
            manifest_bytes = (directory / "SHA256SUMS").read_bytes()
            self.assertNotEqual(invoke("assemble", directory).returncode, 0)
            self.assertEqual((directory / "SHA256SUMS").read_bytes(), manifest_bytes)
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
                self.assertNotEqual(invoke("check", directory).returncode, 0)

    def test_extra_archive_is_refused(self):
        with tempfile.TemporaryDirectory() as temp:
            directory = pathlib.Path(temp)
            for target in TARGETS:
                archive(directory, target)
            self.assertEqual(invoke("assemble", directory).returncode, 0)
            (directory / "unexpected.tar.gz").write_bytes(b"extra")
            self.assertNotEqual(invoke("check", directory).returncode, 0)


if __name__ == "__main__":
    unittest.main()
