# B6 local validation evidence

## Status

B6 implementation and every specified non-soak local qualification gate passed
on 2026-07-23. The implementation checkpoint is
`4040d6984e7ccb7ab44505ed4cfaa35e08f7cfa9`; the policy-clean qualifying source
is `fbafd9e2477fa9f35618d75551c61ef304282189`. The final cumulative B4+B5+B6
qualification, PostgreSQL clean/upgrade paths, committed-source image,
supply-chain scans, and image-backed Compose smoke all passed.

Formal acceptance is not claimed. It remains held by A7/V1A and B1/B2/B3/B4/B5
formal predecessor acceptance, the explicitly deferred B1/B2 72-hour soaks,
and Product, Security, QA, and SRE approval. Engineering correctness is not
evidence or a guarantee of production profitability.

## Implemented authority and safety

- `rebalancing.v1b.1` deterministically optimizes immutable versioned reviewed
  route facts with exact provenance, cost, duration, risk, warning, and manual
  checklist evidence.
- Eligible B5 natural reverse arbitrage is preferred before a reviewed transfer
  route.
- Stale, unapproved, low-confidence, unavailable, ambiguous, incompatible, or
  network/chain-mismatched facts fail closed.
- Migration 000018 stores immutable fact sets and recommendations, validates
  selected evidence at deferred commit, and closes runtime, recorder, and
  read-only grants.
- No transfer or withdrawal execution transport, interface, endpoint, UI
  control, setting, credential, or compiled capability was introduced.

## Source, configuration, and toolchain

- Source: `fbafd9e2477fa9f35618d75551c61ef304282189`.
- Reviewed configuration: `axiom.config.v1b.5`, advisory optimizer
  `rebalancing.v1b.1`, 12 immutable rebalancing parameters, and file SHA-256
  `d7d178c5616429dfe5208779377dcaee51a20c9069a67ff8c8f5848a6611e0ac`.
- Toolchain: Go 1.26.5, Node 24.18.0, pnpm 11.12.0, sqlc 1.31.1,
  PostgreSQL 18.4, and Trivy 0.72.0.
- The committed-source image was built as `axiom:b6-local` with version
  `v1b-b6-local`, the source above, built-at `2026-07-23T20:55:23Z`, and
  `DIRTY=false`.

## Passed qualification

- `make b6-model-qualify`: passed reviewed immutable facts, exact eight-part
  costs, eligible natural-reversal preference, deterministic route/tie
  selection, bounded graph search, provenance and compatibility failures,
  advisory evidence, race detection, fuzzing, and the source no-execution
  boundary. The final cumulative run measured 1.84075 ms p99 across 400 samples
  against the 25 ms ceiling.
- `BenchmarkAdvisoryOptimizer`: 288,678 ns/op, 76,186 B/op, and 658
  allocations/op in the final cumulative run on Go 1.26.5/linux/amd64 with
  eight logical CPUs.
- `make b6-postgres-qualify`: passed clean install through migration 000018
  against `axiom_b6_clean_b6_test` in 4.36 s and exact migrations
  000001-000017 to 000018 upgrade against `axiom_b6_upgrade_b6_test` in 4.50 s.
- Database qualification proves registered configuration identity, immutable
  reload, rejection of unapproved selected facts, exact aggregate evidence,
  and the closed role matrix.
- `make b6-security-qualify`: passed source, API, UI, configuration, and
  compiled-binary checks proving no transfer or withdrawal execution surface.
- `GOFLAGS=-p=1 make b6-local-qualify`: passed B4, B5, and B6 model, race, fuzz,
  benchmark, sqlc, PostgreSQL clean/upgrade, security, and cumulative repository
  `verify` gates in one uninterrupted invocation. Its regression measurements
  included B4 p99 13.528769 ms and B5 p99 1.736622 ms.
- The cumulative `verify` passed exact toolchain preflight, formatting,
  generated contracts, all 89 documentation links, 381 requirements, A/B
  boundaries, lint/staticcheck/policy checks, all Go and frontend tests, full
  race detection, five repository fuzz targets, frontend/backend builds, all
  128 active Compose profile combinations, secret and prohibited-capability
  self-tests, A6/A7 binary boundaries, and `govulncheck` with zero called
  vulnerabilities.

## Image and supply-chain evidence

- `scripts/inspect-image.sh axiom:b6-local`: passed scratch-shell absence,
  numeric non-root user `10001:70`, fixed `/app/platform` entrypoint, read-only
  execution, and credential-like environment-key checks.
- Final local image identity:
  `axiom@sha256:cb92cd4896dd97608c9c0ce91f9ab0ba2357de3f952cff6187ec22361cc66c86`;
  runtime size 10,579,892 bytes.
- `make image-reproducibility`: passed complete runtime
  configuration/root-filesystem comparison with fingerprint
  `sha256:0042d730b59807253f28a24765262ba27b6db31869c20bc67930e21f3939ccd9`.
- The first image-backed Compose invocation encountered a public-recorder
  `wire_finalize_failed` restart and evaluated health while that container was
  starting; its disposable stack cleaned up completely. The immediate clean
  retry of `make compose-smoke IMAGE=axiom:b6-local` passed migration 000018,
  startup recovery, API/engine/recorder/worker health, non-root read-only and
  dropped-capability assertions, login/CSRF/logout, real-trading-disabled
  status, four Prometheus targets, Grafana provisioning, and full cleanup.
- Retained ignored SPDX SBOM:
  `.local/b6-image-evidence/axiom-b6.spdx.json`, 47 packages, SHA-256
  `4dd9c9750406fe6bf8dc6cb6af2c7eb8ffffbc4fa8ff62dcfcae35cf3128d730`.
- Trivy 0.72.0 scanned a read-only image export without a Docker daemon socket
  using `vuln,secret,misconfig,license`, severity `HIGH,CRITICAL`,
  `ignore-unfixed=false`, offline cached databases, and `exit-code=1`; it
  exited zero. The retained ignored JSON is
  `.local/b6-image-evidence/trivy-b6-image.json`, SHA-256
  `3c7935142c3461753c2ae3865034e08735ed4c81ccbe55dd15abdcd7b2945421`,
  with zero qualifying findings in every scanner category.

## Explicit holds and limitations

- The continuous B1 and B2 72-hour qualifications remain explicitly deferred
  and were not run or claimed by B6.
- A7/V1A and B1/B2/B3/B4/B5 formal predecessor acceptance remains pending.
- Product, Security, QA, and SRE formal acceptance remains pending.
- B6 establishes deterministic advisory engineering evidence; it cannot move
  external assets and does not establish or claim a profitable strategy.
- B7-B8 are not implemented.
