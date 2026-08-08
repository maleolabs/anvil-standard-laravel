#!/usr/bin/env bash
#
# release.sh — produce a versioned release of the Laravel delivery lifecycle
# standard and publish it to the registry distribution channel
# (ADR-023, ADR-030; TS-016-03-02).
#
# The pipeline (mirrors the Core release conventions, .github/workflows/
# release.yml + scripts/bump.sh, applied to this standard repository):
#   1. validate the source manifest (scripts/validate-manifest.sh);
#   2. assert the tag version matches the manifest version line — the
#      standard version lives in the manifest `version` field
#      (manifest/registry-metadata.json), and the git tag v<version> must
#      agree with it before a release is produced;
#   3. build the standard executable for the release platforms
#      (linux/darwin × amd64/arm64 — the Core release matrix pattern);
#   4. package the release artifact: a single archive carrying the standard
#      content (platform binaries + source manifest + the standard parts);
#   5. compute the REAL content digest (sha-256, canonical base16) over the
#      archive AND over each platform binary (TS-014-04-04) and derive the
#      publishable registry metadata document from the source manifest —
#      distribution/lifecycle/trust are populated with real values (the
#      source manifest's placeholder trust is never shipped; TS-016-03-02);
#      the archive digest is the unnamed trust.contentDigests entry, each
#      binary digest is a NAMED entry binding the asset to the attested
#      release — closing the same-channel-checksum trust gap
#      (TS-016-04-01 §6 accepted risk 1; Core verifies every installed
#      binary against these digests, TS-014-04-04);
#   6. sign the canonical attestation payload (Ed25519; the payload is
#      utf8(id) || 0x00 || utf8(version) || 0x00 || concat(decoded digest
#      bytes in contentDigests array order) — the exact composition the
#      Core registry client verifies, internal/registry/trust.go) with the
#      release signing key; the signature binds the archive AND every
#      binary asset;
#   7. self-verify the produced document and binaries (the release
#      pipeline never publishes material it cannot verify);
#   8. [--publish] create the GitHub release with the gh CLI (assets:
#      archive, registry metadata document, checksums, platform binaries).
#
# Trust model (ADR-022; PM decision D-01, D-07):
#   - a release-time Ed25519 key pair is generated for every release unless
#     RELEASE_SIGNING_KEY (base64 PEM) or --key <file> supplies a stable
#     signing key;
#   - the attestation proves the release was signed by the holder of the
#     declared public key; publisher origin is established by the adopter
#     pinning that key out of band (trust anchors allowlist). Every release
#     therefore prints its public key and a ready-to-use anchors snippet.
#     NOTE (docs/release.md Trust section): with the release-time key, an
#     anchor copied from the release's own notes is de-facto TOFU — it pins
#     integrity/attestation and detects channel tamper, it does NOT prove
#     publisher origin. Use RELEASE_SIGNING_KEY for a stable key.
#
# Environment: bash, jq, a Go toolchain, tar, and sha256sum (GNU coreutils)
# or shasum (macOS). base64 decoding auto-detects -d (GNU) vs -D (BSD).
#
# Tag convention: v<version> (plain semver) is a stable release. A suffix
# after the version marks a GitHub PRE-RELEASE and is stripped from the
# registry metadata version — e.g. v1.0.0-test or v1.0.0-pre.2 publish
# registry metadata version 1.0.0 as a pre-release (the registry metadata
# version pattern is plain semver only). Test releases of version X.Y.Z
# must not be followed by a stable release of the same X.Y.Z — bump the
# version (the index document is add-only per release version).
#
# Usage:
#   scripts/release.sh [--tag <git-tag>] [--out <dir>]
#                      [--key <file> | --generate-key] [--publish]
#
# This is release-time infrastructure, not part of the standard executable.
#
# Reference: TS-016-03-02, ADR-022 §3, ADR-023 §3, ADR-030 §3

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

# ── Standard identity ───────────────────────────────────────────────
STANDARD_ID="anvil-standard-laravel"
BINARY_NAME="anvil-adapter-laravel"
ADAPTER_DIR="cmd/laravel-adapter"
SOURCE_MANIFEST="${ROOT_DIR}/manifest/registry-metadata.json"
VALIDATOR="${ROOT_DIR}/scripts/validate-manifest.sh"
REPO="maleolabs/anvil-standard-laravel"
REPO_URL="https://github.com/${REPO}"

# Standard content carried by the release archive (the artifact set: the
# standard executable for the release platforms plus the standard parts —
# manifest, lifecycle, verification, templates, compatibility, docs). The
# platform binaries are staged separately (see below).
ARCHIVE_PARTS=(
    "manifest"
    "MANIFEST.md"
    "README.md"
    "LICENSE"
    "docs"
    "lifecycle"
    "verification"
    "templates"
    "compatibility"
)

# Release platforms (Core release.yml matrix pattern).
PLATFORMS=("linux/amd64" "linux/arm64" "darwin/amd64" "darwin/arm64")

# ── Args ───────────────────────────────────────────────────────────
TAG=""
OUT_DIR="${ROOT_DIR}/dist"
KEY_FILE=""
GENERATE_KEY=0
PUBLISH=0

while [ $# -gt 0 ]; do
    case "$1" in
        --tag) TAG="${2:-}"; shift 2 ;;
        --out) OUT_DIR="${2:-}"; shift 2 ;;
        --key) KEY_FILE="${2:-}"; shift 2 ;;
        --generate-key) GENERATE_KEY=1; shift ;;
        --publish) PUBLISH=1; shift ;;
        --help|-h)
            sed -n '1,40p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *) echo "error: unknown argument '$1'" >&2; exit 2 ;;
    esac
done

# ── Helpers ────────────────────────────────────────────────────────

log() { printf '[release] %s\n' "$*"; }
fail() { printf '[release] ERROR: %s\n' "$*" >&2; exit 1; }

is_semver() { # <version>
    printf '%s' "$1" | grep -Eq '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
}

# sha256sum_portable — GNU coreutils sha256sum on Linux, shasum -a 256 on
# macOS (the release pipeline runs on both; see the environment note at the
# top of this script).
sha256sum_portable() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$@"
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$@"
    else
        fail "neither sha256sum (coreutils) nor shasum (macOS) is available — GNU/Linux or macOS is required for release.sh"
    fi
}

# base64_decode — `base64 -d` (GNU) or `base64 -D` (BSD/macOS).
base64_decode() {
    if printf 'eA==' | base64 -d >/dev/null 2>&1; then
        base64 -d
    else
        base64 -D
    fi
}

# normalize_tag <git-tag> — strips the 'v' prefix and any pre-release
# suffix (-test, -pre, optionally -test.N/-pre.N); the suffix marks a
# GitHub pre-release.
# The pattern is FULLY-ANCHORED (^...$): a tag that carries shell
# metacharacters or any other shape is rejected before its value is used
# anywhere (shell, paths, release assets, commit messages).
# Outputs (global): TAG_VERSION, META_VERSION, PRERELEASE
normalize_tag() {
    local raw="${1#v}"
    if ! printf '%s' "${raw}" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-(test|pre)(\.[0-9]+)?)?$'; then
        fail "tag '$1' is not a valid release tag (must match ^v[0-9]+\.[0-9]+\.[0-9]+(-(test|pre)(\.[0-9]+)?)?$)"
    fi
    META_VERSION="$(printf '%s' "${raw}" | sed -E 's/-(test|pre)(\.[0-9]+)?$//')"
    TAG_VERSION="${raw}"
    if [ "${META_VERSION}" != "${raw}" ]; then
        PRERELEASE=1
    else
        PRERELEASE=0
    fi
    if ! is_semver "${META_VERSION}"; then
        fail "tag '$1' does not carry a plain semver version (${META_VERSION})"
    fi
}

# ── 1. Validate the source manifest ────────────────────────────────
log "validating source manifest (${SOURCE_MANIFEST})"
bash "${VALIDATOR}" >/dev/null
log "source manifest validation: PASS"

# ── 2. Version: tag vs manifest version line ───────────────────────
MANIFEST_VERSION="$(jq -r '.version' "${SOURCE_MANIFEST}")"
if [ -z "${TAG}" ]; then
    # Local use without a tag: release the manifest's declared version.
    TAG_VERSION="${MANIFEST_VERSION}"
    META_VERSION="${MANIFEST_VERSION}"
    PRERELEASE=0
    log "no --tag given; using the manifest version line: ${META_VERSION}"
else
    normalize_tag "${TAG}"
    if [ "${META_VERSION}" != "${MANIFEST_VERSION}" ]; then
        fail "tag version ${META_VERSION} does not match the manifest version line ${MANIFEST_VERSION} — bump the manifest version field first (the manifest is the version line, ADR-021 §3.4)"
    fi
fi
log "release ${STANDARD_ID} ${META_VERSION} (tag v${TAG_VERSION}, prerelease=$([ "${PRERELEASE}" -eq 1 ] && echo yes || echo no))"

# ── 3. Build the standard executable for the release platforms ─────
BIN_DIR="${OUT_DIR}/binaries"
mkdir -p "${BIN_DIR}"
for platform in "${PLATFORMS[@]}"; do
    goos="${platform%/*}"
    goarch="${platform#*/}"
    target="${BIN_DIR}/${BINARY_NAME}-${goos}-${goarch}"
    log "building ${target}"
    GOOS="${goos}" GOARCH="${goarch}" CGO_ENABLED=0 \
        go build -trimpath -o "${target}" "./${ADAPTER_DIR}"
done
log "release binaries: PASS (${#PLATFORMS[@]} platforms)"

# ── 4. Package the release archive ─────────────────────────────────
ARCHIVE_NAME="${STANDARD_ID}-${META_VERSION}.tar.gz"
ARCHIVE="${OUT_DIR}/${ARCHIVE_NAME}"
STAGE="$(mktemp -d)"
trap 'rm -rf "${STAGE}"' EXIT
mkdir -p "${STAGE}/binaries"
cp "${BIN_DIR}"/* "${STAGE}/binaries/"
for part in "${ARCHIVE_PARTS[@]}"; do
    if [ -e "${ROOT_DIR}/${part}" ]; then
        cp -r "${ROOT_DIR}/${part}" "${STAGE}/"
    else
        log "warning: archive part '${part}' not found; skipping"
    fi
done
tar -czf "${ARCHIVE}" -C "${STAGE}" .
log "release archive: ${ARCHIVE}"

# ── 5. Signing key (release-time or stable) ────────────────────────
KEY_DIR="${OUT_DIR}/keys"
mkdir -p "${KEY_DIR}"
if [ -n "${KEY_FILE}" ]; then
    log "using signing key: ${KEY_FILE}"
elif [ -n "${RELEASE_SIGNING_KEY:-}" ]; then
    KEY_FILE="${KEY_DIR}/release-signing-key.pem"
    printf '%s' "${RELEASE_SIGNING_KEY}" | base64_decode > "${KEY_FILE}"
    chmod 600 "${KEY_FILE}"
    log "using signing key from RELEASE_SIGNING_KEY"
else
    KEY_FILE="${KEY_DIR}/release-signing-key.pem"
    log "no signing key supplied; generating a release-time key"
    GENERATE_KEY=1
fi
if [ "${GENERATE_KEY}" -eq 1 ] && [ ! -s "${KEY_FILE}" ]; then
    go run "./cmd/release-sign" generate --out "${KEY_DIR}" >/dev/null
fi

# ── 6. Derive + sign the registry metadata document ────────────────
DIST_LOCATION="${REPO_URL}/releases/download/v${TAG_VERSION}/${ARCHIVE_NAME}"
META_DOC="${OUT_DIR}/registry-metadata-${META_VERSION}.json"
go run "./cmd/release-sign" sign \
    --manifest "${SOURCE_MANIFEST}" \
    --version "${META_VERSION}" \
    --archive "${ARCHIVE}" \
    --location "${DIST_LOCATION}" \
    --key "${KEY_FILE}" \
    --binaries "${OUT_DIR}/binaries" \
    --out "${META_DOC}"
PUBLIC_KEY="$(jq -r '.trust.attestation.publicKey' "${META_DOC}")"
log "registry metadata document: ${META_DOC}"

# ── 7. Self-verify the produced release ────────────────────────────
go run "./cmd/release-sign" verify \
    --document "${META_DOC}" \
    --archive "${ARCHIVE}" \
    --binaries "${OUT_DIR}/binaries"
log "self-verification: PASS"

# ── Checksums + trust anchors snippet ──────────────────────────────
(
    cd "${OUT_DIR}"
    sha256sum_portable "${ARCHIVE_NAME}" "registry-metadata-${META_VERSION}.json" \
        binaries/* > SHA256SUMS.txt
)
log "checksums: ${OUT_DIR}/SHA256SUMS.txt"
cat > "${OUT_DIR}/trust-anchors.snippet.json" <<EOF
{
  "publishers": {
    "${STANDARD_ID}": "${PUBLIC_KEY}"
  }
}
EOF
log "trust anchors snippet: ${OUT_DIR}/trust-anchors.snippet.json"

# ── 8. Publish (optional) ──────────────────────────────────────────
if [ "${PUBLISH}" -eq 1 ]; then
    command -v gh >/dev/null 2>&1 || fail "--publish requires the gh CLI (authenticated with repo scope)"
    TITLE="v${TAG_VERSION}"
    PRERELEASE_FLAG=""
    NOTES="${OUT_DIR}/release-notes.md"
    if [ "${PRERELEASE}" -eq 1 ]; then
        PRERELEASE_FLAG="--prerelease"
        TITLE="v${TAG_VERSION} (TEST / pre-release)"
    fi
    cat > "${NOTES}" <<EOF
# ${STANDARD_ID} v${TAG_VERSION}

Release of the Laravel delivery lifecycle standard, registry version
**${META_VERSION}** (contract version $(jq -r '.contractVersion' "${SOURCE_MANIFEST}")).

> $(if [ "${PRERELEASE}" -eq 1 ]; then echo '**TEST / PRE-RELEASE** — created for release-pipeline validation (TS-016-03-02); do not adopt for production use.'; else echo 'Stable release.'; fi)

## Artifacts

- \`${ARCHIVE_NAME}\` — the release content (platform binaries + standard parts); distribution.location of this release
- \`registry-metadata-${META_VERSION}.json\` — the registry metadata document (id, version, contract version, capability, distribution, lifecycle, trust)
- \`SHA256SUMS.txt\` — checksums of every asset (same-channel fallback material for adopters without the attestation path)

## Trust (ADR-022)

This release is signed with an Ed25519 release-time key over the canonical
attestation payload (\`utf8(id) || 0x00 || utf8(version) || 0x00 ||
concat(decoded digest bytes in contentDigests array order)\`). The
attestation binds the release archive AND every platform binary: each
binary's sha-256 digest is carried as a NAMED \`trust.contentDigests\`
entry (TS-014-04-04), so an adoption verifies each installed binary
against attestation-bound material — not only the same-channel
\`SHA256SUMS.txt\`. Pin the public key out of band to establish publisher
origin (no first-use acceptance):

\`\`\`json
{
  "publishers": {
    "${STANDARD_ID}": "${PUBLIC_KEY}"
  }
}
\`\`\`

Public key (base64): \`${PUBLIC_KEY}\`
EOF
    log "creating GitHub release v${TAG_VERSION}"
    GH_TOKEN="${GH_TOKEN:-}" gh release create "v${TAG_VERSION}" \
        --title "${TITLE}" \
        --notes-file "${NOTES}" \
        ${PRERELEASE_FLAG} \
        "${BIN_DIR}"/* \
        "${ARCHIVE}" \
        "${META_DOC}" \
        "${OUT_DIR}/SHA256SUMS.txt"
    log "GitHub release v${TAG_VERSION}: published (${REPO_URL}/releases/tag/v${TAG_VERSION})"
fi

# ── Summary ────────────────────────────────────────────────────────
log "release ${STANDARD_ID} ${META_VERSION} ready in ${OUT_DIR}"
log "  archive:             ${ARCHIVE_NAME}"
log "  metadata document:   registry-metadata-${META_VERSION}.json"
log "  distribution:        ${DIST_LOCATION}"
log "  attestation key:     ${PUBLIC_KEY}"
log "  trust anchors:       ${OUT_DIR}/trust-anchors.snippet.json"
log "done."
