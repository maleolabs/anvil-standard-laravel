# Project Initialization with the Laravel Framework

`anvil init` creates a new Anvil project. With `--framework laravel`, initialization records the framework declaration, resolves it to the installed delivery lifecycle standard `anvil-standard-laravel` (TS-015-02-01, ADR-026 decision 2), and generates the pipeline templates from the installed standard's **template content** when the standard supplies it (TS-015-02-03) — falling back to the Laravel adapter's `template` command (interim distribution path, ADR-020) when the standard declares no template content. The Core applies framework-agnostic compiled defaults — it owns no framework-specific config defaults (TS-015-01-03, ADR-026 decision 1); framework config keys and defaults come from the installed delivery lifecycle standard (TS-015-03-01).

## Command

```bash
anvil init my-app --framework laravel
```

| Flag | Description | Default |
|---|---|---|
| `--path <dir>` | Target directory for the project | `.` |
| `--framework <name>` | Framework declaration; standard-driven template generation (`laravel`, `flutter`, or any framework with an installed standard) | (none — plain project) |

Project names may only contain letters, numbers, hyphens, and underscores. Framework names are **not** validated against a Core whitelist — there is no "unknown framework" error; the standard-missing HARD-FAIL (TS-015-02-02) is what gates an explicit declaration without an installed delivery lifecycle standard (see [Framework selection](#framework-selection)).

## What it generates

```
my-app/
├── anvil.yaml                  # framework-agnostic project config + declaration
└── .anvil/
    ├── project-identity.json   # immutable project identity
    ├── pipelines/
    │   ├── build.yaml          # Laravel build template (from the installed standard's template content, or the adapter — interim)
    │   └── ci.yaml             # only when the standard (or adapter) supplies a CI template
    └── state/
        └── lifecycle.yaml      # project lifecycle state (Created)
```

### `anvil.yaml` — framework declaration only

```yaml
project:
    name: my-app
    version: 1.0.0
    description: ""
    framework: laravel
artifact:
    include: []
    exclude: []
```

The only framework-related value written is the declaration:

1. **`project.framework: laravel`** — records the framework; it is the value the server registry uses to select the adapter (`--adapter laravel`, see [deploy.md](deploy.md)). Plain projects (`anvil init my-app`) do **not** get a `framework` key.
2. **No artifact overrides** — `artifact.include` / `artifact.exclude` stay at the framework-agnostic compiled defaults. The previous Core-owned Laravel defaults (an `artifact.include: [vendor/**]` style override that kept `vendor/` packaged) are removed.

### Framework config extension section (when the standard supplies content)

When the installed delivery lifecycle standard's record carries configuration extension content (the EPIC-013 config extension contract: keys with description / default / required under the framework's own namespace), initialization resolves it (TS-015-03-01, ADR-026 decision 2) and merges the declared keys and their defaults into `anvil.yaml` under the framework's own namespace — `framework.<name>.<key> = default` (ADR-005 §4.4):

```yaml
project:
    name: my-app
    version: 1.0.0
    description: ""
    framework: laravel
artifact:
    include: []
    exclude: []
framework:
    laravel:
        version: 11.0.0          # example — actual keys come from the standard
        cache.store: redis
```

The values come from the installed standard, never from the runtime (the Core owns no framework config keys or defaults, TS-015-01-03). **Only keys with a declared default are written** — the merge skips keys without a declared default (they are user-provided values; validation of extended values is the standard's own flow, TS-015-03-02). A resolved standard that declares **no** config extension content is a valid state (a standard may declare nothing in a category): initialization succeeds with an explicit stderr warning and no `framework:` section. The canonical schema is untouched — extension keys pass through the loader, so `anvil.yaml` stays schema-valid.

### Interim `vendor/` gap (until a mechanism supplies artifact defaults)

Anvil's compiled default excludes strip `vendor/` and `node_modules/` from packaged artifacts — correct for most compiled projects. For Laravel, `vendor/` is **runtime-critical** (Composer autoloading, framework code), and the `vendor_present` verification check (check #1) fails at install without it. Because the Core no longer applies framework-specific artifact defaults, a Laravel project initialized after TS-015-01-03 packages **without** `vendor/`.

Config extension content (TS-015-03-01) and template content (TS-015-02-03) do **not** close this gap: extension content carries framework-namespaced config keys only (`framework.<name>.*`), and template content supplies pipeline files only — nothing maps either onto the Core-owned `artifact.exclude` key. Closing the gap needs a mechanism that can supply or adjust Core artifact defaults — standard content extraction (EPIC-016/EPIC-018 standard content). See [limitations item 6](https://github.com/maleolabs/forge-anvil-cli/blob/develop/wiki/limitations.md) for the full state.

To restore `vendor/` today, override `artifact.exclude` in `anvil.yaml` with the compiled defaults minus `vendor/**` (see [verify.md](verify.md) — do **not** use `artifact.include: [vendor/**]`: a non-empty include list acts as a strict whitelist and would drop every non-vendor file).

### `.anvil/pipelines/build.yaml` — Laravel build template

Generated from the installed delivery lifecycle standard's **template content** when the standard supplies it (TS-015-02-03, ADR-026 decision 2 — templates are distribution content, never runtime content), and validated through the pipeline loader before it is written (the same loader used at execution time, ADR-007 — the generated project is lifecycle-ready). When the installed standard declares no template content (a standard may declare nothing in a category, command-contract §4.1), generation warns explicitly on stderr and falls back to the interim adapter-driven path: the Laravel adapter's `template` command (ADR-020 — the adapter owns the template). Logical content:

```yaml
pipeline:
    name: build
    stages:
        - name: dependencies
          tasks:
            - name: composer-install
              command: composer
              args: [install, --no-dev, --optimize-autoloader]
        - name: assets
          tasks:
            - name: npm-build
              command: npm
              args: [run, build]
        - name: optimize
          tasks:
            - name: cache-config
              command: php
              args: [artisan, config:cache]
            - name: cache-route
              command: php
              args: [artisan, route:cache]
            - name: cache-view
              command: php
              args: [artisan, view:cache]
```

See [build.md](build.md) for how it executes and how to customize it.

### `.anvil/pipelines/ci.yaml`

Written **only** when the installed standard's template content declares a CI template, or (interim) when the adapter's `template` command returns a CI definition — either way validated through the pipeline loader. When neither source supplies a CI template, no `ci.yaml` is written — the Core owns no generic CI template to fall back to (TS-015-01-02). The Laravel adapter currently supplies no CI definition, so a Laravel init typically writes `build.yaml` only.

## Init without a framework

```bash
anvil init my-app
```

Creates a plain project: no `framework` key, no pipeline files (the Core owns no generic pipeline template — TS-015-01-02).

## Init without the adapter installed

When the installed standard declares **no template content** (the current interim state — standard install/update flows do not yet extract template content from standard release content, EPIC-016/EPIC-018), `anvil init my-app --framework laravel` needs the `anvil-adapter-laravel` binary on `PATH` to fetch the adapter-owned template. When the adapter is missing, initialization **still succeeds** (adapters are optional, ADR-009 §9.7) with a warning on stderr and **no pipeline file generated**:

```text
Warning: the delivery lifecycle standard anvil-standard-laravel resolved for framework "laravel" declares no template content; pipeline templates will be generated from the installed adapter's template command (interim path — a standard release that supplies template content generates pipeline templates from the standard, TS-015-02-03).
Warning: could not generate pipeline template from adapter for framework "laravel": ...
no pipeline file was generated. Install the adapter and run 'anvil adapter use laravel' to generate it.
```

When the installed standard **does** carry template content (TS-015-02-03), the pipeline files come from the standard and the adapter is not consulted.

This is distinct from the standard-missing hard-fail: an **installed standard** is required before initialization starts (TS-015-02-02, [Standard resolution](#standard-resolution)); the adapter is only consulted for template content after the declaration resolved.

Recovery after installing the adapter (interim: standard releases are not
published yet — TS-016-03-02 — so build from source and place the binary on
PATH; `anvil adapter install laravel` serves once releases exist):

```bash
go build -o anvil-adapter-laravel ./cmd/laravel-adapter   # from this repository
sudo mv anvil-adapter-laravel /usr/local/bin/             # or place it on PATH
anvil adapter use laravel              # regenerates the adapter-owned template
```

Template generation is non-destructive (TS-P7-28 AC-1): existing pipeline files are never overwritten, so if partial pipeline files exist from a previous run, remove them first (`rm -rf .anvil/pipelines`). When `project.framework` is already set to the requested adapter, `anvil adapter use` reports "Adapter `<name>` is already active" and does not re-run the generator — reset the `framework` key in `anvil.yaml` (or run `anvil adapter use <other> --force` then back) so the regeneration path runs again.

## Framework selection

| Command | Result |
|---|---|
| `anvil init my-app --framework laravel` | Works — resolves the declared framework to the installed delivery lifecycle standard `anvil-standard-laravel` (TS-015-02-01) and creates a Laravel project with the standard-supplied build template (TS-015-02-03) — or the adapter-owned template while the standard declares no template content (interim, ADR-020). See [standard resolution](#standard-resolution) |
| `anvil init my-app --framework flutter` | Works — creates a Flutter project with a platform-aware build template (web / apk / ios). See the [limitations](https://github.com/maleolabs/forge-anvil-cli/blob/develop/wiki/limitations.md) page for the Flutter wiki status |
| `anvil init my-app --framework symfony` | Works when `anvil-standard-symfony` is installed — the declaration is stored in `anvil.yaml`; template generation is standard-driven (a standard without template content falls back to the adapter; a missing `anvil-adapter-symfony` produces the warning above, no pipeline files). Without the installed standard, initialization HARD-FAILS with the standard-missing error (TS-015-02-02) |

There is **no "unknown framework" error**: the Core owns no framework whitelist (TS-015-01-03, ADR-026 decision 1). Framework resolution — including the standard-missing HARD-FAIL for explicit declarations — is standard-driven (TS-015-02-01, TS-015-02-02, [limitations item 8](https://github.com/maleolabs/forge-anvil-cli/blob/develop/wiki/limitations.md)).

## Standard resolution

A framework declaration resolves to the installed delivery lifecycle standard through the installed-standard records (TS-015-02-01, ADR-026 decision 2) — never through runtime knowledge. The declared framework name maps to the standard id by the identity convention (ADR-021 §3.1): `laravel` → `anvil-standard-laravel`.

- **Standard installed** — the resolution is explicit and recorded: the success message reports the resolved standard and its pinned version (below), and the resolved record is passed into initialization. When the record carries configuration extension content, the declared keys and their defaults merge into `anvil.yaml` under `framework.laravel.*` (TS-015-03-01, see [Framework config extension section](#framework-config-extension-section-when-the-standard-supplies-content)); a record without content warns explicitly on stderr and merges nothing. When the record carries template content, the pipeline files are generated from the standard (TS-015-02-03, see [build.yaml](#anvilpipelinesbuildyaml--laravel-build-template)); a record without template content warns explicitly on stderr and generation falls back to the installed adapter's `template` command (interim, ADR-020).
- **Standard not installed** — the no-match case HARD-FAILS initialization (TS-015-02-02, ADR-026 decision 3): an explicit framework declaration requires the installed standard, there is no graceful fallback to a generic lifecycle (§4 — silent degradation hides a missing distribution step), and no project files are created. The error states what is missing (the standard `anvil-standard-<framework>`) and how to resolve it (install the standard with `anvil standard install anvil-standard-laravel <version>`, then re-run init).
- **Installed-standard store corrupt or unreadable** — initialization **FAILS** with an error naming the standard and the record file, plus the remediation (delete the corrupt record file or re-install the standard with `anvil standard install anvil-standard-laravel <version>`). A corrupt record is a real store failure, never treated as "not installed" (no silent no-match).
- **Framework name cannot form a standard id** — e.g. `--framework foo.bar` (dots, slashes, or uppercase are not valid in standard ids): initialization **FAILS** with an error stating the framework name is not a valid standard name; the declaration itself is not whitelisted (TS-015-01-03).

## Example output

```text
# Standard anvil-standard-laravel 1.2.3 installed:
Project 'my-app' created with 'laravel' framework (resolved delivery lifecycle standard anvil-standard-laravel 1.2.3). Ready for use.

# Standard installed with template content (TS-015-02-03):
# .anvil/pipelines/build.yaml is generated from the standard's template content
# (no adapter needed); a broken template in the record fails init BEFORE any
# project file is created, with the reinstall remediation.

# Standard installed, no config extension content (TS-015-03-01; stderr warning):
Warning: the delivery lifecycle standard anvil-standard-laravel resolved for framework "laravel" declares no configuration extension content; no framework config keys or defaults were merged into the project configuration. The standard may declare nothing in a category; a standard release that supplies configuration extension content resolves framework config defaults (TS-015-03-01).
Project 'my-app' created with 'laravel' framework (resolved delivery lifecycle standard anvil-standard-laravel 1.2.3). Ready for use.

# Standard installed, no template content (TS-015-02-03 interim; stderr warning):
Warning: the delivery lifecycle standard anvil-standard-laravel resolved for framework "laravel" declares no template content; pipeline templates will be generated from the installed adapter's template command (interim path — a standard release that supplies template content generates pipeline templates from the standard, TS-015-02-03).

Next steps:
  cd . && anvil config list

# No standard installed (TS-015-02-02 hard-fail; exit code 1, no project files created):
Error: the delivery lifecycle standard for the declared framework "laravel" (anvil-standard-laravel) is not installed.
Reason: framework-declared initialization requires the standard recorded in the installed-standard registry; the declaration cannot be resolved without it (ADR-026 decision 3).
Resolution: install the standard with 'anvil standard install anvil-standard-laravel <version>' (e.g. 'anvil standard install anvil-standard-laravel 1.0.0'), then re-run 'anvil init my-app --framework laravel'.
```

## What init does NOT do

- Does not create runtime state (releases, artifacts, execution history)
- Does not scaffold Laravel application files (controllers, migrations, Blade views, etc.) — Anvil is a deployment tool, not a Laravel installer
- Does not install the adapter binary (see [limitations](https://github.com/maleolabs/forge-anvil-cli/blob/develop/wiki/limitations.md))
- Does not whitelist-validate the framework name (see [Framework selection](#framework-selection)); a declaration still requires its installed delivery lifecycle standard (TS-015-02-02, [Standard resolution](#standard-resolution))
- Does not apply framework config defaults from the runtime — the Core owns no framework config keys or defaults (TS-015-01-03); `framework.<name>.*` defaults are written only from the installed standard's config extension content (TS-015-03-01, see [Framework config extension section](#framework-config-extension-section-when-the-standard-supplies-content))
- Does not supply pipeline template content from the runtime — the Core owns no template content (TS-015-01-02); `.anvil/pipelines/` files come from the installed standard's template content (TS-015-02-03) or, interim, from the installed adapter's `template` command

See also: [Usage overview](README.md) · [Build pipeline](build.md) · [Deploy](deploy.md)
