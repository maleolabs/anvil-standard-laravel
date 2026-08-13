package skillpack

import (
	"strings"
	"testing"
)

// ── Packer (TS-021-06): the vendored reference packer ───────────────

// testSkillsDir is the standard's authored skills content.
const testSkillsDir = "../../skills"

// TestPackStandard_AuthoringContent packs the COMMITTED authored skills
// (skills/) through the vendored packer and asserts the release shape:
// every declared skill becomes a bundle whose asset identifier is bound
// to a named content digest — the exact shape the release pipeline merges
// into the metadata document before signing.
func TestPackStandard_AuthoringContent(t *testing.T) {
	packed, err := PackStandard(testSkillsDir, "anvil-standard-laravel")
	if err != nil {
		t.Fatalf("pack the authored skills: %v", err)
	}
	if len(packed) < 2 {
		t.Fatalf("packed %d skills, want at least 2 (laravel-conventions, laravel-delivery)", len(packed))
	}

	names := map[string]bool{}
	for _, s := range packed {
		names[s.Name] = true
		if !strings.HasPrefix(s.AssetID, assetNamePrefix) {
			t.Errorf("asset %q does not start with %q", s.AssetID, assetNamePrefix)
		}
		if len(s.SHA256Hex) != 64 {
			t.Errorf("asset %s: digest %q is not 64 hex chars", s.AssetID, s.SHA256Hex)
		}
		if len(s.Bundle) == 0 {
			t.Errorf("asset %s: empty bundle", s.AssetID)
		}
	}
	for _, want := range []string{"laravel-conventions", "laravel-delivery"} {
		if !names[want] {
			t.Errorf("missing expected skill %q in the pack output", want)
		}
	}

	// The fragment: skills[] at the root, digests under trust, every
	// declared asset bound to a named digest (TS-021-04 binding).
	frag := BuildFragment(packed)
	if len(frag.Skills) != len(packed) {
		t.Fatalf("fragment skills = %d, want %d", len(frag.Skills), len(packed))
	}
	if len(frag.Trust.ContentDigests) != len(packed) {
		t.Fatalf("fragment digests = %d, want %d", len(frag.Trust.ContentDigests), len(packed))
	}
	binding := map[string]string{}
	for _, d := range frag.Trust.ContentDigests {
		if d.Name == "" {
			t.Fatalf("fragment digest without a name: %+v", d)
		}
		binding[d.Name] = d.Digest
	}
	for _, s := range frag.Skills {
		if binding[s.Asset] == "" {
			t.Errorf("skill %s: asset %s has no named digest binding", s.Name, s.Asset)
		}
	}
}

// TestPackStandard_Deterministic verifies the packer is byte-deterministic
// (the pinned headers/zeroed timestamps of the vendored bundle writer):
// packing the same content twice yields identical bundles and digests —
// the property that makes the release reproducible and the digest
// verifiable against the fixture-packed content.
func TestPackStandard_Deterministic(t *testing.T) {
	first, err := PackStandard(testSkillsDir, "anvil-standard-laravel")
	if err != nil {
		t.Fatalf("first pack: %v", err)
	}
	second, err := PackStandard(testSkillsDir, "anvil-standard-laravel")
	if err != nil {
		t.Fatalf("second pack: %v", err)
	}
	if len(first) != len(second) {
		t.Fatalf("pack sizes differ: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if string(first[i].Bundle) != string(second[i].Bundle) {
			t.Errorf("bundle %s is not deterministic", first[i].AssetID)
		}
		if first[i].SHA256Hex != second[i].SHA256Hex {
			t.Errorf("digest of %s is not deterministic", first[i].AssetID)
		}
	}
}

// TestAssetID verifies the safe asset identifier convention: dots in the
// version are normalized to hyphens (anvil-skill-<name>-<version>).
func TestAssetID(t *testing.T) {
	id, err := AssetID("laravel-conventions", "1.0.0")
	if err != nil {
		t.Fatalf("AssetID: %v", err)
	}
	if id != "anvil-skill-laravel-conventions-1-0-0" {
		t.Errorf("AssetID = %q, want anvil-skill-laravel-conventions-1-0-0", id)
	}
	if _, err := AssetID("Bad_Name", "1.0.0"); err == nil {
		t.Error("AssetID accepted an invalid name")
	}
	if _, err := AssetID("laravel-conventions", "01.0.0"); err == nil {
		t.Error("AssetID accepted a version with a leading zero")
	}
}

// TestSkillSpecsDocumentShape keeps the authored skills.json in sync with
// the packer's input shape (name/version/description per skill).
func TestSkillSpecsDocumentShape(t *testing.T) {
	specs, err := LoadSpecs(testSkillsDir)
	if err != nil {
		t.Fatalf("LoadSpecs: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("skills.json declares %d skills, want 2", len(specs))
	}
	for _, s := range specs {
		if strings.TrimSpace(s.Description) == "" {
			t.Errorf("skill %s has an empty description", s.Name)
		}
	}
}
