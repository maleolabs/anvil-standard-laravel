# Release — Laravel Delivery Lifecycle Standard

This document describes how a versioned release of the Laravel delivery
lifecycle standard is produced and published to the registry distribution
channel (TS-016-03-02; ADR-023, ADR-030).

A standard release is **discoverable and installable through the registry
flow** without any Core release (ADR-025 §3.5, §4.7): the release pipeline
of this repository produces the versioned standard artifact, derives and
signs the registry metadata document, and publishes everything to this
repository's GitHub Releases. Core is never involved in producing or
publishing standard releases.

## Version line

The standard versions independently from the Core runtime (ADR-021 §3.4).
The **version line lives in the source manifest's `version` field**
([`manifest/registry-metadata.json`](../manifest/registry-metadata.json)).
The release pipeline asserts that the git tag version equals the manifest
version before producing a release.

Every release declares in its registry metadata document:

- the **contract version** it targets (`contractVersion` — the delivery
  lifecycle specification version, ADR-024 §3.1; the major version is the
  compatibility unit);
- the **framework-version support scope** (`capability.frameworkVersion` —
  ADR-021 §3.2).

## Release flow (maintainer)

Releases are cut from `main` (the release branch):

```sh
# 1. Bump the version line on develop: edit manifest/registry-metadata.json
#    (version field), commit, and let CI green (T-005).
#    Example: version "1.0.0" -> "1.1.0"

# 2. Promote develop -> main (a merge, keeping main as the release branch)
git checkout main
git merge --no-ff develop
git push origin main

# 3. Tag the release from main and push the tag — this triggers the
#    Release workflow (.github/workflows/release.yml)
git tag v1.1.0
git push origin v1.1.0
```

The workflow then:

1. **Green gate** — runs the same quality steps as the T-005 CI pipeline
   (`go build ./...`, `go vet ./...`, `gofmt`, `go test -race -count=1
   ./...`, `scripts/validate-manifest.sh`). A failing gate aborts the
   release.
2. **Build** — the standard executable for the release platforms
   (linux/darwin × amd64/arm64, `CGO_ENABLED=0`).
3. **Package** — the release archive
   `anvil-standard-laravel-<version>.tar.gz`: the platform binaries plus
   the standard content (source manifest, manifest documentation, lifecycle
   definition, verification, templates, compatibility, docs, license). The
   archive is the release content resolved from
   `distribution.location` at adoption.
4. **Derive + sign** — the publishable registry metadata document is
   derived from the source manifest (identity, contract version, capability
   carried over) with the release-time fields populated from the real
   release: `distribution` (github-releases, https), `lifecycle`
   (`published`), and `trust` with the **real SHA-256 content digest** of
   the archive and an **Ed25519 publisher attestation** over the canonical
   payload (see Trust below). The source manifest's placeholder trust
   values are never shipped.
5. **Self-verify** — the pipeline verifies its own output (integrity +
   attestation) before publishing; it never publishes material it cannot
   verify.
6. **Publish** — creates the GitHub Release with the archive, the registry
   metadata document, `SHA256SUMS.txt`, and the platform binaries; the
   release notes carry the attestation public key and a ready-to-use trust
   anchors snippet.
7. **Index** — commits the registry metadata document to
   `registry/index/anvil-standard-laravel/<version>.json` on `main`: the
   add-only static index (ADR-030; the `anvil` registry client's index
   layout), so a checkout of this repository is the static registry index.

### Test / pre-release tags

A tag suffix marks a GitHub **pre-release** and is stripped from the
registry metadata version (the registry metadata `version` pattern is plain
semver):

```sh
git tag v1.1.0-test     # or v1.1.0-pre, v1.1.0-test.2
git push origin v1.1.0-test
```

The GitHub release is created with the pre-release flag and its title is
labeled `(TEST / pre-release)`. The registry metadata version is `1.1.0`.
A test release of version X.Y.Z must not be followed by a stable release of
the same X.Y.Z — bump the version (the index document is add-only per
release version).

## Artifact set

| Artifact | Meaning |
|---|---|
| `anvil-standard-laravel-<version>.tar.gz` | The release content: platform binaries + standard parts; the content `distribution.location` resolves and `trust.contentDigests` covers |
| `registry-metadata-<version>.json` | The registry metadata document of this release (discoverable, installable through the registry flow) |
| `SHA256SUMS.txt` | Checksums of every asset |
| `binaries/anvil-adapter-laravel-<os>-<arch>` | The standard executable per release platform |

## Trust (ADR-022)

- **Integrity.** `trust.contentDigests` carries the real SHA-256 digest
  (canonical base16) of the release archive. At adoption every declared
  digest must match the recomputed hash of the fetched content
  (all-match semantics).
- **Publisher attestation.** `trust.attestation` is an Ed25519 signature
  over the canonical payload

  ```
  utf8(id) || 0x00 || utf8(version) || 0x00 ||
  concat(decoded digest bytes in contentDigests array order)
  ```

  — the exact composition the Anvil Runtime registry client verifies
  byte-for-byte (Core `internal/registry/trust.go`; PM decision D-01) —
  plus the publisher's base64 verification public key.
- **Signing key.** The default is a **release-time key**: a fresh Ed25519
  key pair is generated for every release (no secret management; the
  private key never leaves the release pipeline). The attestation proves
  the release was signed by the holder of the declared public key;
  **publisher origin is established by the adopter pinning that key out of
  band** (trust anchors allowlist, PM decision D-07 — there is no
  first-use acceptance). Operators pin the key from the release notes or
  `trust-anchors.snippet.json`:

  ```json
  {
    "publishers": {
      "anvil-standard-laravel": "<base64 Ed25519 public key>"
    }
  }
  ```

  Because the key is release-time, the anchor must be updated for each
  release after out-of-band verification of the new key. A stable signing
  key can be supplied instead via `--key <file>` or the
  `RELEASE_SIGNING_KEY` environment variable (base64 PEM) — then the
  anchor stays stable across releases.

## Local release production

`scripts/release.sh` runs the whole pipeline locally (build, package,
derive, sign, self-verify) without publishing:

```sh
bash scripts/release.sh --tag v1.1.0 --out dist
```

Add `--publish` to create the GitHub Release with `gh`:

```sh
bash scripts/release.sh --tag v1.1.0 --out dist --publish
```

The signing tool (`cmd/release-sign`) exposes the individual operations:
`generate`, `sign`, `verify`.

## Discovery and installation (registry flow)

```sh
# Discovery: point the registry client at a checkout of this repository
# (or at the index directory) and list the offered releases
anvil standard list --index <checkout-of-this-repo>

# Inspect one release
anvil standard inspect anvil-standard-laravel 1.1.0 --index <checkout-of-this-repo>

# Install (explicit adoption: validation + integrity + attestation + record)
anvil standard install anvil-standard-laravel 1.1.0 --index <checkout-of-this-repo>
```

Installation requires the operator's trust anchors allowlist to pin this
publisher's key (`--trust-anchors <path>` or the default
`~/.config/anvil/trust-anchors.json`); there is no skip or insecure path
(ADR-022 §3).

*End of release documentation (TS-016-03-02).*
