// Command release-sign is the publisher-side signing tool of the Laravel
// delivery lifecycle standard release pipeline (TS-016-03-02; ADR-022,
// ADR-023, ADR-030).
//
// It implements the trust baseline of the registry metadata format
// (registry-metadata.schema.json; PM decision D-01) exactly as the Anvil
// Runtime registry client consumes it (Core internal/registry/trust.go):
//
//   - integrity — sha-256 content digests over the release archive AND
//     over each binary asset (the platform adapter executables staged
//     under binaries/; TS-014-04-04): the archive digest is the unnamed
//     entry, each binary digest is a named entry
//     (trust.contentDigests[].name);
//
//   - attestation — an Ed25519 signature over the canonical payload
//
//     utf8(id) || 0x00 || utf8(version) || 0x00 ||
//     concat(decoded digest bytes in contentDigests array order)
//
//     verified byte-for-byte by the registry client — the signature
//     binds the archive AND every binary asset of the release;
//
//   - the publisher's Ed25519 verification public key, base64-encoded
//     (RFC-4648 standard with padding), carried in the document and pinned
//     out of band by adopters (trust anchors allowlist, PM decision D-07).
//
// Subcommands:
//
//	generate  create a release signing key pair
//	          (PEM PKCS#8 private key + PEM PKIX public key)
//	sign      derive the publishable registry metadata document from the
//	          source manifest and sign it (real digests — archive and
//	          binaries — + attestation)
//	verify    verify a produced metadata document against the release
//	          content and binaries (integrity + attestation; the release
//	          pipeline never publishes material it cannot verify)
//
// The tool is release-time infrastructure only: it is NOT part of the
// standard executable (cmd/laravel-adapter) and never ships in a release
// artifact.
//
// Reference: TS-016-03-02, ADR-022 §3, ADR-023 §3
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"maleolabs.com/anvil-standard-laravel/internal/release"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) < 1 {
		usage()
		return 2
	}
	switch args[0] {
	case "generate":
		return runGenerate(args[1:])
	case "sign":
		return runSign(args[1:])
	case "verify":
		return runVerify(args[1:])
	case "help", "-h", "--help":
		usage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "error: unknown subcommand %q\n", args[0])
		usage()
		return 2
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `release-sign — publisher-side signing tool of the Laravel standard release pipeline

Usage:
  release-sign generate --out <dir>
  release-sign sign --manifest <source.json> --version <v> --archive <path>
      --location <url> --key <private.pem> [--binaries <dir>]
      [--skills <fragment.json>] [--out <doc.json>] [--sig <doc.json.sig>]
  release-sign verify --document <doc.json> --archive <path>
      [--binaries <dir>] [--skills <assets-dir>] [--sig <doc.json.sig>]

Subcommands:
  generate   create a release signing key pair (release-signing-key.pem,
             release-signing-key.pub.pem in <dir>); prints the base64
             public key
  sign       derive the publishable registry metadata document from the
             source manifest (real digest over --archive, distribution,
             lifecycle, trust) and sign the canonical attestation payload
             with --key; --binaries <dir> additionally attests every
             binary asset in <dir> as a named contentDigests entry
             (TS-014-04-04); --skills <fragment> merges the pack step's
             skills[] declarations + named skill-asset digests into the
             document BEFORE signing (TS-021-06); --sig <path> writes the
             DETACHED Ed25519 signature over the raw document bytes
             (base64, F-1) — the sibling release asset
             registry-metadata-<v>.json.sig the bootstrap installer
             verifies with its pinned publisher key; writes the document
             to --out (default stdout)
  verify     verify the document against the release content: integrity
             (every declared content digest vs recomputed sha-256) and
             attestation (Ed25519 over the canonical payload with the
             declared public key); with --binaries <dir>, also verify
             every binary asset against its declared named digest; with
             --skills <assets-dir>, also verify every skill asset
             against its declared named digest; with --sig <path>, also
             verify the detached signature over the raw document bytes
             with the declared public key
`)
}

func runGenerate(args []string) int {
	fs := flag.NewFlagSet("generate", flag.ExitOnError)
	out := fs.String("out", ".", "directory to write the key pair into")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	privPath := *out + "/release-signing-key.pem"
	pubPath := *out + "/release-signing-key.pub.pem"
	pubB64, err := release.GenerateKeyPair(privPath, pubPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Printf("wrote %s\nwrote %s\n", privPath, pubPath)
	fmt.Printf("public key (base64): %s\n", pubB64)
	return 0
}

func runSign(args []string) int {
	fs := flag.NewFlagSet("sign", flag.ExitOnError)
	manifest := fs.String("manifest", "", "path of the source manifest (manifest/registry-metadata.json)")
	version := fs.String("version", "", "release version, plain semver (must equal the tag version)")
	archive := fs.String("archive", "", "path of the packaged release archive (the release content)")
	location := fs.String("location", "", "https distribution.location of the archive on the release channel")
	key := fs.String("key", "", "path of the Ed25519 signing private key (PEM PKCS#8)")
	binaries := fs.String("binaries", "", "path of the platform binaries staging directory (binaries/); every regular file becomes a named attestation-bound contentDigests entry (TS-014-04-04)")
	skills := fs.String("skills", "", "path of the pack fragment (skills-metadata.json from cmd/skillpack): the skills[] declarations + the named contentDigests entries binding each skill asset to its digest, merged into the document BEFORE signing (TS-021-06)")
	sig := fs.String("sig", "", "path of the detached document signature asset (registry-metadata-<v>.json.sig): the Ed25519 signature over the raw document bytes, base64 (F-1)")
	out := fs.String("out", "", "path of the produced registry metadata document (default: stdout)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *manifest == "" || *version == "" || *archive == "" || *location == "" || *key == "" {
		fmt.Fprintln(os.Stderr, "error: --manifest, --version, --archive, --location, and --key are required")
		return 2
	}
	if !plainSemver(*version) {
		fmt.Fprintf(os.Stderr, "error: version %q is not plain semver (major.minor.patch, no leading zeros)\n", *version)
		return 2
	}

	src, err := release.ReadSourceManifest(*manifest)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	// The manifest `version` field is the standard's version line: a
	// release must never declare a version the repository does not
	// (defense in depth alongside scripts/release.sh's tag assertion).
	if err := release.ValidateVersionMatch(*version, src.Version); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	priv, err := release.LoadPrivateKey(*key)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	digest, err := release.DigestArchive(*archive)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	// The digest set: the release-content digest first (unnamed), then
	// the named digests of the binary assets (TS-014-04-04) — sorted by
	// file name, so the array order is deterministic. The attestation
	// payload concatenates the digests in array order, so the order is
	// signed material.
	contentDigests := []release.ContentDigest{{
		Algorithm: release.DigestAlgorithmSHA256,
		Encoding:  release.DigestEncodingBase16,
		Digest:    digest,
	}}
	if *binaries != "" {
		binDigests, err := release.BinaryAssetDigests(*binaries)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		if len(binDigests) == 0 {
			fmt.Fprintf(os.Stderr, "error: --binaries %s contains no files — a release attests its binary assets or none at all\n", *binaries)
			return 1
		}
		contentDigests = append(contentDigests, binDigests...)
	}
	// The skill fragment (TS-021-06): the pack step's named skill-asset
	// digests are appended AFTER the archive and binary digests, so the
	// array order is deterministic and the attestation payload (which
	// concatenates the digests in array order) signs every skill digest
	// — an attested named digest an adopter's skill install verifies
	// against (fail-closed). MergeSkillsFragment then folds the fragment
	// (digests + skills[]) into the document.
	var skillsFrag *release.SkillsFragment
	if *skills != "" {
		frag, err := release.ReadSkillsFragment(*skills)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		skillsFrag = frag
	}
	payloadDigests := contentDigests
	if skillsFrag != nil {
		payloadDigests = append(append([]release.ContentDigest{}, contentDigests...), skillsFrag.Trust.ContentDigests...)
	}

	payload, err := release.AttestationPayload(src.ID, *version, payloadDigests)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	signature := release.SignAttestation(payload, priv)
	publicKey := release.PublicKeyBase64(priv)

	doc := release.DeriveDocument(src, *version, *location, contentDigests, signature, publicKey)
	// The pack fragment merge (skills[] at the document root) happens
	// BEFORE the self-parse guard and BEFORE the detached document
	// signature covers the bytes: the published document carries the
	// declared skills bound to the attested named digests.
	if skillsFrag != nil {
		if err := release.MergeSkillsFragment(doc, skillsFrag); err != nil {
			fmt.Fprintf(os.Stderr, "error: cannot merge the skills fragment: %v\n", err)
			return 1
		}
	}
	// Self-parse guard: never write a document the strict registry parser
	// would reject (TS-016-03-02 review finding; the release pipeline never
	// publishes material it cannot verify).
	if err := release.ValidateDocumentShape(doc); err != nil {
		fmt.Fprintf(os.Stderr, "error: derived document failed the self-parse guard: %v\n", err)
		return 1
	}
	// The detached document signature (F-1) covers the EXACT bytes that
	// are written to the release asset — the same bytes the bootstrap
	// installer (install.sh) verifies with its pinned publisher key.
	docBytes, err := release.DocumentBytes(doc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if *sig != "" {
		sigB64 := release.SignDocumentBytes(docBytes, priv)
		if err := os.WriteFile(*sig, []byte(sigB64+"\n"), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "error: write detached signature %s: %v\n", *sig, err)
			return 1
		}
	}
	if *out == "" {
		if err := release.WriteDocument(doc, "/dev/stdout"); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		return 0
	}
	if err := release.WriteDocument(doc, *out); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Printf("wrote %s (id %s, version %s, %d content digest(s), %d named asset digest(s), %d skill(s))\n",
		*out, doc.ID, doc.Version,
		len(doc.Trust.ContentDigests)-len(binDigestsFor(doc)), len(binDigestsFor(doc)), len(doc.Skills))
	fmt.Printf("public key (base64): %s\n", publicKey)
	return 0
}

func runVerify(args []string) int {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	document := fs.String("document", "", "path of the produced registry metadata document")
	archive := fs.String("archive", "", "path of the release archive (the release content)")
	binaries := fs.String("binaries", "", "path of the platform binaries staging directory (binaries/); every binary asset is verified against its declared named digest (TS-014-04-04)")
	skills := fs.String("skills", "", "path of the skills assets directory (skills/assets/); every skill asset is verified against its declared named digest (TS-021-06)")
	sig := fs.String("sig", "", "path of the detached document signature asset (registry-metadata-<v>.json.sig); the signature is verified over the RAW document bytes with the document's declared public key (F-1)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *document == "" || *archive == "" {
		fmt.Fprintln(os.Stderr, "error: --document and --archive are required")
		return 2
	}

	doc, err := release.ReadDocument(*document)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	content, err := os.ReadFile(*archive)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: read %s: %v\n", *archive, err)
		return 1
	}
	if err := release.VerifyDocument(doc, content); err != nil {
		fmt.Fprintf(os.Stderr, "error: verification failed: %v\n", err)
		return 1
	}
	if *binaries != "" {
		if err := release.VerifyBinaryAssetDigests(doc, *binaries); err != nil {
			fmt.Fprintf(os.Stderr, "error: binary asset verification failed: %v\n", err)
			return 1
		}
	}
	if *skills != "" {
		if err := release.VerifySkillAssetDigests(doc, *skills); err != nil {
			fmt.Fprintf(os.Stderr, "error: skill asset verification failed: %v\n", err)
			return 1
		}
	}
	if *sig != "" {
		// The detached signature covers the RAW bytes of the document
		// asset — verify against the file bytes, not the parsed doc.
		raw, err := os.ReadFile(*document)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: read %s: %v\n", *document, err)
			return 1
		}
		sigRaw, err := os.ReadFile(*sig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: read detached signature %s: %v\n", *sig, err)
			return 1
		}
		sigB64 := strings.TrimSpace(string(sigRaw))
		if err := release.VerifyDocumentSignature(raw, sigB64, doc.Trust.Attestation.PublicKey); err != nil {
			fmt.Fprintf(os.Stderr, "error: detached document signature verification failed: %v\n", err)
			return 1
		}
	}
	fmt.Printf("OK: %s %s — integrity (sha-256 %s) and attestation (ed25519) verified\n",
		doc.ID, doc.Version, doc.Trust.ContentDigests[0].Digest)
	return 0
}

// binDigestsFor returns the named (asset-bound) digest entries of a
// document — the count the sign summary reports.
func binDigestsFor(doc *release.MetadataDocument) []release.ContentDigest {
	var named []release.ContentDigest
	for _, d := range doc.Trust.ContentDigests {
		if d.Name != "" {
			named = append(named, d)
		}
	}
	return named
}

// plainSemver reports whether v is plain semver without leading zeros
// (registry-metadata.schema.json version pattern).
func plainSemver(v string) bool {
	if v == "" || v[0] < '0' || v[0] > '9' {
		return false
	}
	dots := 0
	for _, r := range v {
		if r == '.' {
			dots++
			continue
		}
		if r < '0' || r > '9' {
			return false
		}
	}
	if dots != 2 || len(v) == 0 || v[len(v)-1] == '.' || v[0] == '.' {
		return false
	}
	parts := split(v, '.')
	for _, p := range parts {
		if p == "" || (len(p) > 1 && p[0] == '0') {
			return false
		}
	}
	return true
}

func split(s string, sep byte) []string {
	var out []string
	cur := ""
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(s[i])
	}
	out = append(out, cur)
	return out
}
