# Verification Checks

The Laravel adapter declares **12 verification checks**: 8 structural checks that validate that an artifact looks like a deployable Laravel application before it is installed on a server, plus 4 lifecycle-conformity checks (TS-018-03-01) that verify framework behavior at lifecycle points against re-checkable evidence embedded in the release.

## Where the checks run

The verification positions are fixed by the lifecycle specification; the checks executing there are declared content (command-contract.md §4.2: `prepare → configure → framework phases → verify → promote`):

1. **Structural checks (1–8)** run during **Release install** on the server — `anvil server release install <project-id> <artifact>` (and its equivalent `anvil deployment install <target-id> <artifact>`): generic artifact integrity verification must pass first (`artifact must be verified first` otherwise), then every declared structural check runs against the artifact; **all 8 must pass** or the install fails with:

```text
adapter verification failed: <check-name>: <details>; ...
```

2. **Lifecycle-conformity checks (9–12)** run at the **post-activation verify stage** of the activation sequence — immediately before the atomic promotion, after the framework activation phases ran (command-contract.md §4.2). That is exactly when the release directory carries the post-activation evidence they inspect (compiled config cache, file-cache restart signal). They also run against a release directory through `anvil artifact verify` (below).

If the adapter binary is missing, install fails earlier with `adapter executable "anvil-adapter-laravel" not found on PATH ...`.

## The checks

### Structural checks (preserved v1.x surface)

| # | Check | Validates | Failure message (details) |
|---|---|---|---|
| 1 | `vendor_present` | `vendor/autoload.php` exists — Composer dependencies are installed and autoloadable | `missing required file(s): vendor/autoload.php` |
| 2 | `bootstrap_structure` | `bootstrap/app.php` exists — application bootstrap is present | `missing required file(s): bootstrap/app.php` |
| 3 | `config_files` | `config/app.php` **and** `.env.example` both exist | `missing required file(s): config/app.php, .env.example` |
| 4 | `artisan_file` | `artisan` CLI entrypoint exists at the project root | `missing required file(s): artisan` |
| 5 | `composer_json` | `composer.json` exists at the project root | `missing required file(s): composer.json` |
| 6 | `env_file` | `.env` **or** `.env.example` exists (either one passes) | `neither .env nor .env.example found` |
| 7 | `app_directory` | `app/` directory exists | `missing required directory: app` |
| 8 | `routes_directory` | `routes/` directory exists | `missing required directory: routes` |

### Lifecycle-conformity checks (TS-018-03-01)

These checks verify the v2 verification depth (ADR-033 §3; Review 19 §3.3) against re-checkable evidence **embedded in the release artifact** — any consumer can re-run the check and derive the same outcome; missing or unreadable evidence fails the check closed (a claim is not evidence, verification-contract.md §5). Full rule and evidence semantics: [verification/checks.md](../verification/checks.md).

| # | Check | Validates | Failure message (details) |
|---|---|---|---|
| 9 | `shared_resource_wiring` | The shared cache store the release declares (`CACHE_STORE` in `.env`, else the `config/cache.php` default) matches the store the release runs with (the compiled `bootstrap/cache/config.php` default after `config:cache`) and is wired in the `config/cache.php` stores map. When the artifact `.env` carries no `CACHE_STORE` (the production shape — the real `.env` is shared-linked on the server), the runtime store is verified and wired and the outcome states that the declared-vs-runtime comparison is not re-checkable from the artifact instead of claiming a match | `declared cache store "redis" does not match the store the release runs with ("file" in bootstrap/cache/config.php): the shared resource is not wired as declared …` |
| 10 | `migration_timing` | Post-promotion migration evidence: the compiled config cache (post-activation marker) is present and the migration set exists at the declared migrations path (`database/migrations`) | `no compiled config cache (bootstrap/cache/config.php): the release carries no post-activation state …` / `declared migrations path database/migrations is missing from the release …` |
| 11 | `queue_restart` | The queue restart signal (cache key `illuminate:queue:restart`, Laravel 10/11/12 `RestartCommand`) is present in the release's file cache store when the store is `file`; for other **known** drivers the evidence location (shared store + key) is declared in the outcome; an unknown store fails closed | `no queue restart signal in the file cache store: expected evidence at storage/framework/cache/data/… — the queue was not restarted after activation` |
| 12 | `rollback_behavior` | Every activation phase declares rollback coverage, the migration rollback is the force-confirmed `migrate:rollback --force`, and the manifest rollback metadata matches the phase table — a **standard-internal drift guard** (the executable table vs the manifest command surface of the same binary), not artifact manifest evidence (ADR-017) | `migrate rollback command "migrate:rollback" does not match the declared force-confirmed rollback "migrate:rollback --force" …` |

The checks work against both extracted directories and artifact archives (tar.gz) — no full extraction is performed for archive inspection.

## Practical notes

- Because `vendor/` must pass check #1, the artifact must contain `vendor/`. Note that `anvil init --framework laravel` no longer writes a framework-specific artifact configuration — the Core applies framework-agnostic compiled defaults (TS-015-01-03), and the compiled `artifact.exclude` default strips `vendor/**`, so new Laravel projects package **without** `vendor/` until a mechanism that can supply framework artifact defaults exists. Config extension content (TS-015-03-01) and template content (TS-015-02-03) do **not** close the gap: extension content carries framework-namespaced config keys only (`framework.<name>.*`) and template content supplies pipeline files only — neither touches the Core-owned `artifact.exclude` key; the closing mechanism is standard content extraction (EPIC-016/EPIC-018; see [limitations item 6](https://github.com/maleolabs/forge-anvil-cli/blob/develop/wiki/limitations.md)). Today, restore `vendor/` by overriding the exclude list in `anvil.yaml`: set `artifact.exclude` to the compiled defaults minus `vendor/**` (do **not** use `artifact.include: [vendor/**]` — a non-empty include list acts as a strict whitelist and would drop every non-vendor file). Without `vendor/` in the artifact, install fails on `vendor_present`.
- Check #6 accepts `.env.example` so CI-built artifacts without real secrets can still pass. On the server, link the real `.env` via `--shared-link from=shared/config/.env,to=.env` at project registration (see [deploy.md](deploy.md)).
- Checks #9–#12 verify lifecycle evidence and run at the post-activation verify stage (command-contract.md §4.2), when the release directory has been through activation (compiled config cache, file-cache restart signal). The compiled config cache is also produced by the build pipeline (`config:cache` build phase), so artifact archives carry part of the evidence already; a pre-activation artifact without it is reported honestly (drift not re-checkable), never as a verified match.

## CLI verification: `anvil artifact verify`

`anvil artifact verify <path>` runs the **generic integrity checks** — archive validity, manifest presence/content, and checksum match — and then the adapter-declared framework checks (the structural checks above, and the lifecycle-conformity checks when `<path>` is a release directory carrying the post-activation evidence) when the active project declares a framework and the adapter is installed (`runFrameworkVerification`, 005-adapter-command-contract §4, TS-P7-11). The adapter is optional (ADR-009 §9.7): without a framework, or with a missing adapter executable, verification runs the generic checks only (a warning is printed when the executable is missing). A present but failing adapter fails the verification with a non-zero exit. See [limitations](https://github.com/maleolabs/forge-anvil-cli/blob/develop/wiki/limitations.md).

See also: [Deploy](deploy.md) — install flow · [Manifest](manifest.md)
