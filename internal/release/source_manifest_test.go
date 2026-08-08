package release

// Tests for the SOURCE manifest of the Laravel delivery lifecycle
// standard (manifest/registry-metadata.json): the authoring-time manifest
// fields (TS-018-01-03, 007 §4).
//
// scripts/validate-manifest.sh validates the manifest's FORMAT (JSON,
// schema URN, semver shapes); these tests pin the manifest's CONTENT —
// the declared identity, target contract version, and framework-version
// support scope — so the Manifest and Compatibility parts cannot
// silently drift from the machine-readable manifest. The release pipeline
// derives the publishable registry metadata document from this source
// manifest (DeriveDocument), so these declarations are what every release
// carries (007 §8).

import (
	"reflect"
	"strings"
	"testing"
)

// sourceManifestPath resolves the repository's source manifest from the
// package directory (go test runs with the working directory set to the
// package dir): internal/release -> ../../manifest/registry-metadata.json.
const sourceManifestPath = "../../manifest/registry-metadata.json"

// loadSourceManifest reads the repository's source manifest.
func loadSourceManifest(t *testing.T) *SourceManifest {
	t.Helper()
	src, err := ReadSourceManifest(sourceManifestPath)
	if err != nil {
		t.Fatalf("ReadSourceManifest(%s): %v", sourceManifestPath, err)
	}
	return src
}

// TestSourceManifest_FieldCompleteness pins the Manifest identity fields
// per 007 §4: standard identity (name), a well-formed version (the
// standard's own semver release line, ADR-021 §3.4), and the target
// contract version (the delivery lifecycle specification version the
// standard is valid against, ADR-024 §3.1 — the major is the
// compatibility unit).
//
// The contract version is pinned deliberately: moving it is a governed
// event (ADR-024 — a contract major bump is a Core-scale event) and must
// be a deliberate manifest + compatibility + test change, never silent.
func TestSourceManifest_FieldCompleteness(t *testing.T) {
	src := loadSourceManifest(t)

	if src.ID != "anvil-standard-laravel" {
		t.Errorf("id = %q, want the standard identity anvil-standard-laravel", src.ID)
	}
	if src.Version == "" {
		t.Error("version = empty, want the standard's semver version line")
	} else if !rePlainSemver.MatchString(src.Version) {
		t.Errorf("version = %q, want well-formed semver (the standard's version line)", src.Version)
	}
	if src.ContractVersion != "1.0.0" {
		t.Errorf("contractVersion = %q, want 1.0.0 (the delivery lifecycle specification version targeted, ADR-024 §3.1)", src.ContractVersion)
	}
}

// TestSourceManifest_DeclaresFrameworkSupportScope pins the declared
// framework-version support scope (007 §4, ADR-021 §3.2): Laravel
// 10.0.0, 11.0.0, and 12.0.0 — unique, well-formed, and exactly the
// scope the Compatibility part declares. A scope change is a content
// decision that must be deliberate across the manifest, the Compatibility
// part, and the verification/lifecycle content validated against it.
func TestSourceManifest_DeclaresFrameworkSupportScope(t *testing.T) {
	src := loadSourceManifest(t)

	want := []string{"10.0.0", "11.0.0", "12.0.0"}
	if !reflect.DeepEqual(src.Capability.FrameworkVersion, want) {
		t.Errorf("capability.frameworkVersion = %v, want %v", src.Capability.FrameworkVersion, want)
	}
}

// TestSourceManifest_DerivedDocumentPreservesDeclarations verifies that
// the release pipeline derives the publishable registry metadata document
// from the source manifest without altering the authoring-time
// declarations (TS-016-03-02, 007 §8): identity, target contract version,
// and the framework-version support scope carry over unchanged — only the
// release-time fields (version, distribution, lifecycle, trust) are
// populated by the release. The derived document must also pass the
// pipeline's self-parse guard.
//
// The trust material follows the TS-014-04-04 shape: the full
// attestation-bound digest set — the unnamed release-archive digest plus
// the named digest of a release binary asset (placeholder canonical
// base16 SHA-256 values, distinct per entry).
func TestSourceManifest_DerivedDocumentPreservesDeclarations(t *testing.T) {
	src := loadSourceManifest(t)

	contentDigests := []ContentDigest{
		{ // the release archive digest (unnamed, ADR-022 §3)
			Algorithm: DigestAlgorithmSHA256,
			Encoding:  DigestEncodingBase16,
			Digest:    strings.Repeat("a", 64),
		},
		{ // a named binary asset digest (TS-014-04-04)
			Algorithm: DigestAlgorithmSHA256,
			Encoding:  DigestEncodingBase16,
			Digest:    strings.Repeat("b", 64),
			Name:      "anvil-adapter-laravel-linux-amd64",
		},
	}

	doc := DeriveDocument(src, src.Version,
		"https://github.com/maleolabs/anvil-standard-laravel/releases/download/v"+src.Version+"/anvil-standard-laravel-"+src.Version+".tar.gz",
		contentDigests,
		strings.Repeat("A", 86)+"==",
		strings.Repeat("A", 43)+"=",
	)

	if doc.ID != src.ID {
		t.Errorf("derived id = %q, want the source identity %q", doc.ID, src.ID)
	}
	if doc.ContractVersion != src.ContractVersion {
		t.Errorf("derived contractVersion = %q, want the source declaration %q", doc.ContractVersion, src.ContractVersion)
	}
	if !reflect.DeepEqual(doc.Capability.FrameworkVersion, src.Capability.FrameworkVersion) {
		t.Errorf("derived capability.frameworkVersion = %v, want the source scope %v", doc.Capability.FrameworkVersion, src.Capability.FrameworkVersion)
	}
	if doc.Version != src.Version {
		t.Errorf("derived version = %q, want the source manifest version %q (the version line)", doc.Version, src.Version)
	}
	if !reflect.DeepEqual(doc.Trust.ContentDigests, contentDigests) {
		t.Errorf("derived contentDigests = %v, want the declared digest set %v (archive + named binary, TS-014-04-04)", doc.Trust.ContentDigests, contentDigests)
	}
	if err := ValidateDocumentShape(doc); err != nil {
		t.Errorf("derived document rejected by the self-parse guard: %v", err)
	}
}
