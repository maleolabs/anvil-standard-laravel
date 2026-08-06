# Verification — Laravel

The Verification part of the standard (ADR-021 §5.4): the Laravel
verification rules — structural checks and lifecycle-conformity checks.
This document is the human-readable form of the executable checks in
[`internal/laravel/verification.go`](../internal/laravel/verification.go);
the Go code is the single source of truth.

## Checks

The standard declares eight verification checks in its capability
declaration. The runtime invokes only declared checks (`verify` command)
during artifact verification; each check inspects the artifact path — an
extracted directory or an Anvil artifact archive (tar.gz) — and reports a
pass/fail outcome (`name`, `passed`, `details`).

| # | Check | Validates | Rule |
|---|---|---|---|
| 1 | `vendor_present` | `vendor/autoload.php` exists | Composer dependencies are installed and autoloadable |
| 2 | `bootstrap_structure` | `bootstrap/app.php` exists | The application bootstrap is present and structured |
| 3 | `config_files` | `config/app.php` **and** `.env.example` exist | Required configuration files are present |
| 4 | `artisan_file` | `artisan` exists in the artifact root | The artisan CLI entrypoint is present |
| 5 | `composer_json` | `composer.json` exists in the artifact root | The composer manifest is present |
| 6 | `env_file` | `.env` **or** `.env.example` exists | The environment file is present (either form passes) |
| 7 | `app_directory` | `app/` directory exists | The application directory is present |
| 8 | `routes_directory` | `routes/` directory exists | The routes directory is present |

## Semantics

- **All-of vs any-of.** Checks 1–5 and 7–8 require every listed path
  (all-of); check 6 accepts either `.env` or `.env.example` (any-of).
- **Archive handling.** For artifact archives, entries are scanned
  directly — no full extraction is performed. Anvil artifact archives
  store deployable content under the `app/` prefix; both prefixed and
  unprefixed entries are accepted so plain directories and archives behave
  consistently.
- **Directory checks in archives.** A directory is present when an
  explicit directory entry exists or when a regular entry lives beneath
  the directory path (packaging stores only regular files).
- **Unknown checks.** An undeclared check name reports a failed outcome
  with an explanatory message — the runtime never invokes undeclared
  checks (declared-check enforcement is the runtime's responsibility).

## Relationship to the verification contract

These checks implement the structural side of the framework verification
rules for Laravel projects (verification-contract.md, specification
corpus). The outcome shape is aligned with the runtime's check results so
standard outcomes merge into the runtime's verification report without
transformation.
