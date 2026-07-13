# V1A hot path

**Status:** Normative A0 architecture contract

## Purpose

The V1A hot path turns one eligible Binance public event into a deterministic virtual decision, simulated execution result, and exact accounting result. It stays inside the shadow engine so module boundaries do not add network, serialization, or database-query latency to candidate evaluation.

## Logical flow

```text
Binance public frame
-> capture immutable raw envelope for the decision-input recorder
-> decode and validate schema, time, instrument, generation, and sequence
-> emit normalized canonical event
-> enqueue on the exchange/instrument ordered partition
-> update the local order book or completed-candle state
-> publish an immutable market-view version
-> evaluate the Trend strategy
-> allocate virtual capital/inventory and create an exclusive reservation
-> run central risk evaluation
-> create an exchange-valid simulated execution plan
-> schedule simulated arrival against the future eligible market view
-> reduce simulated order/fill events
-> atomically persist durable order, reservation, journal, projection, and outbox state
-> publish metrics, audit facts, and resumable API events asynchronously
```

Raw capture means the frame and envelope are accepted into a bounded decision-evidence pipeline before their derived decision can proceed. Compression, final file sync, reporting, and UI delivery are not synchronous hot-path work. If the required evidence pipeline cannot preserve the input, new decisions pause; the engine must not silently substitute the standalone recorder's independently received stream.

## In-process invariant

The following modules share one Go process and communicate through typed project-owned interfaces or the bounded in-process event bus:

```text
market view -> strategy -> allocator -> risk -> planner -> simulator -> accounting
```

There is no internal HTTP/RPC, JSON serialization, database query, API callback, Python process, or frontend dependency between those modules. The Trend strategy cannot call the simulator or mutate a reservation. Only the allocator reserves owned virtual funds/inventory, only risk can approve entry intent, only the order reducer accepts state transitions, and only accounting posts journal transactions.

## Durability boundaries

Candidate calculation reads immutable in-memory views. Critical state is nevertheless durable before the system announces a transition as committed:

- reservation approval is atomic and cannot overspend available ownership;
- simulated order/fill reduction, journal posting, balance/position projection, reservation consumption/release, audit causation, and outbox insertion share an explicit transaction where they are one logical fact;
- every journal transaction balances independently per asset;
- an unavailable or slow critical store rejects new work and moves the affected scope to `PAUSED` or `LOCKED` rather than allowing an in-memory-only success;
- downstream outbox delivery is asynchronous and idempotent.

These persistence commits are state-transition boundaries, not database hops between strategy, allocator, and risk calculations.

## Ordering and views

Each exchange/instrument has one ordered writer. Ingest assigns the ordinal
before fan-out; a recorded dataset is later read only by `(recorded logical
time, ingest_ordinal)`. The writer validates source sequence inside the
exchange/instrument/connection generation while preserving that accepted
ingest order. A sequence defect may reject an event or invalidate and rebuild
the generation, but it never causes recorded events to be re-sorted by exchange
time or sequence.

Once the replay cursor selects an input, timers, simulated arrivals, and other
derived/synthetic work use the five-part scheduler tuple defined in
[deterministic replay](deterministic-replay.md#scheduler-tie-breaking). The
scheduler cannot expose a later recorded input early, so scheduling cannot
change recorded ingest order.

Readers receive immutable versioned views, never mutable maps. A decision records the exact event identity, ingest ordinal, connection generation, book/candle version, configuration, strategy, risk, portfolio, fee, latency, fill, and software versions that influenced it.

The strategy may consume only `HEALTHY` data. An unresolved gap, crossed/invalid book, stale generation, excessive age or clock uncertainty, failed checksum, missing completed candle, or hard quality failure makes that view ineligible. Ineligible data never reaches a new strategy decision.

## Bounded queues and overload

| Event class | Hot-path policy |
|---|---|
| Book snapshot/delta | Never skip one delta inside a generation. On overflow or gap, invalidate the generation, discard unsafe queued work, pause that instrument, and resynchronize. |
| Reservation, risk, order, fill, journal, command | Never drop or coalesce. Reject new entries and fail closed if durable/bounded capacity is unavailable. |
| Decision-input recording | Preserve through bounded staging; otherwise record an explicit gap and pause decisions requiring complete evidence. |
| Expired opportunity | Drop with stable reason and metric; never execute stale work. |
| UI/derived metric update | Coalesce by bounded key; REST snapshots and durable revisions are authoritative. |

Backpressure never expands an unbounded buffer and never turns stale work into execution.

## Cold path

The shadow engine must not wait for React rendering, SSE delivery, charting, analytics queries, aggregate reports, alert delivery, long-term compression, or notification sinks. Raw/normalized segment finalization, reports, aggregate metrics, dashboard projections, alerts, and exports run asynchronously with their own bounds and failure policies.

## Failure behavior

- Decode/schema failure: retain safe raw evidence, reject the event, update adapter health, and incident/alert according to threshold.
- Sequence gap or queue saturation: invalidate the affected generation, pause evaluation, and rebuild from a fresh public snapshot.
- Stale/low-quality view: reject with a stable data-quality reason.
- Reservation conflict: reject without changing balances.
- Missing/overflowed risk input: fail closed.
- Journal or critical transaction failure: no durable transition is reported; pause/lock and reconcile.
- Lease/fencing loss: stop accepting plans before any further protected mutation.
- Decision-evidence failure: pause new decisions and preserve an explicit gap/quarantine record.

## Measurements and acceptance evidence

Expose bounded-label p50/p95/p99 decode/book, strategy, allocation, risk, simulation, and critical-commit durations; partition depth and oldest age; gap/rebuild counts; rejection reasons; decision-evidence lag; and outbox lag. Initial targets include p99 decode/sequence/book update at or below 10 ms and p99 strategy/allocator/risk at or below 25 ms at declared load.

Benchmarks and tests must cover order-book updates, decimal arithmetic, Trend evaluation, allocation/risk, deterministic simulated scheduling, overload, stale-data rejection, journal atomicity, and absence of internal network calls. Performance claims require results on the declared reference machine.

## Known limitations

The hot path is designed for reliable public-Internet research, not colocated HFT. Public order books and modeled latency cannot prove hypothetical production fills or market impact.
