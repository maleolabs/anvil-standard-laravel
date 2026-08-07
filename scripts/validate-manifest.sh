#!/usr/bin/env bash
#
# validate-manifest.sh — validates the SOURCE manifest of the Laravel
# delivery lifecycle standard (manifest/registry-metadata.json) for INTERNAL
# consistency.
#
# What this validates (the source manifest's authoring-time surface):
#   1. the manifest file exists and is parseable JSON;
#   2. the declared $schema is the registry metadata format
#      (urn:anvil:spec:registry-metadata:1.0.0);
#   3. id matches the standard identity pattern ^[a-z0-9][a-z0-9-]*$ and is
#      this standard's identity (anvil-standard-laravel);
#   4. version is well-formed semver (major.minor.patch, no leading zeros);
#   5. contractVersion is well-formed semver with a major >= 1 (the contract
#      major is the compatibility unit, ADR-024 §3.1);
#   6. capability.frameworkVersion is a non-empty, unique array of
#      well-formed framework semvers (the declared framework-version
#      support scope, registry-metadata.schema.json).
#
# What this deliberately does NOT validate (registry parseability):
#   The source manifest carries the release-time fields — distribution,
#   lifecycle and trust — as format-valid PLACEHOLDERS (zero content
#   digests, dummy attestation), to be replaced with real values at
#   publication by the standard's release pipeline (ADR-030, TS-016-03-02).
#   Until then the source manifest is NOT a publishable registry metadata
#   document for the Core registry client's strict parser
#   (registry-metadata.schema.json, which requires distribution, lifecycle
#   and trust) — do not run it through the registry parser. This script
#   checks internal consistency only, so CI stays green on the source
#   manifest.
#
# Mirrors the pattern of the Core repository's
# scripts/validate-contract-version.sh: self-contained bash, [OK]/[FAIL]
# output, RESULT: PASS/FAIL, and a --selftest.
#
# Usage:
#   bash scripts/validate-manifest.sh [MANIFEST_PATH]
#   bash scripts/validate-manifest.sh --selftest
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
MANIFEST_DEFAULT="${ROOT_DIR}/manifest/registry-metadata.json"
EXPECTED_ID="anvil-standard-laravel"
SCHEMA_URN="urn:anvil:spec:registry-metadata:1.0.0"

# ── Helpers ────────────────────────────────────────────────────────

# Strict semver: major.minor.patch without leading zeros
# (registry-metadata.schema.json version/contractVersion pattern).
is_semver() { # <version>
    printf '%s' "$1" | grep -Eq '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
}

is_positive_int() { # <value>
    printf '%s' "$1" | grep -Eq '^[0-9]+$' && [ "$1" -ge 1 ]
}

# ── Core validation ────────────────────────────────────────────────

# run_checks <manifest>
# Exit 0 when the source manifest is internally consistent, 1 otherwise.
run_checks() {
    local manifest="$1"
    local failed=0 json id version contract_version schema framework_count
    local major dupes bad

    f() { printf '[FAIL] %s\n' "$*"; failed=1; }
    o() { printf '[OK]   %s\n' "$*"; }

    if [ ! -f "${manifest}" ]; then
        f "manifest not found: ${manifest}"
        return 1
    fi

    if ! json="$(jq -e . "${manifest}" 2>/dev/null)"; then
        f "manifest is not parseable JSON: ${manifest}"
        return 1
    fi
    o "manifest parses as JSON: ${manifest}"

    # ── $schema declaration ────────────────────────────────────────
    schema="$(jq -r '."$schema" // empty' <<<"${json}")"
    if [ -z "${schema}" ]; then
        f "no '$schema' declaration; expected ${SCHEMA_URN}"
    elif [ "${schema}" != "${SCHEMA_URN}" ]; then
        f "declared '$schema' is '${schema}'; expected ${SCHEMA_URN}"
    else
        o "declared \$schema: ${schema}"
    fi

    # ── id ─────────────────────────────────────────────────────────
    id="$(jq -r '.id // empty' <<<"${json}")"
    if [ -z "${id}" ]; then
        f "missing 'id' field"
    else
        if ! printf '%s' "${id}" | grep -Eq '^[a-z0-9][a-z0-9-]*$'; then
            f "id '${id}' does not match the standard identity pattern ^[a-z0-9][a-z0-9-]*$ (registry-metadata schema)"
        else
            o "id matches the standard identity pattern: ${id}"
        fi
        if [ "${id}" != "${EXPECTED_ID}" ]; then
            f "id '${id}' is not this standard's identity; expected ${EXPECTED_ID}"
        else
            o "id is this standard's identity: ${id}"
        fi
    fi

    # ── version ────────────────────────────────────────────────────
    version="$(jq -r '.version // empty' <<<"${json}")"
    if [ -z "${version}" ]; then
        f "missing 'version' field"
    elif ! is_semver "${version}"; then
        f "version '${version}' is not well-formed semver (major.minor.patch, no leading zeros)"
    else
        o "version: ${version}"
    fi

    # ── contractVersion ────────────────────────────────────────────
    contract_version="$(jq -r '.contractVersion // empty' <<<"${json}")"
    if [ -z "${contract_version}" ]; then
        f "missing 'contractVersion' field"
    else
        if ! is_semver "${contract_version}"; then
            f "contractVersion '${contract_version}' is not well-formed semver (major.minor.patch, no leading zeros)"
        else
            major="$(printf '%s' "${contract_version}" | cut -d. -f1)"
            if ! is_positive_int "${major}"; then
                f "contractVersion major '${major}' must be >= 1 (contract majors start at 1, ADR-024 §3.1)"
            else
                o "contractVersion: ${contract_version} (contract major ${major})"
            fi
        fi
    fi

    # ── capability.frameworkVersion ────────────────────────────────
    fv_type="$(jq -r '.capability.frameworkVersion | type' <<<"${json}" 2>/dev/null || true)"
    if [ -z "${fv_type}" ] || [ "${fv_type}" != "array" ]; then
        f "capability.frameworkVersion must be an array (declared framework-version support scope); got: '${fv_type:-<missing>}'"
    else
        framework_count="$(jq -r '.capability.frameworkVersion | length' <<<"${json}")"
        if [ "${framework_count}" = "0" ]; then
            f "capability.frameworkVersion must be a non-empty array (declared framework-version support scope)"
        else
            dupes="$(jq -r '.capability.frameworkVersion | group_by(.)[] | select(length > 1) | .[0]' <<<"${json}" | tr '\n' ' ')"
            if [ -n "${dupes}" ]; then
                f "capability.frameworkVersion contains duplicate entries: ${dupes}"
            fi
            bad="$(jq -r '.capability.frameworkVersion[]' <<<"${json}" | while read -r v; do
                is_semver "${v}" || printf '%s\n' "${v}"
            done | tr '\n' ' ')"
            if [ -n "${bad}" ]; then
                f "capability.frameworkVersion entries are not well-formed semver: ${bad}"
            else
                o "capability.frameworkVersion: ${framework_count} supported framework version(s)"
            fi
        fi
    fi

    return "${failed}"
}

# ── Self-test ──────────────────────────────────────────────────────

SELFTEST_TMP=""

selftest() {
    local total=0 passed=0
    SELFTEST_TMP="$(mktemp -d)"
    trap 'rm -rf "${SELFTEST_TMP}"' EXIT

    cat > "${SELFTEST_TMP}/valid.json" <<'EOF'
{
  "$schema": "urn:anvil:spec:registry-metadata:1.0.0",
  "title": "Laravel Delivery Lifecycle Standard",
  "id": "anvil-standard-laravel",
  "version": "1.0.0",
  "contractVersion": "1.0.0",
  "capability": { "frameworkVersion": ["10.0.0", "11.0.0", "12.0.0"] }
}
EOF

    run_case() { # <name> <expected pass|fail> <manifest>
        local name="$1" expected="$2" manifest="$3" rc=0
        total=$((total + 1))
        if run_checks "${manifest}" >/dev/null 2>&1; then rc=0; else rc=1; fi
        if { [ "${expected}" = "pass" ] && [ "${rc}" -eq 0 ]; } || \
           { [ "${expected}" = "fail" ] && [ "${rc}" -ne 0 ]; }; then
            passed=$((passed + 1))
            printf '[OK]   %s (expected %s)\n' "${name}" "${expected}"
        else
            printf '[FAIL] %s: expected %s, got %s\n' "${name}" "${expected}" "${rc}"
        fi
    }

    run_case "valid-source-manifest" pass "${SELFTEST_TMP}/valid.json"
    jq '.version = "1.0"' "${SELFTEST_TMP}/valid.json" > "${SELFTEST_TMP}/bad-version.json"
    run_case "malformed-version" fail "${SELFTEST_TMP}/bad-version.json"
    jq '.contractVersion = "0.5.0"' "${SELFTEST_TMP}/valid.json" > "${SELFTEST_TMP}/zero-major.json"
    run_case "contract-major-zero" fail "${SELFTEST_TMP}/zero-major.json"
    jq '.id = "some-other-standard"' "${SELFTEST_TMP}/valid.json" > "${SELFTEST_TMP}/wrong-id.json"
    run_case "wrong-id" fail "${SELFTEST_TMP}/wrong-id.json"
    jq 'del(.capability)' "${SELFTEST_TMP}/valid.json" > "${SELFTEST_TMP}/no-capability.json"
    run_case "missing-capability" fail "${SELFTEST_TMP}/no-capability.json"
    jq '.capability.frameworkVersion = ["10.0.0", "10.0"]' "${SELFTEST_TMP}/valid.json" > "${SELFTEST_TMP}/bad-framework.json"
    run_case "malformed-framework-version" fail "${SELFTEST_TMP}/bad-framework.json"
    jq '."$schema" = "urn:anvil:spec:registry-metadata:0.9.0"' "${SELFTEST_TMP}/valid.json" > "${SELFTEST_TMP}/wrong-schema.json"
    run_case "wrong-schema" fail "${SELFTEST_TMP}/wrong-schema.json"
    printf 'not json' > "${SELFTEST_TMP}/broken.json"
    run_case "invalid-json" fail "${SELFTEST_TMP}/broken.json"

    printf '\nSELFTEST: %d/%d cases passed\n' "${passed}" "${total}"
    [ "${passed}" -eq "${total}" ]
}

# ── Main ───────────────────────────────────────────────────────────

main() {
    if [ "${1:-}" = "--selftest" ]; then
        selftest
        return $?
    fi
    local manifest="${1:-${MANIFEST_DEFAULT}}"
    printf 'Source manifest validation: %s\n' "${manifest}"
    if ! run_checks "${manifest}"; then
        printf '\nRESULT: FAIL\n'
        exit 1
    fi
    printf '\nRESULT: PASS\n'
}

main "$@"
