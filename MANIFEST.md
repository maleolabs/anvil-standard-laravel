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
following declaration (`capabilities` command). The declaration is the
standard's complete lifecycle surface per 007 §4 — the runtime invokes
only declared capability; undeclared capability is never called.

### Lifecycle phases (activation)

The activation phases run in declaration order, each as
`php artisan <args>` from the release's working directory:

| # | Phase | Group | Command | Irreversible |
|---|---|---|---|---|
| 1 | `migrate` | migration | `php artisan migrate --force` | no — rollback: `php artisan migrate:rollback --force` |
| 2 | `config_cache` | cache warming | `php artisan config:cache` | yes |
| 3 | `route_cache` | cache warming | `php artisan route:cache` | yes |
| 4 | `event_cache` | cache warming | `php artisan event:cache` | yes |
| 5 | `queue_restart` | queue | `php artisan queue:restart` | yes |

- **Migration timing.** Migrations run at the declared **post-promotion**
  timing (`post_promotion`, `MigrationTiming()` in
  `internal/laravel/activation.go`): in the `server` deployment model the
  release is promoted to `active` **before** the activation phases run,
  and the migration phase executes against the promoted release.
- **Rollback semantics.** The migration phase is reversed by
  `php artisan migrate:rollback --force` (force-confirmed because the
  standard executes artisan non-interactively); the cache and queue
  phases are irreversible — a rollback reports an informational result
  and never blocks on them. Full per-phase failure and rollback
  semantics: [lifecycle/definition.md](lifecycle/definition.md).

### Build phases

`composer`, `npm`, `config_cache`, `route_cache`, `view_cache` — in build
execution order.

### Verification checks

`vendor_present`, `bootstrap_structure`, `config_files`, `artisan_file`,
`composer_json`, `env_file`, `app_directory`, `routes_directory`,
`shared_resource_wiring`, `migration_timing`, `queue_restart`,
`rollback_behavior` — the structural verification rules (the preserved
v1.x surface) plus the lifecycle-conformity rules (TS-018-03-01:
shared-resource wiring, migration timing relative to promotion, queue
restart, rollback behavior; ADR-033 §3)
([verification/checks.md](verification/checks.md)).

### Config extensions

`framework.laravel.migrations.path`, `framework.laravel.cache.store`,
`framework.laravel.version`, `framework.laravel.php_version`,
`framework.laravel.composer_flags` — declared by the `extension` command
and validated by the `validate` command under the `framework.laravel.`
namespace (ADR-005 §4.4; [templates/README.md](templates/README.md)).

### Templates

The Laravel build pipeline definition (derived from the build phase
table) and the generic CI scaffold, returned through the `template`
command ([templates/README.md](templates/README.md)).

### Deployment model

`server` — releases deploy to a server and are activated in place
(ADR-016).

## Command surface

The standard executable implements the full standard command contract:
`capabilities`, `build`, `activate`, `verify`, `extension`, `validate`,
`template`, `manifest`. The subprocess contract — JSON payload as a single
argument, JSON result on stdout, exit-code convention — is preserved
unchanged from the pre-split adapter contract (ADR-025 §3.4, §12.2).

## Machine-readable manifest

The machine-readable manifest is
[`manifest/registry-metadata.json`](manifest/registry-metadata.json). It
follows the registry metadata format of the delivery lifecycle
specification (`docs/specification-corpus/registry-metadata.schema.json`,
Core repository): the same format the Anvil Runtime registry client reads
(internal/registry, EPIC-014). The document is the source manifest of the
standard; the release-time `trust` material (content digests and publisher
attestation over the release artifact) is populated by the standard's
release pipeline at publication (ADR-030, TS-016-03-02) — the values in
the source manifest are format-valid placeholders.

## Versioning

The standard versions independently from the Core runtime (ADR-021 §3.4):
standard releases and runtime releases are decoupled, and every release
declares the contract version it targets and its framework-version support
scope (see [compatibility/compatibility.md](compatibility/compatibility.md)).
