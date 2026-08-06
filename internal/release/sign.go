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
// verifies it (Core internal/registry/trust.go: attestationPayload):
//
//	utf8(id) || 0x00 || utf8(version) || 0x00 ||
//	concat(decoded digest bytes in contentDigests array order)
//
// where 0x00 is a single NUL byte. Any deviation in the composition — extra
// or missing separator, reordering, string-level construction — invalidates
// the attestation. The signature and public key are strict RFC-4648 base64
// (standard alphabet with padding): a 64-byte signature, a 32-byte key.

// AttestationPayload composes the canonical signed payload byte-for-byte.
// It rejects NUL bytes inside id or version: a NUL inside a claim would
// make the composition ambiguous (the NUL is the separator), mirroring the
// defensive check in Core trust.go.
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
//  1. integrity — every declared content digest must equal the recomputed
//     SHA-256 of the release content (all-match semantics);
//  2. attestation — the Ed25519 signature verifies over the canonical
//     payload with the declared public key.
//
// Origin (the out-of-band trust anchor) is an operator-side concern and is
// deliberately NOT verified here: the pipeline proves the release is
// self-consistent and signed by the holder of the declared key; establishing
// publisher origin is the adopter's anchor allowlist (PM decision D-07).
func VerifyDocument(doc *MetadataDocument, content []byte) error {
	if len(doc.Trust.ContentDigests) == 0 {
		return errors.New("the release declares no content digests; a release without integrity material cannot be verified (ADR-022 §3)")
	}
	sum := sha256Bytes(content)
	for i, d := range doc.Trust.ContentDigests {
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
