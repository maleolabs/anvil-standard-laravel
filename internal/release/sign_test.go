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
		&SourceManifest{ID: "anvil-standard-laravel", Version: "1.0.0", ContractVersion: "1.0.0", Capability: Capability{FrameworkVersion: []string{"10.0.0", "11.0.0", "12.0.0"}}},
		"1.0.0",
		"https://github.com/maleolabs/anvil-standard-laravel/releases/download/v1.0.0/anvil-standard-laravel-1.0.0.tar.gz",
		[]ContentDigest{{Algorithm: DigestAlgorithmSHA256, Encoding: DigestEncodingBase16, Digest: digest}},
		sig, pubB64,
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
		&SourceManifest{ID: "anvil-standard-laravel", Version: "1.0.0", ContractVersion: "1.0.0", Capability: Capability{FrameworkVersion: []string{"10.0.0", "11.0.0", "12.0.0"}}},
		"1.0.0",
		"https://github.com/maleolabs/anvil-standard-laravel/releases/download/v1.0.0/anvil-standard-laravel-1.0.0.tar.gz",
		[]ContentDigest{{Algorithm: DigestAlgorithmSHA256, Encoding: DigestEncodingBase16, Digest: digest}},
		SignAttestation(payload, priv), pubB64,
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
		[]ContentDigest{{Algorithm: DigestAlgorithmSHA256, Encoding: DigestEncodingBase16, Digest: strings.Repeat("a", 64)}},
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

// ── Binary asset attestation (TS-014-04-04) ─────────────────────────

// buildAttestedRelease derives + signs a release document over the given
// digest set (content first, then the named binary digests) and returns
// the document, the archive content, and the private key.
func buildAttestedRelease(t *testing.T, content []byte, contentDigests []ContentDigest) (*MetadataDocument, []byte) {
	t.Helper()
	dir := t.TempDir()
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
	payload, err := AttestationPayload("anvil-standard-laravel", "1.0.0", contentDigests)
	if err != nil {
		t.Fatalf("AttestationPayload: %v", err)
	}
	doc := DeriveDocument(
		&SourceManifest{ID: "anvil-standard-laravel", Version: "1.0.0", ContractVersion: "1.0.0", Capability: Capability{FrameworkVersion: []string{"10.0.0", "11.0.0", "12.0.0"}}},
		"1.0.0",
		"https://github.com/maleolabs/anvil-standard-laravel/releases/download/v1.0.0/anvil-standard-laravel-1.0.0.tar.gz",
		contentDigests, SignAttestation(payload, priv), pubB64,
	)
	return doc, content
}

// TestBinaryAssetDigests_ComputesSortedNamedEntries verifies the
// pipeline's per-asset digest computation: every regular file becomes a
// named base16 entry, sorted by file name for a deterministic array
// order (the order is signed material).
func TestBinaryAssetDigests_ComputesSortedNamedEntries(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "anvil-adapter-laravel-darwin-arm64"), []byte("darwin arm64"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "anvil-adapter-laravel-linux-amd64"), []byte("linux amd64"), 0o644); err != nil {
		t.Fatal(err)
	}
	digests, err := BinaryAssetDigests(dir)
	if err != nil {
		t.Fatalf("BinaryAssetDigests: %v", err)
	}
	if len(digests) != 2 {
		t.Fatalf("digests = %d, want 2", len(digests))
	}
	if digests[0].Name != "anvil-adapter-laravel-darwin-arm64" || digests[1].Name != "anvil-adapter-laravel-linux-amd64" {
		t.Fatalf("entries not sorted by name: %v", digests)
	}
	if digests[0].Digest != SHA256Base16([]byte("darwin arm64")) {
		t.Errorf("entry digest mismatch for darwin arm64")
	}
	for i, d := range digests {
		if d.Algorithm != DigestAlgorithmSHA256 || d.Encoding != DigestEncodingBase16 {
			t.Errorf("entry [%d] algorithm/encoding: %+v", i, d)
		}
	}
}

// TestVerifyDocumentAndBinaryDigests_Valid asserts the full
// TS-014-04-04 pipeline self-verification: a document carrying the
// content digest plus named binary digests passes the shape guard, the
// content verification (unnamed entries only), the attestation (over ALL
// entries), and the per-asset binary verification.
func TestVerifyDocumentAndBinaryDigests_Valid(t *testing.T) {
	dir := t.TempDir()
	content := []byte("release archive bytes")
	bins := map[string][]byte{
		"anvil-adapter-laravel-linux-amd64":  []byte("linux amd64 binary"),
		"anvil-adapter-laravel-linux-arm64":  []byte("linux arm64 binary"),
		"anvil-adapter-laravel-darwin-amd64": []byte("darwin amd64 binary"),
	}
	contentDigests := []ContentDigest{{
		Algorithm: DigestAlgorithmSHA256, Encoding: DigestEncodingBase16, Digest: SHA256Base16(content),
	}}
	names := []string{"anvil-adapter-laravel-darwin-amd64", "anvil-adapter-laravel-linux-amd64", "anvil-adapter-laravel-linux-arm64"}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), bins[name], 0o644); err != nil {
			t.Fatal(err)
		}
		contentDigests = append(contentDigests, ContentDigest{
			Algorithm: DigestAlgorithmSHA256, Encoding: DigestEncodingBase16,
			Digest: SHA256Base16(bins[name]), Name: name,
		})
	}

	doc, content := buildAttestedRelease(t, content, contentDigests)
	if err := ValidateDocumentShape(doc); err != nil {
		t.Fatalf("shape guard: %v", err)
	}
	if err := VerifyDocument(doc, content); err != nil {
		t.Fatalf("VerifyDocument: %v", err)
	}
	if err := VerifyBinaryAssetDigests(doc, dir); err != nil {
		t.Fatalf("VerifyBinaryAssetDigests: %v", err)
	}
}

// TestVerifyDocument_NamedEntriesNotComparedWithContent asserts named
// entries are asset-bound, not compared with the release content: a
// named digest of the binary payload differs from the content hash and
// verification still passes.
func TestVerifyDocument_NamedEntriesNotComparedWithContent(t *testing.T) {
	content := []byte("release archive bytes")
	contentDigests := []ContentDigest{
		{Algorithm: DigestAlgorithmSHA256, Encoding: DigestEncodingBase16, Digest: SHA256Base16(content)},
		{Algorithm: DigestAlgorithmSHA256, Encoding: DigestEncodingBase16, Digest: SHA256Base16([]byte("binary bytes, not content")), Name: "anvil-adapter-laravel-linux-amd64"},
	}
	doc, content := buildAttestedRelease(t, content, contentDigests)
	if err := VerifyDocument(doc, content); err != nil {
		t.Fatalf("VerifyDocument must not compare named entries with the release content: %v", err)
	}
}

// TestVerifyBinaryAssetDigests_TamperedBinary asserts a binary that does
// not match its declared digest fails the pipeline self-verification —
// the exact tamper an adoption-time verifier aborts on (TS-014-04-04).
func TestVerifyBinaryAssetDigests_TamperedBinary(t *testing.T) {
	dir := t.TempDir()
	name := "anvil-adapter-laravel-linux-amd64"
	pristine := []byte("pristine linux amd64 binary")
	if err := os.WriteFile(filepath.Join(dir, name), pristine, 0o644); err != nil {
		t.Fatal(err)
	}
	contentDigests := []ContentDigest{
		{Algorithm: DigestAlgorithmSHA256, Encoding: DigestEncodingBase16, Digest: SHA256Base16([]byte("archive"))},
		{Algorithm: DigestAlgorithmSHA256, Encoding: DigestEncodingBase16, Digest: SHA256Base16(pristine), Name: name},
	}
	doc, content := buildAttestedRelease(t, []byte("archive"), contentDigests)

	// Tamper AFTER the release was produced (the same-channel attacker's
	// move: the binary is replaced, the digest declaration is NOT — the
	// signature still covers the declared digest).
	if err := os.WriteFile(filepath.Join(dir, name), append(pristine, []byte("TAMPERED")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyDocument(doc, content); err != nil {
		t.Fatalf("VerifyDocument should pass (archive untouched): %v", err)
	}
	err := VerifyBinaryAssetDigests(doc, dir)
	if err == nil || !strings.Contains(err.Error(), "does not match its declared digest") {
		t.Fatalf("tampered binary must fail the asset verification, got: %v", err)
	}
}

// TestVerifyBinaryAssetDigests_MissingAndUndeclaredAssets asserts the
// two-way strict binding: a file without a declared digest and a
// declared digest without its file both fail the release.
func TestVerifyBinaryAssetDigests_MissingAndUndeclaredAssets(t *testing.T) {
	dir := t.TempDir()
	name := "anvil-adapter-laravel-linux-amd64"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	contentDigests := []ContentDigest{
		{Algorithm: DigestAlgorithmSHA256, Encoding: DigestEncodingBase16, Digest: SHA256Base16([]byte("archive"))},
		{Algorithm: DigestAlgorithmSHA256, Encoding: DigestEncodingBase16, Digest: SHA256Base16([]byte("binary")), Name: name},
	}
	doc, _ := buildAttestedRelease(t, []byte("archive"), contentDigests)

	// A file with no declared entry.
	if err := os.WriteFile(filepath.Join(dir, "anvil-adapter-laravel-linux-arm64"), []byte("undeclared"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyBinaryAssetDigests(doc, dir); err == nil || !strings.Contains(err.Error(), "has no declared digest") {
		t.Fatalf("undeclared asset must fail, got: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "anvil-adapter-laravel-linux-arm64")); err != nil {
		t.Fatal(err)
	}

	// A declared entry whose file is missing.
	doc2 := *doc
	doc2.Trust.ContentDigests = append(doc2.Trust.ContentDigests, ContentDigest{
		Algorithm: DigestAlgorithmSHA256, Encoding: DigestEncodingBase16,
		Digest: SHA256Base16([]byte("ghost")), Name: "anvil-adapter-laravel-darwin-arm64",
	})
	if err := VerifyBinaryAssetDigests(&doc2, dir); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("declared asset without a file must fail, got: %v", err)
	}
}
