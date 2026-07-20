# A7 local validation in progress

**Date:** 2026-07-20

**Candidate branch:** `a7-resync-soak-fix`

**Entry gate:** A6 is verified and merged into `main`.

## Current outcome

The A7 implementation and short checks pass, but A7 is not a completed phase.
The first continuous 72-hour production-public run completed and did not qualify;
a new formal run from the repaired candidate commit remains required.

Implemented scope includes the compiled credential-free Binance public
transport, server-time uncertainty, metadata/trades/candles, stream-first
snapshot bridging, immutable exact-decimal books, bounded reconnect/renewal,
raw-before-canonical recording, explicit gaps, crash-safe Parquet manifests,
bounded replay verification, and `platform recorder` composition for BTC/USDT
and ETH/USDT.

## Prior formal-run result and repair

The preserved `a7-b54af05-r1` run completed the full 72-hour duration. Its
sanitized terminal evidence passed dataset integrity, gap, decode, queue, book,
bounded replay, and Go-heap checks. It failed the unchanged 15-second
gap-to-healthy objective because the BTCUSDT and ETHUSDT resynchronization p95
values were above the limit.

Review found that reconnect attempts continued escalating after a collector had
returned to `HEALTHY`. The repair records a typed generation outcome, resets
backoff after health, escalates only consecutive pre-health failures, and measures
one resynchronization interval from loss of health through every failed attempt
and delay until health returns. Qualification evidence now includes the exact
source commit, bounded reconnect-reason counts, dedicated resynchronization
sample/over-limit/p95/exact-maximum metrics, and an atomically replaced
five-minute `a7-soak-status.json`. Periodic flush and status-write failures fail
qualification closed. Recorded market data remains outside Git and all earlier
qualification directories remain preserved.

## Retained checks

- Unit and race suites cover sequence bridging, conflicting duplicates, gaps,
  crossed/stale/checksum failures, immutable views, exact VWAP/depth, reconnect,
  resynchronization, renewal, time uncertainty, route/DNS denial, raw/canonical
  linkage, capacity failure, manifest mutation, and replay verification.
- The A6 deterministic loopback emulator drives an A7 snapshot bridge and gap
  invalidation scenario.
- The opt-in production-public probe passed metadata, time, snapshot, recent
  trade, 4-hour candle, depth, trade-stream, and candle-stream checks on both
  approved instruments without credentials.
- The 20-second two-instrument qualification smoke passed segment flush and
  bounded replay verification.
- The operational recorder-role integration passed against ephemeral
  PostgreSQL 18.4 and the compiled Binance public hosts, reached truthful book
  readiness, finalized segments, and registered both wire and canonical segment
  proofs.
- Source and platform-binary scans find the required public collector/recorder,
  exclude the emulator, and reject broader Binance origins and callable private
  or order surfaces.

## Formal soak gate

The formal command requires an empty dedicated output directory and cannot be
shortened:

```text
AXIOM_A7_SOAK=1 \
AXIOM_A7_SOAK_OUTPUT=<absolute-empty-artifact-directory> \
AXIOM_A7_SOURCE_COMMIT=<full-40-character-commit> \
go test ./internal/qualification \
  -run '^TestA7Continuous72HourPublicSoak$' -count=1 -timeout=73h -v
```

The resulting directory retains raw/canonical Parquet segments, cumulative
dataset manifests, rolling `a7-soak-status.json`, terminal
`a7-soak-evidence.json`, the exact source commit, bounded reconnect reasons,
resynchronization sample/over-limit/p95/exact-maximum metrics, incident/rebuild
samples, heap samples, final book eligibility, and the bounded canonical replay
checksum. A7 advances only if the terminal artifact says `qualified: true` and
the final cumulative candidate checks pass.

## Limitations

- The short smoke and public probe do not substitute for 72 continuous hours.
- Public-data correctness and availability are not profitability evidence.
- A7 adds no account, signing, order, transfer, withdrawal, margin, leverage,
  derivative, staking, lending, borrowing, or short-selling capability.
