# Lifecycle Definition — Laravel

The Lifecycle Definition part of the standard (ADR-021 §5.4): the Laravel
lifecycle content — activation and rollback phases, their declared order,
and their per-phase failure and rollback semantics. This document is the
human-readable form of the executable phase table in
[`internal/laravel/activation.go`](../internal/laravel/activation.go) — the
Go code is the single source of truth; this document is the adopters'
reference. Phase names, order, grouping, migration timing, failure
semantics, and rollback semantics are declared there and must not diverge.

Content implements the standard command contract of the delivery lifecycle
specification (EPIC-013): the exchange payloads (`ActivationRequest`,
`ActivationResult`, capability declaration) live in
`internal/contracts/`; this standard declares no contract definitions.

## Activation phases

Release activation runs the declared activation phases in declaration
order, each as `php artisan <args>` from the release's working directory
(the release context `working_dir` passed by the runtime). The phases are
grouped and ordered by group: **migration → cache warming → queue**:

| # | Group | Phase | Command | Reversible |
|---|---|---|---|---|
| 1 | migration | `migrate` | `php artisan migrate --force` | ✅ rollback: `php artisan migrate:rollback` |
| 2 | cache warming | `config_cache` | `php artisan config:cache` | ❌ irreversible |
| 3 | cache warming | `route_cache` | `php artisan route:cache` | ❌ irreversible |
| 4 | cache warming | `event_cache` | `php artisan event:cache` | ❌ irreversible |
| 5 | queue | `queue_restart` | `php artisan queue:restart` | ❌ irreversible |

### Migration timing relative to promotion

Migrations run at the declared **post-promotion** timing
(`MigrationTiming()` → `post_promotion`): in the server deployment model
(ADR-016) the release directory becomes the live application, so the
release is promoted to `active` **before** the activation phases run, and
the migration phase therefore executes against the promoted release. A
failed migration leaves the release recorded as active — the activation is
re-run to converge: `migrate --force` is idempotent per migration record
(already-applied migrations are skipped), so re-execution converges to the
intended schema.

### Cache warming order and irreversibility

The cache-warming group runs in the declared order **`config_cache` →
`route_cache` → `event_cache`**: configuration is cached first because the
route and event caches compile against the application configuration; the
order is part of the declared contract and is pinned by the executable
table (tests assert it).

Every cache-warming phase is **irreversible**: a rollback cannot restore
the previous release's caches — the previous release's own activation
regenerates them from its code. Caches reflect exactly one release's code;
they are never repaired, only regenerated.

### Queue restart

The `queue_restart` phase recycles the queue workers **after** activation:
`php artisan queue:restart` signals every running worker to restart once
its current job finishes, so workers pick up the new code, migrations, and
caches. It is the **last** declared phase: worker recycling runs only after
migration and cache warming completed, so no worker ever processes a job
against stale migrations or caches. The signal is **irreversible** (it
cannot be un-signaled) — see rollback semantics below.

## Per-phase failure semantics

Every phase reports a structured outcome (`success`, `output`, `error`)
through the activation contract; the JSON result is authoritative for the
phase outcome. A failing phase fails the activation — the runtime records
the failure and the operator re-runs activation to converge (phases are
re-executed from the start). Per phase:

| Phase | Failure semantics |
|---|---|
| `migrate` | A failing migration fails the activation. The release stays recorded as active (post-promotion timing); re-running the activation re-executes the phase — Laravel skips migrations already recorded as applied, so re-execution converges to the intended schema. |
| `config_cache` / `route_cache` / `event_cache` | A failing cache phase fails the activation. Re-running the activation regenerates the cache from the release's code — caches reflect exactly one release and are never repaired, only regenerated. |
| `queue_restart` | A failing queue restart fails the activation. Re-running the activation re-sends the restart signal — worker recycling is idempotent: workers already running the new release simply receive the signal again. |

The executable table carries these semantics per phase
(`phase.failureSemantics`); tests assert every phase declares non-empty
failure semantics.

## Rollback semantics

The rollback operation reverses the reversible phases and documents the
irreversible ones as informational — **irreversibility never blocks
rollback**:

| Phase | Rollback behavior |
|---|---|
| `migrate` | Runs `php artisan migrate:rollback` — reverses the migration batch applied by the activation. |
| `config_cache` / `route_cache` / `event_cache` | Irreversible — no command executed; the adapter reports an informational success documenting that the cache cannot be undone. The restored release's own activation regenerates its caches from its code. |
| `queue_restart` | Irreversible — no command executed; the adapter reports an informational success documenting that the restart signal cannot be un-sent. The restored release's own activation re-signals its workers. |

Rollback order (reverse activation order) is the orchestrator's
responsibility; the phase table provides each phase's own rollback
operation, and irreversible phases always return an informational success
so the orchestrator's rollback never blocks on them.

## Manifest command metadata

The `manifest` command returns the full command strings stored in the
artifact manifest at packaging time (ADR-017) and executed by the
orchestrator during release activation and rollback:

- **Activation commands:** `php artisan migrate --force`,
  `php artisan config:cache`, `php artisan route:cache`,
  `php artisan view:cache`, `php artisan queue:restart` (in execution
  order)
- **Rollback commands:** `php artisan migrate:rollback`

The manifest metadata surface carries `view:cache` in the activation
command list (TS-P7-15 AC-3) — the metadata form differs from the
executable phase table by design (`view:cache` vs `event:cache`, TD-012);
the queue restart command appears in both surfaces, always last.
