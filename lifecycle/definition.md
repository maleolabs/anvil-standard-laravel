# Lifecycle Definition — Laravel

The Lifecycle Definition part of the standard (ADR-021 §5.4): the Laravel
lifecycle content — activation and rollback phases and their semantics. This
document is the human-readable form of the executable phase table in
[`internal/laravel/activation.go`](../internal/laravel/activation.go) — the
Go code is the single source of truth; this document is the adopters'
reference.

## Activation phases

Release activation runs the declared activation phases in declaration
order, each as `php artisan <args>` from the release's working directory
(the release context `working_dir` passed by the runtime):

| # | Phase | Command | Reversible |
|---|---|---|---|
| 1 | `migrate` | `php artisan migrate --force` | ✅ rollback: `php artisan migrate:rollback` |
| 2 | `config_cache` | `php artisan config:cache` | ❌ irreversible |
| 3 | `route_cache` | `php artisan route:cache` | ❌ irreversible |
| 4 | `event_cache` | `php artisan event:cache` | ❌ irreversible |

### Semantics

- Each phase reports a structured outcome (`success`, `output`, `error`)
  through the activation contract; the JSON result is authoritative for the
  phase outcome.
- The migration phase is applied **before** cache warming: the caches must
  reflect the migrated application.
- **Irreversible phases.** The cache phases' effects cannot be undone by a
  rollback — the previous release's caches cannot be restored because the
  previous release's own activation regenerates them from its code. A
  rollback request on an irreversible phase returns an informational
  success that does **not** block the rollback.
- **`view_cache` is deliberately absent from the activation table.**
  `php artisan view:cache` is not reversible as a rollback operation, so
  the executable activation pipeline cannot include it. It exists as a
  build phase (`build` command) and appears in the manifest command
  metadata surface.

## Rollback semantics

| Operation | Behavior |
|---|---|
| `migrate` rollback | Runs `php artisan migrate:rollback` |
| Irreversible phase rollback | Informational success, no command executed, rollback proceeds |
| Rollback order | Reverse activation order is the orchestrator's responsibility; the phase table provides each phase's own rollback operation |

## Manifest command metadata

The `manifest` command returns the full command strings stored in the
artifact manifest at packaging time (ADR-017) and executed by the
orchestrator during release activation and rollback:

- **Activation commands:** `php artisan migrate --force`,
  `php artisan config:cache`, `php artisan route:cache`,
  `php artisan view:cache` (in execution order)
- **Rollback commands:** `php artisan migrate:rollback`

The manifest metadata surface carries `view:cache` in the activation
command list (TS-P7-15 AC-3) — the metadata form differs from the
executable phase table by design (the table excludes non-reversible
operations; the metadata records the full activation command set).
