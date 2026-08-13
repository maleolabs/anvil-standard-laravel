# Registering the Standard with the Anvil CLI (Registry + Trust Anchors)

This guide explains how an operator registers `anvil-standard-laravel` with
the Anvil CLI so `anvil standard install` and `anvil skill install` work
on a fresh machine.

The Anvil CLI verifies everything it adopts (ADR-022 — fail-closed, no
first-use acceptance). Two things must be registered locally before the
first install:

1. **The registry index** — the static, decentralized index directory the
   CLI resolves release metadata from (ADR-030). There is no bundled or
   canonical hosted index; the operator points the CLI at a directory of
   registry metadata documents.
2. **The trust anchors** — the out-of-band allowlist of publisher public
   keys the operator explicitly trusts (PM decision D-07).

A first run of any Anvil command already creates the layout:
`~/.config/anvil/registry/` and `~/.config/anvil/trust-anchors.json`
(empty allowlist). The steps below fill them in.

## 1. Register the registry index

Create the standard's directory under the default index and download the
registry metadata document **from the GitHub release** — the release
asset, not the repository source file (the release asset is the exact
document that was signed and published):

```bash
mkdir -p ~/.config/anvil/registry/anvil-standard-laravel

curl -fL -o ~/.config/anvil/registry/anvil-standard-laravel/1.1.1.json \
  https://github.com/maleolabs/anvil-standard-laravel/releases/download/v1.1.1/registry-metadata-1.1.1.json
```

The file name is the release version (`1.1.1.json`) — the CLI's static
index layout is `<index>/<standard-id>/<version>.json`.

> Download the registry document from the **release** (`/releases/download/…`),
> never from a `raw.githubusercontent.com` source link: the release asset is
> the attested artifact, the repository file is just its working copy.

## 2. Register the trust anchors

Extract the publisher public key from the downloaded metadata document
and add it to the trust anchors allowlist:

```bash
PUBKEY=$(jq -r '.trust.attestation.publicKey' \
  ~/.config/anvil/registry/anvil-standard-laravel/1.1.1.json)

jq -n --arg k "$PUBKEY" '{publishers: {"anvil-standard-laravel": $k}}' \
  > ~/.config/anvil/trust-anchors.json
```

Without `jq`, write the file manually — the allowlist maps the standard id
to its base64 Ed25519 public key:

```json
{
  "publishers": {
    "anvil-standard-laravel": "<base64 public key from .trust.attestation.publicKey>"
  }
}
```

The release notes of every stable release also carry a ready-to-use
trust anchors snippet (the same key). The key is a release-time key for
v1.1.1; pin it out of band — verification fails without an anchored key,
by design.

## 3. Verify and install

```bash
anvil standard list                         # shows the standard as offered
anvil standard install anvil-standard-laravel   # version optional: resolves latest
anvil skill list                            # skills declared by the standard
anvil skill install --all --scope global    # or pick skills interactively
```

## Notes

- The default index directory is `~/.config/anvil/registry` (Linux;
  `os.UserConfigDir()/anvil/registry` per platform). To keep the index
  elsewhere, pass `--index <path>` or set `ANVIL_REGISTRY_INDEX`.
- The default trust anchors file is `~/.config/anvil/trust-anchors.json`;
  override with `--trust-anchors <path>` or `ANVIL_TRUST_ANCHORS`.
- This standard's skills (`laravel-conventions`, `laravel-delivery`) are
  shipped as per-skill release assets and installed through the same
  gated pipeline (`anvil skill install`).
