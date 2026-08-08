// Package tests validates the seven-part structure of this delivery
// lifecycle standard (ADR-021 §3.2, Transition Plan §5.4) and the
// acceptance-readiness bars of ADR-027 §3: structure (all seven parts
// present), conformance (the declared contract version is well-formed,
// consistently declared across the Manifest and Compatibility parts, and
// the machine manifest uses the registry metadata format it declares),
// and the manifest surface the registry validates at acceptance.
//
// These tests concern the standard itself — the Tests part — not an
// adopting project's release (project-facing checks are the Verification
// part).
package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// repoRoot resolves the standard repository root (the directory
// containing go.mod) from the test package location.
func repoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above test package directory")
		}
		dir = parent
	}
}

// semverPattern is the registry metadata semver pattern
// (registry-metadata.schema.json): major.minor.patch, no leading zeroes.
var semverPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// registryMetadataSchemaURN is the registry metadata format the manifest
// declares via "$schema" (registry-metadata.schema.json); the contract
// version of the registry metadata format itself is 1.0.0.
const registryMetadataSchemaURN = "urn:anvil:spec:registry-metadata:1.0.0"

// standardID is this standard's identity (registry-metadata.json `id`).
const standardID = "anvil-standard-laravel"

// manifest mirrors the authoring-time fields of the registry metadata
// document (registry-metadata.schema.json conventions; Core
// internal/registry/metadata.go). Release-time fields (distribution,
// lifecycle, trust) are populated by the release pipeline and are not
// part of the source manifest's acceptance-relevant surface.
type manifest struct {
	Schema          string `json:"$schema"`
	ID              string `json:"id"`
	Version         string `json:"version"`
	ContractVersion string `json:"contractVersion"`
	Capability      struct {
		FrameworkVersion []string `json:"frameworkVersion"`
	} `json:"capability"`
}

// readManifest reads and parses the source manifest
// (manifest/registry-metadata.json).
func readManifest(t *testing.T) manifest {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(repoRoot(t), "manifest", "registry-metadata.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("manifest is not valid JSON: %v", err)
	}
	return m
}

// TestSevenPartStructureExists verifies that the repository carries the
// seven-part standard structure (ADR-021 §3.2, Transition Plan §5.4):
// Manifest, Lifecycle Definition, Verification, Templates, Compatibility,
// Documentation, Tests. This is the ADR-027 §3 acceptance bar
// "Structure — all seven parts of the standard structure are present".
func TestSevenPartStructureExists(t *testing.T) {
	root := repoRoot(t)

	parts := map[string]string{
		"Manifest":             filepath.Join("manifest", "registry-metadata.json"),
		"Lifecycle Definition": filepath.Join("lifecycle", "definition.md"),
		"Verification":         filepath.Join("verification", "checks.md"),
		"Templates":            filepath.Join("templates", "README.md"),
		"Compatibility":        filepath.Join("compatibility", "compatibility.md"),
		"Documentation":        filepath.Join("docs", "README.md"),
		"Tests":                filepath.Join("tests", "README.md"),
	}
	for part, rel := range parts {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("part %q missing: %s (%v)", part, rel, err)
		}
	}
}

// TestManifestDeclaresIdentity verifies the source manifest declares the
// standard identity per the registry metadata format: schema, id,
// version, contract version, and framework-version support scope — all
// semver where the format requires semver, and the registry metadata
// schema URN is the declared format version. This is the manifest
// surface the registry validates at acceptance (ADR-023, EPIC-014).
func TestManifestDeclaresIdentity(t *testing.T) {
	m := readManifest(t)

	if m.Schema != registryMetadataSchemaURN {
		t.Errorf("$schema = %q, want %q", m.Schema, registryMetadataSchemaURN)
	}
	if m.ID != standardID {
		t.Errorf("id = %q, want %q", m.ID, standardID)
	}
	if !semverPattern.MatchString(m.Version) {
		t.Errorf("version = %q, want semver", m.Version)
	}
	if !semverPattern.MatchString(m.ContractVersion) {
		t.Errorf("contractVersion = %q, want semver", m.ContractVersion)
	}
	if len(m.Capability.FrameworkVersion) == 0 {
		t.Fatal("capability.frameworkVersion is empty, want at least one supported framework version")
	}
	for _, v := range m.Capability.FrameworkVersion {
		if !semverPattern.MatchString(v) {
			t.Errorf("capability.frameworkVersion entry %q is not semver", v)
		}
	}
}

// TestContractVersionConformanceTarget verifies the declared contract
// version is a valid conformance target per ADR-024 §3.1: well-formed
// semver with major >= 1 (the contract major is the compatibility unit).
// The standard is validated against this declared version at registry
// acceptance (ADR-027 §3 "Conformance"); this test pins the declaration
// so the conformance target cannot drift silently.
func TestContractVersionConformanceTarget(t *testing.T) {
	m := readManifest(t)

	major := m.ContractVersion[:strings.Index(m.ContractVersion, ".")]
	if major == "" || major == "0" {
		t.Errorf("contractVersion = %q, want semver with major >= 1 (contract majors start at 1, ADR-024 §3.1)", m.ContractVersion)
	}
}

// TestManifestContractVersionMatchesCompatibility verifies the manifest's
// declared contract version agrees with the Compatibility part — the two
// parts of the standard must not drift. Compatibility with the runtime is
// negotiated at adoption from the declared contract version (ADR-021
// §3.4, 007 §8); a drift between the declaration and the compatibility
// documentation would break that negotiation.
func TestManifestContractVersionMatchesCompatibility(t *testing.T) {
	m := readManifest(t)

	compat, err := os.ReadFile(filepath.Join(repoRoot(t), "compatibility", "compatibility.md"))
	if err != nil {
		t.Fatalf("read compatibility declaration: %v", err)
	}
	if !regexp.MustCompile(`\Q` + m.ContractVersion + `\E`).Match(compat) {
		t.Errorf("compatibility part does not reference the manifest's declared contract version %q", m.ContractVersion)
	}
}

// TestManifestHumanFormAgreesWithMachineForm verifies the human-readable
// Manifest part (MANIFEST.md) agrees with the machine-readable manifest
// (manifest/registry-metadata.json) on identity, target contract version,
// and framework-version support scope. The human form is the adopters'
// reference; a drift between the two forms would mislead adopters about
// the declaration the registry validates.
func TestManifestHumanFormAgreesWithMachineForm(t *testing.T) {
	m := readManifest(t)

	human, err := os.ReadFile(filepath.Join(repoRoot(t), "MANIFEST.md"))
	if err != nil {
		t.Fatalf("read MANIFEST.md: %v", err)
	}
	text := string(human)

	if !strings.Contains(text, "`"+m.ID+"`") {
		t.Errorf("MANIFEST.md does not declare the standard id %q", m.ID)
	}
	if !strings.Contains(text, m.ContractVersion) {
		t.Errorf("MANIFEST.md does not declare the target contract version %q", m.ContractVersion)
	}
	for _, v := range m.Capability.FrameworkVersion {
		if !strings.Contains(text, "`"+v+"`") {
			t.Errorf("MANIFEST.md does not declare supported framework version %q", v)
		}
	}
}
