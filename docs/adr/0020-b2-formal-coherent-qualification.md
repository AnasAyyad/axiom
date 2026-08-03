# ADR 0020: B2 formal coherent-view qualification

- Status: Accepted
- Date: 2026-07-27
- Owners: Market Data, Storage, SRE

## Context

B2 already defines deterministic cross-exchange as-of views and Tier-A
multi-exchange datasets. Short live evidence cannot prove continuous operation,
recovery bounds, exact sample retention, or final dataset completeness across a
72-hour declared load. A formal runner must inherit the hardened Binance and
Bybit transport and collector behavior without adding a second recovery model.

## Decision

The formal gate runs three approved spot instruments on Binance and Bybit:
BTC/USDT, ETH/USDT, and ETH/BTC. All six collectors use 1,000 retained book
levels, 16,384-event queues, 15m/1h/4h candles, one process monotonic source,
and one dataset-wide ingest-ordinal allocator. Binance recovery snapshots use
5,000 levels. Separate candidate recorders retain Binance and Bybit data.

The runner waits no more than two minutes for all six combined book/clock
health snapshots and all three coherent pairs to be eligible. The official
72-hour clock begins only after that gate. It then samples every pair every five
seconds with `axiom.coherent-view-policy.v1` and its unchanged inclusive limits:
250 ms book age, 250 ms inter-book skew, and 100 ms clock uncertainty.
Corrected clock intervals must overlap; generations must be active; gaps must be
resolved; and every input must be at or before the trigger.

Every success and bounded rejection is retained in atomic five-minute
hash-chained segments. Arbitrary errors are not part of the schema. A rejection
opens a pair-specific degradation interval. Recovery at exactly 15 seconds
passes; recovery later than 15 seconds fails. An unresolved interval at the
official end also fails. Final verification checks every segment checksum and
hash link and deterministically replays every sample.

The runner reuses the synchronous named qualification journal, atomic JSON
writer, empty-root preflight, bounded failure evidence, resource sampling, and
collector lifecycle sink. Existing A7 and B1 schemas and acceptance behavior do
not change.

B2 uses a 1 GiB heap ceiling, each recorder's existing 512 MiB pending limit and
128 MiB proactive-flush signal, and a 10 GiB minimum free-space floor. Missing
capacity evidence fails closed. Collector exit, diagnostic loss, decoder error,
source gap, hot-path p99 above 10 ms, recovery above 15 seconds, evidence loss,
recorder failure, or terminal-evidence failure also fails closed.

At official end, all six health snapshots and all three pair states are frozen
before cancellation. After collectors stop, both recorder chains and datasets
are verified, one `axiom.multi-exchange-dataset.v1` Tier-A aggregate is retained
atomically, and the terminal B2 evidence is written.

The 20-second smoke target is explicitly non-formal. A successful smoke proves
only that the harness can operate; it cannot set `qualified:true` or promote B2.

## Consequences

Formal operation requires an exact clean committed source, an explicit opt-in,
a bounded collector region, and a dedicated absolute empty output root. The
future systemd unit must use a dedicated log, a public-only environment,
`Restart=no`, and a 73-hour timeout. This ADR does not install that unit or start
the formal run.

B2 remains pending until the complete 72-hour evidence passes and the required
approvers accept it. Existing A7 and B1 evidence remains immutable and is not
rerun or reinterpreted by this decision.

## Rejected alternatives

- Reusing the short live qualification: it does not prove continuous recovery,
  resource, evidence, or final dataset behavior.
- Copying A7/B1 harnesses: duplicated evidence logic would drift and could
  silently change their accepted schemas.
- Starting the clock before readiness: startup latency would be misclassified
  as official coherent-view degradation.

## Validation

Deterministic boundary, replay, corruption, atomic-write, resource, cancellation,
recorder-contention, Tier-A, race, public-boundary, PostgreSQL, and full
repository gates must pass. The non-formal smoke must run from the exact clean
committed source before a formal service is prepared.

## Revisit when

A versioned coherent-view policy, declared instrument set, recorder schema,
collector recovery contract, or qualification resource limit changes. A change
requires a superseding ADR and new formal evidence; prior evidence is not
rewritten.
