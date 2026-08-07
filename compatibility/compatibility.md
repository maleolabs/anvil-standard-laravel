# Compatibility — Laravel

The Compatibility part of the standard (ADR-021 §5.4): the declared
compatibility — the contract version targeted and the framework versions
supported. Compatibility is declared, validated, and recorded — not assumed
(A2, PRD-002 §5.8): a standard that does not declare compatibility is
rejected at adoption.

## Declared contract version

| Field | Value |
|---|---|
| **Target contract version** | `1.0.0` |
| **Contract compatibility unit** | major version (`1`) — the unit of compatibility per ADR-024 §3.1 |

The standard implements the standard command contract as published in the
delivery lifecycle specification corpus version `1.0.0`
(`docs/specification-corpus/` in the Core repository): `capabilities`,
`build`, `activate`, `verify`, `extension`, `validate`, `template`,
`manifest`. Compatibility with the runtime is negotiated at adoption —
registry validation plus runtime verification (ADR-021 §3.4) — and the
runtime enforces the declared contract version from the standard's
manifest (`contractVersion` in `manifest/registry-metadata.json`).

## Framework-version support scope

| Field | Value |
|---|---|
| **Supported framework versions** | Laravel `10.0.0`, `11.0.0`, `12.0.0` |

The framework-version support scope is declared per release
(`capability.frameworkVersion` in the manifest), enabling compatibility
validation against the adopting project's framework version as a
validation fact, not an assumption.

The verification checks and lifecycle content in this standard are
validated against the supported framework versions listed above; content
changes for newer framework versions are released as new standard
versions, never silently.

## Versioning policy

- The standard versions independently from the Core runtime (ADR-021
  §3.4): a standard update never requires a Core release, and a Core
  update never silently breaks a standard that declares a supported
  contract version.
- Breaking the declared contract is a governed event (ADR-024): a
  contract major bump is a Core-scale event, and the standard's target
  contract version moves with it under the compatibility bounds of
  ADR-024 §3.4 (at most two concurrently supported contract majors).
