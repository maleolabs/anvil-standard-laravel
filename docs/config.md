# Laravel Configuration Keys

The Laravel adapter declares framework-specific configuration keys under the **`framework.laravel.`** namespace. The keys are declared and validated by the adapter; Anvil's Core enforces namespace isolation (`framework.<framework>.` only).

## Keys

| Key | Type | Default | Validation rule |
|---|---|---|---|
| `framework.laravel.migrations.path` | string | `database/migrations` | Non-empty **relative** path; absolute paths rejected; no `..` traversal segments |
| `framework.laravel.cache.store` | string | `file` | One of the known Laravel cache drivers: `apc`, `array`, `database`, `file`, `memcached`, `redis`, `dynamodb` |
| `framework.laravel.version` | string (SemVer) | — (no default) | Must be SemVer `MAJOR.MINOR.PATCH`, e.g. `11.0.0` |
| `framework.laravel.php_version` | string (SemVer) | — (optional) | Empty is valid; otherwise SemVer `MAJOR.MINOR.PATCH`, e.g. `8.3.0` |
| `framework.laravel.composer_flags` | string | — (optional) | Whitespace-separated Composer flags; **no shell metacharacters**; **no `--no-dev`** (the build phase already excludes dev dependencies) |

## Validation examples

| Key | Value | Result |
|---|---|---|
| `framework.laravel.cache.store` | `redis` | Valid |
| `framework.laravel.cache.store` | `memcache` | Invalid — `"memcache" is not a known Laravel cache store` |
| `framework.laravel.migrations.path` | `/abs/path` | Invalid — `must be a relative path` |
| `framework.laravel.version` | `11` | Invalid — `not valid SemVer (expected MAJOR.MINOR.PATCH)` |
| `framework.laravel.composer_flags` | `--prefer-dist; rm -rf /` | Invalid — `contains shell metacharacters` |
| `framework.laravel.composer_flags` | `--no-dev` | Invalid — `must not contain --no-dev` |
| `framework.laravel.composer_flags` | `--prefer-dist --no-interaction` | Valid |

Unknown keys under the namespace are rejected.

## Runtime validation from the installed standard

Independent of the adapter's `validate` command (which owns the framework's
value rules, C6), the runtime enforces the **installed delivery lifecycle
standard's config extension rules** on the framework section
(TS-015-03-02, ADR-026 decision 2): `anvil config validate` — and the
config command family (`config get` / `config list` / `config levels`) —
validates `framework.<name>.*` keys against the standard's declared rules
(required declarations enforced; values must be strings, the contract
extends no other value type). Errors identify the offending
fully-qualified key and the expected format; a declared framework without
an installed standard hard-fails with the install remediation (ADR-026
decision 3). No hardcoded framework validation rules exist in the runtime.

When the installed standard declares no config extension content, the
framework section passes through (a standard may declare nothing in a
category — command-contract §4.1). See [limitations](../limitations.md)
item 6.

## Status: reserved, not yet effective

**Note:** these keys are recognized and validated, but not yet consumed anywhere.

- There is **no dedicated `framework.laravel:` section in `anvil.yaml`** when the installed delivery lifecycle standard supplies no config extension content — `anvil init --framework laravel` writes the section only when the resolved standard's record carries content (keys with declared defaults, TS-015-03-01); see [limitations](../limitations.md) item 3
- The values are **not used** by the build pipeline, activation phases, or any other flow yet
- They are declared by the adapter's `extension` command and validated by its `validate` command — the plumbing exists, the consumers do not

Until consumption lands, treat these keys as a **reserved contract**: their names, defaults, and validation rules are stable, but setting them has no effect on behavior. Tracked in [limitations](../limitations.md).

See also: [Build pipeline](build.md) · [Deploy](deploy.md) · [Glossary](../glossary.md)
