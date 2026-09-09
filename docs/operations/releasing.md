---
type: runbook
title: Releasing the Tearoom binary
description: The public artifact contract, installation checks, release credentials, and rollback boundary for Tearoom binaries.
resource: .github/workflows/release.yml
tags: [runbook, release, artifacts, security, rollback]
timestamp: 2026-08-30
---

# Releasing the Tearoom binary

This is the canonical release and installation contract. Source remains in the
private repository; the release surface is a public, compiled binary and its
verification metadata. A binary is not a secrecy boundary: it can be
reverse-engineered, so it must contain no source-host credentials, R2 secrets,
tokens, private endpoints, or other secrets.

## Public artifact boundary

Cloudflare R2 is the only public artifact source. GitHub Actions is the private
build and publication control plane; GitHub Releases and a second public
distribution repository are not parallel download channels.

Each version is published under an immutable object prefix:

```text
releases/vX.Y.Z/tearoom_vX.Y.Z_darwin_amd64.tar.gz
releases/vX.Y.Z/tearoom_vX.Y.Z_darwin_arm64.tar.gz
releases/vX.Y.Z/tearoom_vX.Y.Z_linux_amd64.tar.gz
releases/vX.Y.Z/tearoom_vX.Y.Z_linux_arm64.tar.gz
releases/vX.Y.Z/SHA256SUMS
releases/vX.Y.Z/tearoom_vX.Y.Z_sbom.spdx.json
```

Each archive contains the root `LICENSE` file and a deterministic
`THIRD_PARTY_NOTICES` file; there are no separately published license objects.
Both the notices and SPDX generators consume only the actual runtime module
graph from `go list -deps -json ./cmd/tearoom`. The current graph reports
`modules=22, license-files=25, missing=0`; a module without a root-level
`LICENSE`, `COPYING`, or `NOTICE` text fails packaging rather than producing an
incomplete notice file.

The version prefix and its objects are never overwritten. The only mutable
pointer is:

```text
releases/latest.json
```

`latest.json` records the current tag, source commit, `storeFormat: 2`, the
checksum filename, and each artifact's R2 object key and SHA256 digest; it is a
convenience pointer, not evidence that an older version changed. Pin an
immutable version URL for reproducible installation and rollback.

The checked-in workflow is [`.github/workflows/release.yml`](../../.github/workflows/release.yml),
named `Release`. It runs on a `push` of any `v*` tag, on one
`ubuntu-latest` `release` job. The job checks out the tagged commit, sets up Go
from `go.mod`, configures the AWS-compatible client, runs the package build,
runs a second artifact check, and uploads to R2. Its concurrency group is
`tearoom-release-${{ github.ref_name }}` and it does not cancel an in-progress
run for the same tag.

## Supported platforms

The shipped product requires Go 1.22+ to build and a Unix-like operating
system to run. The current workflow publishes exactly these targets:

```text
darwin-amd64  darwin-arm64  linux-amd64  linux-arm64
```

Windows is not a supported concurrent Room platform: its current lock
implementation does not provide a safe cross-process primitive. Other pairs
may be buildable by Go but are not release support unless the workflow
publishes and verifies them.

The binary reports its build identity and storage compatibility:

```text
tearoom --version
tearoom version=<version> commit=<sha> store-format=2
```

## Install a pinned release

Choose an immutable version and the matching object for the host. Replace
`ARTIFACT_BASE_URL` with the public R2 domain configured for the deployment;
the path below is the contract, not a second host.

```bash
version=vX.Y.Z
artifact=tearoom_${version}_darwin_arm64.tar.gz
base_url=ARTIFACT_BASE_URL

curl --fail --location --output "$artifact" \
  "$base_url/releases/$version/$artifact"
curl --fail --location --output SHA256SUMS \
  "$base_url/releases/$version/SHA256SUMS"

grep "  $artifact$" SHA256SUMS | shasum -a 256 --check -
tar --extract --gzip --file "$artifact"
```

The SHA256 check must pass before the binary is installed or put on `PATH`.
Do not resolve `latest.json` and then silently treat the result as pinned;
record the version it returns and use that version prefix for the download.

## Release credentials

Only the release publication process may hold R2 write credentials: normally
the `Release` workflow, or a deliberately controlled operator invocation of
`release-upload`. Configure them as repository or environment secrets in the
private source repository, scoped to the release bucket and to object writes
needed by publication. The build must not receive those credentials as linker
values or embed them in the binary.

Rotate credentials by creating the replacement key, updating the workflow
secret, running a non-destructive publication check, and revoking the old key
only after the new key works. If a key may have leaked, revoke it first, then
repair the workflow and republish the affected immutable version under a new
version. Never overwrite a published version to repair provenance.

The R2 token policy must cover the target bucket's `head-object`/read and
create/write operations. Upload preflights each immutable object with a HEAD
request, accepts an existing object only when its stored SHA256 metadata
matches, and the publication step must be able to create missing objects. A
write-only token cannot safely resume or prove immutability and is invalid for
this workflow.

The current P0 local server has no hosted authentication layer. `TEAROOM_ACTOR`
identifies a local Actor and is not a release credential. Hosted credentials
remain a deferred product boundary; see [domain concepts](../domain/concepts.md).

## Publish and verify

Before creating a release tag, run the repository's complete verification
command:

```bash
make check
```

The workflow runs `make check` before its release-specific package build and
artifact checks. Treat a tag as publishable only after that verification passes
on the tagged commit.

The local release entry points are `make package`, `make package-check`,
`make release-check`, and `make release-upload`. `package` cross-builds the
four published targets, creates the archives and SPDX document, writes
`SHA256SUMS` and `latest.json`, and verifies the result. `package-check` builds
twice in disposable directories with a fixed commit timestamp and compares
`SHA256SUMS` to prove deterministic bytes. `release-upload` uses the AWS S3
API against R2, skips an existing immutable object only when its `sha256`
metadata matches, refuses mismatches, uploads the package, `SHA256SUMS`, and
SBOM, then updates `releases/latest.json` with no-cache headers. It finally
reads the public domain back, verifies the Linux amd64 checksum, and executes
its `--version` output.

The `Release` workflow runs `make check`, builds from the tagged commit,
injects the tag and commit into `internal/version`, and invokes these
checked-in entry points. The package check verifies archive contents, SHA256
entries, the SPDX 2.3 document, and the generated manifest before upload.
Updating `latest.json` is followed by public checksum, archive, and
`tearoom --version` readback.

For a tagged release, the workflow supplies `RELEASE_TAG=vX.Y.Z` and
`VERSION_LDFLAGS` for both `internal/version.Version` and
`internal/version.Commit`. Publication requires the repository variables
`R2_ENDPOINT` and `R2_BUCKET`, plus the secrets
`R2_ACCESS_KEY_ID` and `R2_SECRET_ACCESS_KEY`; `R2_PUBLIC_BASE_URL` is also a
repository variable. Secrets are exposed to the script as
`AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` only in the upload step for the
AWS-compatible API.

Do not publish from an unreviewed working tree. A release is complete only when
the immutable objects, their SHA256 file, the version output, and the mutable
latest pointer agree on one tag and commit.

## Rollback

Rollback is a pointer and deployment decision, not an overwrite. Stop using a
bad version, select a previously verified immutable version, and repoint
`releases/latest.json` only after checking that version's checksum and
compatibility. Keep the bad immutable prefix for investigation. The current
`release-upload` command never changes existing immutable bytes: an existing
object with matching SHA256 metadata is safely skipped, while an object with a
different digest is rejected. An operator must use the R2 control plane to
update only the mutable `latest.json` object during rollback.

The current storage contract is `format: 2`. A format-2 cutover is a clean
break: an older binary does not read the new layout. Before a storage-changing
release, preserve the operator-owned data snapshot required by the prior
binary. To roll back after cutover, deploy the matched prior binary and restore
its matching pre-cutover snapshot. Never rely on `latest.json` alone to make a
new-format dataset readable by an older binary.

See [status and clean-break migration](../architecture/status.md) for the
implementation boundary and [domain concepts](../domain/concepts.md) for the
trust boundary around local data and public binaries.
