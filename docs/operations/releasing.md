---
type: runbook
title: Releasing the Tearoom binary
description: The public artifact contract, installation checks, and rollback boundary for Tearoom binaries.
resource: .github/workflows/release.yml
tags: [runbook, release, artifacts, security, rollback]
timestamp: 2026-09-09
---

# Releasing the Tearoom binary

GitHub Releases distributes the source and binary artifacts from this
repository. A binary is not a secrecy boundary: it can be reverse-engineered,
so it must contain no credentials, private endpoints, or other secrets.

## Artifacts

Each versioned release carries four platform archives, `SHA256SUMS`, and
`tearoom_vX.Y.Z_sbom.spdx.json`. The supported targets are:

```text
darwin-amd64  darwin-arm64  linux-amd64  linux-arm64
```

Each archive contains the binary, root `LICENSE`, and deterministic
`THIRD_PARTY_NOTICES`. The notices and SPDX document come from the Cargo
dependency graph. Missing license text fails packaging. Windows is not a
supported concurrent Room platform: its current lock implementation does not
provide a safe cross-process primitive.

The binary reports its build identity and storage compatibility:

```text
tearoom version=<version> commit=<sha> store-format=4
```

## Install a pinned release

Select a release tag and the archive for your host. For example, on macOS
ARM64, download both the archive and checksums from the same release:

```bash
version=vX.Y.Z
artifact=tearoom_${version}_darwin_arm64.tar.gz
base_url="https://github.com/williamfzc/tearoom/releases/download/$version"
curl --fail --location --output "$artifact" "$base_url/$artifact"
curl --fail --location --output SHA256SUMS "$base_url/SHA256SUMS"
grep "  $artifact$" SHA256SUMS | shasum -a 256 --check -
tar --extract --gzip --file "$artifact"
```

Verify the checksum before installing the binary. Pin the version for
reproducible installation and rollback; GitHub's latest release is a mutable
convenience, not a version pin. Public download requires a public repository.

## Publish and verify

Run `make check` before creating a release tag. The checked-in
[`Release` workflow](../../.github/workflows/release.yml) runs it again when a
version-shaped `v` tag is pushed; packaging validates strict semantic versions.
Each target is built twice on a native runner, and the archives are compared.
The assembly step verifies archive contents and architecture, checksums, and
SPDX data before creating a GitHub draft release. It uploads the assets,
downloads and checks them again, runs the downloaded Linux AMD64 binary's
`--version`, then publishes the draft. It checks that the remote tag resolves to the
verified commit before creating and publishing the draft; tag protection is
still needed to prevent a change between a check and publication. Prerelease
tags are marked prerelease.

The workflow uses the built-in GitHub token with `contents: write` only for the
release job. It refuses to create a release if one already exists for the tag;
it does not overwrite assets. A failed run may leave a draft: inspect the
draft and its assets before deciding how to recover. Release immutability can
be enabled in GitHub repository settings; without it, a repository maintainer
can modify or delete release assets. No R2 credentials are needed.

The local entry points are `make release-target`, `make package`,
`make package-check`, and `make release-check`. `release-target` builds an
archive and Cargo metadata; `package` assembles four targets and writes
checksums and SPDX. `package-check` compares two assembled directories.
Treat a release as complete only after the tagged workflow and downloaded
artifact verification succeed. The workflow has not itself been proven until
a tagged release runs.

The `.tearoom` workspace directory is the trust domain, and `TEAROOM_ACTOR`
is only a default signature label, not a credential. See
[domain concepts](../domain/concepts.md).

## Rollback

Use a previously verified release rather than overwriting a published version.
The current storage contract is `format: 4`; an older binary does not read a
new layout. Before a storage-changing release, preserve the operator-owned data
snapshot needed by the prior binary. To roll back after cutover, use the
matched prior binary and restore its matching pre-cutover snapshot.

See [status and clean-break migration](../architecture/status.md).
