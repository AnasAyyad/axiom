# V1C PR1 readiness

## Current decision

**Locally qualified; not yet a frozen merge candidate.** The corrected
`v1c-pr1-local-qualify` target passed C1, C2, C3, clean PostgreSQL 18,
exact B8-to-V1C upgrade, race/fuzz/security, all 256 Compose renders, builds,
and cumulative repository verification. The local runtime image is
reproducible, minimal, and clean under SPDX and Trivy inspection.

Image-backed Compose smoke correctly remains blocked because the current local
artifact records `DIRTY=true`; A11 startup recovery fails closed with
`a11_startup_recovery_build_invalid`. Do not relabel that artifact as clean.
Freeze and commit the candidate, rebuild with truthful `DIRTY=false` metadata,
rerun image-backed smoke and artifact scans, then record security and owner
acceptance before merge.

This status makes no claim about C4, C5, C6, either exchange canary, or the
V1C 72-hour soak. A7, B1, B2, and other A/B results are not V1C acceptance
evidence.

## Candidate identity

| Field | Value |
|---|---|
| Merged-main baseline | `3b82872a230c3fa473410d700c66f0bcf2cd21b1` |
| Branch | `v1c-c1-c3-foundation` |
| Candidate commit | pending |
| Configuration schema | `axiom.config.v1c.1`, integrations/submission default off |
| Migrations | `000021`, `000022` |
| C1 result | local qualification passed |
| C2 result | local qualification passed |
| C3 result | local qualification passed |
| Aggregate result | `v1c-pr1-local-qualify` passed |
| Local image ID | `sha256:d46a7f400fe77f260db662d8ecaa2eb5f4bf49f62fa43f6c8160f0ddec8005fa`; `DIRTY=true` |
| Image payload fingerprint | `sha256:42a10c56f8b816d1c61eef5d003702c55efb660d1024eecf42db375d500d9cc9` |
| Image-backed smoke | blocked as designed by dirty-build recovery admission |

Any code, configuration, migration, image, or evidence-contract change
invalidates a recorded candidate identity.

Detailed local results are in
[`evidence/v1c-pr1-local-validation.md`](evidence/v1c-pr1-local-validation.md).
