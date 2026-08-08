package release

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// sha256Bytes returns the raw 32-byte SHA-256 digest of data.
func sha256Bytes(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}

// Publisher attestation (TS-016-03-02; ADR-022 §3; PM decision D-01).
//
// The attestation is an Ed25519 signature over the canonical payload,
// composed byte-for-byte exactly as the Anvil Runtime registry client
// verifies it (Core internal/registry/trust.go: attestationPayload;
// registry-metadata.md §4.7):
//
//	utf8(id) || 0x00 || utf8(version) || 0x00 ||
//	concat(entry bytes in contentDigests array order)
//
// where 0x00 is a single NUL byte and each entry contributes its decoded
// digest bytes, prefixed by utf8(name) || 0x00 when the entry carries a
// name (TS-014-04-04; security review F-2 — the asset binding is SIGNED
// material: a name can be neither stripped nor renamed across assets
// without invalidating the attestation). Releases predating binary
// attestation carry no named entries and compose byte-identically to the
// pre-F-2 payload — their signatures keep verifying. Any deviation in
// the composition — extra or missing separator, reordering, string-level
// construction — invalidates the attestation. The signature and public
// key are strict RFC-4648 base64 (standard alphabet with padding): a
// 64-byte signature, a 32-byte key.

// AttestationPayload composes the canonical signed payload byte-for-byte.
// It rejects NUL bytes inside id, version, or an asset name: a NUL inside
// a claim would make the composition ambiguous (the NUL is the
// separator), mirroring the defensive check in Core trust.go.
func AttestationPayload(id, version string, digests []ContentDigest) ([]byte, error) {
	if strings.Contains(id, "\x00") || strings.Contains(version, "\x00") {
		return nil, fmt.Errorf("id or version contains a NUL byte, which the canonical payload composition uses as a separator; the schema patterns exclude it")
	}
	buf := make([]byte, 0, len(id)+1+len(version)+1+32*len(digests))
	buf = append(buf, id...)
	buf = append(buf, 0x00)
	buf = append(buf, version...)
	buf = append(buf, 0x00)
	for i, d := range digests {
		decoded, err := decodeDigest(d)
		if err != nil {
			return nil, fmt.Errorf("content digest entry [%d] (%s) is not verification material: %v", i, d.Encoding, err)
		}
		if d.Name != "" {
			if strings.Contains(d.Name, "\x00") {
				return nil, fmt.Errorf("content digest entry [%d] carries an asset name with a NUL byte, which the canonical payload composition uses as a separator — the schema pattern excludes it", i)
			}
			buf = append(buf, d.Name...)
			buf = append(buf, 0x00)
		}
		buf = append(buf, decoded...)
	}
	return buf, nil
}

// decodeDigest decodes one declared digest to its 32 bytes: the entry must
// declare the supported sha-256 algorithm and a canonical base16 encoding
// (exactly 64 lowercase hex characters) — the encoding the release pipeline
// produces. Other encodings are rejected at signing time: the pipeline never
// signs material its consumers cannot decode.
func decodeDigest(d ContentDigest) ([]byte, error) {
	if d.Algorithm != DigestAlgorithmSHA256 {
		return nil, fmt.Errorf("declares digest algorithm %q, only %q is supported (PM decision D-01)", d.Algorithm, DigestAlgorithmSHA256)
	}
	if d.Encoding != DigestEncodingBase16 {
		return nil, fmt.Errorf("declares digest encoding %q, only %q is supported by the release pipeline", d.Encoding, DigestEncodingBase16)
	}
	if len(d.Digest) != 64 {
		return nil, fmt.Errorf("digest %q is not the canonical base16 encoding of a SHA-256 digest — exactly 64 lowercase hex characters", d.Digest)
	}
	for _, r := range d.Digest {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return nil, fmt.Errorf("digest %q is not the canonical base16 encoding of a SHA-256 digest — lowercase hex only (^[0-9a-f]{64}$)", d.Digest)
		}
	}
	decoded, err := hex.DecodeString(d.Digest)
	if err != nil {
		return nil, fmt.Errorf("digest %q is not decodable as base16: %v", d.Digest, err)
	}
	return decoded, nil
}

// SignAttestation signs the canonical attestation payload with the private
// key and returns the strict RFC-4648 base64 (standard alphabet with
// padding) encoding of the 64-byte Ed25519 signature.
func SignAttestation(payload []byte, priv ed25519.PrivateKey) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, payload))
}

// Detached document signature (TS-014-04-04, security review F-1).
//
// The release pipeline ALSO publishes a detached Ed25519 signature over
// the RAW bytes of the release metadata document (the exact bytes written
// to the registry-metadata-<version>.json release asset), carried as a
// sibling release asset "registry-metadata-<version>.json.sig". This is a
// DISTINCT signature from the in-document canonical-payload attestation
// (trust.attestation.signature): the attestation binds the document's
// DECLARED claims to the digest values, while the detached signature
// binds the document's BYTES themselves, so a consumer that cannot run
// the full registry trust validation (the bootstrap installer,
// install.sh) can still verify the document it reads was produced by the
// holder of the pinned publisher key before trusting any digest inside
// it. The signature is over the raw bytes — no canonical payload
// composition is involved.
//
// The detached signature is only meaningful with a STABLE signing key:
// a release-time key cannot be pinned out of band, so the release
// pipeline emits the .sig asset only when a stable key is supplied
// (RELEASE_SIGNING_KEY / --key), never for generated release-time keys
// (scripts/release.sh; a .sig that no pinned key verifies would make the
// installer fail closed against every release).

// SignDocumentBytes signs the RAW document bytes (byte-exact: the same
// bytes written to the release asset) with the private key and returns
// the strict RFC-4648 base64 (standard alphabet with padding) encoding
// of the 64-byte Ed25519 signature — the exact content of the
// registry-metadata-<version>.json.sig release asset.
func SignDocumentBytes(document []byte, priv ed25519.PrivateKey) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, document))
}

// VerifyDocumentSignature verifies a detached signature over raw document
// bytes with the given base64-encoded Ed25519 public key (strict
// RFC-4648, 32 bytes). It mirrors the shape checks of VerifyAttestation
// and is used by the pipeline's self-verification.
func VerifyDocumentSignature(document []byte, signatureB64, publicKeyB64 string) error {
	signature, err := base64.StdEncoding.Strict().DecodeString(signatureB64)
	if err != nil {
		return fmt.Errorf("the detached signature is not strict RFC-4648 base64: %v", err)
	}
	if len(signature) != ed25519.SignatureSize {
		return fmt.Errorf("the detached signature decodes to %d bytes, want exactly %d bytes (Ed25519)", len(signature), ed25519.SignatureSize)
	}
	publicKey, err := base64.StdEncoding.Strict().DecodeString(publicKeyB64)
	if err != nil {
		return fmt.Errorf("the verification public key is not strict RFC-4648 base64: %v", err)
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("the verification public key decodes to %d bytes, want exactly %d bytes (Ed25519)", len(publicKey), ed25519.PublicKeySize)
	}
	if !ed25519.Verify(ed25519.PublicKey(publicKey), document, signature) {
		return errors.New("the detached signature does not verify over the raw document bytes with the declared public key — the document was tampered with or was not signed by the holder of the key")
	}
	return nil
}

// PublicKeyBase64 returns the strict RFC-4648 base64 encoding of the
// Ed25519 verification public key corresponding to priv.
func PublicKeyBase64(priv ed25519.PrivateKey) string {
	pub := priv.Public().(ed25519.PublicKey)
	return base64.StdEncoding.EncodeToString(pub)
}

// GenerateKeyPair creates a fresh release-time Ed25519 key pair and writes
// the private key to privPath (PEM PKCS#8) and the public key to pubPath
// (PEM PKIX). The public key is also returned base64-encoded (the shape the
// registry metadata document and the trust anchors allowlist carry).
func GenerateKeyPair(privPath, pubPath string) (publicKeyBase64 string, err error) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return "", fmt.Errorf("generate Ed25519 key pair: %w", err)
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return "", fmt.Errorf("marshal private key: %w", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", fmt.Errorf("marshal public key: %w", err)
	}
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	if err := os.WriteFile(privPath, privPEM, 0o600); err != nil {
		return "", fmt.Errorf("write private key %s: %w", privPath, err)
	}
	if err := os.WriteFile(pubPath, pubPEM, 0o644); err != nil {
		return "", fmt.Errorf("write public key %s: %w", pubPath, err)
	}
	return base64.StdEncoding.EncodeToString(pub), nil
}

// LoadPrivateKey reads a PEM PKCS#8 Ed25519 private key.
func LoadPrivateKey(path string) (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read private key %s: %w", path, err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("private key %s is not PEM-encoded", path)
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key %s: %w", path, err)
	}
	priv, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key %s is not an Ed25519 key (got %T)", path, key)
	}
	return priv, nil
}

// VerifyAttestation verifies the document's publisher attestation over the
// canonical payload composed from the document's own declared values, with
// the document's declared public key. It mirrors the Core registry client's
// checkAttestation semantics (internal/registry/trust.go): strict base64
// decode of signature (64 bytes) and public key (32 bytes), byte-for-byte
// payload composition, Ed25519 verification.
func VerifyAttestation(doc *MetadataDocument) error {
	if doc.Trust.Attestation.Algorithm != AttestationAlgorithmEd25519 {
		return fmt.Errorf("declared attestation algorithm %q, only %q is supported (PM decision D-01)", doc.Trust.Attestation.Algorithm, AttestationAlgorithmEd25519)
	}
	payload, err := AttestationPayload(doc.ID, doc.Version, doc.Trust.ContentDigests)
	if err != nil {
		return fmt.Errorf("cannot construct the canonical attestation payload: %v", err)
	}
	signature, err := base64.StdEncoding.Strict().DecodeString(doc.Trust.Attestation.Signature)
	if err != nil {
		return fmt.Errorf("the declared signature is not strict RFC-4648 base64: %v", err)
	}
	if len(signature) != ed25519.SignatureSize {
		return fmt.Errorf("the declared signature decodes to %d bytes, want exactly %d bytes (Ed25519)", len(signature), ed25519.SignatureSize)
	}
	publicKey, err := base64.StdEncoding.Strict().DecodeString(doc.Trust.Attestation.PublicKey)
	if err != nil {
		return fmt.Errorf("the declared public key is not strict RFC-4648 base64: %v", err)
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("the declared public key decodes to %d bytes, want exactly %d bytes (Ed25519)", len(publicKey), ed25519.PublicKeySize)
	}
	if !ed25519.Verify(ed25519.PublicKey(publicKey), payload, signature) {
		return errors.New("the attestation signature does not verify with the declared public key over the canonical payload (utf8(id) || 0x00 || utf8(version) || 0x00 || concat(decoded digest bytes in contentDigests array order))")
	}
	return nil
}

// VerifyDocument verifies a produced release end-to-end, mirroring the
// adoption-time checks of the Core registry client (VerifyTrust,
// internal/registry/trust.go):
//
//  0. shape — the document must satisfy the strict-parser format surface
//     (ValidateDocumentShape, the self-parse guard);
//  1. integrity — every declared CONTENT digest (the entries without a
//     name, TS-014-04-04) must equal the recomputed SHA-256 of the
//     release content (all-match semantics); named entries are
//     asset-bound and are verified by VerifyBinaryAssetDigests;
//  2. attestation — the Ed25519 signature verifies over the canonical
//     payload (which concatenates EVERY declared digest, named entries
//     included) with the declared public key.
//
// Origin (the out-of-band trust anchor) is an operator-side concern and is
// deliberately NOT verified here: the pipeline proves the release is
// self-consistent and signed by the holder of the declared key; establishing
// publisher origin is the adopter's anchor allowlist (PM decision D-07).
func VerifyDocument(doc *MetadataDocument, content []byte) error {
	if err := ValidateDocumentShape(doc); err != nil {
		return err
	}
	contentDigests := 0
	for _, d := range doc.Trust.ContentDigests {
		if d.Name == "" {
			contentDigests++
		}
	}
	if contentDigests == 0 {
		return errors.New("the release declares no content digest for the release content (every trust.contentDigests entry is a named asset digest); a release without release-content integrity material cannot be verified (ADR-022 §3)")
	}
	sum := sha256Bytes(content)
	for i, d := range doc.Trust.ContentDigests {
		if d.Name != "" {
			// Asset-bound entry: verified against its named asset by
			// VerifyBinaryAssetDigests, not against the release content
			// (TS-014-04-04).
			continue
		}
		decoded, err := decodeDigest(d)
		if err != nil {
			return fmt.Errorf("content digest entry [%d] is not verification material: %v", i, err)
		}
		if !bytes.Equal(decoded, sum) {
			return fmt.Errorf("content digest mismatch: entry [%d] (%s, declared %q) does not match the recomputed SHA-256 of the release content", i, d.Encoding, d.Digest)
		}
	}
	return VerifyAttestation(doc)
}

// VerifyBinaryAssetDigests verifies the binary assets of a release
// against the document's named digest entries (TS-014-04-04), two-way
// strict: every binary file in dir must have a declared entry that
// matches its recomputed SHA-256, and every declared named entry must
// have its file — the pipeline never publishes a digest without its
// asset, and never ships an asset without its digest. A directory with
// no files fails the release: a release that declares named entries must
// actually carry the assets (mirrors the Core asset-install verifier
// semantics: mismatch aborts).
func VerifyBinaryAssetDigests(doc *MetadataDocument, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read binaries directory %s: %w", dir, err)
	}
	declared := make(map[string]ContentDigest)
	for _, d := range doc.Trust.ContentDigests {
		if d.Name != "" {
			if _, dup := declared[d.Name]; dup {
				return fmt.Errorf("binary asset %q is declared twice (two entries cannot bind the same asset)", d.Name)
			}
			declared[d.Name] = d
		}
	}

	files := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		files++
		name := entry.Name()
		d, ok := declared[name]
		if !ok {
			return fmt.Errorf("binary asset %s has no declared digest in trust.contentDigests — every shipped binary must be attested (TS-014-04-04)", name)
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("read binary asset %s: %w", name, err)
		}
		expected, err := decodeDigest(d)
		if err != nil {
			return fmt.Errorf("binary asset %s: declared entry is not verification material: %v", name, err)
		}
		if !bytes.Equal(expected, sha256Bytes(data)) {
			return fmt.Errorf("binary asset %s does not match its declared digest (%s %s) — the asset was tampered with or the digest is stale; aborting the release", name, d.Encoding, d.Digest)
		}
	}
	if files == 0 {
		return fmt.Errorf("binaries directory %s is empty — the release declares no binary assets to verify", dir)
	}
	for name := range declared {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("declared binary asset %s is missing from %s — every declared digest must have its asset (TS-014-04-04)", name, dir)
		}
	}
	return nil
}
