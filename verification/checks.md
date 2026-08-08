# Verification — Laravel

The Verification part of the standard (ADR-021 §5.4; 007 §6): the Laravel
verification rules in two categories — **structural checks** (the
preserved v1.x surface: files and structures exist) and
**lifecycle-conformity checks** (framework behavior at lifecycle points:
shared-resource wiring, migration timing relative to promotion, queue
restart, rollback behavior — Review 19 §3.3, ADR-033 §3, TS-018-03-01).
This document is the human-readable form of the executable checks in
[`internal/laravel/verification.go`](../internal/laravel/verification.go);
the Go code is the single source of truth.

The verification positions are fixed by the lifecycle specification, the
checks executing there are declared content (command-contract.md §4.2:
`prepare → configure → framework phases → verify → promote`): the
structural checks run at release install; the lifecycle-conformity
checks run at the **post-activation verify stage**, immediately before
the atomic promotion — exactly when the release directory carries the
post-activation evidence they inspect.

## Checks

The standard declares **twelve verification checks** in its capability
declaration. The runtime invokes only declared checks (`verify` command)
during artifact verification; each check inspects the artifact path — an
extracted directory or an Anvil artifact archive (tar.gz) — and reports a
pass/fail outcome (`name`, `passed`, `details`).

### Structural checks (preserved v1.x surface)

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

### Lifecycle-conformity checks (v2 verification depth)

| # | Check | Validates | Rule |
|---|---|---|---|
| 9 | `shared_resource_wiring` | The shared cache store the release declares (`.env` `CACHE_STORE`, else the `config/cache.php` default) equals the store the release runs with (the compiled config cache default after activation) when **both** are re-checkable from the artifact, and the runtime store is a known driver wired in the `config/cache.php` stores map | The shared resource is wired for the release; a declared store that drifts from the compiled configuration is a mis-wired release; when the artifact `.env` carries no `CACHE_STORE` (production shape — the real `.env` is shared-linked on the server) the outcome states the comparison is not re-checkable instead of claiming a match |
| 10 | `migration_timing` | Re-checkable post-promotion migration evidence: the compiled config cache (the post-activation marker) is present and the migration set exists at the declared migrations path (`database/migrations`) | Migrations ran at the declared **post-promotion** timing (`post_promotion`, [lifecycle/definition.md](../lifecycle/definition.md)): the release carries the post-activation state and the migration set the phase applies |
| 11 | `queue_restart` | The queue restart signal (cache key `illuminate:queue:restart` — Laravel 10/11/12 `RestartCommand`) is present in the release's file cache store when the store is `file`; for any other **known** driver the evidence location is the shared store, declared in the outcome; an unknown store fails closed | The queue was restarted after activation (`queue:restart`, the last declared activation phase) |
| 12 | `rollback_behavior` | Every activation phase declares rollback coverage (a rollback command when reversible, the irreversible marker when not), the migration rollback is the force-confirmed `migrate:rollback --force`, and the manifest rollback metadata matches the executable phase table — a **standard-internal drift guard** (executable table vs manifest command surface of the same binary), not artifact manifest (ADR-017) evidence | Rollback produces the declared state; irreversible phases never block rollback |

## Semantics

### Structural checks

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

### Lifecycle-conformity checks

The lifecycle-conformity checks verify **re-checkable evidence embedded
in the release artifact** — the compiled configuration, the migration
set, the file-cache restart signal, the declared phase table — so any
consumer (the runtime, a CI pipeline, a human operator) can re-run the
check and derive the same outcome. Evidence that cannot be re-checked is
invalid (verification-contract.md §5.4 E2/E5): when the evidence is
missing or unreadable the check **fails closed** — it never passes on a
claim.

**`shared_resource_wiring`.** The standard declares the cache store as
the shared resource it knows about (`framework.laravel.cache.store`,
default `file`). The check resolves three artifact-embedded facts:

1. The **declared store**: `CACHE_STORE` in `.env`, else the
   `config/cache.php` default (`env('CACHE_STORE', …)` fallback or a
   literal default).
2. The **runtime store**: the `cache.default` of the compiled config
   cache (`bootstrap/cache/config.php` — written by the `config:cache`
   activation phase), else the declared store.
3. The **wiring**: the store keys declared in the `config/cache.php`
   `stores` map.

The check fails when both the declared and the runtime store are
re-checkable from the artifact and differ (after `config:cache`, Laravel
serves the compiled value, not `.env` — the classic "declared redis,
running file" drift), when the runtime store is not a known Laravel
driver, or when the runtime store has no entry in the `stores` map (not
wired). When the artifact `.env` carries no `CACHE_STORE` — the normal
production shape, where the real `.env` is shared-linked on the server at
install — the declared-vs-runtime comparison is **not re-checkable**: the
check verifies and reports the runtime store's wiring and states the
comparison could not be made; it never claims a match that was not
verified. A release without `config/cache.php` cannot be re-checked and
fails closed, and a compiled config cache whose cache default cannot be
extracted fails closed (evidence exists but is not re-checkable — never
a silent fallback to the declared store).

**`migration_timing`.** The standard declares migrations as the **first**
activation phase at **post-promotion** timing (`MigrationTiming()` →
`post_promotion`); the `config:cache` phase runs after it, so the
compiled config cache is the release's post-activation marker. The check
requires that marker plus the migration set at the declared migrations
path (`database/migrations` — the standard's default for
`framework.laravel.migrations.path`): the directory must exist (a
missing directory means migrations were stripped from the release and
the post-promotion phase would apply nothing), and every file inside
must be a PHP migration. An empty migrations directory passes — the
migration phase is then a declared no-op; the outcome reports the
evidence found, so the timing declaration (`post_promotion`) and the
evidence merge into the runtime's verification report as re-checkable
lifecycle evidence.

**`queue_restart`.** The standard declares `queue:restart` as the last
activation phase; Laravel's `RestartCommand` writes the restart signal to
the shared cache store under the key `illuminate:queue:restart` on every
framework version in the support scope (Laravel 10/11/12 — the legacy
`laravel_database_queues_restart` shape is Laravel ≤8). With the `file`
store — the standard's declared default shared store — the signal is a
file inside the release's file cache store
(`storage/framework/cache/data/…`, path derived from sha1 of the bare key
exactly like Laravel's `FileStore::path`; Laravel 10/11/12 file stores
carry no cache prefix) and the check verifies it directly. With any other
**known** store the signal lives in that shared store, external to the
release directory: the check verifies the store determination and
declares the evidence location (store + key) in the outcome — the
recorded `queue_restart` activation outcome is the runtime's lifecycle
evidence there, and the signal is re-checkable in the shared store. When
the store cannot be determined, or is not a known Laravel driver, the
check fails closed — the evidence location cannot be re-checked.

**`rollback_behavior`.** The check verifies that rollback produces the
declared state ([lifecycle/definition.md](../lifecycle/definition.md)):
every activation phase declares rollback coverage — a reversible phase
carries a rollback command, an irreversible phase is marked irreversible
and carries none (the adapter reports an informational success that
never blocks rollback, TS-P7-10 AC-2); the migration rollback is the
declared force-confirmed `migrate:rollback --force` (non-interactive
production rollback would otherwise be cancelled by Laravel's
`ConfirmableTrait`); and the manifest rollback metadata
(`RollbackCommands()`) matches the executable phase table — the two
surfaces must not diverge. This is a **standard-internal drift guard**:
it compares the executable phase table with the manifest command surface
of the same standard binary, catching internal divergence (e.g. a future
change that drops `--force` from one surface). It does **not** read the
artifact's embedded manifest (ADR-017) — that manifest is not part of
this check's evidence. The outcome reports the derived per-phase
rollback table, which any consumer can re-derive from the declared phase
table.

## Relationship to the verification contract

These checks implement the Laravel standard's verification content
against the verification contract (verification-contract.md,
specification corpus): gate semantics and evidence requirements are the
contract's — gates remain mandatory and unskippable, outcomes merge into
the runtime's verification report and are recorded as lifecycle evidence,
and evidence is re-checkable, never merely claimed. The standard **adds**
checks; it never weakens gates (ADR-033 §3; contract rule G4). The
outcome shape is aligned with the runtime's check results so standard
outcomes merge into the runtime's verification report without
transformation.

## Known limitations

- `shared_resource_wiring` and `queue_restart` read the standard's
  declared evidence shapes: the Laravel-shipped `config/cache.php`
  (the `env('CACHE_STORE', …)` default and the `'name' => [`
  stores-map entries), the compiled `bootstrap/cache/config.php`, and
  the default file-store path `storage/framework/cache/data`. Projects
  that heavily customize these shapes may produce outcomes the check
  cannot parse — the check fails closed rather than guessing. A
  compiled config cache that is present yet unparseable (e.g. a
  double-quoted shape) fails closed: the evidence exists but is not
  re-checkable, so the check never silently falls back to the declared
  store.
- `migration_timing` verifies the standard's default declared migrations
  path (`database/migrations`); a project that overrides
  `framework.laravel.migrations.path` with a custom value cannot be
  verified by this check (the check receives only the artifact path —
  project configuration is not part of the verification exchange).
- `queue_restart` verifies the file-store signal at the default file
  cache path; a customized file-store path is not re-checkable from the
  artifact.
- `rollback_behavior` is a standard-internal drift guard: it validates
  the declared rollback semantics of the standard's own surfaces (phase
  table vs manifest command metadata). It does not read the artifact's
  embedded manifest (ADR-017) — artifact-manifest verification is the
  runtime's integrity gate, not this check's evidence.
