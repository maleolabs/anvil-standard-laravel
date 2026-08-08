# Adopting the Laravel Delivery Lifecycle

This guide explains how a Laravel project **adopts the Laravel delivery
lifecycle standard** (`anvil-standard-laravel`) and what the adopted
lifecycle does: what activation runs, what rollback reverses, what
verification checks, what the configuration surface is, what is
irreversible, and how compatibility is validated at adoption. It is the
adopter-facing entry point of the standard's Documentation part (007 §9).

The standard's executable content enforces what this guide describes
(manifesto §6 — documentation is a claim, enforcement is a fact): the
phase table, checks, and validation rules live in the executable and are
declared in the [Manifest](../MANIFEST.md); this guide only points at
them.

> **Terminology.** "Standard" here means the delivery lifecycle standard
> `anvil-standard-laravel` — the distributable unit of Laravel lifecycle
> knowledge. "Adapter" is the v1.x term for the standard's executable
> (`anvil-adapter-laravel`); the term mapping is part of the standard
> command contract (ADR-032).

## 1. What the standard declares

The standard declares its identity and capability surface in the
[Manifest](../MANIFEST.md) and its compatibility in the
[Compatibility part](../compatibility/compatibility.md):

| Declaration | Value |
|---|---|
| **Standard id / name** | `anvil-standard-laravel` — Laravel delivery lifecycle standard |
| **Target contract version** | `1.0.0` (delivery lifecycle specification, ADR-024 §3.1) |
| **Framework-version support scope** | Laravel `10.0.0`, `11.0.0`, `12.0.0` |
| **Deployment model** | `server` — releases deploy to a server and are activated in place |
| **Activation phases** | `migrate`, `config_cache`, `route_cache`, `event_cache`, `queue_restart` |
| **Verification checks** | 8 structural checks (see [Verification](#6-what-verification-checks)) |
| **Config extensions** | 5 keys under `framework.laravel.*` (see [Configuration surface](#7-configuration-surface)) |

Compatibility is validated **at adoption** — the registry rejects a
standard that violates the specification's contracts, and the declared
contract version and framework-version support scope are checked before
the standard becomes installable — and **re-verified at runtime** when
the runtime executes the standard (007 §8, [compatibility.md](../compatibility/compatibility.md)).

## 2. Prerequisites

- **Anvil Runtime** whose supported contract majors include the standard's
  target contract version (`1.0.0`, major `1` — the contract major is the
  compatibility unit, ADR-024 §3.1).
- **The standard installed through the registry flow** (below) — a
  framework declaration without the installed standard hard-fails
  initialization (ADR-026 decision 3).
- **PHP** (with the `artisan` entrypoint) and **Composer** on the
  server / build machine, plus **Node.js/npm** for the asset build.
- **The standard executable `anvil-adapter-laravel`** on `PATH` — required
  for server install/activate/rollback. Interim install path (standard
  releases not published yet): build from source, see
  [docs/README.md](README.md#install-the-adapter-interim--build-from-source).

## 3. Install the standard (registry flow)

The standard is installed through the registry flow — explicit adoption:
validation (contract version, capability, framework-version support
scope) + integrity (content digests) + attestation (publisher signature)
+ record. There is no skip or insecure path (ADR-022 §3):

```bash
# Discovery: list the offered releases of the standard
anvil standard list --index <checkout-of-anvil-standard-laravel>

# Inspect one release (its declared compatibility, capability, trust)
anvil standard inspect anvil-standard-laravel 1.0.0 --index <checkout-of-anvil-standard-laravel>

# Install — explicit adoption; the operator's trust anchors allowlist
# must pin the publisher's key (--trust-anchors or the default
# ~/.config/anvil/trust-anchors.json)
anvil standard install anvil-standard-laravel 1.0.0 --index <checkout-of-anvil-standard-laravel>
```

Installation records the installed standard (id, pinned version); the
record is what subsequent `anvil init --framework laravel` resolves
(TS-015-02-01). See [docs/release.md](release.md) for the registry flow
and the trust model.

## 4. Adopt the lifecycle in a project

```bash
# 1. Initialize a Laravel project — records the framework declaration and
#    generates the Laravel build pipeline (template content)
anvil init my-app --framework laravel
cd my-app

# 2. Build (composer → npm → artisan optimization caches)
anvil pipeline build

# 3. Package an immutable artifact
anvil artifact package

# 4. On the target server: initialize the Runtime and register the project
anvil server init
anvil server project register \
  --project-id my-app \
  --install-root /srv/apps/my-app \
  --adapter laravel \
  --non-interactive

# 5. Install the artifact as a Release (runs the 8 verification checks)
anvil server release install my-app .anvil/artifacts/<artifact>.tar.gz

# 6. Activate the Release (runs the 5 activation phases)
anvil server release activate my-app <release-id>

# 7. Roll back if needed
anvil server release rollback my-app
```

Initialization writes the framework declaration (`project.framework:
laravel`) and, when the standard's record carries config extension
content, the `framework.laravel.*` keys with their declared defaults
(TS-015-03-01) — see [docs/init.md](init.md).

## 5. What activation runs

Release activation runs the declared activation phases in order, each as
`php artisan <command>` from the release directory — **migration →
cache warming → queue**:

| # | Phase | Command | Reversible? |
|---|---|---|---|
| 1 | `migrate` | `php artisan migrate --force` | Reversible (`migrate:rollback --force`) |
| 2 | `config_cache` | `php artisan config:cache` | Irreversible |
| 3 | `route_cache` | `php artisan route:cache` | Irreversible |
| 4 | `event_cache` | `php artisan event:cache` | Irreversible |
| 5 | `queue_restart` | `php artisan queue:restart` | Irreversible |

Key semantics (full detail in the [Lifecycle Definition](../lifecycle/definition.md)):

- **Migration timing.** Migrations are declared **post-promotion**
  (`post_promotion`): the release is promoted to `active` before the
  activation phases run, so the migration phase executes against the
  promoted release. A failed phase fails the activation and leaves the
  release recorded as active — re-run `anvil server release activate` to
  converge; `migrate --force` is idempotent per migration record.
- **Cache warming order.** `config_cache` → `route_cache` → `event_cache`
  is declared order (routes and events compile against the cached
  configuration).
- **Queue restart last.** `queue_restart` runs last so no worker
  processes a job against stale migrations or caches; re-running the
  activation re-sends the (idempotent) signal.

## 6. What verification checks

Release install runs the standard's **8 structural verification checks**
against the artifact — all must pass or the install fails (see
[docs/verify.md](verify.md) and [verification/checks.md](../verification/checks.md)):

`vendor_present` · `bootstrap_structure` · `config_files` · `artisan_file`
· `composer_json` · `env_file` · `app_directory` · `routes_directory`

The checks validate that the artifact looks like a deployable Laravel
application (Composer autoloader, bootstrap, config, artisan entrypoint,
composer manifest, environment file, app and routes directories) before
it is installed on a server. Gates are mandatory and unskippable — the
standard adds checks, it never weakens gates (007 §6).

## 7. Configuration surface

The standard declares **5 configuration keys** under the
`framework.laravel.` namespace (ADR-005 §4.4) — declared by the
`extension` command, validated by the `validate` command, and enforced
by the runtime on `framework.<name>.*` keys from the installed standard's
config extension rules (TS-015-03-02; [docs/config.md](config.md)):

| Key | Default | Validation |
|---|---|---|
| `framework.laravel.migrations.path` | `database/migrations` | Non-empty relative path; no `..` traversal |
| `framework.laravel.cache.store` | `file` | One of the known Laravel cache drivers |
| `framework.laravel.version` | — (no default) | SemVer `MAJOR.MINOR.PATCH` |
| `framework.laravel.php_version` | — (optional) | SemVer `MAJOR.MINOR.PATCH` when present |
| `framework.laravel.composer_flags` | — (optional) | Whitespace-separated flags; no shell metacharacters; no `--no-dev` |

Unknown keys under the namespace are rejected. The declared
`framework.laravel.version` is the adopter-side counterpart of the
standard's framework-version support scope: a project's framework version
outside the scope is a compatibility validation fact, not an assumption
(see [Compatibility](../compatibility/compatibility.md)).

## 8. What rollback reverses — and what is irreversible

Rollback restores the previously active release, then runs the adapter
rollback operations. **Irreversibility never blocks rollback** — the
adapter reports an informational result for irreversible phases and the
rollback proceeds:

| Phase | Rollback behavior |
|---|---|
| `migrate` | Reversed by `php artisan migrate:rollback --force` (force-confirmed because the standard runs artisan non-interactively — without `--force`, Laravel's confirmable rollback would be cancelled in production) |
| `config_cache` / `route_cache` / `event_cache` | **Irreversible** — a rollback cannot undo a cache; the restored release's own activation regenerates its caches from its code |
| `queue_restart` | **Irreversible** — a rollback cannot un-send the worker restart signal; the restored release's own activation re-signals its workers |

Caches and the worker restart signal are the irreversible surface of the
Laravel lifecycle: they reflect exactly one release's state and are never
repaired, only regenerated by the next activation.

## 9. Where the details live

| Topic | Document |
|---|---|
| Standard identity and capability declaration | [Manifest](../MANIFEST.md) |
| Contract version and framework-version support scope | [Compatibility](../compatibility/compatibility.md) |
| Activation, rollback, and failure semantics per phase | [Lifecycle Definition](../lifecycle/definition.md) |
| Verification rules | [Verification](../verification/checks.md) · [docs/verify.md](verify.md) |
| Templates and config extension | [Templates](../templates/README.md) · [docs/config.md](config.md) |
| Server deployment flow | [docs/deploy.md](deploy.md) |
| Project initialization | [docs/init.md](init.md) |
| Build pipeline | [docs/build.md](build.md) |
| Manifest command metadata | [docs/manifest.md](manifest.md) |

See also: [Laravel adapter guide](README.md) · [Limitations](https://github.com/maleolabs/forge-anvil-cli/blob/develop/wiki/limitations.md) · [Glossary](https://github.com/maleolabs/forge-anvil-cli/blob/develop/wiki/glossary.md)
