// Command release-sign is the publisher-side signing tool of the Laravel
// delivery lifecycle standard release pipeline (TS-016-03-02; ADR-022,
// ADR-023, ADR-030).
//
// It implements the trust baseline of the registry metadata format
// (registry-metadata.schema.json; PM decision D-01) exactly as the Anvil
// Runtime registry client consumes it (Core internal/registry/trust.go):
//
//   - integrity — sha-256 content digests over the release archive
//     (canonical base16 encoding);
//
//   - attestation — an Ed25519 signature over the canonical payload
//
//     utf8(id) || 0x00 || utf8(version) || 0x00 ||
//     concat(decoded digest bytes in contentDigests array order)
//
//     verified byte-for-byte by the registry client;
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
//	          source manifest and sign it (real digests + attestation)
//	verify    verify a produced metadata document against the release
//	          content (integrity + attestation; the release pipeline never
//	          publishes material it cannot verify)
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
      --location <url> --key <private.pem> [--out <doc.json>]
  release-sign verify --document <doc.json> --archive <path>

Subcommands:
  generate   create a release signing key pair (release-signing-key.pem,
             release-signing-key.pub.pem in <dir>); prints the base64
             public key
  sign       derive the publishable registry metadata document from the
             source manifest (real digest over --archive, distribution,
             lifecycle, trust) and sign the canonical attestation payload
             with --key; writes the document to --out (default stdout)
  verify     verify the document against the release content: integrity
             (every declared digest vs recomputed sha-256) and
             attestation (Ed25519 over the canonical payload with the
             declared public key)
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

	payload, err := release.AttestationPayload(src.ID, *version, []release.ContentDigest{{
		Algorithm: release.DigestAlgorithmSHA256,
		Encoding:  release.DigestEncodingBase16,
		Digest:    digest,
	}})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	signature := release.SignAttestation(payload, priv)
	publicKey := release.PublicKeyBase64(priv)

	doc := release.DeriveDocument(src, *version, *location, digest, signature, publicKey)
	// Self-parse guard: never write a document the strict registry parser
	// would reject (TS-016-03-02 review finding; the release pipeline never
	// publishes material it cannot verify).
	if err := release.ValidateDocumentShape(doc); err != nil {
		fmt.Fprintf(os.Stderr, "error: derived document failed the self-parse guard: %v\n", err)
		return 1
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
	fmt.Printf("wrote %s (id %s, version %s, digest %s)\n", *out, doc.ID, doc.Version, digest)
	fmt.Printf("public key (base64): %s\n", publicKey)
	return 0
}

func runVerify(args []string) int {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	document := fs.String("document", "", "path of the produced registry metadata document")
	archive := fs.String("archive", "", "path of the release archive (the release content)")
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
	fmt.Printf("OK: %s %s — integrity (sha-256 %s) and attestation (ed25519) verified\n",
		doc.ID, doc.Version, doc.Trust.ContentDigests[0].Digest)
	return 0
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
