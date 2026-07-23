# B3 local validation evidence

## Status

B3 is implemented and locally verified for every specified non-soak gate on
2026-07-22. Exact completed-candle decisions, stressed sizing, no averaging
down, bounded holding/cooldown, shared allocation/risk/execution/accounting,
immutable PostgreSQL evidence, independent research validation, cumulative
repository verification, and the clean image-backed Compose smoke passed.

Formal acceptance is not claimed. It remains held by A7/V1A and B1/B2 formal
predecessor acceptance, the explicitly deferred predecessor soaks, and Product,
Security, QA, and SRE approval. B3 strategy viability is `undetermined`; these
engineering results are not evidence or a guarantee of production profitability.

## Source, configuration, and toolchain identity

- Branch: `b3-mean-reversion`, created from merged B2 `main`
  `0c2fce26cae9e171d4e622c080aaf9af5cab018f`.
- Passing implementation source:
  `8457d8cf75206a7565fe868933c6ea2e20090990`.
- Reviewed configuration: `axiom.config.v1b.2`, strategy
  `mean-reversion.v1b.1`, primary timeframe 1h, higher timeframe 4h, and 27
  complete immutable parameter contracts.
- Reviewed configuration file SHA-256:
  `e95d950c393e1270243381481800e976477a2aca2b4791823da48c527cb22e67`.
- Toolchain: Go 1.26.5 linux/amd64, Node 24.18.0, pnpm 11.12.0, sqlc 1.31.1,
  Python 3.12.3, PostgreSQL 18.4, and Docker Engine 29.6.1.
- Clean image build timestamp: `2026-07-22T15:06:06Z`.
- Clean image identity:
  `axiom@sha256:ed106246ef8f191136edb0f51d90eb1ceb7061fdc9dfff47f26529f76cfb38e7`.

## Implemented authority and safety

- Pure Go mean-reversion evaluator with population z-score 20, Wilder ATR14
  and ADX14, simple-mean-seeded EMA200, exact 0.50% ten-candle decline, and
  completed UTC 1h/4h admission. Missing, incomplete, stale, gapped,
  conflicting, regressed, misaligned, or future-observed evidence rejects.
- Entry requires z-score at or below -2.0, ADX strictly below 25, acceptable
  regime/market quality/spread, no risk pause, no existing position, and no
  active three-candle protective-loss cooldown.
- Adverse exit precedence is ATR intrabar low, protective z-score at or below
  -3.5, normalization at or above -0.25, then the 12th holding candle.
- Sizing uses the first post-latency executable observation, 0.25% stressed
  equity risk, actual-fill ATR stop, gap/slippage/entry/exit fees, 75-USDT and
  10% caps, central limits/reserve, and conservative instrument rounding.
- Accepted candidates use the real A9 allocator, central risk engine, A8
  planner, simulator, reducer, and accounting. Rejection releases funds and
  liquidity claims. Ownership is isolated from Trend; any positive owned
  position blocks another buy transactionally.
- Migration 000015 stores canonical explanations with exact B2 coherent-view,
  configuration, strategy, ownership, risk-policy, instrument, model,
  correlation, and causation references. Risk hashes and model types are
  verified in PostgreSQL, and decision/report rows are immutable.
- `mean-reversion-report.v1` is separate from the immutable Trend contract. It
  requires registered chronological/walk-forward evidence, serial block
  bootstrap, neighborhood/capacity/stress/benchmark coverage, regime and
  failure breakdowns, and explicit provisional viability language.
- No authenticated exchange, production order, testnet/demo, margin,
  derivative, leverage, short, transfer, withdrawal, borrowing, lending, or
  staking capability was introduced.

## Passed qualification

- `make b3-model-qualify GO=.local/toolchains/go/bin/go
  NODE=.local/toolchains/node/bin/node`: passed golden indicator vectors,
  threshold equality and one-unit boundaries, no-look-ahead and finality,
  deterministic hashes, restart/mode parity, no averaging down, shared
  allocator/risk/planner/simulation/accounting, rejection recovery, race
  detection, and the B3 source boundary. The final declared-profile run measured
  7.834142 ms p99 across 200 samples against the 25 ms limit.
- `make b3-postgres-qualify ...`: passed clean install and exact migrations
  000001-000014 to 000015 upgrade against isolated PostgreSQL 18.4 databases
  `axiom_clean_b3_test` and `axiom_upgrade_b3_test`. The recorded focused run
  completed the clean gate in 2.89 s and upgrade gate in 4.78 s.
- `make b3-research-qualify GO=.local/toolchains/go/bin/go`: passed all seven
  independent Python tests and the Go deterministic report, walk-forward,
  bootstrap, stress/benchmark, tamper, and final-window tests.
- `GOFLAGS=-p=1 make verify GO=.local/toolchains/go/bin/go
  NODE=.local/toolchains/node/bin/node
  COREPACK=.local/toolchains/node/bin/corepack`: passed preflight, format,
  generated contracts, documentation, lint/policy, all Go and frontend tests,
  the full race detector, five fuzz targets, builds, all 128 active Compose
  profile combinations, secret/prohibited-capability and binary scans, and
  `govulncheck` with zero called vulnerabilities.
- `GOFLAGS=-p=1 make b3-local-qualify ...`: passed the complete model/race,
  sqlc, PostgreSQL clean/upgrade, independent research, and cumulative `verify`
  chain in one invocation. The aggregate target clears the two destructive-test
  DSNs before `verify`, so whole-repository tests cannot accidentally rerun the
  clean-install fixtures against populated qualification databases.
- `make image IMAGE=axiom:b3-local VERSION=v1b-b3-local
  COMMIT=8457d8cf75206a7565fe868933c6ea2e20090990
  BUILT_AT=2026-07-22T15:06:06Z DIRTY=false`: passed and produced the clean
  image identity above.
- `make compose-smoke IMAGE=axiom:b3-local
  GO=.local/toolchains/go/bin/go`: passed migration 000015, clean A11 startup
  recovery, API/engine/recorder/worker health, login/CSRF/logout, read-only
  runtime and dropped-capability assertions, four Prometheus targets, and
  Grafana provisioning. Temporary secrets, containers, networks, and volumes
  were removed by the harness.
- `git diff --check`: passed for the implementation and evidence updates.

## Fail-closed negative evidence

Before the implementation commit, a deliberately honest dirty image
`sha256:c689a87db59a98e254e86da9fea2bffeabe3b6fca90572b71e447a50130127a9`
was rejected by shadow startup recovery with
`a11_startup_recovery_build_invalid`. No provenance flag was falsified. After
the exact implementation source was committed, the clean image above passed
the same smoke. The initial smoke also exposed that its host-side bootstrap
hash helper did not honor the pinned `GO` command; the harness now receives the
Makefile toolchain explicitly.

## Explicit holds and limitations

- The continuous B1 and B2 72-hour qualifications remain explicitly deferred
  and were not run or claimed by B3.
- A7/V1A, B1, and B2 formal predecessor acceptance remains pending.
- Product, Security, QA, and SRE formal acceptance remains pending.
- B3 establishes deterministic platform correctness and registered report
  handling; it does not claim a viable or profitable strategy result.
- B4-B8 are not implemented, and B4 must start only after B3 is merged into
  `main` and that exact merged SHA is used as its baseline.
