# V1C PR1 local validation

## Decision

C1, C2, C3, and the aggregate PR1 local qualification passed on
`v1c-c1-c3-foundation`. This is local implementation evidence, not a merge
acceptance or a frozen-candidate result.

The image-backed Compose smoke is intentionally not counted as passed. The
local artifact records `DIRTY=true`, and shadow startup correctly remains
fail-closed with `a11_startup_recovery_build_invalid`. A committed candidate
must be rebuilt and requalified before merge.

## Identity

| Field | Value |
|---|---|
| Main baseline | `3b82872a230c3fa473410d700c66f0bcf2cd21b1` |
| Candidate commit | pending |
| Dirty | `true` |
| Configuration schema | `axiom.config.v1c.1` |
| `internal/config/v1c.go` hash | `5c2cd9cc311e3390f65a9f526bca8bdfdcc464597e4bebb6b90875258b9a1ea9` |
| `internal/config/v1c_validation.go` hash | `797c1f04a4953419cd4806023097d3fe1d33c4a2e4282639212076c97e2d9cc4` |
| Migration `000021` hash | `b5d01e62171117140a526ea4c04573d814ebfee806bed14674aea23f704df250` |
| Migration `000022` hash | `04f28df50e80369b5a8229c465a55989ddddc2adb8e015fe7f7d5e82f8364cbf` |
| `docker-compose.yml` hash | `b7bca47b56f2be293f9cd4943f7e03851c664a4dcfadf16d82b40b256f1e4be8` |
| Evidence completed | `2026-07-27T14:59:57Z` |

## Toolchains

- Go `1.26.5`
- Node `24.18.0`
- pnpm `11.12.0`
- PostgreSQL `18.4`
- Trivy `0.72.0`

## Passed gates

- `c1-security-qualify`
- `c2-auth-qualify`
- `c3-recovery-qualify`
- Independent clean install on isolated `axiom_clean24_v1c_test`
- Independent exact B8 upgrade on isolated `axiom_b8_upgrade24_v1c_test`
- `v1c-pr1-local-qualify`, including cumulative `make verify`
- Aggregate clean install on isolated `axiom_clean25_v1c_test`
- Aggregate exact B8 upgrade on isolated `axiom_b8_upgrade25_v1c_test`
- OpenAPI generation, documentation links, source policy, race, fuzz, backend
  and frontend unit tests, frontend typecheck/build, 256 Compose renders,
  source/binary/secret/prohibited-capability scans, and `govulncheck`
- Minimal non-root/read-only image inspection and runtime-payload
  reproducibility
- SPDX SBOM and Trivy HIGH/CRITICAL plus secret scan

The Go vulnerability scan found zero reachable vulnerabilities. It reported
one required-module vulnerability that the built code does not call.

The cumulative target ran with `GOFLAGS=-p=1`. An earlier unconstrained
all-package run exceeded three existing 25 ms p99 performance thresholds under
host contention. Each exact benchmark passed in isolation at 5.5 ms, 8.0 ms,
and 7.1 ms, and the serialized cumulative suite then passed without changing
the thresholds or excluding tests.

## Local artifact evidence

| Artifact | Result |
|---|---|
| Image ID | `sha256:d46a7f400fe77f260db662d8ecaa2eb5f4bf49f62fa43f6c8160f0ddec8005fa`; 10,736,836 bytes |
| Runtime payload fingerprint | `sha256:42a10c56f8b816d1c61eef5d003702c55efb660d1024eecf42db375d500d9cc9` |
| SPDX SBOM | 47 packages; local ignored file hash `63209a77dedd36ca349b90c8e5d25e972c3aa820f563a6bbe575b10794fda101` |
| Trivy image report | database updated `2026-07-27T07:48:00Z`; zero HIGH/CRITICAL, secret, misconfiguration, or license findings; local ignored file hash `4bf3e7458b06ad541399c0b2642655da98b5a18461d0098261a34538ed1b9ca8` |

The ignored local artifacts are under
`.local/v1c-pr1-image-evidence-current/`. The reports contain no credentials,
signatures, TOTP/session material, private payloads, prices, quantities, or
account exports.

## Retained failures and remaining actions

1. Early PostgreSQL qualification attempts exposed nullable claim scanning,
   committed-inbox recovery, audit-chain serialization, and timestamp-location
   defects. Those defects were fixed before the final clean/upgrade pass.
2. The aggregate Make target originally leaked qualification DSNs into ordinary
   C2/C3 tests and its nested `verify`. The target now clears those DSNs outside
   the dedicated PostgreSQL step.
3. Image-backed smoke initially exposed missing engine-role and TOTP fixture
   files; the isolated smoke harness now creates them.
4. Strict source policy exposed oversized functions and files after the final
   C1-C3 hardening. The code was split by dispatcher, cancellation,
   plan-reference, audit, and rotation-completion responsibility without
   exceptions; the targeted tests and full lint gate then passed.
5. Image-backed smoke then correctly rejected the current dirty artifact with
   `a11_startup_recovery_build_invalid` while migration 22 and the other
   application services reached healthy state. Freeze a
   commit, rebuild with truthful `DIRTY=false`, rerun reproducibility,
   inspection, Compose smoke, SPDX, Trivy, and security review.
