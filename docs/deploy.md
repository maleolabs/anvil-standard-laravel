# Server Deployment (Laravel)

Laravel uses the **`server` deployment model**: artifacts are installed on a server and the release is **activated in place** — the release directory becomes the live application and Laravel's own commands (migrations, caches) are executed against it.

> **Terminology:** `anvil server release ...` commands are project-centric (for server operators). The equivalent target-centric commands (`anvil deployment install/activate/rollback/info`) are local aliases: they perform the same underlying operations on the local server runtime and honor the adapter the same way. Choose one style per workflow. `anvil deployment upload` is the only SSH transport command — it delivers the artifact from CI to the server and needs no local server state (TD-006).

## 1. Register the project with the adapter

```bash
anvil server init   # once per server, if not already done

anvil server project register \
  --project-id my-app \
  --install-root /srv/apps/my-app \
  --adapter laravel \
  --non-interactive
```

### Register flags

| Flag | Required | Description |
|---|---|---|
| `--project-id <id>` | Yes (non-interactive) | Unique project identifier |
| `--install-root <path>` | Yes (non-interactive) | Absolute install path on the server |
| `--adapter <name>` | — | Deployment adapter — use `laravel` to select the Laravel adapter |
| `--display-name <name>` | — | Human-readable display name |
| `--owner <user>` / `--group <group>` | — | Ownership for the installed files |
| `--shared-link from=<path>,to=<path>` | — | Repeatable; symlinks shared resources (e.g. `.env`, `storage`) into the release |
| `--non-interactive` | — | Skip prompts; requires `--project-id` and `--install-root` (also implied when both are provided) |
| `--server-root <path>` | — | Override config root (non-production only) |

Interactive mode (no flags) prompts for the same fields.

## 2. Install the artifact as a Release

```bash
anvil server release install my-app .anvil/artifacts/<artifact>.tar.gz
```

Install runs, in order:

1. Generic artifact integrity verification (archive, manifest, checksum)
2. **The 8 Laravel verification checks** — all must pass or the install fails (see [verify.md](verify.md))
3. Project ID match between artifact manifest and registry
4. Release creation in `ready` stage

The Release is **not** automatically activated.

## 3. Activate the Release

```bash
anvil server release activate my-app <release-id>
```

The artifact is extracted into the release directory (shared links applied), the release is promoted to active, and then the **adapter activation phases** run — each as `php artisan <command>` from the release directory, in the declared order (migration → cache warming → queue; see the [Lifecycle Definition](../lifecycle/definition.md)):

| Order | Group | Phase | Command | Reversible? |
|---|---|---|---|---|
| 1 | migration | `migrate` | `php artisan migrate --force` | Reversible (`migrate:rollback`) |
| 2 | cache warming | `config_cache` | `php artisan config:cache` | Irreversible |
| 3 | cache warming | `route_cache` | `php artisan route:cache` | Irreversible |
| 4 | cache warming | `event_cache` | `php artisan event:cache` | Irreversible |
| 5 | queue | `queue_restart` | `php artisan queue:restart` | Irreversible |

**Migration timing relative to promotion.** The release is promoted to `active` **before** the adapter phases run — migrations are declared **post-promotion** (`post_promotion`, pinned by `MigrationTiming()` in `internal/laravel/activation.go`). A failed phase fails the activation command with an error and leaves the release recorded as active — re-run `anvil server release activate` to converge (the phases are re-executed from the start; `migrate --force` skips already-applied migrations).

**Failure semantics.** A failing phase fails the activation; per-phase semantics are declared in the [Lifecycle Definition](../lifecycle/definition.md) — migrations converge on re-run, caches are regenerated from code, and the queue restart signal is idempotent (re-sending it is safe).

**Queue restart.** `queue_restart` runs last, after migration and cache warming: `php artisan queue:restart` signals running workers to recycle so they pick up the new code, migrations, and caches — no worker processes a job against stale state.

## 4. Roll back a Release

```bash
anvil server release rollback my-app
```

Rollback restores the previously active release, then runs the adapter rollback operations:

| Phase | Rollback behavior |
|---|---|---|
| `migrate` | `php artisan migrate:rollback` — reverses the migration batch applied by the activation |
| `config_cache` / `route_cache` / `event_cache` | **Irreversible** — a rollback cannot undo a cache. The adapter reports an *informational* result and the rollback proceeds without undoing these operations. The restored release's own activation regenerates its caches from its code. |
| `queue_restart` | **Irreversible** — a rollback cannot un-send the worker restart signal. The adapter reports an *informational* result and the rollback proceeds; the restored release's own activation re-signals its workers. |

Rollback never blocks on irreversible phases.

## Adapter binary requirement

Install, activate, and rollback all invoke the adapter executable `anvil-adapter-laravel` (resolved on `PATH`). If it is missing, the operation fails with:

```text
adapter executable "anvil-adapter-laravel" not found on PATH: ... (install the adapter binary or configure its path)
```

Install it once per server. **Interim distribution path:** standard releases
are not published yet (registry publication is TS-016-03-02, the
installer/discovery switch-over is TS-016-04-01), so the Laravel standard
executable is **not yet shipped with Anvil releases** — build it from this
repository:

```bash
go build -o anvil-adapter-laravel ./cmd/laravel-adapter
sudo mv anvil-adapter-laravel /usr/local/bin/
```

Once standard releases are published, the versioned release assets become
installable at setup time (`install.sh --with-adapters laravel`) or
post-install (`anvil adapter install laravel`), and `anvil update` refreshes
installed adapters automatically. See the [Laravel adapter guide](README.md).

## Server directory layout (per project)

```text
/srv/apps/my-app/
├── artifacts/            # installed artifact archives
├── releases/             # extracted releases (one dir per release)
├── shared/               # shared resources (config, storage, logs)
├── state/releases/       # release state JSON
└── runtime-state.json    # active release pointer
```

See also: [Verification checks](verify.md) · [Manifest commands](manifest.md) · [Glossary](https://github.com/maleolabs/forge-anvil-cli/blob/develop/wiki/glossary.md)
