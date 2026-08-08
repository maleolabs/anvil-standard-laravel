package release

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Source manifest and derived document shapes (TS-016-03-02).
//
// The source manifest (manifest/registry-metadata.json) follows the
// registry metadata format field conventions but carries the release-time
// sections (distribution, lifecycle, trust) as format-valid placeholders
// (TS-016-03-01 manifest nuance). The derived release document is the
// publishable registry metadata document: same identity, contract version,
// and capability declaration, with distribution, lifecycle, and trust
// populated from the real release (ADR-030; TS-016-03-02 DoD).
//
// The json tags reproduce the registry-metadata.schema.json field names
// exactly: the produced document must be parseable by the Core registry
// client's strict parser (internal/registry/parse.go).

// SourceManifest is the authoring-time manifest of the standard. The
// release-time sections are optional in the source (the Flutter standard's
// source manifest omits them; this standard's source manifest carries
// placeholders) — the derived document always carries real values.
type SourceManifest struct {
	Schema          string        `json:"$schema"`
	Title           string        `json:"title"`
	Description     string        `json:"description"`
	ID              string        `json:"id"`
	Version         string        `json:"version"`
	ContractVersion string        `json:"contractVersion"`
	Capability      Capability    `json:"capability"`
	Distribution    *Distribution `json:"distribution,omitempty"`
	Lifecycle       *Lifecycle    `json:"lifecycle,omitempty"`
	Trust           *Trust        `json:"trust,omitempty"`
}

// Capability is the capability declaration of a release: the
// framework-version support scope (ADR-021 §3.2).
type Capability struct {
	FrameworkVersion []string `json:"frameworkVersion"`
}

// Distribution is the distribution location of the release content
// (ADR-030 §3): type "github-releases" and an https location on the
// standard's own release channel.
type Distribution struct {
	Type     string `json:"type"`
	Location string `json:"location"`
}

// Lifecycle is the governed availability state of the release (ADR-023 §3,
// ADR-027 §3). A fresh release is always published.
type Lifecycle struct {
	State string `json:"state"`
}

// ContentDigest is one integrity digest entry (registry-metadata.schema.json
// §contentDigest): algorithm sha-256, encoding base16/base32/base64, and
// the digest value in the declared encoding. The optional Name binds the
// entry to a named release asset of the same release (TS-014-04-04 — e.g.
// an adapter binary "anvil-adapter-laravel-linux-amd64"); absent for the
// release-content digest (the archive). Every entry — named or not — is
// covered by the publisher attestation (the canonical payload
// concatenates all decoded digest bytes in array order).
//
// FIELD ORDER NOTE: Name is deliberately the LAST struct field — the
// produced document's JSON carries it last inside each entry object, and
// Core's install.sh extracts per-asset digests with a line-oriented
// parser that relies on that order (docs/install.sh usage; TS-014-04-04).
type ContentDigest struct {
	Algorithm string `json:"algorithm"`
	Encoding  string `json:"encoding"`
	Digest    string `json:"digest"`
	Name      string `json:"name,omitempty"`
}

// Attestation is the publisher attestation of a release (ADR-022 §3): an
// Ed25519 signature over the canonical attestation payload plus the
// publisher's verification public key, both base64 (RFC-4648 standard with
// padding).
type Attestation struct {
	Algorithm string `json:"algorithm"`
	Signature string `json:"signature"`
	PublicKey string `json:"publicKey"`
}

// Trust carries the trust fields of a release: content digest(s) and
// publisher attestation, present from day one (ADR-022 §3). Since
// TS-014-04-04 the digest set covers the release content (unnamed
// entries) AND the release's binary assets (named entries).
type Trust struct {
	ContentDigests []ContentDigest `json:"contentDigests"`
	Attestation    Attestation     `json:"attestation"`
}

// MetadataDocument is the publishable registry metadata document of one
// release: the distribution unit of the standard registry (ADR-023 §3).
type MetadataDocument struct {
	Schema          string       `json:"$schema,omitempty"`
	Title           string       `json:"title,omitempty"`
	Description     string       `json:"description,omitempty"`
	ID              string       `json:"id"`
	Version         string       `json:"version"`
	ContractVersion string       `json:"contractVersion"`
	Capability      Capability   `json:"capability"`
	Distribution    Distribution `json:"distribution"`
	Lifecycle       Lifecycle    `json:"lifecycle"`
	Trust           Trust        `json:"trust"`
}

// Format constants (registry-metadata.schema.json; metadata.go in Core).
const (
	// SchemaURN is the $id of the registry metadata schema this document
	// targets.
	SchemaURN = "urn:anvil:spec:registry-metadata:1.0.0"

	// DistributionTypeGitHubReleases is the only supported distribution
	// channel pattern (ADR-030 §3).
	DistributionTypeGitHubReleases = "github-releases"

	// LifecycleStatePublished marks a standard discoverable, installable,
	// and validated against the declared contract version (ADR-027 §3).
	LifecycleStatePublished = "published"

	// DigestAlgorithmSHA256 is the trust-baseline digest algorithm.
	DigestAlgorithmSHA256 = "sha-256"

	// DigestEncodingBase16 is the conventional default digest encoding
	// (lowercase hex).
	DigestEncodingBase16 = "base16"

	// DigestEncodingBase32 is the RFC-4648 base32 digest encoding
	// (supported by the registry format; the release pipeline produces
	// base16 only).
	DigestEncodingBase32 = "base32"

	// DigestEncodingBase64 is the RFC-4648 standard (padded) base64 digest
	// encoding (supported by the registry format; the release pipeline
	// produces base16 only).
	DigestEncodingBase64 = "base64"

	// AttestationAlgorithmEd25519 is the trust-baseline attestation
	// algorithm (PM decision D-01).
	AttestationAlgorithmEd25519 = "ed25519"
)

// ReadSourceManifest reads and decodes the source manifest.
func ReadSourceManifest(path string) (*SourceManifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read source manifest %s: %w", path, err)
	}
	var src SourceManifest
	if err := json.Unmarshal(raw, &src); err != nil {
		return nil, fmt.Errorf("decode source manifest %s: %w", path, err)
	}
	if src.ID == "" || src.Version == "" || src.ContractVersion == "" {
		return nil, fmt.Errorf("source manifest %s must declare id, version, and contractVersion", path)
	}
	return &src, nil
}

// SHA256Base16 returns the lowercase-hex SHA-256 digest of data — the
// canonical base16 encoding of a 32-byte digest
// (registry-metadata.schema.json §contentDigest: ^[0-9a-f]{64}$).
func SHA256Base16(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// DigestArchive computes the canonical base16 SHA-256 digest of the release
// archive at path — the content digest the release metadata document
// declares for the content resolved from distribution.location (ADR-022
// §3: a claim is not evidence; the digest is recomputed and compared at
// adoption).
func DigestArchive(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read release archive %s: %w", path, err)
	}
	return SHA256Base16(data), nil
}

// BinaryAssetDigests computes the NAMED content digest entries of every
// regular file in dir (the release pipeline's platform binaries staging
// directory, "binaries/" — TS-014-04-04): each entry declares the
// canonical base16 SHA-256 digest of one binary asset, bound to the asset
// file name. The entries are sorted by file name so the derived
// document's contentDigests array order is deterministic (the
// attestation payload concatenates the digests in array order, so the
// order is signed material). A file that cannot be read fails the
// release: the pipeline never publishes a digest it cannot compute.
func BinaryAssetDigests(dir string) ([]ContentDigest, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read binaries directory %s: %w", dir, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	digests := make([]ContentDigest, 0, len(names))
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("read binary asset %s: %w", name, err)
		}
		digests = append(digests, ContentDigest{
			Algorithm: DigestAlgorithmSHA256,
			Encoding:  DigestEncodingBase16,
			Digest:    SHA256Base16(data),
			Name:      name,
		})
	}
	return digests, nil
}

// DeriveDocument derives the publishable release metadata document from the
// source manifest: identity, contract version, and capability declaration
// are carried over unchanged; version, distribution, lifecycle, and trust
// are populated from the real release (TS-016-03-02).
//
// contentDigests is the FULL attestation-bound digest set: the release
// archive digest (unnamed entry) followed by the named digests of the
// release's binary assets (TS-014-04-04). signature and publicKey are the
// Ed25519 attestation material over the canonical payload composed from
// the declared digests (sign.go) — the signature binds the archive AND
// every binary asset.
func DeriveDocument(src *SourceManifest, version, location string, contentDigests []ContentDigest, signature, publicKey string) *MetadataDocument {
	return &MetadataDocument{
		Schema:          schemaOr(src.Schema, SchemaURN),
		Title:           src.Title,
		Description:     src.Description,
		ID:              src.ID,
		Version:         version,
		ContractVersion: src.ContractVersion,
		Capability:      src.Capability,
		Distribution: Distribution{
			Type:     DistributionTypeGitHubReleases,
			Location: location,
		},
		Lifecycle: Lifecycle{
			State: LifecycleStatePublished,
		},
		Trust: Trust{
			ContentDigests: contentDigests,
			Attestation: Attestation{
				Algorithm: AttestationAlgorithmEd25519,
				Signature: signature,
				PublicKey: publicKey,
			},
		},
	}
}

// DocumentBytes renders the metadata document as pretty-printed JSON
// with a trailing newline — the EXACT bytes written to the release asset
// (registry-metadata-<version>.json) and the bytes the detached document
// signature covers (SignDocumentBytes, security review F-1).
func DocumentBytes(doc *MetadataDocument) ([]byte, error) {
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode metadata document: %w", err)
	}
	return append(raw, '\n'), nil
}

// WriteDocument writes the metadata document as pretty-printed JSON
// (DocumentBytes) to path.
func WriteDocument(doc *MetadataDocument, path string) error {
	raw, err := DocumentBytes(doc)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("write metadata document %s: %w", path, err)
	}
	return nil
}

// ReadDocument reads and decodes a produced metadata document.
func ReadDocument(path string) (*MetadataDocument, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read metadata document %s: %w", path, err)
	}
	var doc MetadataDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("decode metadata document %s: %w", path, err)
	}
	return &doc, nil
}

// schemaOr returns the source's declared $schema, defaulting to the schema
// URN when the source omits it.
func schemaOr(declared, fallback string) string {
	if declared != "" {
		return declared
	}
	return fallback
}
