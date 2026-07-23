# A7 implementation complete — formal 72-hour qualification pending

**Date:** 2026-07-20

**Candidate branch:** `a7-resync-soak-fix`

**Entry gate:** A6 is verified and merged into `main`.

## Current outcome

The A7 implementation and short checks pass, but A7 is not a completed phase.
The first continuous 72-hour production-public run completed and did not qualify;
a new formal run from the repaired candidate commit remains required.
The owner separately authorized speculative A8-A11 work on isolated branches;
that exception does not satisfy or bypass the formal A7 gate.

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
and delay until health returns.

The preserved `a7-eb96f15-r1` repair run used source commit
`eb96f1531c8053de68eeb21f3708c065bd644d4b`. It stopped after approximately
15.5 hours with `periodic_flush_failed`. Its terminal dataset still verified
5,718,748 records across 186 segment pairs. Review reproduced a local recorder
race: raw and canonical calls intentionally occur in order but held the recorder
lock separately, so the periodic flush could observe the in-flight raw suffix
and reject it as incomplete. The absence of leftover partial files and a later
successful final flush were consistent with that interleaving; this failure was
not attributed to Binance.

The recorder repair allocates and appends ordinals under one lock, flushes only
the complete ordered raw/canonical prefix, defers an in-flight suffix, and
commits recorder state only after the cumulative manifest is durable. Terminal
flush remains strict. Filesystem and finalizer failures retain only fixed stage,
class, cause, and errno evidence.

The preserved `a7-5d34bcc-r1` run used source commit
`5d34bcc447955d09bbfbc256d23474e4ccb83207`. Both collectors reached healthy,
then terminated internally after approximately 20 minutes at the recorder
boundary. The final successful flush contained 195,534 linked raw/canonical
records, took 3.747 seconds, and advanced the manifest to revision 4. Because
the prior monitor observed only timers, it continued writing status with the
frozen revision after both collector goroutines had exited; the owner stopped
the service and the run has no terminal qualification claim.

Code-path reconstruction identified exhaustion of the 512 MiB recorder pending
budget, while the adapter wrapper hid the bounded recorder cause from the old
lifecycle log. The repair proactively flushes at 128 MiB, reports pending,
reserved, used, limit, threshold, and high-water bytes, preserves the underlying
recorder error through adapter wrappers, and treats any collector return as an
immediate terminal event. Rolling status now includes explicit per-instrument
running state. These changes prevent silent post-exit sampling and leave memory
headroom for segment construction.

Qualification evidence now includes the exact source commit, bounded reconnect
reason counts, dedicated resynchronization sample/over-limit/p95/exact-maximum
metrics, stage timings, HTTP status/retry-after, clock offset/uncertainty,
bounded objective fault attribution, process RSS/high-water/open-FD samples,
and filesystem capacity/inode samples. The five-minute
`a7-soak-status.json` is atomically replaced. The append-only
`a7-soak-events.jsonl` is synchronized per event, hash-chained, mirrored as
`A7_EVENT` service-log records, and verified at termination. Periodic flush,
capacity flush, collector termination, status-write, or journal failures fail
qualification closed. Status, journal, and terminal schemas also report
recorder pressure/high-water facts and declared collector running state. Recorded market
data remains outside Git and all earlier qualification directories remain
preserved. ADR-0011 keeps the all-sample 15-second SLO unchanged and records
attribution separately; attribution never converts a failed run into a pass.

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
- The 20-second two-instrument harness smoke passed public ingestion, segment
  flush, bounded replay verification, atomic status/evidence writes, and journal
  integrity. It records live book readiness and collector metrics but does not
  apply the 72-hour eligibility or SLO gates; short public endpoint timing is not
  a deterministic qualification window.
- The operational recorder-role integration passed against ephemeral
  PostgreSQL 18.4 and the compiled Binance public hosts, reached truthful book
  readiness, finalized segments, and registered both wire and canonical segment
  proofs.
- Source and platform-binary scans find the required public collector/recorder,
  exclude the emulator, and reject broader Binance origins and callable private
  or order surfaces.

The current dual-soak forensic candidate also records bounded REST response-body
timing and size metadata, distinguishing header timeout, body timeout,
interrupted body, close failure, empty success body, and oversized body. It
retains no response payload, URL, address, credential, or arbitrary error text.
The same taxonomy is used by the isolated Bybit runner.

## Formal soak gate

The formal command requires an empty dedicated output directory and cannot be
shortened:

```text
make a7-soak-smoke AXIOM_A7_SOURCE_COMMIT=<full-40-character-commit>

AXIOM_A7_SOAK=1 \
AXIOM_A7_SOAK_OUTPUT=<absolute-empty-artifact-directory> \
AXIOM_A7_SOURCE_COMMIT=<full-40-character-commit> \
go test ./internal/qualification \
  -run '^TestA7Continuous72HourPublicSoak$' -count=1 -timeout=73h -v
```

The resulting directory retains raw/canonical Parquet segments, cumulative
dataset manifests, append-only `a7-soak-events.jsonl`, rolling
`a7-soak-status.json`, terminal `a7-soak-evidence.json`, the exact source
commit, bounded reconnect reasons and lifecycle diagnostics,
resynchronization sample/over-limit/p95/exact-maximum metrics, incident/rebuild
samples, Go heap and process resource samples, storage-capacity samples, final
book eligibility, recorder pending/reserved/used/limit/threshold/high-water
facts, collector running state, and the bounded canonical replay checksum. The
service log retains immediate structured `binance_collector_lifecycle`,
`recorder_capacity_flush_requested`, `A7_EVENT`, and
emergency fallback records. A7 advances only if the terminal artifact says
`qualified: true` and the final cumulative candidate checks pass.

## Limitations

- The short smoke and public probe do not substitute for 72 continuous hours.
  Only the formal run applies book eligibility, rebuild, hot-path, and 15-second
  resynchronization gates.
- Public-data correctness and availability are not profitability evidence.
- A7 adds no account, signing, order, transfer, withdrawal, margin, leverage,
  derivative, staking, lending, borrowing, or short-selling capability.
