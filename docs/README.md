# Laravel Adapter — Usage Guide

> **Documentation part of the Laravel delivery lifecycle standard.** This
> guide migrated from the Anvil Core wiki (`wiki/adapters/laravel/`) as part
> of the repository split (ADR-025): Laravel lifecycle documentation now
> lives in the standard repository and is maintained by the standard's
> maintainers. Vocabulary note: "adapter" is the v1.x term for the standard
> executable (`anvil-adapter-laravel`); the term mapping is part of the
> standard command contract (ADR-032).

The Laravel adapter gives Anvil Laravel-specific behavior for building, packaging, verifying, and deploying Laravel applications. It is the first framework adapter and uses the **`server` deployment model**: releases are deployed to a server and activated in place.

## What you get

| Capability | What the adapter does |
|---|---|
| **Init template** | `anvil init my-app --framework laravel` generates the Laravel build pipeline from the installed standard's template content when the standard supplies it (TS-015-02-03), or from the adapter's `template` command as the interim fallback (ADR-020) — and records the framework declaration in a framework-agnostic project config — no Core-owned framework defaults ([init.md](init.md)) |
| **Build pipeline** | Composer → npm → artisan cache commands as the default `.anvil/pipelines/build.yaml` ([build.md](build.md)) |
| **Verification** | 8 Laravel structure checks that must pass before an artifact is installed on a server ([verify.md](verify.md)) |
| **Activation** | `php artisan migrate --force`, `config:cache`, `route:cache`, `event:cache`, `queue:restart` on release activate — migrations post-promotion, cache warming first, worker recycling last ([deploy.md](deploy.md), [Lifecycle Definition](../lifecycle/definition.md)) |
| **Rollback** | `php artisan migrate:rollback --force` on release rollback; cache and queue phases are irreversible and never block rollback ([deploy.md](deploy.md)) |
| **Manifest metadata** | Activation/rollback command strings for the artifact manifest (ADR-017) ([manifest.md](manifest.md)) |
| **Config extension** | Reserved `framework.laravel.*` configuration keys ([config.md](config.md)) |

## Prerequisites

- **Anvil CLI** — installed and on `PATH`
- **Adapter binary `anvil-adapter-laravel`** — on `PATH` (required for server install/activate/rollback; interim install path: build from source per [Install the adapter](#install-the-adapter-interim--build-from-source) — standard releases are not published yet)
- On the server / build machine: **PHP** (with the `artisan` entrypoint), **Composer**, and **Node.js/npm** (for the asset build)

## Install the adapter (interim — build from source)

> **Interim distribution path.** Standard releases are not published yet:
> registry publication (TS-016-03-02) and the installer/discovery
> switch-over (TS-016-04-01) are pending, so the Laravel standard
> executable is **not yet shipped with Anvil releases** and the
> `install.sh --with-adapters laravel` / `anvil adapter install laravel`
> paths do not serve it yet. Until then, build it from this repository.

**Build from source (the interim install path):**

```bash
go build -o anvil-adapter-laravel ./cmd/laravel-adapter
# make it discoverable, e.g.:
sudo mv anvil-adapter-laravel /usr/local/bin/
```

Anvil resolves the adapter as `anvil-adapter-laravel` on `PATH`. If it is missing, server operations fail with:

```text
adapter executable "anvil-adapter-laravel" not found on PATH: ... (install the adapter binary or configure its path)
```

Once standard releases are published (TS-016-03-02), the standard release
pipeline produces versioned `anvil-adapter-laravel-<os>-<arch>` assets
(checksum-verified), installable at setup time (`install.sh
--with-adapters laravel`) or post-install (`anvil adapter install laravel`),
with `anvil update` refreshing installed adapters (refresh-only — it never
installs new ones) and `anvil adapter uninstall laravel` removing them.

## Quick start

```bash
# 1. Initialize a Laravel project (generates Laravel build pipeline + config)
anvil init my-app --framework laravel
cd my-app

# 2. Build (runs the generated build.yaml: composer → npm → artisan caches)
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

# 5. Install the artifact as a Release (runs the 8 Laravel verification checks)
anvil server release install my-app .anvil/artifacts/<artifact>.tar.gz

# 6. Activate the Release (runs Laravel activation phases)
anvil server release activate my-app <release-id>

# 7. Roll back if needed
anvil server release rollback my-app
```

## Documentation pages

| Page | Contents |
|---|---|
| [init.md](init.md) | What `anvil init --framework laravel` generates; framework selection errors |
| [build.md](build.md) | Build pipeline template, `--env` flag, `environments:` overrides |
| [deploy.md](deploy.md) | Server deployment: register, install, activate, rollback |
| [verify.md](verify.md) | The 8 verification checks (table) and where they run |
| [manifest.md](manifest.md) | Activation/rollback commands in the artifact manifest |
| [config.md](config.md) | `framework.laravel.*` keys (reserved) |

See also: [Adapters Wiki](../README.md) · [Limitations](https://github.com/maleolabs/forge-anvil-cli/blob/develop/wiki/limitations.md) · [Glossary](https://github.com/maleolabs/forge-anvil-cli/blob/develop/wiki/glossary.md)
