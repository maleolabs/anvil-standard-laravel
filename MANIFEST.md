# Manifest — Laravel Delivery Lifecycle Standard

The Manifest part of the standard (ADR-021 §5.4): standard identity, release
version, target contract version, capability declaration, and
framework-version support scope.

## Identity

| Field | Value |
|---|---|
| **id** | `anvil-standard-laravel` |
| **name** | Laravel delivery lifecycle standard |
| **version** | `1.0.0` |
| **target contract version** | `1.0.0` (delivery lifecycle specification, ADR-024 §3.1) |
| **deployment model** | `server` (releases deploy to a server and are activated in place) |
| **framework-version support scope** | Laravel `10.0.0`, `11.0.0`, `12.0.0` |

## Capability declaration

The standard executable (`anvil-adapter-laravel`) answers the standard
command contract (command-contract.md, specification corpus) with the
following declaration (`capabilities` command):

- **Activation phases:** `migrate`, `config_cache`, `route_cache`,
  `event_cache`
- **Build phases:** `composer`, `npm`, `config_cache`, `route_cache`,
  `view_cache`
- **Verification checks:** `vendor_present`, `bootstrap_structure`,
  `config_files`, `artisan_file`, `composer_json`, `env_file`,
  `app_directory`, `routes_directory`
- **Deployment model:** `server`

## Command surface

The standard executable implements the full standard command contract:
`capabilities`, `build`, `activate`, `verify`, `extension`, `validate`,
`template`, `manifest`. The subprocess contract — JSON payload as a single
argument, JSON result on stdout, exit-code convention — is preserved
unchanged from the pre-split adapter contract (ADR-025 §3.4, §12.2).

## Machine-readable manifest

The machine-readable manifest is
[`manifest/registry-metadata.json`](registry-metadata.json). It follows the
registry metadata format of the delivery lifecycle specification
(`docs/specification-corpus/registry-metadata.schema.json`, Core repository):
the same format the Anvil Runtime registry client reads (internal/registry,
EPIC-014). The document is the source manifest of the standard; the
release-time `trust` material (content digests and publisher attestation
over the release artifact) is populated by the standard's release pipeline
at publication (ADR-030, TS-016-03-02) — the values in the source manifest
are format-valid placeholders.

## Versioning

The standard versions independently from the Core runtime (ADR-021 §3.4):
standard releases and runtime releases are decoupled, and every release
declares the contract version it targets and its framework-version support
scope (see [compatibility/](../compatibility/)).
