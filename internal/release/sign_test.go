package release

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── Attestation payload composition ─────────────────────────────────

func TestAttestationPayload_Composition(t *testing.T) {
	// The canonical payload is byte-for-byte:
	//   utf8(id) || 0x00 || utf8(version) || 0x00 || decoded digest bytes
	// (Core internal/registry/trust.go: attestationPayload).
	// id = "anvil-standard-laravel", version = "1.0.0", digest =
	// 64 x '0' (hex of 32 zero bytes).
	digest := strings.Repeat("0", 64)
	payload, err := AttestationPayload("anvil-standard-laravel", "1.0.0", []ContentDigest{{
		Algorithm: DigestAlgorithmSHA256,
		Encoding:  DigestEncodingBase16,
		Digest:    digest,
	}})
	if err != nil {
		t.Fatalf("AttestationPayload: %v", err)
	}

	want := []byte("anvil-standard-laravel")
	want = append(want, 0x00)
	want = append(want, "1.0.0"...)
	want = append(want, 0x00)
	want = append(want, bytes.Repeat([]byte{0x00}, 32)...)

	if !bytes.Equal(payload, want) {
		t.Fatalf("payload composition mismatch:\n got %x\nwant %x", payload, want)
	}
	if len(payload) != len("anvil-standard-laravel")+1+len("1.0.0")+1+32 {
		t.Fatalf("unexpected payload length %d", len(payload))
	}
}

func TestAttestationPayload_MultipleDigestsInOrder(t *testing.T) {
	// Two digests (two encodings of different content) must be concatenated
	// in array order: the signature binds the array order.
	d1 := strings.Repeat("0", 64) // 32 x 0x00
	d2 := strings.Repeat("1", 64) // 32 x 0x11
	payload, err := AttestationPayload("id", "2.0.0", []ContentDigest{
		{Algorithm: DigestAlgorithmSHA256, Encoding: DigestEncodingBase16, Digest: d1},
		{Algorithm: DigestAlgorithmSHA256, Encoding: DigestEncodingBase16, Digest: d2},
	})
	if err != nil {
		t.Fatalf("AttestationPayload: %v", err)
	}
	if !bytes.Equal(payload[9:], append(bytes.Repeat([]byte{0x00}, 32), bytes.Repeat([]byte{0x11}, 32)...)) {
		t.Fatalf("digest concatenation is not in array order: %x", payload[9:])
	}

	reversed, err := AttestationPayload("id", "2.0.0", []ContentDigest{
		{Algorithm: DigestAlgorithmSHA256, Encoding: DigestEncodingBase16, Digest: d2},
		{Algorithm: DigestAlgorithmSHA256, Encoding: DigestEncodingBase16, Digest: d1},
	})
	if err != nil {
		t.Fatalf("AttestationPayload (reversed): %v", err)
	}
	if bytes.Equal(payload, reversed) {
		t.Fatal("reordered digests must compose a different payload")
	}
}

func TestAttestationPayload_RejectsNULInClaims(t *testing.T) {
	if _, err := AttestationPayload("id\x00x", "1.0.0", nil); err == nil {
		t.Fatal("NUL inside id must be rejected (separator ambiguity)")
	}
	if _, err := AttestationPayload("id", "1.0.0\x00", nil); err == nil {
		t.Fatal("NUL inside version must be rejected (separator ambiguity)")
	}
}

func TestAttestationPayload_RejectsNonCanonicalDigest(t *testing.T) {
	_, err := AttestationPayload("id", "1.0.0", []ContentDigest{{
		Algorithm: DigestAlgorithmSHA256,
		Encoding:  DigestEncodingBase16,
		Digest:    strings.ToUpper(strings.Repeat("a", 64)), // uppercase: not canonical
	}})
	if err == nil {
		t.Fatal("non-canonical (uppercase) base16 digest must be rejected")
	}
	_, err = AttestationPayload("id", "1.0.0", []ContentDigest{{
		Algorithm: "md5",
		Encoding:  DigestEncodingBase16,
		Digest:    strings.Repeat("a", 64),
	}})
	if err == nil {
		t.Fatal("unsupported digest algorithm must be rejected")
	}
}

// ── Sign / verify roundtrip ────────────────────────────────────────

func TestSignVerifyRoundtrip(t *testing.T) {
	dir := t.TempDir()
	privPath := filepath.Join(dir, "key.pem")
	pubPath := filepath.Join(dir, "key.pub.pem")
	pubB64, err := GenerateKeyPair(privPath, pubPath)
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	if _, err := os.Stat(privPath); err != nil {
		t.Fatalf("private key file not written: %v", err)
	}
	priv, err := LoadPrivateKey(privPath)
	if err != nil {
		t.Fatalf("LoadPrivateKey: %v", err)
	}
	if PublicKeyBase64(priv) != pubB64 {
		t.Fatal("public key file and derived key disagree")
	}

	digest := strings.Repeat("c", 64)
	payload, err := AttestationPayload("anvil-standard-laravel", "1.0.0", []ContentDigest{{
		Algorithm: DigestAlgorithmSHA256, Encoding: DigestEncodingBase16, Digest: digest,
	}})
	if err != nil {
		t.Fatalf("AttestationPayload: %v", err)
	}
	sig := SignAttestation(payload, priv)
	if _, err := base64.StdEncoding.DecodeString(sig); err != nil {
		t.Fatalf("signature is not base64: %v", err)
	}
	if len(mustDecode(t, sig)) != 64 {
		t.Fatalf("signature must be 64 bytes, got %d", len(mustDecode(t, sig)))
	}

	doc := DeriveDocument(
		&SourceManifest{ID: "anvil-standard-laravel", Version: "1.0.0", ContractVersion: "1.0.0"},
		"1.0.0",
		"https://github.com/maleolabs/anvil-standard-laravel/releases/download/v1.0.0/anvil-standard-laravel-1.0.0.tar.gz",
		digest, sig, pubB64,
	)
	if err := VerifyAttestation(doc); err != nil {
		t.Fatalf("VerifyAttestation: %v", err)
	}

	// Tampering with any claim must invalidate the attestation.
	tampered := *doc
	tampered.Version = "1.0.1"
	if err := VerifyAttestation(&tampered); err == nil {
		t.Fatal("attestation must fail when version is tampered")
	}
	tampered2 := *doc
	tampered2.Trust.ContentDigests = []ContentDigest{{
		Algorithm: DigestAlgorithmSHA256, Encoding: DigestEncodingBase16, Digest: strings.Repeat("d", 64),
	}}
	if err := VerifyAttestation(&tampered2); err == nil {
		t.Fatal("attestation must fail when the digest is tampered")
	}
}

func TestVerifyDocument_IntegrityAndAttestation(t *testing.T) {
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
		&SourceManifest{ID: "anvil-standard-laravel", Version: "1.0.0", ContractVersion: "1.0.0"},
		"1.0.0",
		"https://github.com/maleolabs/anvil-standard-laravel/releases/download/v1.0.0/anvil-standard-laravel-1.0.0.tar.gz",
		digest, SignAttestation(payload, priv), pubB64,
	)

	if err := VerifyDocument(doc, content); err != nil {
		t.Fatalf("VerifyDocument: %v", err)
	}

	// Content tamper: the digest no longer matches.
	if err := VerifyDocument(doc, append(content, []byte("tamper")...)); err == nil {
		t.Fatal("VerifyDocument must fail when content does not match the declared digest")
	}
}

// ── Document derivation ────────────────────────────────────────────

func TestDeriveDocument_ReplacesPlaceholders(t *testing.T) {
	// The Laravel source manifest carries format-valid placeholder trust
	// (zero digests, dummy attestation) — the derived document must carry
	// real values (TS-016-03-02; the release pipeline replaces the
	// placeholders, never ships them).
	src := &SourceManifest{
		Schema:          SchemaURN,
		Title:           "Laravel Delivery Lifecycle Standard",
		ID:              "anvil-standard-laravel",
		Version:         "1.0.0",
		ContractVersion: "1.0.0",
		Capability:      Capability{FrameworkVersion: []string{"10.0.0", "11.0.0", "12.0.0"}},
		Distribution: &Distribution{
			Type:     DistributionTypeGitHubReleases,
			Location: "https://github.com/maleolabs/anvil-standard-laravel/releases",
		},
		Lifecycle: &Lifecycle{State: LifecycleStatePublished},
		Trust: &Trust{
			ContentDigests: []ContentDigest{{
				Algorithm: DigestAlgorithmSHA256,
				Encoding:  DigestEncodingBase16,
				Digest:    strings.Repeat("0", 64),
			}},
			Attestation: Attestation{
				Algorithm: AttestationAlgorithmEd25519,
				Signature: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA==",
				PublicKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
			},
		},
	}
	doc := DeriveDocument(
		src,
		"1.0.0",
		"https://github.com/maleolabs/anvil-standard-laravel/releases/download/v1.0.0/anvil-standard-laravel-1.0.0.tar.gz",
		strings.Repeat("a", 64),
		"c2ln", "cHVi",
	)
	if doc.ID != "anvil-standard-laravel" || doc.Version != "1.0.0" || doc.ContractVersion != "1.0.0" {
		t.Fatalf("identity/version/contract carried over incorrectly: %+v", doc)
	}
	if len(doc.Capability.FrameworkVersion) != 3 {
		t.Fatalf("capability declaration not carried over: %+v", doc.Capability)
	}
	if doc.Distribution.Type != DistributionTypeGitHubReleases {
		t.Fatalf("distribution type: %q", doc.Distribution.Type)
	}
	if doc.Lifecycle.State != LifecycleStatePublished {
		t.Fatalf("lifecycle state: %q", doc.Lifecycle.State)
	}
	if len(doc.Trust.ContentDigests) != 1 || doc.Trust.ContentDigests[0].Digest != strings.Repeat("a", 64) {
		t.Fatalf("placeholder digest not replaced: %+v", doc.Trust.ContentDigests)
	}
	if doc.Trust.Attestation.Signature != "c2ln" || doc.Trust.Attestation.PublicKey != "cHVi" {
		t.Fatalf("placeholder attestation not replaced: %+v", doc.Trust.Attestation)
	}
}

func TestDigestArchive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "archive.tar.gz")
	if err := os.WriteFile(path, []byte("content"), 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	got, err := DigestArchive(path)
	if err != nil {
		t.Fatalf("DigestArchive: %v", err)
	}
	// sha256("content") — known vector.
	const want = "ed7002b439e9ac845f22357d822bac1444730fbdb6016d3ec9432297b9ec9f73"
	if got != want {
		t.Fatalf("digest mismatch: got %s want %s", got, want)
	}
}

func mustDecode(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return b
}
