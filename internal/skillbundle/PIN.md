# Pinned vendored packer — internal/skillbundle + internal/skillpack

## Pin declaration (TS-021-06, 2026-08-13)

The packages under `internal/skillbundle/` (bundle.go, frontmatter.go,
manifest.go, path.go — the pack side only) and `internal/skillpack/`
are a **pinned vendored snapshot** of the Anvil Core reference packer
(`maleolabs.com/anvil`), copied from commit `c08f4b9`
(`docs(planning): add ANVIL-V2.1-S2 follow-up sprint`, the ANVIL-V2.1-S2
base — the same commit that carries the ST-021-03 packer).

## Why vendored, not imported

1. **Go internal rule.** The Core packer lives under
   `maleolabs.com/anvil/internal/`; Go's internal-package rule only allows
   imports from packages inside the `maleolabs.com/anvil/` tree. This
   module's import paths (`maleolabs.com/anvil-standard-laravel/...`) sit
   outside it, so `internal/skillpack` / `internal/skillbundle` cannot be
   imported cross-module.
2. **Not published to the Go module proxy.** `go run
   maleolabs.com/anvil/cmd/skillpack@v2.1.0` would require the module to
   be fetchable (proxy.golang.org returns 404 for `maleolabs.com/anvil`,
   verified 2026-08-13). Vendoring removes the dependency on Core module
   publication for the standard's release pipeline.

## Pin policy

- The **skill-bundle-format contract** (skillbundle.SupportedContractMajor
  = 1, contract version 1.0.0) is the unit of compatibility (ADR-024 §3.1;
  skill-bundle-format.md §4.3). The vendored packer must NOT change while
  the contract major is 1 — a bundle produced here must stay byte-identical
  to what the Core CLI's strict extractor accepts.
- **Re-copy from Core only on a governed format change** (contract major
  bump): copy the pack-side files from
  `maleolabs.com/anvil/internal/skillbundle/{bundle,frontmatter,manifest,path}.go`
  and re-adapt `internal/skillpack` if its API changed, then update this
  file (new Core commit + date).
- The `gopkg.in/yaml.v3` version in `go.mod` is pinned to the version Core
  pins (`v3.0.1`): the vendored frontmatter parser must behave identically.
- The authored skill content lives in `skills/` (skills.json + one
  directory per skill); it is the authoring source, seeded from the Core
  fixture `fixtures/standard-skills/anvil-standard-laravel/skills` at
  TS-021-06.

## Byte-identity guard

`internal/skillpack` uses the SAME deterministic bundle writer as Core
(pinned tar/gzip headers, zeroed ownership/timestamps), so packing the
same content in this repository or in Core produces the same bytes and the
same SHA-256 — the Core real-release E2E test
(`TestSkillInstall_LiveStandardRelease_FixtureParity`) locks this
invariant against the live release.
