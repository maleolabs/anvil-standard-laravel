package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── Self-parse guard (ValidateDocumentShape) ──────────────────────

// validDocument builds a shape-valid derived document for the tests.
func validDocument(t *testing.T) *MetadataDocument {
	t.Helper()
	return &MetadataDocument{
		Schema:          SchemaURN,
		Title:           "Laravel Delivery Lifecycle Standard",
		ID:              "anvil-standard-laravel",
		Version:         "1.0.0",
		ContractVersion: "1.0.0",
		Capability:      Capability{FrameworkVersion: []string{"10.0.0", "11.0.0", "12.0.0"}},
		Distribution: Distribution{
			Type:     DistributionTypeGitHubReleases,
			Location: "https://github.com/maleolabs/anvil-standard-laravel/releases/download/v1.0.0/anvil-standard-laravel-1.0.0.tar.gz",
		},
		Lifecycle: Lifecycle{State: LifecycleStatePublished},
		Trust: Trust{
			ContentDigests: []ContentDigest{{
				Algorithm: DigestAlgorithmSHA256,
				Encoding:  DigestEncodingBase16,
				Digest:    strings.Repeat("a", 64),
			}},
			Attestation: Attestation{
				Algorithm: AttestationAlgorithmEd25519,
				Signature: strings.Repeat("A", 86) + "==", // 64 zero bytes, base64
				PublicKey: strings.Repeat("A", 43) + "=",  // 32 zero bytes, base64
			},
		},
	}
}

func TestValidateDocumentShape_ValidDocument(t *testing.T) {
	if err := ValidateDocumentShape(validDocument(t)); err != nil {
		t.Fatalf("valid document rejected: %v", err)
	}
}

func TestValidateDocumentShape_MissingRequiredFields(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*MetadataDocument)
		want   string
	}{
		{"missing-id", func(d *MetadataDocument) { d.ID = "" }, "'id'"},
		{"missing-version", func(d *MetadataDocument) { d.Version = "" }, "'version'"},
		{"missing-contractVersion", func(d *MetadataDocument) { d.ContractVersion = "" }, "'contractVersion'"},
		{"empty-capability", func(d *MetadataDocument) { d.Capability.FrameworkVersion = nil }, "capability.frameworkVersion"},
		{"missing-distribution-type", func(d *MetadataDocument) { d.Distribution.Type = "" }, "distribution.type"},
		{"missing-distribution-location", func(d *MetadataDocument) { d.Distribution.Location = "" }, "distribution.location"},
		{"missing-lifecycle-state", func(d *MetadataDocument) { d.Lifecycle.State = "" }, "lifecycle.state"},
		{"missing-contentDigests", func(d *MetadataDocument) { d.Trust.ContentDigests = nil }, "contentDigests"},
		{"missing-signature", func(d *MetadataDocument) { d.Trust.Attestation.Signature = "" }, "signature"},
		{"missing-publicKey", func(d *MetadataDocument) { d.Trust.Attestation.PublicKey = "" }, "publicKey"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := validDocument(t)
			tc.mutate(doc)
			err := ValidateDocumentShape(doc)
			if err == nil {
				t.Fatalf("expected rejection, document passed")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("rejection does not mention %q: %v", tc.want, err)
			}
		})
	}
}

func TestValidateDocumentShape_RejectsInvalidValues(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*MetadataDocument)
		want   string
	}{
		{"version-not-semver", func(d *MetadataDocument) { d.Version = "1.0" }, "version"},
		{"version-leading-zero", func(d *MetadataDocument) { d.Version = "01.0.0" }, "version"},
		{"contractVersion-not-semver", func(d *MetadataDocument) { d.ContractVersion = "1.0.0-rc1" }, "contractVersion"},
		{"framework-not-semver", func(d *MetadataDocument) { d.Capability.FrameworkVersion = []string{"10"} }, "frameworkVersion"},
		{"framework-duplicate", func(d *MetadataDocument) { d.Capability.FrameworkVersion = []string{"10.0.0", "10.0.0"} }, "duplicate"},
		{"distribution-type-wrong", func(d *MetadataDocument) { d.Distribution.Type = "http" }, "distribution.type"},
		{"location-not-https", func(d *MetadataDocument) { d.Distribution.Location = "http://example.com/a.tar.gz" }, "https"},
		{"location-with-whitespace", func(d *MetadataDocument) { d.Distribution.Location = "https://example.com/a b.tar.gz" }, "whitespace"},
		{"location-with-userinfo", func(d *MetadataDocument) { d.Distribution.Location = "https://user:pass@example.com/a.tar.gz" }, "userinfo"},
		{"lifecycle-state-wrong", func(d *MetadataDocument) { d.Lifecycle.State = "published!" }, "lifecycle.state"},
		{"digest-algorithm-wrong", func(d *MetadataDocument) { d.Trust.ContentDigests[0].Algorithm = "md5" }, "algorithm"},
		{"digest-uppercase", func(d *MetadataDocument) { d.Trust.ContentDigests[0].Digest = strings.ToUpper(strings.Repeat("a", 64)) }, "base16"},
		{"digest-too-short", func(d *MetadataDocument) { d.Trust.ContentDigests[0].Digest = strings.Repeat("a", 63) }, "base16"},
		{"attestation-algorithm-wrong", func(d *MetadataDocument) { d.Trust.Attestation.Algorithm = "rsa" }, "algorithm"},
		{"signature-not-base64", func(d *MetadataDocument) { d.Trust.Attestation.Signature = "!!!not-base64!!!" }, "base64"},
		{"publicKey-wrong-length", func(d *MetadataDocument) { d.Trust.Attestation.PublicKey = strings.Repeat("A", 86) + "==" }, "bytes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := validDocument(t)
			tc.mutate(doc)
			err := ValidateDocumentShape(doc)
			if err == nil {
				t.Fatalf("expected rejection, document passed")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("rejection does not mention %q: %v", tc.want, err)
			}
		})
	}
}

func TestValidateDocumentShape_RejectsUnknownDigestEncoding(t *testing.T) {
	doc := validDocument(t)
	doc.Trust.ContentDigests[0].Encoding = "base64"
	doc.Trust.ContentDigests[0].Digest = "K7gNU3sdo+OL0wNhqoVWhr3g6s1xYv72ol/pe/Unols="
	err := ValidateDocumentShape(doc)
	if err == nil || !strings.Contains(err.Error(), "base16") {
		t.Fatalf("the pipeline must reject encodings it does not produce: %v", err)
	}
}

func TestVerifyDocument_RunsShapeGuardFirst(t *testing.T) {
	// The self-parse guard is part of VerifyDocument: a shape-invalid
	// document fails verification even when content and signature are
	// consistent.
	doc := validDocument(t)
	doc.Lifecycle.State = "bogus"
	if err := VerifyDocument(doc, []byte("any content")); err == nil {
		t.Fatal("VerifyDocument must reject a shape-invalid document")
	}
}

// ── Version-line assertion (ValidateVersionMatch) ─────────────────

func TestValidateVersionMatch(t *testing.T) {
	if err := ValidateVersionMatch("1.0.0", "1.0.0"); err != nil {
		t.Fatalf("matching versions rejected: %v", err)
	}
	err := ValidateVersionMatch("1.0.1", "1.0.0")
	if err == nil || !strings.Contains(err.Error(), "1.0.0") {
		t.Fatalf("mismatch must be rejected and name the manifest version: %v", err)
	}
}

// ── End-to-end document roundtrip through the self-parse guard ────

func TestSignVerifyRoundtrip_WithShapeGuard(t *testing.T) {
	dir := t.TempDir()
	content := []byte("release archive bytes")
	archive := filepath.Join(dir, "archive.tar.gz")
	if err := os.WriteFile(archive, content, 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	privPath := filepath.Join(dir, "key.pem")
	pubPath := filepath.Join(dir, "key.pub.pem")
	pubB64, err := GenerateKeyPair(privPath, pubPath)
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	priv, err := LoadPrivateKey(privPath)
	if err != nil {
		t.Fatalf("LoadPrivateKey: %v", err)
	}

	digest := SHA256Base16(content)
	payload, err := AttestationPayload("anvil-standard-laravel", "1.0.0", []ContentDigest{{
		Algorithm: DigestAlgorithmSHA256, Encoding: DigestEncodingBase16, Digest: digest,
	}})
	if err != nil {
		t.Fatalf("AttestationPayload: %v", err)
	}
	doc := DeriveDocument(
		&SourceManifest{ID: "anvil-standard-laravel", Version: "1.0.0", ContractVersion: "1.0.0", Capability: Capability{FrameworkVersion: []string{"10.0.0", "11.0.0", "12.0.0"}}},
		"1.0.0",
		"https://github.com/maleolabs/anvil-standard-laravel/releases/download/v1.0.0/anvil-standard-laravel-1.0.0.tar.gz",
		digest, SignAttestation(payload, priv), pubB64,
	)

	// Shape guard passes on a properly derived document…
	if err := ValidateDocumentShape(doc); err != nil {
		t.Fatalf("derived document failed the self-parse guard: %v", err)
	}
	// …and VerifyDocument (shape + integrity + attestation) passes too.
	if err := VerifyDocument(doc, content); err != nil {
		t.Fatalf("VerifyDocument: %v", err)
	}
}
