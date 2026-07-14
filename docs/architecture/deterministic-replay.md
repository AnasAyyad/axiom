# Deterministic replay

**Status:** Normative architecture contract; A3 runtime primitives implemented

## A3 implementation checkpoint

`internal/runtime` now implements the canonical envelope, pre-fan-out ingest
ordinal, strict two-field replay cursor, five-field derived-work scheduler,
real and deterministic clocks, keyed random draws, sequence validation, and
immutable market-view vectors described here. Strategy, simulator, checkpoint,
and dataset compatibility behavior remains owned by A7-A10 and must not be
inferred from these primitives.

## Purpose

Given the same approved dataset, immutable configuration, software identity, starting state, and root seed, V1A must produce the same ordered decisions, simulated orders/fills, journal state, balances, P&L, and canonical result hash. Goroutine scheduling, replay speed, machine wall time, and unrelated work must not change the result.

Backtest, replay, paper, and shadow use the same Trend strategy, allocator, risk, execution-planning, simulation, and accounting packages. Replay adapters supply time and events; they do not reimplement business logic.

## Reproducibility identity

Every run manifest records at least:

- immutable run ID and mode;
- code commit, build flags, toolchain version, dependency-lock hashes, and relevant architecture compatibility identity;
- dataset manifest and ordered raw/normalized segment hashes;
- event schema, parser/normalization, canonical serialization, and deterministic scheduler versions;
- immutable configuration hash and strategy/risk/accounting policy versions;
- starting balances, positions, reservations, and checkpoints;
- instrument metadata, asset-screening, fee, latency, spread, slippage, fill, valuation, and quality-model versions;
- configured root seed and keyed-random derivation version;
- expected canonical event/result hashes.

Missing or incompatible identity makes a run ineligible for a reproducibility claim. Compatibility translation must be explicit, versioned, deterministic, and covered by golden fixtures; data is never silently reinterpreted.

## Event envelope

Every recorded or decision-relevant event has an immutable envelope containing:

- schema version, stable event ID, payload hash, and raw-segment reference;
- recorder/session ID, source connection ID, and connection generation;
- exchange and instrument when applicable;
- exchange event time and source sequence when supplied;
- local UTC receipt time and monotonic offset from session start;
- recorded logical time for dataset replay and scheduled logical time for
  derived/synthetic work;
- session-local monotonically increasing ingest ordinal assigned before concurrent fan-out;
- parser/normalization version and run/partition identity where applicable.

UTC timestamps describe evidence. Monotonic/logical time controls duration, age, deadlines, and scheduling. Wall-clock corrections may not reorder an already-ingested event or make age negative.

## Ordering domains

Recorded input order, source-sequence validation, and scheduler tie-breaking
are separate contracts. Their comparator and sentinel versions are pinned in
the run manifest. [ADR-0008](../adr/0008-dataset-replay-and-scheduler-ordering.md)
is the durable decision.

### Dataset replay order

Within the ordering scope declared by the dataset manifest, replay reads
recorded events by exactly:

1. recorded logical time;
2. unique `ingest_ordinal`.

The ordinal is assigned before concurrent fan-out and is the authoritative
dataset-wide tie-breaker. A missing or duplicate ordinal invalidates the
dataset for deterministic replay. Exchange event time, source sequence, and
stable event ID remain evidence/identity fields; none may re-sort recorded
input or conceal an ordinal defect.

### Per-book source validation

The ordered book writer consumes the recorded ingest stream and validates
source sequence only within one exchange, instrument, connection, and
generation. A duplicate, regression, out-of-order update, bridge failure, or
gap is rejected or invalidates that generation and pauses decisions; the
validator never sorts the input to manufacture continuity. Exchange event time
supports exchange semantics, candle finality, freshness, clock-uncertainty,
and as-of evidence, but it is not a global clock or a dataset merge key.

### Scheduler tie-breaking

After the dataset cursor has selected the next recorded input, timers, delayed
effects, simulated arrivals, faults, and other derived or synthetic work use
the implementation plan's five-part deterministic tuple:

1. replay or scheduled logical time;
2. causal exchange event time when applicable;
3. valid causal source sequence within its source/generation when applicable;
4. causal ingest ordinal when applicable;
5. stable event or work ID as the final defensive tie-breaker.

The scheduler/serialization version defines canonical sentinels for optional
fields. The replay cursor does not expose multiple recorded inputs for this
tuple to re-sort: a later recorded input becomes eligible only after the prior
input boundary and its defined consequences are reduced. The five-part tuple
therefore determines scheduler work without changing the relative order of
recorded ingest.

Parallel workers may calculate independent values, but a deterministic reducer commits their outputs by stable key and canonical order. Replay never selects a result based on which goroutine finishes first.

## Time and scheduling

All time-dependent code depends on project-owned `Clock` and `Scheduler` interfaces:

- real time uses monotonic duration plus persisted UTC evidence;
- replay/backtest use a logical clock advanced only by the scheduler;
- timers become scheduled events ordered with market and model events;
- pause does not advance logical time;
- step processes one canonical event and all explicitly defined same-timestamp consequences;
- accelerated and maximum-speed modes change only wall-clock pacing, never logical order or outcomes;
- seek restores a verified checkpoint and replays forward; it does not skip state-producing events.

No strategy, simulator, risk rule, or journal timestamp may call an ambient wall clock directly for authoritative behavior.

## Keyed randomness

Stochastic latency, slippage, fill, adverse-selection, and fault models use independent deterministic substreams. A substream key includes the run, model/version, decision, order/leg, event/fault identity, and configured root seed as applicable.

There is no process-global sequential random stream. Retrying, adding logging, changing worker count, or scheduling an unrelated order therefore cannot consume another model's draws. Keys and derivation algorithms are part of the run manifest.

## Exact arithmetic and serialization

Authoritative financial values use project-owned exact decimal wrappers with explicit contexts, scales, traps, and rounding. Binary floating point may be used only for non-authoritative display/statistics after exact inputs are fixed.

Hashes use one versioned canonical byte encoding with fixed field order, decimal text normalization, UTC representation, enum representation, collection order, and omission/null rules. JSON object or Go map iteration is not a hash input unless keys are explicitly canonicalized.

## Checkpoints and recovery

A checkpoint includes its input cursor/ordinal, logical clock, scheduler queue, keyed-model state/version, market-view versions, strategy state, simulated liquidity, orders/plans, reservations, journal/projection version, and manifest/hash chain. It is committed only with a durable cursor and verified before use.

After a crash, replay restores the last verified checkpoint and idempotently reprocesses subsequent events. Duplicate inbox/event identities and journal constraints prevent duplicate orders, fills, or postings. A corrupt or incompatible checkpoint is quarantined; the engine falls back to an earlier verified checkpoint or full replay.

## Failure and confidence rules

Raw/normalized gaps, incompatible schemas, corrupt segments, missing metadata, unresolvable sequence gaps, or absent model versions are recorded in the manifest. Policy either rejects the run or assigns the documented lower confidence tier; it never silently claims full reproducibility. Candle-only data supports Trend research but is not execution-grade evidence for arbitrage.

## Required evidence

- Ten identical runs produce byte-identical canonical hashes and exact decisions, orders, journal entries, balances, and P&L.
- Results remain identical across supported concurrency settings, replay speeds, and injected scheduler interleavings.
- Golden tests separately cover `(recorded logical time, ingest_ordinal)`
  dataset order, adversarial exchange-time/sequence values, ordinal defects,
  all five scheduler tie-breakers, missing optional fields, map/set
  canonicalization, decimal encoding, keyed randomness, pause/step/seek, and
  checkpoint restart.
- Compatibility fixtures cover every supported prior event/schema version.
- No-look-ahead tests prove completed-candle and simulated-arrival semantics.

## Known limitations

Reproducibility is guaranteed only for declared compatible toolchains, architectures, schemas, models, configurations, and complete/qualified datasets. Deterministic simulation makes assumptions repeatable; it does not make public-data fill assumptions equivalent to production execution.
