package release

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── Skills fragment merge (TS-021-06) ───────────────────────────────

// skillsTestFragment builds a valid fragment declaring two skills with
// their named digest entries (the shape cmd/skillpack emits).
func skillsTestFragment(t *testing.T) *SkillsFragment {
	t.Helper()
	return &SkillsFragment{
		Skills: []Skill{
			{Name: "laravel-conventions", Version: "1.0.0", Asset: "anvil-skill-laravel-conventions-1-0-0", Description: "conventions"},
			{Name: "laravel-delivery", Version: "1.0.0", Asset: "anvil-skill-laravel-delivery-1-0-0", Description: "delivery"},
		},
		Trust: SkillsTrustFragment{
			ContentDigests: []ContentDigest{
				{Algorithm: DigestAlgorithmSHA256, Encoding: DigestEncodingBase16, Digest: strings.Repeat("a", 64), Name: "anvil-skill-laravel-conventions-1-0-0"},
				{Algorithm: DigestAlgorithmSHA256, Encoding: DigestEncodingBase16, Digest: strings.Repeat("b", 64), Name: "anvil-skill-laravel-delivery-1-0-0"},
			},
		},
	}
}

// skillsTestDoc builds a derived-style document carrying the archive
// digest and one named binary digest.
func skillsTestDoc() *MetadataDocument {
	return &MetadataDocument{
		ID:              "anvil-standard-laravel",
		Version:         "1.1.1",
		ContractVersion: "1.0.0",
		Capability:      Capability{FrameworkVersion: []string{"10.0.0"}},
		Distribution:    Distribution{Type: DistributionTypeGitHubReleases, Location: "https://github.com/maleolabs/anvil-standard-laravel/releases/download/v1.1.1/anvil-standard-laravel-1.1.1.tar.gz"},
		Lifecycle:       Lifecycle{State: LifecycleStatePublished},
		Trust: Trust{
			ContentDigests: []ContentDigest{
				{Algorithm: DigestAlgorithmSHA256, Encoding: DigestEncodingBase16, Digest: strings.Repeat("c", 64)},
				{Algorithm: DigestAlgorithmSHA256, Encoding: DigestEncodingBase16, Digest: strings.Repeat("d", 64), Name: "anvil-adapter-laravel-linux-amd64"},
			},
			Attestation: Attestation{
				Algorithm: AttestationAlgorithmEd25519,
				// 64-byte signature + 32-byte key, base64 (RFC-4648).
				Signature: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA==",
				PublicKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
			},
		},
	}
}

// TestMergeSkillsFragment_AppendsInOrder verifies the merge appends the
// fragment's named digests AFTER the existing content/binary digests
// (deterministic array order — the attestation payload concatenates the
// digests in array order, so the order is signed material) and sets the
// skills[] declarations.
func TestMergeSkillsFragment_AppendsInOrder(t *testing.T) {
	doc := skillsTestDoc()
	frag := skillsTestFragment(t)
	if err := MergeSkillsFragment(doc, frag); err != nil {
		t.Fatalf("MergeSkillsFragment: %v", err)
	}
	if len(doc.Skills) != 2 {
		t.Fatalf("doc.Skills = %d, want 2", len(doc.Skills))
	}
	wantOrder := []string{"", "anvil-adapter-laravel-linux-amd64", "anvil-skill-laravel-conventions-1-0-0", "anvil-skill-laravel-delivery-1-0-0"}
	if len(doc.Trust.ContentDigests) != len(wantOrder) {
		t.Fatalf("contentDigests = %d entries, want %d", len(doc.Trust.ContentDigests), len(wantOrder))
	}
	for i, name := range wantOrder {
		if doc.Trust.ContentDigests[i].Name != name {
			t.Errorf("contentDigests[%d].name = %q, want %q", i, doc.Trust.ContentDigests[i].Name, name)
		}
	}
	// Every declared skill asset is bound to a named digest.
	if err := ValidateDocumentShape(doc); err != nil {
		t.Fatalf("merged document fails the self-parse guard: %v", err)
	}
}

// TestMergeSkillsFragment_RejectsUnboundAsset verifies the parser-enforced
// rule: a declared skill asset without a fragment digest fails the merge.
func TestMergeSkillsFragment_RejectsUnboundAsset(t *testing.T) {
	doc := skillsTestDoc()
	frag := skillsTestFragment(t)
	frag.Trust.ContentDigests = frag.Trust.ContentDigests[:1] // drop the second asset's digest
	if err := MergeSkillsFragment(doc, frag); err == nil {
		t.Fatal("merge succeeded with an unbound skill asset, want an error")
	}
}

// TestMergeSkillsFragment_RejectsDuplicateNames verifies two digest
// entries cannot bind the same asset (uniqueItems).
func TestMergeSkillsFragment_RejectsDuplicateNames(t *testing.T) {
	doc := skillsTestDoc()
	frag := skillsTestFragment(t)
	frag.Trust.ContentDigests[1].Name = frag.Trust.ContentDigests[0].Name
	if err := MergeSkillsFragment(doc, frag); err == nil {
		t.Fatal("merge succeeded with a duplicate asset binding, want an error")
	}
}

// TestMergeSkillsFragment_RejectsEmpty verifies a fragment declaring no
// skills fails the merge — a release that runs the pack step must pack at
// least one skill.
func TestMergeSkillsFragment_RejectsEmpty(t *testing.T) {
	doc := skillsTestDoc()
	frag := &SkillsFragment{}
	if err := MergeSkillsFragment(doc, frag); err == nil {
		t.Fatal("merge succeeded with an empty fragment, want an error")
	}
}

// TestValidateDocumentShape_Skills verifies the self-parse guard accepts a
// fully-bound skills section and rejects the unbound-asset shape.
func TestValidateDocumentShape_Skills(t *testing.T) {
	doc := skillsTestDoc()
	if err := MergeSkillsFragment(doc, skillsTestFragment(t)); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if err := ValidateDocumentShape(doc); err != nil {
		t.Fatalf("valid document rejected: %v", err)
	}
	// Unbind one asset: the guard must reject it (the strict parser does).
	doc.Trust.ContentDigests = doc.Trust.ContentDigests[:len(doc.Trust.ContentDigests)-1]
	if err := ValidateDocumentShape(doc); err == nil {
		t.Fatal("document with an unbound skill asset passed the self-parse guard")
	}
}

// ── Skills asset verification (TS-021-06) ───────────────────────────

// skillsTestAssetsDir writes the packed skill assets of a doc into a temp
// dir (file content = a deterministic byte pattern; the digests must
// match the doc's declared values).
func skillsTestAssetsDir(t *testing.T, doc *MetadataDocument) string {
	t.Helper()
	dir := t.TempDir()
	for _, d := range doc.Trust.ContentDigests {
		if d.Name == "" || !IsSkillAssetName(d.Name) {
			continue
		}
		data := []byte("skill content of " + d.Name)
		if err := os.WriteFile(filepath.Join(dir, d.Name), data, 0o644); err != nil {
			t.Fatal(err)
		}
		// Rewrite the declared digest to the recomputed one so the
		// two-way verification passes with real material.
		d.Digest = SHA256Base16(data)
		for i := range doc.Trust.ContentDigests {
			if doc.Trust.ContentDigests[i].Name == d.Name {
				doc.Trust.ContentDigests[i].Digest = d.Digest
			}
		}
	}
	return dir
}

func TestVerifySkillAssetDigests_OK(t *testing.T) {
	doc := skillsTestDoc()
	if err := MergeSkillsFragment(doc, skillsTestFragment(t)); err != nil {
		t.Fatalf("merge: %v", err)
	}
	dir := skillsTestAssetsDir(t, doc)
	if err := VerifySkillAssetDigests(doc, dir); err != nil {
		t.Fatalf("VerifySkillAssetDigests: %v", err)
	}
}

func TestVerifySkillAssetDigests_Tampered(t *testing.T) {
	doc := skillsTestDoc()
	if err := MergeSkillsFragment(doc, skillsTestFragment(t)); err != nil {
		t.Fatalf("merge: %v", err)
	}
	dir := skillsTestAssetsDir(t, doc)
	// Tamper with one asset: verification must fail (mismatch aborts).
	name := "anvil-skill-laravel-conventions-1-0-0"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifySkillAssetDigests(doc, dir); err == nil {
		t.Fatal("verification passed with a tampered skill asset")
	}
}

func TestVerifySkillAssetDigests_MissingAsset(t *testing.T) {
	doc := skillsTestDoc()
	if err := MergeSkillsFragment(doc, skillsTestFragment(t)); err != nil {
		t.Fatalf("merge: %v", err)
	}
	dir := skillsTestAssetsDir(t, doc)
	if err := os.Remove(filepath.Join(dir, "anvil-skill-laravel-delivery-1-0-0")); err != nil {
		t.Fatal(err)
	}
	if err := VerifySkillAssetDigests(doc, dir); err == nil {
		t.Fatal("verification passed with a missing declared skill asset")
	}
}

// TestReadSkillsFragment verifies the fragment file round-trip (the file
// cmd/skillpack emits).
func TestReadSkillsFragment(t *testing.T) {
	frag := skillsTestFragment(t)
	path := filepath.Join(t.TempDir(), "skills-metadata.json")
	raw, err := json.MarshalIndent(frag, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadSkillsFragment(path)
	if err != nil {
		t.Fatalf("ReadSkillsFragment: %v", err)
	}
	if len(got.Skills) != 2 || len(got.Trust.ContentDigests) != 2 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}
