# B1 local validation evidence

## Status

Locally verified for every implemented and non-soak B1 gate on 2026-07-21.
Formal phase acceptance remains on an explicit owner-authorized hold for the
open A7 predecessor and the deferred continuous 72-hour B1 soak. No 72-hour
run is claimed by this evidence.

## Source and toolchain identity

- Merged B1 implementation: `d9bba565b6cab3b3b4a2f4669a8694b919aa8721`.
- B1 final source after the CI test-size repair: `f4675667b939a346af3319c622ce2b31b6d495c1`.
- Merged B1 `main`: `91d8bab54216210f2ef54dc20fed716ccf22c831`.
- Post-merge `main` CI run `29893542073`: succeeded on 2026-07-22.
- Reviewed configuration SHA-256:
  `8a5ada09d2e689d33f92f567d569ddc74cd6aae24bce55e8805958a77cf0685a`.
- Toolchain: Go 1.26.5, Node 24.18.0, pnpm 11.12.0, sqlc 1.31.1,
  PostgreSQL 18.4, Docker Engine 29.6.1.

## B1 qualification results

The following commands passed against the completion source:

- `make b1-model-qualify GO=.local/toolchains/go/bin/go`: common contracts,
  Binance, Bybit, deterministic emulator, market-data book, and recorder.
- `make b1-adapter-qualify GO=.local/toolchains/go/bin/go`: transport,
  endpoint denial, snapshot/reset/delete semantics, non-consecutive Bybit
  update IDs, batched trades, Spot ticker semantics, lifecycle, bounded queue,
  reconnect/gap behavior, raw-before-canonical ordering, and fuzzing. The final
  three-second fuzz run completed 46,682 executions.
- `make b1-postgres-qualify ...`: passed a concurrent clean install and exact
  migrations 000001-000011 to B1 upgrade against isolated PostgreSQL 18.4
  databases `axiom_clean_b1_test` and `axiom_upgrade_b1_test`. It also proved
  migration idempotency, relational exchange/strategy ownership, append-only
  public clock/connection evidence, and the closed role matrix.
- `make b1-security-qualify GO=.local/toolchains/go/bin/go`: passed endpoint,
  credential, secret, prohibited-capability, scanner self-test, and A6/A7
  binary boundary gates.
- `AXIOM_B1_LIVE_PUBLIC=1 make b1-live-qualify ...`: passed credential-free
  Bybit production-public REST, public WebSocket, subscription, heartbeat,
  order-book, public-trade, Spot ticker, 15m/1h/4h candle, and recorder-manifest
  qualification.
- `make verify GO=.local/toolchains/go/bin/go`: passed formatting, generated
  contracts, documentation, lint, Go/frontend tests, race tests, fuzz smoke,
  builds, all 128 Compose profile combinations, security scans, binary
  boundaries, and `govulncheck` with no called vulnerabilities.
- `scripts/inspect-image.sh axiom:b1-complete`: passed the minimal non-root,
  read-only image inspection.
- `make image-reproducibility ... DIRTY=false`: passed with runtime fingerprint
  `sha256:57a1a0d9ed7970a07512f0aaa5ff4a474d67221a6b374e3f1fce49aba3b0856a`.
- `make compose-smoke IMAGE=axiom:b1-complete`: passed PostgreSQL migration,
  API, shadow, recorder, worker, Prometheus, Grafana, health, and runtime
  confinement checks.
- `docker scout cves --only-severity critical,high --exit-code
  local://axiom:b1-complete`: passed with 0 critical and 0 high findings across
  45 indexed packages.
- `git diff --check`: passed before the completion-source commit.

## Retained local evidence

Public market recordings and generated supply-chain artifacts remain ignored
and local because repository policy treats recordings as sensitive.

- Short Bybit dataset root:
  `.local/b1-short-public-20260721/b1-live-1784645182185058811`.
- Canonical dataset-manifest hash:
  `004ab342a3bc2e51661a1aaeba2a8401616fd6aa953aee3494a68d842d18c5e1`.
- Manifest-file SHA-256:
  `b2c4b97eddbe2e8eccad64ab80a9c038f45f0e1e03547792c3e2d53fcbe1b3b7`.
- Raw/canonical linkage: 3 raw records and 3 canonical records; validated by
  the production-public recorder integration test.
- Exact image digest:
  `sha256:246dc0cf2e7773ef19e801dca546dbcefa8f3b9d66ed4589814278d8468d24e5`.
- SPDX SBOM: `.local/b1-image-evidence/axiom-b1.spdx.json`, 45 packages,
  SHA-256 `028e502ad8e2c8afbf94f2c00349ec6786a71fef7255859b4a1a41a66fd172a3`.

## Explicitly deferred formal gates

- A7 formal qualification and dependent V1A acceptance.
- Continuous isolated 72-hour declared-load Binance/Bybit recording soak and
  its combined retained manifest bundle.
- Product, Security, QA, and SRE formal acceptance after those gates close.

## New isolated B1 formal runner

The current candidate adds a dedicated Bybit formal runner rather than sharing
process state or artifacts with A7. This implementation is not a claim that the
72-hour B1 gate has passed. Before a formal run, the 20-second smoke is:

```text
make b1-soak-smoke AXIOM_B1_SOURCE_COMMIT=<full-40-character-commit>
```

The formal command requires a new empty absolute output directory:

```text
AXIOM_B1_SOAK=1 \
AXIOM_B1_SOAK_OUTPUT=<absolute-empty-artifact-directory> \
AXIOM_B1_SOURCE_COMMIT=<full-40-character-commit> \
go test ./internal/qualification \
  -run '^TestB1Continuous72HourPublicSoak$' -count=1 -timeout=73h -v
```

The directory contains Bybit raw/canonical segments, cumulative manifests,
atomically replaced `b1-soak-status.json`, synchronized hash-chained
`b1-soak-events.jsonl`, and terminal `b1-soak-evidence.json`. Rolling and
terminal collector evidence contains reconnect reason and cause counts,
attribution, attempts, generations, exact request/header/body timing, bounded
response size facts, resynchronization sample count, over-15-second count, p95,
and exact maximum, book health, memory, filesystem capacity, and the exact
source commit. Immediate `bybit_collector_lifecycle` and `B1_EVENT` records are
written to the dedicated service log.

Binance and Bybit formal runs must use distinct output directories and service
units. One run cannot qualify the other. The unchanged 15-second all-sample
resynchronization objective remains fail-closed even when facts attribute a
sample to the network or upstream exchange; attribution explains the failure
and supports a clean rerun but never rewrites a failed artifact into a pass.
