# Templates — Laravel

The Templates part of the standard (ADR-021 §5.4): generated content — the
build pipeline template and the configuration extension. This document is
the human-readable form of the executable content in
[`internal/laravel/template.go`](../internal/laravel/template.go) (pipeline
definitions) and [`internal/laravel/config.go`](../internal/laravel/config.go)
(config extension); the Go code is the single source of truth.

## Build pipeline template

The standard owns the Laravel build pipeline definition, returned through
the `template` command and written by the runtime to
`.anvil/pipelines/build.yaml` at generation time (ADR-020 §1: framework
knowledge lives in the standard, not the Core binary). The definition
mirrors the commands of the standard's build phase table (single source of
build knowledge).

The stage/task layout keeps the structure of the pre-ADR-020 Core template
so existing projects' `build.yaml` stays unchanged and the generated YAML
passes the runtime's pipeline loader validation.

### Stages

| Stage | Task | Command |
|---|---|---|
| `dependencies` | `composer-install` | `composer install --no-dev --optimize-autoloader` |
| `assets` | `npm-build` | `npm run build` |
| `optimize` | `cache-config` | `php artisan config:cache` |
| `optimize` | `cache-route` | `php artisan route:cache` |
| `optimize` | `cache-view` | `php artisan view:cache` |

### CI scaffold

The standard also supplies the generic CI scaffold (build + test
placeholder stages) returned through the `template` command and written to
`.anvil/pipelines/ci.yaml`. The CI pipeline is generic placeholder data —
not framework knowledge — but supplying it here keeps the `ci.yaml` output
of framework initializations complete; the runtime no longer owns default
pipeline template data.

## Configuration extension

The standard declares its framework-specific configuration keys under the
`framework.laravel.` namespace (ADR-005 §4.4) through the `extension`
command, and validates provided values through the `validate` command. The
runtime enforces namespace isolation when registering the extension; the
standard owns the value validation rules.

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

## Wire shape

The `template` command result carries the pipeline definitions in the
exact JSON shape the runtime's pipeline loader parses: the struct field
names and tags are a verbatim mirror of the runtime's pipeline types so
the subprocess contract is preserved byte-compatible across the repository
split (ADR-025 §3.4).
