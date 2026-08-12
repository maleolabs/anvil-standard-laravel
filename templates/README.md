# Templates — Laravel

The Templates part of the standard (ADR-021 §5.4): generated content — the
build pipeline template and the configuration extension — supplied as
distribution content served from the installed standard's executable
through the `template` / `extension` commands (A10: distribution content,
not engine content). This document is the human-readable form of the
executable content in
[`internal/laravel/template.go`](../internal/laravel/template.go) (pipeline
definitions) and [`internal/laravel/config.go`](../internal/laravel/config.go)
(config extension); the Go code is the single source of truth.

## Build pipeline template

The standard owns the Laravel build pipeline definition, returned through
the `template` command and written by the runtime to
`.anvil/pipelines/build.yaml` at generation time (ADR-020 §1: framework
knowledge lives in the standard, not the Core binary).

The definition is **derived from the standard's build phase table**
(`internal/laravel/build.go` — the single source of build knowledge):
every build phase becomes one template task with the phase's program as
the command and the phase's arguments as the full task command line
(artisan phases run as `php artisan <args>`). Deriving the template from
the phase table guarantees the generated `build.yaml` always covers the
framework's build steps and can never drift from the steps the standard
executes (TS-018-01-02, Review 19 §3.3) — the template tests lock the
correspondence.

The stage/task layout keeps the structure of the pre-ADR-020 Core template
so existing projects' `build.yaml` stays unchanged and the generated YAML
passes the runtime's pipeline loader validation.

### Stages

The phase-to-stage and phase-to-task mappings (the pre-ADR-020 layout) are
deliberate, test-enforced decisions:

| Phase | Stage | Task | Command |
|---|---|---|---|
| `composer` | `dependencies` | `composer-install` | `composer install --no-dev --optimize-autoloader` |
| `npm` | `assets` | `npm-build` | `npm run build` |
| `config_cache` | `optimize` | `cache-config` | `php artisan config:cache` |
| `route_cache` | `optimize` | `cache-route` | `php artisan route:cache` |
| `view_cache` | `optimize` | `cache-view` | `php artisan view:cache` |

Adding a build phase to the phase table requires a deliberate layout
decision — a stage and a task-name mapping — before the template can ship
it; the completeness test fails on any unmapped phase.

### CI scaffold

The standard also supplies the generic CI scaffold (build + test
placeholder stages) returned through the `template` command and written to
`.anvil/pipelines/ci.yaml`. The CI pipeline is generic placeholder data —
not framework knowledge — but supplying it here keeps the `ci.yaml` output
of framework initializations complete; the runtime no longer owns default
pipeline template data.

## Configuration extension

The standard declares its framework-specific configuration keys under the
`framework.laravel.` namespace (ADR-005 §4.4, 007 §7) through the
`extension` command, and validates provided values through the `validate`
command (C6: the standard validates its own extension values; the runtime
enforces namespace isolation and passes values through). Every declared
key has a validation rule — a declared key can never silently pass
through the unknown-key rejection (test-enforced).

| Key | Description | Default | Validation |
|---|---|---|---|
| `framework.laravel.migrations.path` | Relative path to the Laravel migration files | `database/migrations` | non-empty, relative, no `..` traversal |
| `framework.laravel.cache.store` | Laravel cache store driver | `file` | one of: apc, array, database, file, memcached, redis, dynamodb |
| `framework.laravel.version` | Laravel framework version constraint | — | SemVer `MAJOR.MINOR.PATCH`, non-empty |
| `framework.laravel.php_version` | PHP version constraint | — (optional) | SemVer `MAJOR.MINOR.PATCH` when present |
| `framework.laravel.composer_flags` | Additional composer install flags | — (optional) | whitespace-separated flags; no shell metacharacters; no `--no-dev` |

### Validation rules

- **Migrations path:** must not be empty; must be a relative path;
  traversal segments (`..`) are rejected after path cleaning.
- **Cache store:** must be a known Laravel cache driver.
- **Version / php_version:** SemVer-compatible `MAJOR.MINOR.PATCH`
  (e.g. `11.0.0`); `php_version` is optional (empty is valid).
- **Composer flags:** a safe, whitespace-separated flag list — shell
  metacharacters are rejected (the value is appended to a command line),
  and `--no-dev` is rejected (the build phase already installs without dev
  dependencies — one source of truth for the flag).
- Unknown keys are rejected.

## Template freshness (maintenance practice)

Template content — the build steps and the config extension validation
rules — tracks the framework versions in the standard's support scope
([Compatibility part](../compatibility/compatibility.md)). Freshness is a
**maintainer responsibility** (007 §7; Transition Plan §4.7): the runtime
executes what the standard ships and never patches stale content, so the
standard itself must not rot.

The review triggers — when a maintainer must re-verify this part:

- **A framework version enters the support scope** (or a supported
  version's support ends): verify the build steps and the validation
  rules still match that Laravel version.
- **Laravel changes a build step**: e.g. an asset build migration (Vite
  era) or a new default cache driver — update the build phase table
  (`internal/laravel/build.go`), its template mappings (a new phase needs
  a deliberate stage/task decision), and/or the cache store driver list
  (`internal/laravel/config.go`), then re-run the tests that lock the
  correspondence.
- **Laravel changes a supported runtime prerequisite**: e.g. a minimum
  PHP version — update the `php_version` validation documentation, not
  the rule shape.

Freshness is enforced, not aspirational: the template tests fail when a
build phase lacks a stage/task mapping, and the config tests fail when a
declared key lacks a validation rule. Content changes ship as **new
standard versions** — never silently, never as a Core change
(ADR-021 §3.5, ADR-025 §3.5).

## Wire shape

The `template` command result carries the pipeline definitions in the
exact JSON shape the runtime's pipeline loader parses: the struct field
names and tags are a verbatim mirror of the runtime's pipeline types so
the subprocess contract is preserved byte-compatible across the repository
split (ADR-025 §3.4).
