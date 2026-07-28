# V1C PR1 readiness

## Current decision

**Frozen and locally qualified; awaiting merge acceptance.** The corrected
`v1c-pr1-local-qualify` target passed C1, C2, C3, clean PostgreSQL 18,
exact B8-to-V1C upgrade, race/fuzz/security, all 256 Compose renders, builds,
and cumulative repository verification. The local runtime image is
reproducible, minimal, and clean under SPDX and Trivy inspection.

The implementation candidate is frozen at
`fe624e26b8f889d360e8d5fa96b2b2264fadd8c2`. Its truthful `DIRTY=false`
image passed reproducibility, minimal-runtime inspection, image-backed Compose
smoke, SPDX generation, and current-database Trivy scanning. Formal security
and owner acceptance remain required before merge.

This status makes no claim about C4, C5, C6, either exchange canary, or the
V1C 72-hour soak. A7, B1, B2, and other A/B results are not V1C acceptance
evidence.

## Candidate identity

| Field | Value |
|---|---|
| Merged-main baseline | `3b82872a230c3fa473410d700c66f0bcf2cd21b1` |
| Branch | `v1c-c1-c3-foundation` |
| Candidate commit | `fe624e26b8f889d360e8d5fa96b2b2264fadd8c2` |
| Configuration schema | `axiom.config.v1c.1`, integrations/submission default off |
| Migrations | `000021`, `000022` |
| C1 result | local qualification passed |
| C2 result | local qualification passed |
| C3 result | local qualification passed |
| Aggregate result | `v1c-pr1-local-qualify` passed |
| Local image ID | `sha256:5f8499be5f34ceb093bb0e8d8f1427c73c774746e08df9d0b6f3667fd83ac663`; `DIRTY=false` |
| Image payload fingerprint | `sha256:5d3a385bfc2c8bfd2d2415c86033f835ff0071d08e9356181e6398c1ee9b6bfa` |
| Image-backed smoke | passed; migration 22 and all required services healthy |

Any code, configuration, migration, image, or evidence-contract change
invalidates a recorded candidate identity.

Detailed local results are in
[`evidence/v1c-pr1-local-validation.md`](evidence/v1c-pr1-local-validation.md).
