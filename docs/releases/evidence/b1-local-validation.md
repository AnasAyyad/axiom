# B1 formal qualification evidence

## Status

B1 is formally qualified from source
`da47d143ac26806eb8f318d8e141f396d5576fea`. The immutable 72-hour result is
`qualified:true`, with 4,212,483 verified records and zero recoveries over
15 seconds. Exact hashes and retained-artifact facts are in
[B1 formal qualification](b1-formal-qualification-2026-07-26.md).

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

## Remaining release gates

- B1 qualification does not formally accept B2 or any B3-B8 phase.
- B2 retains its own continuous 72-hour qualification and approver gates.
- V1B release-level Product, Security, QA, and SRE acceptance remains pending.

## New isolated B1 formal runner

The dedicated Bybit formal runner does not share process state or artifacts with
A7. Its first completed run is the formally qualified source and bundle linked
above. Future candidate runs must first pass the 20-second smoke:

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

The first instrumented formal-start attempt was stopped and preserved before
its first periodic flush after both instruments reconnected at the 20-second
heartbeat boundary. The new lifecycle evidence isolated the trigger: Bybit Spot
acknowledges a client `{"op":"ping"}` with a successful message whose `op`
remains `ping` and whose `ret_msg` is `pong`; the decoder had accepted only
`op=pong`. The repair accepts both documented public forms, retains a fixed
`heartbeat_response_invalid` cause for malformed responses, records heartbeat
frames with their dedicated kind, and makes the production-public test wait for
and verify an actual pong rather than merely sending a ping.

The preserved `b1-5d34bcc-r1` run used source commit
`5d34bcc447955d09bbfbc256d23474e4ccb83207`. Before the owner stopped the
candidate, BTCUSDT recorded 19 decoder-triggered reconnects and ETHUSDT
recorded 31. All 50 recoveries returned to healthy in about 2.1 seconds or less,
but the old fixed cause was only `stream_receive_failed`.

Raw/canonical linkage analysis of manifest revision 36 identified every decoder
error as a valid `publicTrade` batch containing the documented Bybit `BT` and
`RPI` classification fields and at least one RPI-classified trade. The DTO
already represented both fields, but normalization incorrectly rejected a trade
whenever either flag was true. This was a local adapter policy defect, not
evidence that two concurrent soak services interfered or that Bybit was
malformed.

The repair accepts `BT` and `RPI` public Spot executions in both stream and
recent-trade normalization without changing their shared canonical price,
quantity, side, time, or sequence semantics. Strict unknown-field rejection and
all identity, enum, sequence, numeric, envelope, and batch limits remain. A
decoder-error canonical record now stores bounded failure kind, operation, and
fixed cause and remains linked to the exact retained raw frame, so a future
protocol mismatch is diagnosable without logging payloads. B1 also receives the
same proactive recorder pressure flush, exact collector-terminal handling, and
rolling running-state evidence as A7.

Binance and Bybit formal runs must use distinct output directories and service
units. One run cannot qualify the other. The unchanged 15-second all-sample
resynchronization objective remains fail-closed even when facts attribute a
sample to the network or upstream exchange; attribution explains the failure
and supports a clean rerun but never rewrites a failed artifact into a pass.
