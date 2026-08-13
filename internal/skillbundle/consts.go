package skillbundle

// Bundle size caps (skill-bundle-format.md §3; vendored from Core
// internal/skillbundle/extract.go, commit c08f4b9).
//
// These constants live in extract.go in Core. The install-side extractor
// (extract.go) is deliberately NOT vendored — this repository only PACKS
// skill bundles (internal/skillpack); the strict extractor runs in the
// Core CLI at install time. The caps are part of the bundle FORMAT and
// the packer enforces them at pack time so a produced bundle is never
// rejected at extraction for size (CreateBundle checks them).

const (
	// MaxAssetSize caps one content file of a skill bundle.
	MaxAssetSize = 10 << 20

	// MaxTotalSize caps the total uncompressed content of a skill bundle.
	MaxTotalSize = 64 << 20

	// MaxFileCount caps the number of content files of a skill bundle.
	MaxFileCount = 512

	// MaxPathDepth caps the component depth of one content path.
	MaxPathDepth = 16
)
