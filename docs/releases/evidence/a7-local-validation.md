# A7 local validation in progress

**Date:** 2026-07-14

**Candidate branch:** `a7-8-9-candidates`

**Entry gate:** A6 is verified and merged into `main`.

## Current outcome

The A7 implementation exists and its short checks pass. It is not yet a
completed phase: the required continuous 72-hour production-public soak has not
completed, so no A8 work may start.

Implemented scope includes the compiled credential-free Binance public
transport, server-time uncertainty, metadata/trades/candles, stream-first
snapshot bridging, immutable exact-decimal books, bounded reconnect/renewal,
raw-before-canonical recording, explicit gaps, crash-safe Parquet manifests,
bounded replay verification, and `platform recorder` composition for BTC/USDT
and ETH/USDT.

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
go test ./internal/qualification \
  -run '^TestA7Continuous72HourPublicSoak$' -count=1 -timeout=73h -v
```

The resulting directory retains raw/canonical Parquet segments, cumulative
dataset manifests, `a7-soak-evidence.json`, incident/rebuild samples, heap
samples, latency percentiles, final book eligibility, and the bounded canonical
replay checksum. A7 advances only if that artifact says `qualified: true` and
the final cumulative candidate checks pass.

## Limitations

- The short smoke and public probe do not substitute for 72 continuous hours.
- Public-data correctness and availability are not profitability evidence.
- A7 adds no account, signing, order, transfer, withdrawal, margin, leverage,
  derivative, staking, lending, borrowing, or short-selling capability.
