# A1 immutable-candidate local validation

This record captures local validation for Phase A1. It identifies an immutable
implementation candidate, but it is not hosted-CI gate evidence and does not
mark A1 verified.

## Validation identity

| Field                     | Value                                                                                |
| ------------------------- | ------------------------------------------------------------------------------------ |
| Validation end            | 2026-07-13T12:21:48Z                                                                 |
| Candidate version         | `0.1.0-a1`                                                                           |
| Source commit             | `d028115a3b61521d5be06af2601c181111179ec5`                                           |
| Source checkout           | Fresh detached checkout at the candidate commit                                      |
| Clean-tree proof          | Empty tracked status and diffs after `make verify`                                    |
| Go                        | 1.26.5                                                                               |
| Node                      | 24.18.0                                                                              |
| pnpm                      | 11.12.0 through Corepack                                                             |
| PostgreSQL                | `postgres:18.4-alpine` in ephemeral Compose projects                                  |
| Image build time          | 2026-07-13T12:18:53Z                                                                 |
| Local image index         | `sha256:5292e2607ade0eb8e04af457c43e12469d94435294b2c775ab6b2b36ecb1bacf`           |
| Runtime manifest          | `sha256:94aa334f4daf0616b4f3c658ca741a26c3b79c0d5095398c119893a0ab45339c`           |
| Runtime config            | `sha256:51fc2cb952936fd7c6a4e7bce4953225aa9230efe14f768130eb1f0472529d9a`           |
| Runtime descriptor digest | `sha256:11ea24b88ffc2f14c3d5bc9f52d0540b7eee98939bb483a3c72148c486c0423d`           |

The checkout was cloned from the repository and detached at the full commit
above. Go, Node, pnpm, PostgreSQL, and application dependency pins matched the
committed manifests. The JavaScript dependency tree was installed with:

```text
CI=true corepack pnpm install --offline --frozen-lockfile \
  --store-dir=/tmp/axiom-pnpm-store
```

This reused a verified content-addressed store because approval for a new
network download timed out. The candidate and previously network-installed
candidate had byte-identical `go.mod`, `go.sum`, root/web package manifests, and
`pnpm-lock.yaml`. The production image independently fetched the same locked
dependencies in its isolated build stages. This is immutable-candidate proof,
not a substitute for the still-pending separate clean-machine walkthrough.

## Passing local checks

With `GO`, `NODE`, and `COREPACK` set to the exact toolchains above and the
offline pnpm store identity propagated, the complete repository-local gate ran
from the fresh checkout:

```text
CI=true pnpm_config_store_dir=/tmp/axiom-pnpm-store make verify
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
- all 32 active Compose profile combinations rendered safely and the exact
  image-entrypoint command contract passed;
- reserved `testnet` and `demo` profile placeholders remained inert;
- secret and prohibited-capability scans plus their seeded negative self-tests
  passed;
- documentation-link validation passed for all 45 Markdown files;
- the A0 traceability check passed with 381 unique requirements, 37 verified A0
  rows, 10 retired IDs, complete reverse coverage, and 30 exact A11 endpoints;
  and
- the pinned `govulncheck` reported no known vulnerabilities.

The exact-commit production image was built twice with `DIRTY=false`, the full
candidate commit, version, and UTC build time embedded:

```text
make image-reproducibility \
  IMAGE=axiom:a1-d028115 \
  REBUILD_IMAGE=axiom:a1-d028115-rebuild \
  VERSION=0.1.0-a1 \
  COMMIT=d028115a3b61521d5be06af2601c181111179ec5 \
  BUILT_AT=2026-07-13T12:18:53Z \
  DIRTY=false
```

The two builds had identical complete Docker runtime configuration, root
filesystem layers, byte size, architecture, and operating system, producing the
runtime descriptor digest above. Both used the same runtime manifest and config.
`scripts/inspect-image.sh` proved numeric non-root user `10001:70`, the exact
platform entrypoint, no shell, read-only execution, no credential-like
environment key, and a working embedded binary.

BuildKit retained a detached provenance attestation. Its timestamped envelope
made the rebuild's top-level OCI index
`sha256:9339eb99f61c4d659fd5868493987f7ce1daf4945a0c91d5cdaace8da17a5a56`
different from the candidate index while leaving the runtime manifest, config,
layers, and descriptor digest identical. CI now retains both the final candidate
index and the runtime reproducibility result; provenance was not disabled to
manufacture a byte-identical envelope.

The exact image then passed the full deployment walkthrough:

```text
make compose-smoke IMAGE=axiom:a1-d028115
```

The ephemeral project initialized PostgreSQL roles from file-backed secrets,
completed the least-privilege migration, and started healthy API, shadow engine,
recorder, and worker containers. The smoke verified dependency-aware readiness,
the embedded UI, and `real_trading_enabled:false`. It removed its containers,
networks, volumes, secret files, and temporary market-data directory afterward.

The source-level PostgreSQL integration suite also passed migration,
dependency-up/down readiness, forbidden-mode, exact-command, embedded-UI, and
graceful-shutdown checks as part of the earlier candidate validation; its source
and lockfiles are unchanged in this candidate.

## Open A1 evidence

- This repository has no configured remote, so GitHub Actions has not run for
  the candidate. There is no positive hosted-CI run or retained CI log.
- The PR-only dependency review, full-history Gitleaks action, SPDX SBOM,
  Trivy vulnerability/secret/misconfiguration/license scan, and retained
  supply-chain artifact have not run in their authoritative CI environment.
- Docker Scout was not used as a substitute because that path can transmit
  private local-image metadata to a third party and was not authorized.
- A separate clean-machine setup and governance-document walkthrough has not
  been executed. CI now automates the locked build and full image-backed setup,
  but the workflow has not run on a fresh hosted runner.

Phase A1 remains **In progress** until hosted CI, its retained supply-chain
evidence, and the separate clean-machine walkthrough complete for an immutable
candidate.
