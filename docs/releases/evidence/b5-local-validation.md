# B5 local validation evidence

## Status

B5 implementation and every specified non-soak local qualification gate passed
on 2026-07-23. The implementation checkpoint is
`5ebd016d90a155670c1c7a188941f3b390650012`, built on the committed and locally
verified B4 checkpoint. The final cumulative B4+B5 qualification, PostgreSQL
clean/upgrade paths, committed-source image, supply-chain scans, and
image-backed Compose smoke all passed.

Formal acceptance is not claimed. It remains held by A7/V1A and B1/B2/B3/B4
formal predecessor acceptance, the explicitly deferred B1/B2 soaks, and
Product, Security, QA, and SRE approval. B5 strategy viability is
`undetermined`; engineering correctness is not evidence or a guarantee of
production profitability.

## Implemented authority and safety

- `cross-exchange.v1b.1` exhaustively evaluates BTC-USDT and ETH-USDT in both
  Binance/Bybit directions from exact two-member B2 coherent views.
- Exact closed-cycle economics separately charge fees, spread/depth, latency,
  recovery, maximum one-leg loss, marginal replacement, natural reversal,
  advisory rebalancing, exchange concentration, and USDT concentration.
- Owned sell-side base inventory, owned buy-side USDT, exact 30/50/70 bands,
  natural reversal, and advisory-only rebalancing are enforced.
- Central risk precedes a fenced atomic claim over both balances, both fees,
  both depth slices, and recovery capacity.
- Deterministic concurrent simulation records all complete, partial, missed,
  negative-before-arrival, and delayed-unknown outcomes. Unknown state is
  verified before at most one risk-authorized retry, protected unwind, or
  quarantine.
- Eleven independently balanced journal categories retain execution, BTC and
  ETH inventory, stablecoin, fees, spread, slippage, latency, recovery,
  restoration, and combined P&L.
- No authenticated exchange, production order, testnet/demo, short sale,
  margin, derivative, leverage, transfer, withdrawal, borrowing, lending, or
  staking capability was introduced.

## Source, configuration, and toolchain

- Source: `5ebd016d90a155670c1c7a188941f3b390650012`.
- Reviewed configuration: `axiom.config.v1b.4`, strategy
  `cross-exchange.v1b.1`, 20 immutable cross-exchange parameters, and file
  SHA-256
  `4cf5a7f7ce4982d94112728ef56ba49a5e614fbcec678a8297184f0f8d2c393a`.
- Toolchain: Go 1.26.5, Node 24.18.0, pnpm 11.12.0, sqlc 1.31.1,
  PostgreSQL 18.4, and Trivy 0.72.0.
- The committed-source image was built as `axiom:b5-local` with version
  `v1b-b5-local`, the source above, built-at
  `2026-07-23T11:06:57Z`, and `DIRTY=false`.

## Passed qualification

- `make b5-model-qualify`: passed exact closed-cycle economics, both
  instruments and directions, owned-inventory and 30/50/70 band boundaries,
  atomic contention, every concurrent outcome, verify/retry/recovery,
  quarantine, eleven balanced journal categories, restart, determinism, race
  detection, fuzzing, and the B5 source boundary. The final cumulative run
  measured 2.26782 ms p99 across 400 declared-profile samples against the
  25 ms ceiling.
- `BenchmarkCrossExchangeEvaluator`: 434,908 ns/op, 25,740 B/op, and 513
  allocations/op in the final cumulative run on Go 1.26.5/linux/amd64 with
  eight logical CPUs.
- `make b5-postgres-qualify`: passed clean install through migration 000017
  against `axiom_b5_clean_b5_test` in 13.80 s and exact migrations
  000001-000016 to 000017 upgrade against `axiom_b5_upgrade_b5_test` in
  12.90 s.
- The database qualification proved registered configuration identity, exact
  copied B2 members, immutable candidates, exact seven-resource contention,
  fence rejection, quarantine retention, terminal simulation, advisory-only
  rebalancing, eleven balanced journal links, and reviewed role grants.
- `GOFLAGS=-p=1 make b5-local-qualify`: passed B4 and B5 model, race, fuzz,
  benchmark, sqlc, PostgreSQL clean/upgrade, and the cumulative repository
  `verify` chain in one uninterrupted invocation. Its B4 regression gate
  measured 8.408405 ms p99 and 2,944,663 ns/op.
- The same cumulative `verify` passed exact toolchain preflight, formatting,
  generated contracts, all 86 documentation links, 381 requirements,
  A/B strategy boundaries, lint/staticcheck/policy checks, all Go and frontend
  tests, full race detection, five repository fuzz targets, frontend/backend
  builds, all 128 active Compose profile combinations, secret and prohibited
  capability self-tests, A6/A7 binary boundaries, and `govulncheck` with zero
  called vulnerabilities.

## Image and supply-chain evidence

- `scripts/inspect-image.sh axiom:b5-local`: passed scratch-shell absence,
  numeric non-root user `10001:70`, fixed `/app/platform` entrypoint, read-only
  execution, and credential-like environment-key checks.
- Final local image identity:
  `axiom@sha256:aa325c660d61d0af938dcf6e8ead16bac06e2d8b2f5d628dda7adfcf18712388`;
  runtime size 10,572,650 bytes.
- `make image-reproducibility`: passed complete runtime
  configuration/root-filesystem comparison with fingerprint
  `sha256:8b7b83d8720ed78e0b0a36916da83412df8b977eb625093a3ea0e7554ec6cf32`.
- `make compose-smoke IMAGE=axiom:b5-local`: passed migration 000017, startup
  recovery, API/engine/recorder/worker health, non-root read-only and
  dropped-capability assertions, login/CSRF/logout,
  real-trading-disabled status, four Prometheus targets, Grafana provisioning,
  and cleanup of its temporary containers, networks, volumes, and secrets.
- Retained ignored SPDX SBOM:
  `.local/b5-image-evidence/axiom-b5.spdx.json`, 47 packages, SHA-256
  `8ee3cb6ed7e4bf2b3b3de2f8c46677e295de7f2fce389bbc8ef2e273be010a90`.
- Trivy 0.72.0 scanned a read-only image export without a Docker daemon socket
  using `vuln,secret,misconfig,license`, severity `HIGH,CRITICAL`,
  `ignore-unfixed=false`, and `exit-code=1`; it exited zero. The retained
  ignored JSON is `.local/b5-image-evidence/trivy-b5-image.json`, SHA-256
  `93b9520adaeaea043c4413f6adf0c928152d954d20c964ee3e78356f72a4468f`,
  with zero qualifying findings in every scanner category.

## Explicit holds and limitations

- The continuous B1 and B2 72-hour qualifications remain explicitly deferred
  and were not run or claimed by B5.
- A7/V1A and B1/B2/B3/B4 formal predecessor acceptance remains pending.
- Product, Security, QA, and SRE formal acceptance remains pending.
- B5 establishes deterministic platform correctness, recovery, and accounting
  evidence; it does not establish or claim a viable or profitable strategy.
- B6-B8 are not implemented.
