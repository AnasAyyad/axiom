# A1 immutable-candidate local validation

This record captures local validation for Phase A1. It identifies an immutable
implementation candidate, but it is not hosted-CI gate evidence and does not
mark A1 verified.

## Validation identity

| Field             | Value                                                              |
| ----------------- | ------------------------------------------------------------------ |
| Validation end    | 2026-07-13T11:42:41Z                                               |
| Candidate version | `0.1.0-a1`                                                         |
| Source commit     | `30889cdf55c01258559531f474b3ea40df8382fa`                         |
| Source checkout   | Fresh clone at the candidate commit                                |
| Clean-tree proof  | Empty tracked status and diffs before and after `make verify`       |
| Go                | 1.26.5                                                             |
| Node              | 24.18.0                                                            |
| pnpm              | 11.12.0 through Corepack                                           |
| PostgreSQL        | `postgres:18.4-alpine` in an ephemeral local container             |
| Image build time  | 2026-07-13T11:38:04Z                                               |
| Local image index | `sha256:5841ce948dbb11b91fb20b5995d24c0f1472bdbe0ed3554ba74412d6fb545320` |

The checkout was cloned from the repository after the source-only embedded
asset marker fix. Dependency installation used the committed Go and pnpm lock
files. No generated or tracked file changed during validation.

## Passing local checks

The complete repository-local gate ran from the fresh checkout:

```text
make verify
```

Observed results:

- exact toolchain preflight passed;
- formatting, generated OpenAPI consistency, Go vet/staticcheck, frontend lint,
  Go source policy, and file/package policy passed;
- all Go tests and both frontend component/accessibility tests passed;
- the Go race suite passed;
- the execution-mode fuzz smoke ran for three seconds and passed;
- frontend type checking/build, embedded-asset generation, and platform build
  passed;
- all 32 active Compose profile combinations rendered safely;
- reserved `testnet` and `demo` profile placeholders remained inert;
- secret and prohibited-capability scans plus their seeded negative self-tests
  passed;
- documentation-link validation passed for all 45 Markdown files;
- the A0 traceability check passed with 381 unique requirements, 37 verified A0
  rows, 10 retired IDs, complete reverse coverage, and 30 exact A11 endpoints;
  and
- the pinned `govulncheck` reported no known vulnerabilities.

An exact-commit production image was built with `DIRTY=false`, the candidate
commit, version, and UTC build time embedded. `scripts/inspect-image.sh` proved
numeric non-root user `10001:70`, the exact platform entrypoint, no shell,
read-only execution, no credential-like environment key, and a working embedded
binary.

A second build used identical source and build arguments. Both builds produced
the same runtime manifest `sha256:ca973535315d20108a0ecda1b6340ae359da666210eb06b929e28a47bf3c722e`,
config `sha256:48fea8c9c8a3381b494cc7977434935b555c06f9fdb89f6e16fec9b25aa2bbfd`,
three layer digests, 3,859,997-byte size, entrypoint, and user. Their top-level
OCI index digests differed because BuildKit generated a new detached provenance
attestation: the first was the local image index above and the second was
`sha256:7001a89b229a0c9d9922817553e7717d9a6bbde10b2aeff08e775d6d3b811d87`.
The runtime payload comparison passed; byte-identical provenance-envelope
reproduction remains unresolved.

Finally, an ephemeral PostgreSQL instance passed the migration,
API/worker/recorder/shadow process, dependency-up/down readiness,
forbidden-mode, exact-command, embedded-UI, and graceful-shutdown smoke suite.
The test database container was removed afterward.

## Open A1 evidence

- This repository has no configured remote, so GitHub Actions has not run for
  the candidate. There is no positive hosted-CI run or retained CI log.
- The PR-only dependency review, full-history Gitleaks action, SPDX SBOM,
  Trivy vulnerability/secret/misconfiguration/license scan, and retained
  supply-chain artifact have not run in their authoritative CI environment.
- Docker Scout was not used as a substitute because that path can transmit
  private local-image metadata to a third party and was not authorized.
- A separate clean-machine setup and governance-document walkthrough has not
  been executed; a fresh clone on the current host is not equivalent.
- The BuildKit provenance envelope is not byte-identical across otherwise
  identical local builds, as documented above.

Phase A1 remains **In progress** until hosted CI, retained supply-chain evidence,
the clean-machine walkthrough, and the remaining reproducibility disposition are
complete for an immutable candidate.
