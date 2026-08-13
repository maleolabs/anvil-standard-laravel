package release

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

// ValidateDocumentShape asserts that a derived metadata document satisfies
// the strict-format surface the Core registry client's parser enforces
// (registry-metadata.schema.json + parse.go strict layer, TS-014-01-02).
//
// This is the release pipeline's SELF-PARSE GUARD (TS-016-03-02 review
// finding): the pipeline must never publish a document the registry client
// cannot parse. The checks mirror the Core parser's enforceable surface,
// self-contained in this repository (no Core dependency):
//
//   - all required root fields present: id, version, contractVersion,
//     capability, distribution, lifecycle, trust;
//   - version and contractVersion match the plain-semver pattern
//     (^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$);
//   - capability.frameworkVersion is a non-empty array of unique
//     well-formed framework semvers (^[0-9]+\.[0-9]+\.[0-9]+$);
//   - distribution.type is "github-releases" and distribution.location is
//     a well-formed absolute https URL (no whitespace, no userinfo);
//   - lifecycle.state is one of published/deprecated/retired;
//   - trust.contentDigests is non-empty, each entry declares algorithm
//     "sha-256", a supported encoding, and a canonical digest value for
//     that encoding (base16: exactly 64 lowercase hex characters — the
//     encoding this pipeline produces);
//   - trust.attestation declares algorithm "ed25519", a strict RFC-4648
//     base64 signature decoding to exactly 64 bytes, and a strict
//     RFC-4648 base64 public key decoding to exactly 32 bytes.
//
// The check is format-level only (like parse.go): it does not verify the
// signature or the content digests — that is VerifyDocument's job.
func ValidateDocumentShape(doc *MetadataDocument) error {
	var problems []string

	// ── Required root fields ────────────────────────────────────────
	if doc.ID == "" {
		problems = append(problems, "missing required field 'id'")
	}
	if doc.Version == "" {
		problems = append(problems, "missing required field 'version'")
	} else if !rePlainSemver.MatchString(doc.Version) {
		problems = append(problems, fmt.Sprintf("version %q is not plain semver (^(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)$)", doc.Version))
	}
	if doc.ContractVersion == "" {
		problems = append(problems, "missing required field 'contractVersion'")
	} else if !rePlainSemver.MatchString(doc.ContractVersion) {
		problems = append(problems, fmt.Sprintf("contractVersion %q is not plain semver", doc.ContractVersion))
	}

	// ── capability ──────────────────────────────────────────────────
	if len(doc.Capability.FrameworkVersion) == 0 {
		problems = append(problems, "capability.frameworkVersion must be a non-empty array (ADR-021 §3.2)")
	} else {
		seen := make(map[string]bool, len(doc.Capability.FrameworkVersion))
		for i, fv := range doc.Capability.FrameworkVersion {
			if !reFrameworkVersion.MatchString(fv) {
				problems = append(problems, fmt.Sprintf("capability.frameworkVersion[%d] %q is not a well-formed framework version (^[0-9]+\\.[0-9]+\\.[0-9]+$)", i, fv))
			}
			if seen[fv] {
				problems = append(problems, fmt.Sprintf("capability.frameworkVersion contains duplicate %q (uniqueItems)", fv))
			}
			seen[fv] = true
		}
	}

	// ── distribution ────────────────────────────────────────────────
	if doc.Distribution.Type != DistributionTypeGitHubReleases {
		problems = append(problems, fmt.Sprintf("distribution.type %q is not supported (only %q)", doc.Distribution.Type, DistributionTypeGitHubReleases))
	}
	if doc.Distribution.Location == "" {
		problems = append(problems, "missing required field 'distribution.location'")
	} else if err := checkHTTPSLocation(doc.Distribution.Location); err != nil {
		problems = append(problems, fmt.Sprintf("distribution.location: %v", err))
	}

	// ── lifecycle ───────────────────────────────────────────────────
	switch doc.Lifecycle.State {
	case "published", "deprecated", "retired":
	default:
		problems = append(problems, fmt.Sprintf("lifecycle.state %q is not one of published, deprecated, retired", doc.Lifecycle.State))
	}

	// ── trust ───────────────────────────────────────────────────────
	if len(doc.Trust.ContentDigests) == 0 {
		problems = append(problems, "trust.contentDigests must contain at least one digest (ADR-022 §3)")
	} else {
		seen := make(map[string]bool, len(doc.Trust.ContentDigests))
		seenNames := make(map[string]bool, len(doc.Trust.ContentDigests))
		for i, d := range doc.Trust.ContentDigests {
			if d.Algorithm != DigestAlgorithmSHA256 {
				problems = append(problems, fmt.Sprintf("trust.contentDigests[%d].algorithm %q is not supported (only %q)", i, d.Algorithm, DigestAlgorithmSHA256))
			}
			switch d.Encoding {
			case DigestEncodingBase16:
				if !reDigestBase16.MatchString(d.Digest) {
					problems = append(problems, fmt.Sprintf("trust.contentDigests[%d].digest %q is not the canonical base16 encoding of a SHA-256 digest (^[0-9a-f]{64}$)", i, d.Digest))
				} else if _, err := hex.DecodeString(d.Digest); err != nil {
					problems = append(problems, fmt.Sprintf("trust.contentDigests[%d].digest is not decodable as base16: %v", i, err))
				}
			case DigestEncodingBase32:
				problems = append(problems, fmt.Sprintf("trust.contentDigests[%d] uses encoding %q, which the release pipeline does not produce (only base16)", i, d.Encoding))
			case DigestEncodingBase64:
				problems = append(problems, fmt.Sprintf("trust.contentDigests[%d] uses encoding %q, which the release pipeline does not produce (only base16)", i, d.Encoding))
			default:
				problems = append(problems, fmt.Sprintf("trust.contentDigests[%d].encoding %q is not supported (base16, base32, base64)", i, d.Encoding))
			}
			// Optional asset binding (TS-014-04-04): a safe asset
			// identifier, unique across entries — two entries can never
			// bind the same asset (mirrors Core parse.go).
			if d.Name != "" {
				if !reAssetName.MatchString(d.Name) {
					problems = append(problems, fmt.Sprintf("trust.contentDigests[%d].name %q is not a safe asset name (^[a-z0-9][a-z0-9-]*$ — lowercase alphanumeric with hyphens)", i, d.Name))
				}
				if seenNames[d.Name] {
					problems = append(problems, fmt.Sprintf("trust.contentDigests[%d].name %q duplicates an earlier entry (two entries cannot bind the same asset)", i, d.Name))
				}
				seenNames[d.Name] = true
			}
			key := d.Algorithm + "\x00" + d.Encoding + "\x00" + d.Digest
			if seen[key] {
				problems = append(problems, fmt.Sprintf("trust.contentDigests[%d] duplicates an earlier digest entry (uniqueItems)", i))
			}
			seen[key] = true
		}
	}

	// ── attestation ─────────────────────────────────────────────────
	if doc.Trust.Attestation.Algorithm != AttestationAlgorithmEd25519 {
		problems = append(problems, fmt.Sprintf("trust.attestation.algorithm %q is not supported (only %q)", doc.Trust.Attestation.Algorithm, AttestationAlgorithmEd25519))
	}
	if doc.Trust.Attestation.Signature == "" {
		problems = append(problems, "trust.attestation.signature must not be empty (ADR-022 §3)")
	} else {
		sig, err := base64.StdEncoding.Strict().DecodeString(doc.Trust.Attestation.Signature)
		if err != nil {
			problems = append(problems, fmt.Sprintf("trust.attestation.signature is not strict RFC-4648 base64: %v", err))
		} else if len(sig) != ed25519.SignatureSize {
			problems = append(problems, fmt.Sprintf("trust.attestation.signature decodes to %d bytes, want %d (Ed25519)", len(sig), ed25519.SignatureSize))
		}
	}
	if doc.Trust.Attestation.PublicKey == "" {
		problems = append(problems, "trust.attestation.publicKey must not be empty (ADR-022 §3)")
	} else {
		pub, err := base64.StdEncoding.Strict().DecodeString(doc.Trust.Attestation.PublicKey)
		if err != nil {
			problems = append(problems, fmt.Sprintf("trust.attestation.publicKey is not strict RFC-4648 base64: %v", err))
		} else if len(pub) != ed25519.PublicKeySize {
			problems = append(problems, fmt.Sprintf("trust.attestation.publicKey decodes to %d bytes, want %d (Ed25519)", len(pub), ed25519.PublicKeySize))
		}
	}

	// ── skills (optional additive section, TS-021-04) ───────────────
	// The section is optional; when present it is strict: name
	// (^[a-z0-9][a-z0-9-]*$, ≤64), version (plain semver), asset (safe
	// asset name, ≤128, unique within the section) and covered by a
	// NAMED trust.contentDigests entry (the parser-enforced binding).
	if len(doc.Skills) > 0 {
		seenSkills := make(map[string]bool, len(doc.Skills))
		named := make(map[string]bool, len(doc.Trust.ContentDigests))
		for _, d := range doc.Trust.ContentDigests {
			if d.Name != "" {
				named[d.Name] = true
			}
		}
		for i, s := range doc.Skills {
			path := fmt.Sprintf("skills[%d]", i)
			if !reSkillName.MatchString(s.Name) || len(s.Name) > 64 {
				problems = append(problems, fmt.Sprintf("%s.name %q is not a safe skill name (^[a-z0-9][a-z0-9-]*$, max 64)", path, s.Name))
			}
			if seenSkills[s.Name] {
				problems = append(problems, fmt.Sprintf("%s.name %q duplicates an earlier skill (names are unique within one release)", path, s.Name))
			}
			seenSkills[s.Name] = true
			if !rePlainSemver.MatchString(s.Version) {
				problems = append(problems, fmt.Sprintf("%s.version %q is not plain semver", path, s.Version))
			}
			if !reAssetName.MatchString(s.Asset) || len(s.Asset) > 128 {
				problems = append(problems, fmt.Sprintf("%s.asset %q is not a safe asset identifier (^[a-z0-9][a-z0-9-]*$, max 128)", path, s.Asset))
			}
			if !named[s.Asset] {
				problems = append(problems, fmt.Sprintf("%s.asset %q is not covered by a named trust.contentDigests entry — every declared skill asset must be attestation-bound (registry-metadata.md §4.8)", path, s.Asset))
			}
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("derived registry metadata document is not strict-parser-compatible (%d problem%s):\n  - %s",
			len(problems), plural(len(problems)), strings.Join(problems, "\n  - "))
	}
	return nil
}

// ValidateVersionMatch asserts that the release version being signed equals
// the source manifest's declared version — the manifest `version` field is
// the standard's version line (ADR-021 §3.4), and a release must never
// declare a version the repository does not. Defense in depth alongside
// scripts/release.sh's tag assertion (TS-016-03-02 review finding).
func ValidateVersionMatch(declared, manifestVersion string) error {
	if declared != manifestVersion {
		return fmt.Errorf("release version %q does not match the source manifest version %q — the manifest `version` field is the version line; bump it first", declared, manifestVersion)
	}
	return nil
}

// checkHTTPSLocation enforces the distribution.location strict format: a
// well-formed absolute https URL with a host, no whitespace or control
// characters, no userinfo (mirrors Core parse.go checkHTTPSURL).
func checkHTTPSLocation(location string) error {
	for _, r := range location {
		if r < 0x20 || unicode.IsSpace(r) {
			return fmt.Errorf("must not contain whitespace or control characters — the location is a resolvable https URL, not free text")
		}
	}
	u, err := url.Parse(location)
	if err != nil || u.Scheme != "https" || u.Host == "" || !u.IsAbs() {
		return fmt.Errorf("must be a well-formed absolute https URL with a host — the scheme is pinned to https")
	}
	if u.User != nil {
		return fmt.Errorf("must not contain userinfo (username or password)")
	}
	return nil
}

// Schema-format patterns mirroring registry-metadata.schema.json.
var (
	rePlainSemver      = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	reFrameworkVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	reDigestBase16     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	// reAssetName is the optional contentDigest.name pattern
	// (TS-014-04-04): safe asset identifiers only.
	reAssetName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
)

// plural returns "s" for counts other than one.
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
