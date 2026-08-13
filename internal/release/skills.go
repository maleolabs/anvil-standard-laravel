package release

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Standard skills as release assets (TS-021-06; ADR-037 D2; TS-021-04).
//
// A standard ships its authored skills as per-skill release assets in the
// standard's release channel. The release pipeline (scripts/release.sh +
// cmd/skillpack) packs each skill into a deterministic
// anvil-skill-<name>-<version> bundle, emits a fragment carrying the
// skills[] declarations AND the named content-digest entries that bind
// each skill asset to its SHA-256 (attestation-bound, TS-014-04-04), and
// merges that fragment into the release metadata document BEFORE the
// document is signed. This file models that fragment and the merge.

// Skill is one declaration in the optional additive skills section of the
// release metadata document (registry-metadata.md §4.8): name (the
// install target), version (semver), asset (the safe release-asset
// identifier, e.g. anvil-skill-overview-1-0-0), and an optional
// description. The asset must be covered by a NAMED trust.contentDigests
// entry of the same document — the parser enforces the binding, and
// VerifySkillAssetDigests verifies the shipped bytes against it.
type Skill struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Asset       string `json:"asset"`
	Description string `json:"description,omitempty"`
}

// SkillsTrustFragment is the trust half of the pack fragment: the named
// content-digest entries binding every skill asset to its digest.
type SkillsTrustFragment struct {
	ContentDigests []ContentDigest `json:"contentDigests"`
}

// SkillsFragment is the pack step's contribution to the release metadata
// document: the skills[] declarations (document root) plus the named
// contentDigests entries (under trust) — the exact schema positions, so
// the pipeline merges the fragment without re-mapping field names.
type SkillsFragment struct {
	Skills []Skill             `json:"skills"`
	Trust  SkillsTrustFragment `json:"trust"`
}

// SkillAssetNamePrefix is the fixed prefix of a skill release asset
// identifier (registry-metadata.md §4.8; mirrored by cmd/skillpack).
const SkillAssetNamePrefix = "anvil-skill-"

// reSkillName mirrors the skills[].name pattern of registry-metadata.md
// §4.8 (^[a-z0-9][a-z0-9-]*$, max 64).
var reSkillName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// IsSkillAssetName reports whether a named content-digest entry binds a
// skill release asset (anvil-skill-*). The pipeline uses the prefix to
// route named entries to the right verifier: binary assets are verified
// against the binaries staging dir, skill assets against the skills
// assets dir.
func IsSkillAssetName(name string) bool {
	return strings.HasPrefix(name, SkillAssetNamePrefix)
}

// ReadSkillsFragment reads and decodes the pack fragment emitted by
// cmd/skillpack (skills-metadata.json). A missing file is an error: a
// pipeline configured for skills must have run the pack step.
func ReadSkillsFragment(path string) (*SkillsFragment, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read skills fragment %s: %w", path, err)
	}
	var frag SkillsFragment
	if err := json.Unmarshal(raw, &frag); err != nil {
		return nil, fmt.Errorf("decode skills fragment %s: %w", path, err)
	}
	return &frag, nil
}

// MergeSkillsFragment merges the pack fragment into a derived release
// metadata document, BEFORE the document is signed:
//
//   - the fragment's named content-digest entries are appended to
//     trust.contentDigests (after the release-content and binary
//     digests — the attestation payload concatenates the digests in
//     array order, so the array order is signed material);
//   - the fragment's skills[] declarations become the document's skills
//     section (document root, registry-metadata.md §4.8).
//
// The merge is strict — a fragment that would produce a document the
// registry parser rejects fails the release (the pipeline never publishes
// material it cannot verify):
//
//   - the fragment must declare at least one skill;
//   - every fragment digest must be a well-formed named sha-256 base16
//     entry with a name starting anvil-skill- (the packer's asset
//     convention), unique across the document;
//   - every declared skill asset must be covered by a fragment digest
//     (the parser-enforced binding — an undeclared or unbound asset is
//     rejected at parse).
func MergeSkillsFragment(doc *MetadataDocument, frag *SkillsFragment) error {
	if frag == nil {
		return nil
	}
	if len(frag.Skills) == 0 {
		return fmt.Errorf("the skills fragment declares no skills — a release that declares skills[] must pack at least one skill")
	}

	// Skill names must be unique within one release (parser-enforced).
	seen := make(map[string]bool, len(frag.Skills))
	for _, s := range frag.Skills {
		if !reSkillName.MatchString(s.Name) || len(s.Name) > 64 {
			return fmt.Errorf("skills fragment: skill name %q is not a safe identifier (^[a-z0-9][a-z0-9-]*$, max 64)", s.Name)
		}
		if seen[s.Name] {
			return fmt.Errorf("skills fragment: skill name %q is declared more than once — names must be unique within one release", s.Name)
		}
		seen[s.Name] = true
	}

	// The fragment's digest entries bind every skill asset (attested
	// named digest, TS-014-04-04). Duplicate names across the whole
	// document are rejected — two entries cannot bind the same asset.
	digestByName := make(map[string]ContentDigest, len(doc.Trust.ContentDigests)+len(frag.Trust.ContentDigests))
	for _, d := range doc.Trust.ContentDigests {
		if d.Name != "" {
			digestByName[d.Name] = d
		}
	}
	for i, d := range frag.Trust.ContentDigests {
		if d.Name == "" || !IsSkillAssetName(d.Name) {
			return fmt.Errorf("skills fragment: content digest entry [%d] is not a named anvil-skill-* entry — the fragment's digests bind skill assets only", i)
		}
		if _, err := decodeDigest(d); err != nil {
			return fmt.Errorf("skills fragment: content digest entry [%d] is not verification material: %v", i, err)
		}
		if _, dup := digestByName[d.Name]; dup {
			return fmt.Errorf("skills fragment: asset %q is bound by more than one digest entry — two entries cannot bind the same asset", d.Name)
		}
		digestByName[d.Name] = d
	}

	// Every declared skill asset must be covered by a fragment digest.
	for _, s := range frag.Skills {
		if _, ok := digestByName[s.Asset]; !ok {
			return fmt.Errorf("skills fragment: skill %q declares asset %q but no fragment digest binds it — every declared skill asset must be covered by an attested named digest (registry-metadata.md §4.8)", s.Name, s.Asset)
		}
	}

	doc.Trust.ContentDigests = append(doc.Trust.ContentDigests, frag.Trust.ContentDigests...)
	doc.Skills = frag.Skills
	return nil
}

// VerifySkillAssetDigests verifies the skill assets of a release against
// the document's named digest entries, two-way strict (mirroring
// VerifyBinaryAssetDigests): every skill asset file in dir must have a
// declared named entry matching its recomputed SHA-256, and every declared
// anvil-skill-* named entry must have its file — the pipeline never
// publishes a digest without its asset, and never ships an asset without
// its digest. A document declaring skills[] must carry the assets: a
// missing file fails the release.
func VerifySkillAssetDigests(doc *MetadataDocument, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read skills assets directory %s: %w", dir, err)
	}
	declared := make(map[string]ContentDigest)
	for _, d := range doc.Trust.ContentDigests {
		if d.Name != "" && IsSkillAssetName(d.Name) {
			if _, dup := declared[d.Name]; dup {
				return fmt.Errorf("skill asset %q is declared twice (two entries cannot bind the same asset)", d.Name)
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
			return fmt.Errorf("skill asset %s has no declared digest in trust.contentDigests — every shipped skill asset must be attested (TS-014-04-04)", name)
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("read skill asset %s: %w", name, err)
		}
		expected, err := decodeDigest(d)
		if err != nil {
			return fmt.Errorf("skill asset %s: declared entry is not verification material: %v", name, err)
		}
		if !bytes.Equal(expected, sha256Bytes(data)) {
			return fmt.Errorf("skill asset %s does not match its declared digest (%s %s) — the asset was tampered with or the digest is stale; aborting the release", name, d.Encoding, d.Digest)
		}
	}
	if files == 0 {
		return fmt.Errorf("skills assets directory %s is empty — a release that declares skills[] must ship the packed skill assets", dir)
	}
	for name := range declared {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("declared skill asset %s is missing from %s — every declared digest must have its asset (TS-014-04-04)", name, dir)
		}
	}
	return nil
}
