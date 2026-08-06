// Package release implements the publisher-side release pipeline of the
// Laravel delivery lifecycle standard (TS-016-03-02; ADR-023, ADR-030):
//
//   - deriving the publishable registry metadata document from the source
//     manifest (manifest/registry-metadata.json);
//   - computing the REAL content digests over the release archive (the
//     source manifest's trust fields are format-valid placeholders and are
//     replaced at publication);
//   - producing the publisher attestation over the canonical payload, in
//     the exact shape the Anvil Runtime registry client consumes (Core
//     internal/registry/trust.go; registry-metadata.schema.json).
//
// The package is release-time infrastructure only: it is compiled into the
// release tooling (cmd/release-sign), never into the standard executable
// (cmd/laravel-adapter) and never shipped in a release artifact.
//
// Trust baseline (PM decision D-01; registry-metadata.schema.json):
//
//   - integrity — SHA-256 content digest(s), base16 (lowercase hex) is the
//     canonical default encoding;
//
//   - attestation — an Ed25519 signature over the canonical payload
//
//     utf8(id) || 0x00 || utf8(version) || 0x00 ||
//     concat(decoded digest bytes in contentDigests array order)
//
//     where 0x00 is a single NUL byte; consumers verify the signature over
//     exactly these bytes, byte-for-byte (trust.go: attestationPayload);
//
//   - the publisher's Ed25519 verification public key, base64-encoded
//     (RFC-4648 standard with padding), carried in the document.
//
// Reference: TS-016-03-02, ADR-022 §3, ADR-023 §3, ADR-030 §3
package release
