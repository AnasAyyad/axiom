# B8 local validation evidence

## Status

B8 implementation and every specified non-soak local qualification gate passed
on 2026-07-26. The implementation checkpoint is
`06e32634ed3331d1b6cca1b5750bb08e4114c024`; the final qualifying source is
`30c3ade6ff1023a91e00a038a26379b47175dafa`. The final uninterrupted B4-B8
qualification, PostgreSQL clean/upgrade paths, committed-source image,
supply-chain scans, and image-backed Compose smoke all passed.

Formal acceptance is not claimed. It remains held by A7/V1A, the deferred B1
and B2 72-hour qualifications, formal predecessor acceptance, and Product,
Security, QA, and SRE approval.

## Implemented authority and safety

- Generic versioned exchange, opportunity, strategy, inventory, rebalancing,
  champion/challenger, replay-fault, and report-export resources are defined in
  OpenAPI and generated for Go and TypeScript.
- A11 Binance, Trend, portfolio, replay, shadow, incident, audit, and command
  aliases remain available.
- REST snapshots expose a global outbox revision before resumable SSE.
- Opportunity detail exposes immutable legs, inventory impact, recovery,
  cost attribution, and evidence timelines.
- Inventory is isolated by exchange, asset, strategy version, experiment,
  portfolio, and virtual owner. The API contract fixes
  `combined_balance=false`.
- Rebalancing remains advisory-only. The API fixes
  `execution_available=false`; the console has no transfer control.
- Replay faults are queued-replay-only, revisioned, idempotent, immutable,
  actor/session bound, deterministic, and simulation-only.
- Report exports are deterministic immutable JSON/CSV research artifacts and
  retain the no-profitability-claim disclaimer.
- B8 introduces no private exchange route, authenticated exchange transport,
  external order, withdrawal, transfer, leverage, lending, staking, or short
  selling.

## Source and toolchain

- Source: `30c3ade6ff1023a91e00a038a26379b47175dafa`.
- Toolchain: Go 1.26.5, Node 24.18.0, pnpm 11.12.0, sqlc 1.31.1,
  Python 3.12.3, PostgreSQL 18.4, and Trivy 0.72.0.
- The committed-source image was built as `axiom:b8-local` with version
  `v1b-b8-local`, the source above, built-at `2026-07-26T09:12:22Z`, and
  `DIRTY=false`.

## Passed qualification

- Go compilation and targeted storage/API/replay unit tests.
- PostgreSQL 18.4 clean install 000001-000020 and exact 000001-000019 to 000020
  upgrade in `axiom_b8_clean_b8_test` and `axiom_b8_upgrade_b8_test`; the final
  cumulative run completed those gates in 10.71 s and 7.92 s.
- PostgreSQL qualification executed every generic read projection plus missing
  detail paths, then covered immutable fault schedules, expected revisions,
  idempotent replay, deterministic materialized injection, immutable audit
  emission, idempotency conflict, and simulation-only resources.
- OpenAPI Go and TypeScript contract generation.
- Strict TypeScript compilation and frontend unit tests: 15 tests passed,
  including combined-balance rejection.
- B8 component axe validation for the keyboard-native opportunity detail row.
- Playwright B8 workflow: desktop and Pixel 7 projects passed navigation,
  detail expansion, inventory isolation, advisory review, report export,
  keyboard reachability, and horizontal-overflow checks.
- `GOFLAGS=-p=1 make b8-local-qualify` passed every B4-B8 model, race, fuzz,
  benchmark, sqlc, PostgreSQL clean/upgrade, independent research, API,
  frontend, security, browser, and cumulative repository gate in one
  uninterrupted invocation from `2026-07-26T08:32:32Z` through
  `2026-07-26T09:09:46Z`.
- The cumulative database gates passed B4 clean/upgrade in 18.27 s / 7.50 s,
  B5 in 9.98 s / 9.44 s, B6 in 5.26 s / 4.12 s, B7 in
  12.50 s / 13.61 s, and B8 in 10.71 s / 7.92 s.
- The cumulative `verify` passed exact toolchain preflight, formatting,
  generated OpenAPI, all 95 documentation files, 381 requirements, 20
  migrations and 49 required tables, all A/B boundaries, lint/static/policy
  checks, all Go and 15 frontend tests, full race detection, five repository
  fuzz targets, frontend/backend builds, all 128 active Compose profile
  combinations, secret and prohibited-capability self-tests, A6/A7 binary
  boundaries, and `govulncheck` with zero called or imported-package
  vulnerabilities. One advisory exists only in a required but uncalled module.

## Image and supply-chain evidence

- `scripts/inspect-image.sh axiom:b8-local`: passed scratch-shell absence,
  numeric non-root user `10001:70`, fixed `/app/platform` entrypoint, read-only
  execution, and credential-like environment-key checks.
- Final local image identity:
  `axiom@sha256:cb34e3b98494d4d0a42cccc30dd6d68a0baa3a8a28fa9005b2853e3f52957c51`;
  runtime size 10,666,880 bytes.
- `make image-reproducibility`: passed complete runtime
  configuration/root-filesystem comparison with fingerprint
  `sha256:c03d41ad86aaa3c1532c858663674684da421b1491a0c8bb6d9729aa7a4f700e`.
- `make compose-smoke IMAGE=axiom:b8-local`: passed migration 000020, startup
  recovery, API/engine/recorder/worker health, non-root read-only and
  dropped-capability assertions, login/CSRF/logout, real-trading-disabled
  status, four Prometheus targets, Grafana provisioning, and full cleanup.
- Retained ignored SPDX 2.3 SBOM:
  `.local/b8-image-evidence/axiom-b8.spdx.json`, 47 packages, SHA-256
  `6ae42e3d92ca0f194d366247a18374e7bbbb19de07ba31900b889a15009f24c3`.
- Trivy 0.72.0 scanned a read-only image export without a Docker daemon socket
  or network using `vuln,secret,misconfig,license`, severity `HIGH,CRITICAL`,
  `ignore-unfixed=false`, cached offline databases, and `exit-code=1`; it
  exited zero. The retained ignored JSON is
  `.local/b8-image-evidence/trivy-b8-image.json`, SHA-256
  `359bcdf88d468a0de95339ac1281f9466e8aa83c43b1b342e06656feed5c8091`,
  with zero qualifying findings in every scanner category.

## Explicit holds

- The continuous B1 and B2 72-hour qualifications remain deferred and are not
  claimed by B8.
- A7/V1A and formal B1-B7 predecessor acceptance remain pending.
- Product, Security, QA, and SRE acceptance remains pending.
- Console usability, platform correctness, and research maturity do not prove
  profitability.
