# B2 local validation evidence

## Status

B2 implementation is complete and its model, deterministic replay, recorder,
Tier A, and PostgreSQL 18 gates passed locally on 2026-07-22. The short
production-public run retained a valid 122-record Binance/Bybit Tier A dataset,
but this runner did not close the independent coherent-view live gate: measured
clock uncertainty was 126.825012 ms for Binance and 176.871953 ms for Bybit,
above the immutable 100 ms B2 limit. The join rejected the views fail-closed.

Therefore B2 is not yet phase-verified, B3 must not start, and no 72-hour B2
soak or formal acceptance is claimed.

## Source and toolchain identity

- Branch: `b2-coherent-views`, based on merged B1 `main`
  `91d8bab54216210f2ef54dc20fed716ccf22c831`.
- Baseline post-merge CI run: `29893542073`, succeeded before B2 changes.
- Evidence timestamp: `2026-07-22T07:07:03Z`.
- Toolchain: Go 1.26.5, Node 24.18.0, pnpm 11.12.0, sqlc 1.31.1,
  PostgreSQL 18.4, Docker Engine 29.6.1.

## Implemented authority and policy

- Shared monotonic ordering epoch and bounded midpoint clock estimator for
  Binance and Bybit, with a 30-second fusion window and fail-closed arithmetic.
- Immutable per-market versions with connection generation, book version,
  monotonic receive offset, exact UTC nanoseconds, ingest ordinal, clock offset
  and uncertainty, state hash, and collector instance/region.
- Versioned deterministic as-of policy
  `axiom.coherent-view-policy.v1`: maximum book age 250 ms, maximum inter-book
  skew 250 ms, and maximum clock uncertainty 100 ms. All boundaries are
  inclusive; one nanosecond over is rejected.
- Canonical `(exchange, base, quote)` member ordering and SHA-256 version-vector
  identity stable across permutations, restarts, and repeated runs.
- PostgreSQL migration 000014 with atomic complete-view commits, exact
  nanosecond interval checks, immutable members, decision references, frozen
  dataset evidence, Tier A child membership, per-exchange coverage, and closed
  role grants. Both clean installation and exact B1-to-B2 upgrade are covered.
- Backward-compatible V1 recorder manifests plus V2 candidate manifests and a
  verified multi-exchange Tier A aggregate. Tier A assignment requires child
  chain validation, raw/canonical linkage, replay hashing, and a combined
  no-hole ingest-ordinal proof.

## Passed qualification

- `make b2-model-qualify GO=.local/toolchains/go/bin/go`: passed shared clock,
  Binance/Bybit adapter, market-data bridge, coherent-view, restart,
  deterministic hash, recorder, Tier A, and qualification tests.
- `make b2-postgres-qualify ...`: passed clean install and migrations
  000001-000013 to 000014 upgrade against isolated PostgreSQL 18.4 databases
  `axiom_clean_b2_test` and `axiom_upgrade_b2_test`.
- `/home/anas/.local/bin/sqlc generate --file sqlc.yaml`: passed and retained
  current generated models.
- `make verify GO=.local/toolchains/go/bin/go NODE=.local/toolchains/node/bin/node
  COREPACK=.local/toolchains/node/bin/corepack`: passed preflight, formatting,
  generated contracts, documentation, lint, backend/frontend tests, the full
  Go race detector, fuzz smoke, builds, all 128 active Compose profile
  combinations, security and binary-boundary scans, and `govulncheck` with no
  called vulnerabilities.
- `git diff --check`: passed after the final source and evidence updates.

## Retained short production-public evidence

The run was public and record-only. It accepted no credentials and exposed no
private, order, withdrawal, transfer, margin, derivative, or external execution
surface.

- Root:
  `.local/b2-live-20260722/b2-live-1784703984488961762`.
- Tier A manifest:
  `qualification/b2-live-tier-a.tier-a.json`.
- Tier A identity:
  `ced498d7465e7ab46e8eb9f58093c06b8490407904398ea526c42bd60c3fe4db`.
- Manifest-file SHA-256:
  `5b5fc8ec84ffceffdeb2fcb530d847c7398f9cbdf1e6b8d1c5e4d3f449f0091c`.
- Aggregate: 122 raw records, 122 linked canonical records, two exchanges,
  zero declared gaps, zero hidden gaps, complete, quality tier A.
- Binance member: 101 records; manifest
  `85e7ee29b37a934e8fbe6525a6ae08b52912bae9a786586a3998a03da5dbd167`;
  replay
  `c49debe22d615d6a06195fb73c80f05eacac4b8cd735255c8784f56edcd7afc8`.
- Bybit member: 21 records; manifest
  `f9a79c52ddd323c127091d81fe4f413ac42d109194aee0d5be79a53ae62e22a1`;
  replay
  `480d71220356dca1f8d90c0a63e2838dbb93c254a76334146e22b311f8ec3a64`.
- Coherent-view result: rejected with `coherent_view_rejected:uncertainty`.
  This is the expected safe outcome for measurements above 100 ms and is not
  recorded as a passing live coherent-view gate.

## Remaining qualification hold

- Repeat the short public coherent-view gate from a runner whose conservative
  Binance and Bybit clock intervals are each at most 100 ms; retain its view
  identity and exact member vector.
- The continuous B2 72-hour qualification remains explicitly deferred and was
  not run.
- Product, Security, QA, and SRE formal acceptance remains pending after the
  predecessor and deferred phase gates close.
