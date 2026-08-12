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

1. **Tag validation** — the tag arrives via the workflow environment and is
   validated against a fully-anchored pattern before it is used anywhere;
   a **stable tag must point at a commit on `main`** (asserted with
   `git merge-base --is-ancestor` after fetching `origin/main`); test /
   pre-release tags are exempt from the main check (they may come from any
   branch).
2. **Green gate** — runs the same quality steps as the T-005 CI pipeline
   (`go build ./...`, `go vet ./...`, `gofmt`, `go test -race -count=1
   ./...`, `scripts/validate-manifest.sh`). A failing gate aborts the
   release.
3. **Build** — the standard executable for the release platforms
   (linux/darwin × amd64/arm64, `CGO_ENABLED=0`).
4. **Package** — the release archive
   `anvil-standard-laravel-<version>.tar.gz`: the platform binaries plus
   the standard content (source manifest, manifest documentation, lifecycle
   definition, verification, templates, compatibility, docs, license). The
   archive is the release content resolved from
   `distribution.location` at adoption.
5. **Derive + sign** — the publishable registry metadata document is
   derived from the source manifest (identity, contract version, capability
   carried over) with the release-time fields populated from the real
   release: `distribution` (github-releases, https), `lifecycle`
   (`published`), and `trust` with the **real SHA-256 content digest** of
   the archive AND of every platform binary (TS-014-04-04) and an
   **Ed25519 publisher attestation** over the canonical payload (see Trust
   below). The source manifest's placeholder trust
   values are never shipped; the derived document is run through the
   pipeline's self-parse guard (the strict-parser format surface) and
   self-verified (integrity + attestation + per-binary digests) before
   publishing.
6. **Publish** — creates the GitHub Release with the archive, the registry
   metadata document, `SHA256SUMS.txt`, and the platform binaries; the
   release notes carry the attestation public key and a ready-to-use trust
   anchors snippet.
7. **Index (stable releases only)** — commits the registry metadata
   document to `registry/index/anvil-standard-laravel/<version>.json` on
   `main`: the add-only static index (ADR-030; the `anvil` registry
   client's index layout), so a checkout of this repository is the static
   registry index. **Pre-release/test releases never touch the index** —
   they create the GitHub pre-release with assets only, keeping the stable
   version namespace clean.

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
Pre-release tags may come from any branch (the main-ancestor check applies
to stable tags only) and **never touch the static index** on `main` — the
index document is published for stable releases only. Because the index is
add-only per release version, a test release of version X.Y.Z should not be
followed by a stable release of the same X.Y.Z (bump the version, or remove
the test index document if one was ever published for that version).

## Artifact set

| Artifact | Meaning |
|---|---|
| `anvil-standard-laravel-<version>.tar.gz` | The release content: platform binaries + standard parts; the content `distribution.location` resolves and `trust.contentDigests` covers |
| `registry-metadata-<version>.json` | The registry metadata document of this release (discoverable, installable through the registry flow) |
| `registry-metadata-<version>.json.sig` | The DETACHED Ed25519 signature over the raw metadata document bytes (F-1), published only when signing with a STABLE key; the bootstrap installer verifies it with its pinned publisher key before trusting the document's digests |
| `SHA256SUMS.txt` | Checksums of every asset (same-channel fallback material for adopters without the attestation path) |
| `binaries/anvil-adapter-laravel-<os>-<arch>` | The standard executable per release platform |

## Trust (ADR-022)

- **Integrity.** `trust.contentDigests` carries the real SHA-256 digest
  (canonical base16) of the release archive plus a NAMED entry per
  platform binary (TS-014-04-04: `contentDigests[].name` binds the entry
  to its asset, e.g. `anvil-adapter-laravel-linux-amd64`). At adoption
  every declared CONTENT digest must match the recomputed hash of the
  fetched content (all-match semantics), and every installed binary is
  verified against its named entry — closing the same-channel, unsigned
  `SHA256SUMS.txt` trust gap (TS-016-04-01 §6 accepted risk 1).
- **Publisher attestation.** `trust.attestation` is an Ed25519 signature
  over the canonical payload

  ```
  utf8(id) || 0x00 || utf8(version) || 0x00 ||
  concat(decoded digest bytes in contentDigests array order)
  ```

  — the exact composition the Anvil Runtime registry client verifies
  byte-for-byte (Core `internal/registry/trust.go`; PM decision D-01) —
  plus the publisher's base64 verification public key. Because the payload
  concatenates EVERY declared digest in array order — each named entry
  contributing `utf8(name) || 0x00 || digest bytes` (security review
  F-2) — the signature binds the archive AND each binary asset AND the
  asset NAME: a same-channel attacker who swaps a binary (and its
  checksum entry) cannot adjust the named digest, strip the name (to
  force the checksum fallback), or rename it across assets without the
  signing key.

  The pipeline additionally publishes a DETACHED signature over the RAW
  metadata document bytes (`registry-metadata-<version>.json.sig`,
  security review F-1) when signing with a STABLE key
  (`RELEASE_SIGNING_KEY` / `--key`): the bootstrap installer
  (install.sh) verifies it against its pinned publisher key before
  trusting any digest inside the document. Release-time-generated keys
  ship no `.sig` — nothing could verify them out of band, and a stray
  signature would fail installs closed.
- **What the attestation actually guarantees.** The Ed25519 signature
  proves the release was signed by the holder of the declared public key
  and that the declared claims (id, version, content digests) are bound
  together. With the default **release-time key** (a fresh key pair
  generated for every release, no secrets in CI), the key itself is NOT a
  publisher identity: anyone can generate a key pair and sign a release.
  Origin is established only by the out-of-band trust anchor allowlist
  (PM decision D-07 — there is no privileged path and no automatic
  first-use acceptance in the registry client).
- **Honest note on anchor management (release-time key).** The registry
  client's anchor check has no TOFU, but an OPERATOR who copies the public
  key from the release's own notes (or `trust-anchors.snippet.json`
  attached to the release) into the anchors file is practicing
  **de-facto TOFU**: the anchor then pins content integrity and
  attestation — it detects tampering in the release channel after first
  contact — but it does **not** prove publisher origin, because the key
  was obtained from the very artifact being trusted. Operators who
  require origin guarantees must distribute the anchor **out of band**
  (organization key distribution, signed announcements, ceremonies) and
  must not re-pin silently from release notes. Because the key changes
  every release, the anchor must be updated per release — verify each new
  key out of band before pinning.
- **Stable signing key (recommended for production).** Set the
  `RELEASE_SIGNING_KEY` repository secret (base64 PEM PKCS#8 Ed25519
  private key) in the standard repository's GitHub settings, or pass
  `--key <file>` locally. The pipeline then signs every release with the
  same key: the trust anchor stays stable across releases, and the
  out-of-band pinning ceremony happens once. Generate a key pair locally
  with:

  ```sh
  go run ./cmd/release-sign generate --out ./keys
  # set the secret: gh secret set RELEASE_SIGNING_KEY < ./keys/release-signing-key.pem
  # (the secret value is the base64 encoding of the PEM file)
  base64 -w0 ./keys/release-signing-key.pem | gh secret set RELEASE_SIGNING_KEY -R maleolabs/anvil-standard-laravel
  ```

  The workflow passes `RELEASE_SIGNING_KEY` to the pipeline; when the
  secret is unset, the release-time key path above is used.

  Anchors file shape (one entry per publisher; the standard id is the
  publisher identity):

  ```json
  {
    "publishers": {
      "anvil-standard-laravel": "<base64 Ed25519 public key>"
    }
  }
  ```

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
