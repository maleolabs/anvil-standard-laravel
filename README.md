# anvil-standard-laravel

The **Laravel delivery lifecycle standard** for Anvil (ADR-021, ADR-025): the
distributable unit of Laravel framework lifecycle knowledge for the Anvil
Runtime — lifecycle phases, verification rules, configuration extension,
pipeline templates, and the standalone standard executable.

This repository is the home of everything Laravel-specific in the Anvil
ecosystem. The Anvil Core repository (`maleolabs/anvil`) is deliberately
framework-free (ADR-026): it knows that a lifecycle exists, not what the
Laravel lifecycle contains. Framework knowledge ships from here.

## What is a delivery lifecycle standard?

Anvil as a whole is *the* software delivery standard — the system of delivery
lifecycle specification and runtime. Each framework artifact is *an* delivery
lifecycle standard: a distributable standard that defines the lifecycle
content for one framework and must conform to the specification
(ANVIL_V2_TRANSITION_PLAN §5.1). Standards are executed by the Anvil Runtime
as standalone executables through the standard command contract (subprocess,
JSON).

## Repository structure

The repository carries the seven-part standard structure (ADR-021 §5.4):

| Part | Location | What it is |
|---|---|---|
| **Manifest** | [`manifest/`](manifest/) | Standard identity: name, version, target contract version, capability declaration, framework-version support scope |
| **Lifecycle Definition** | [`lifecycle/`](lifecycle/) | Activation and rollback phases and their semantics |
| **Verification** | [`verification/`](verification/) | Structural and lifecycle-conformity verification rules |
| **Templates** | [`templates/`](templates/) | Build pipeline template and configuration extension |
| **Compatibility** | [`compatibility/`](compatibility/) | Declared contract version and supported framework versions |
| **Documentation** | [`docs/`](docs/) | The Laravel lifecycle documentation for adopters |
| **Tests** | Go tests throughout | The standard's own tests, validated at registry acceptance |

The executable implementation lives in [`internal/laravel/`](internal/laravel/)
(the framework lifecycle content) and [`cmd/laravel-adapter/`](cmd/laravel-adapter/)
(the standard executable entrypoint, binary name `anvil-adapter-laravel`).

## Building

```sh
go build -o anvil-adapter-laravel ./cmd/laravel-adapter
```

The binary answers the standard command contract commands: `capabilities`,
`build`, `activate`, `verify`, `extension`, `validate`, `template`, `manifest`
(JSON result on stdout, exit-code convention per the contract).

## Testing

```sh
go test ./...
```

## CI

Every push and pull request against `develop` and `main` runs the
repository's CI pipeline ([`.github/workflows/ci.yml`](.github/workflows/ci.yml)):

1. builds the standard executable (`go build -o anvil-adapter-laravel ./cmd/laravel-adapter`);
2. runs the standard's test suite (`go test -race -count=1 ./...`), gated by
   `go build ./...`, `go vet ./...`, and a `gofmt` check;
3. validates the source manifest (`manifest/registry-metadata.json`) for
   internal consistency via [`scripts/validate-manifest.sh`](scripts/validate-manifest.sh):
   the manifest must parse as JSON and the declared contract version
   (`contractVersion`) must be well-formed semver. Registry parseability is
   deliberately out of scope for the source manifest — the release-time
   fields (`distribution`, `lifecycle`, `trust`) are populated at publication
   by the release pipeline (TS-016-03-02), which is why the source manifest
   is not a strict-parser registry document by design.

**Release gate.** A failing pipeline blocks release production: `develop` and
`main` are protected branches that require this CI to pass before merge, and
release candidates are produced only from a green integration branch (the
release publication pipeline itself is TS-016-03-02 scope).

## Versioning and compatibility

This standard versions independently from the Core runtime (ADR-021 §3.4).
Every release declares the contract version it targets and its
framework-version support scope in the [manifest](manifest/) (ADR-023 §3,
PRD-002 §5.8). See [compatibility/](compatibility/).

## License

[Apache License 2.0](LICENSE)
