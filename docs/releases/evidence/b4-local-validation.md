# B4 local validation evidence

## Status

B4 is implemented and locally verified for every specified non-soak gate on
2026-07-23. Exact exhaustive cycles, full-depth fixed-point conversion,
fee/filter/rounding/dust treatment, central risk, all-or-nothing multi-resource
claims, sequential arrival simulation, bounded recovery/quarantine, opportunity
lifetime, balanced categorized accounting, immutable PostgreSQL evidence,
cumulative repository verification, and the committed-source image gates
passed.

Formal acceptance is not claimed. It remains held by A7/V1A and B1/B2/B3 formal
predecessor acceptance, the explicitly deferred B1/B2 soaks, and Product,
Security, QA, and SRE approval. B4 strategy viability is `undetermined`; these
engineering results are not evidence or a guarantee of production
profitability.

## Source, configuration, and toolchain identity

- Branch: `b4-b5-end-to-end`, created from merged B3 `main`
  `5d7cb43a90473909bf2091f5af268d5a000633cd`.
- Passing implementation and image source:
  `ef134bc1fd95771754f95c6c9faf7e9f4522acdc`.
- Reviewed configuration: `axiom.config.v1b.3`, strategy
  `triangular.v1b.1`, exact-depth model `triangular-exact-depth.v1`,
  atomic claim model `atomic-multi-resource.v1`, and 18 complete immutable
  parameter contracts.
- Reviewed configuration file SHA-256:
  `30d13c4d08219e9e6cd19551ef815cdcac0b61aad66edfbadbeee80a0868d9cc`.
- Toolchain: Go 1.26.5 linux/amd64, Node 24.18.0, pnpm 11.12.0, sqlc 1.31.1,
  PostgreSQL 18.4, and Trivy 0.72.0.
- Clean image build argument timestamp: `2026-07-23T09:23:53Z`.
- Clean image identity:
  `axiom@sha256:22f5d99114964b9dcf50357f0e930c97262b279d875df2f070b29e65051379b0`.

## Implemented authority and safety

- Exact conversion exhaustively evaluates
  `USDT-BTC-ETH-USDT` and `USDT-ETH-BTC-USDT` through BTC-USDT,
  ETH-USDT, and native ETH-BTC or inverse BTC-ETH books. Buy and sell
  orientation, full executable depth, VWAP, tick/step/minimum/maximum filters,
  source/target/third-asset fees, spread/depth cost, and source dust use project
  decimal wrappers without binary floating point.
- Every reviewed 10, 25, 50, and 100 USDT ladder point plus dynamically clipped
  owned capacity is checked. The strategy budget, recovery allowance, fee
  balances, full depth, and global reserve cap execution; tests demonstrate
  that profitability changes with size.
- Admission requires active, generation-valid, sequence-healthy approved books
  no older than 250 ms, three valid/executable legs, positive expected and
  worst-case net, a worst-case edge strictly above the additional 15 bps
  margin, and a candidate within its deterministic 250 ms lifetime.
- Central risk approval precedes allocation. Capital, fee buffers, every exact
  displayed-liquidity slice, and recovery capacity are one atomic claim group
  with canonical ordering, fencing, revisions, exact partial/final settlement,
  release, expiry, quarantine, and canonical restart validation. Failed or
  contended acquisition leaves no partial hold.
- Three saga legs dispatch sequentially against deterministic future books. The
  actual rounded net output from each fill is the next input. Terminal evidence
  distinguishes full success, partial cycle, missed leg, negative after
  latency, stranded asset, recovery cost, and unresolved quarantined exposure.
  Recovery is limited to one immediate exact conversion back to USDT.
- Balanced double-entry evidence separates trade economics, fees,
  spread/depth, rounding dust, latency, recovery/unwind, stranded inventory,
  and explicit reconciliation. Lifetime evidence retains first/last
  profitable observation, peak/arrival edge, total lifetime, and p50/p95/p99
  latency survival.
- Migration 000016 stores exact decision, leg, model, configuration, metadata,
  risk, causation/correlation, claim, saga outcome, journal, and lifetime
  evidence. Registered configuration hashes, three-leg paths/output chaining,
  orientation/metadata, model types, immutability, role ownership, and
  least-privilege function execution are enforced in PostgreSQL.
- No authenticated exchange, production order, testnet/demo, margin,
  derivative, leverage, short, transfer, withdrawal, borrowing, lending, or
  staking capability was introduced.

## Passed qualification

- `make b4-model-qualify GO=.local/toolchains/go/bin/go
  NODE=.local/toolchains/node/bin/node`: passed exact conversion and both-cycle
  vectors, inverse orientation, depth/size/filter/fee/expiry boundaries,
  central risk, atomic contention and settlement, every sequential outcome,
  recovery/quarantine/restart, balanced accounting, opportunity lifetime,
  deterministic permutation, race detection, graph-cycle fuzzing, and the B4
  source boundary. The final cumulative run measured 12.917944 ms p99 across
  200 declared-profile samples against the 25 ms ceiling.
- `BenchmarkTriangularEvaluator`: 2,966,713 ns/op, 129,321 B/op, and 3,806
  allocations/op in the final cumulative run on Go 1.26.5/linux/amd64 with
  eight logical CPUs.
- `make b4-postgres-qualify ...`: passed clean install through migration 000016
  and exact migrations 000001-000015 to 000016 upgrade against isolated
  PostgreSQL 18.4 databases `axiom_clean_b4_test` and
  `axiom_upgrade_b4_test`. The final cumulative run completed the clean gate in
  7.92 s and upgrade gate in 4.26 s.
- `GOFLAGS=-p=1 make b4-local-qualify ...`: passed the complete
  model/race/fuzz/benchmark, sqlc, PostgreSQL clean/upgrade, and cumulative
  `verify` chain in one invocation. `verify` passed preflight, formatting,
  generated contracts, documentation, lint/policy, all Go and frontend tests,
  full race detection, five repository fuzz targets, builds, all 128 active
  Compose profile combinations, secret/prohibited-capability and binary scans,
  and `govulncheck` with zero called vulnerabilities.
- `make image IMAGE=axiom:b4-local VERSION=v1b-b4-local
  COMMIT=ef134bc1fd95771754f95c6c9faf7e9f4522acdc
  BUILT_AT=2026-07-23T09:23:53Z DIRTY=false`: built the clean image identity
  above from a clean worktree.
- `scripts/inspect-image.sh axiom:b4-local`: passed scratch-shell absence,
  `10001:70` user, fixed `/app/platform` entrypoint, read-only execution, and
  credential-like environment-key checks. The runtime image size was
  10,555,116 bytes.
- `make image-reproducibility ...`: passed complete runtime
  configuration/root-filesystem comparison with fingerprint
  `sha256:3eae501defc4ee51e1560e88c949dce3773f83de9d445eaf82ab24e43a844342`.
- `make compose-smoke IMAGE=axiom:b4-local
  GO=.local/toolchains/go/bin/go`: passed migration 000016, startup recovery,
  API/engine/recorder/worker health, non-root read-only/dropped-capability
  assertions, login/CSRF/logout, real-trading-disabled status, four Prometheus
  targets, and Grafana provisioning. The harness removed its temporary
  containers, networks, volumes, and secrets.
- Trivy 0.72.0 scanned a read-only export of the image with
  `vuln,secret,misconfig,license`, severity `HIGH,CRITICAL`,
  `ignore-unfixed=false`, and `exit-code=1`; it exited zero. The report found
  zero HIGH/CRITICAL vulnerabilities in `app/platform` and surfaced no
  secret, misconfiguration, or license finding. The scanner received no Docker
  daemon socket.
- `git diff --check`: passed, and the worktree was clean at the image source.

## Explicit holds and limitations

- The continuous B1 and B2 72-hour qualifications remain explicitly deferred
  and were not run or claimed by B4.
- A7/V1A and B1/B2/B3 formal predecessor acceptance remains pending.
- Product, Security, QA, and SRE formal acceptance remains pending.
- B4 establishes deterministic platform correctness and recovery/accounting
  evidence; it does not claim a viable or profitable strategy result.
- B5-B8 are not implemented. The owner authorized B5 to begin only from this
  locally verified, committed B4 checkpoint on the same branch.
