# B7 local validation evidence

## Status

B7 implementation and every specified non-soak local qualification gate passed
on 2026-07-26. The implementation checkpoint is
`d84aa5dd1e8ff48b0c1c4ff4bbf60057f90dc32a`; the policy-aligned checkpoint is
`d506983fd0b651a256aa0ff2c42d06de4f0105dc`; and the final qualifying source is
`54924b1b8b93647421406e8123047e98a90e85b1`. The final cumulative
B4+B5+B6+B7 qualification, PostgreSQL clean/upgrade paths, independent
statistics, committed-source image, supply-chain scans, and image-backed
Compose smoke all passed.

Formal acceptance is not claimed. It remains held by A7/V1A, B1/B2 deferred
72-hour qualifications, B1-B6 formal predecessor acceptance, and Product,
Security, QA, and SRE approval. Research maturity is not evidence or a
guarantee of production profitability.

## Implemented authority and safety

- `research-preregistration.v1` canonically fixes the generation, strategy
  version, parameter search, chronological windows, models, benchmarks,
  thresholds, rules, seed, and registration time before final testing.
- `multi-strategy-validation.v1` binds one-use final-test consumption to
  walk-forward, bootstrap, neighborhood, capacity, stress, benchmark, regime,
  multiple-testing, Sharpe, source, and measurable-criterion evidence.
- The dependency-free Python 3.12.3 checker independently recalculates the B7
  statistical results and maturity eligibility against a committed Go/Python
  golden.
- Only formal Tier A primary backtest, replay, and shadow evidence can qualify;
  paper, Tier B, low-confidence, and integration-only evidence cannot be
  primary promotion evidence.
- Promotion is an explicit recent-reauthenticated `research.promote` command
  with immutable evidence, expected revision, idempotency, PostgreSQL-side
  authorization, one-winner concurrency, and applied/rejected audit evidence.
- B7 adds no broker, exchange credential, external order, transfer, withdrawal,
  accounting, or portfolio mutation authority.

## Source and toolchain

- Source: `54924b1b8b93647421406e8123047e98a90e85b1`.
- Evidence contracts: `research-preregistration.v1` and
  `multi-strategy-validation.v1`.
- Toolchain: Go 1.26.5, Node 24.18.0, pnpm 11.12.0, sqlc 1.31.1,
  Python 3.12.3, PostgreSQL 18.4, and Trivy 0.72.0.
- The committed-source image was built as `axiom:b7-local` with version
  `v1b-b7-local`, the source above, built-at `2026-07-26T05:36:30Z`, and
  `DIRTY=false`.

## Passed qualification

- `make b7-model-qualify`: passed preregistration canonicality, final-window
  locking, deterministic Benjamini-Hochberg false-discovery-rate correction,
  probabilistic and trial-deflated Sharpe calculations, incomplete/tampered
  evidence rejection, Tier-A-only eligibility, champion/challenger
  non-authority, explicit recent-auth promotion, sequential maturity,
  idempotency, concurrency, race detection, fuzzing, and the source boundary.
- `BenchmarkB7ValidationSuite`: 78,923 ns/op, 10,996 B/op, and 163
  allocations/op on Go 1.26.5/linux/amd64 with eight logical CPUs. B7 is a
  cold research-governance path and has no latency-SLO p99 gate.
- `make b7-postgres-qualify`: passed clean install through migration 000019
  against `axiom_b7_clean_b7_test` in 5.72 s and exact migrations
  000001-000018 to 000019 upgrade against `axiom_b7_upgrade_b7_test` in
  6.91 s.
- Database qualification proves immutable preregistration/suite/report
  persistence, exact suite binding, recent authenticated permission checks
  before state or idempotency disclosure, one-winner concurrency, durable
  rejection evidence, sequential maturity revisions, and the closed runtime,
  recorder, and read-only role matrix.
- `make b7-research-qualify`: the dependency-free Python 3.12.3 validator
  passed all 10 independent tests against the shared committed golden.
- `GOFLAGS=-p=1 make b7-local-qualify`: passed B4, B5, B6, and B7 model, race,
  fuzz, benchmark, sqlc, PostgreSQL clean/upgrade, research, security, and
  cumulative repository `verify` gates in one uninterrupted invocation.
- The cumulative regression measurements were B4 p99 10.861408 ms with
  3,635,599 ns/op, 129,276 B/op, and 3,806 allocations/op; B5 p99 7.3821 ms
  with 440,667 ns/op, 25,737 B/op, and 513 allocations/op; and B6 p99
  1.463595 ms with 178,106 ns/op, 76,212 B/op, and 658 allocations/op.
- The cumulative PostgreSQL gates also passed B4 clean/upgrade in 4.59 s /
  4.37 s, B5 in 4.01 s / 3.59 s, and B6 in 3.74 s / 4.18 s.
- The cumulative `verify` passed exact toolchain preflight, formatting,
  generated OpenAPI, all 92 documentation links, 381 requirements, 19
  migrations and 49 required tables, all A/B boundaries, lint/static/policy
  checks, all Go and frontend tests, full race detection, five repository fuzz
  targets, frontend/backend builds, all 128 active Compose profile
  combinations, secret and prohibited-capability self-tests, A6/A7 binary
  boundaries, and `govulncheck` with zero called or imported-package
  vulnerabilities. One advisory exists only in a required but uncalled module.

## Image and supply-chain evidence

- `scripts/inspect-image.sh axiom:b7-local`: passed scratch-shell absence,
  numeric non-root user `10001:70`, fixed `/app/platform` entrypoint, read-only
  execution, and credential-like environment-key checks.
- Final local image identity:
  `axiom@sha256:4c135eeb53d4ee05e7a8727dbad0c6eef62e742c4c202467804c0d013480b72c`;
  runtime size 10,582,485 bytes.
- `make image-reproducibility`: passed complete runtime
  configuration/root-filesystem comparison with fingerprint
  `sha256:e31085366929d84afd8a7c0e31d403e689be5ef08018cefd445f9b92a65c08a7`.
- `make compose-smoke IMAGE=axiom:b7-local`: passed migration 000019, startup
  recovery, API/engine/recorder/worker health, non-root read-only and
  dropped-capability assertions, login/CSRF/logout, real-trading-disabled
  status, four Prometheus targets, Grafana provisioning, and full cleanup.
- Retained ignored SPDX SBOM:
  `.local/b7-image-evidence/axiom-b7.spdx.json`, 47 packages, SHA-256
  `3dc20187c882475887e2a36023a3a921b7d5622d51ba633ea9adeddd911626e6`.
- Trivy 0.72.0 scanned a read-only image export without a Docker daemon socket
  using `vuln,secret,misconfig,license`, severity `HIGH,CRITICAL`,
  `ignore-unfixed=false`, offline cached databases, and `exit-code=1`; it
  exited zero. The retained ignored JSON is
  `.local/b7-image-evidence/trivy-b7-image.json`, SHA-256
  `01575060a2e7d86209e282c33e9280f751660be9890a04a35b35a2f6d8bc1c3c`,
  with zero qualifying findings in every scanner category.

## Explicit holds and limitations

- The continuous B1 and B2 72-hour qualifications remain explicitly deferred
  and were not run or claimed by B7.
- A7/V1A and B1-B6 formal predecessor acceptance remains pending.
- Product, Security, QA, and SRE formal acceptance remains pending.
- B7 establishes deterministic research-governance engineering evidence. It
  cannot submit external orders or move external assets and does not establish
  or claim a profitable strategy.
- B8 is not implemented.
