# Tests — The Standard's Own Tests

The Tests part of the standard (ADR-021 §3.2, Transition Plan §5.4): the
standard's own tests — the checks that validate that the standard behaves
as it declares, validated at registry acceptance (ADR-027 §3). The tests
concern the standard itself, not an adopting project's release —
project-facing checks are the Verification part ([`verification/checks.md`](../verification/checks.md)).

## What is tested

| Area | Location |
|---|---|
| Standard executable behavior (command contract) | `internal/laravel/*_test.go` — the standard's test suite: capability declaration, activation phases (declaration order, migration timing `post_promotion`, cache-warming order, irreversibility, rollback semantics, `view_cache` exclusion), build pipeline, verification checks (8 structural + 4 lifecycle-conformity), config extension + validation, template command, manifest command, exit-code semantics |
| Contract types and shape (internal/laravel ↔ runtime exchange) | `internal/contracts/*_test.go` — the JSON command-contract payloads and capability surface |
| Executable entrypoint | `internal/laravel/binary_test.go` — builds `cmd/laravel-adapter` and exercises the real binary |
| Release pipeline metadata derivation and signing | `internal/release/*_test.go` — registry metadata document derivation (DeriveDocument), shape validation, and signing |
| Source manifest content (007 §4, §8) | `internal/release/source_manifest_test.go` — pins the source manifest's declared identity, target contract version, and framework-version support scope |
| Seven-part structure + manifest + conformance declaration | `tests/standard_structure_test.go` — the seven parts exist (ADR-027 §3 structure bar); the manifest is a consistent registry-metadata-format declaration; the declared contract version is a valid conformance target (ADR-024 §3.1) and agrees across the Manifest and Compatibility parts (conformance bar) |

## Running

```sh
go test -race -count=1 ./...
```

CI runs the same suite on every push and pull request
([`.github/workflows/ci.yml`](../.github/workflows/ci.yml)), together with
`go build ./...`, `go vet ./...`, the `gofmt` check, and the source
manifest validation ([`scripts/validate-manifest.sh`](../scripts/validate-manifest.sh)).

## Registry acceptance

Registry acceptance validates a standard on four bars — structure,
conformance, tests, and maintainership (ADR-027 §3; 007 §2). The content
side of that bar is carried here:

- **Structure** — `TestSevenPartStructureExists` proves all seven parts
  are present in the repository.
- **Conformance** — the declared contract version (`1.0.0`) is a valid
  conformance target (well-formed semver, major `1` — the compatibility
  unit, ADR-024 §3.1), declared identically in the machine manifest
  (`manifest/registry-metadata.json`), the Compatibility part, and the
  human-readable Manifest part (`MANIFEST.md`); the executable's contract
  surface is exercised by the command-contract tests above. The registry
  re-validates conformance mechanically at acceptance (ADR-023, EPIC-014).
- **Tests** — the standard's own suite passes (this repository's CI green
  gate) and is the Tests part the registry validates at acceptance.
- **Maintainership** — declared and accountable in the
  [Manifest](../MANIFEST.md) (Maintainership section).

The Tests part is the standard's first responsibility toward the
registry-acceptance bar ([007 §2](https://github.com/maleolabs/forge-anvil-cli/blob/develop/docs/architecture/007-delivery-lifecycle-standard-specification.md));
registry validation mechanics themselves belong to the registry (EPIC-014,
ADR-023) and are not implemented in this repository.
