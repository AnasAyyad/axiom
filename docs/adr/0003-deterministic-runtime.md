# ADR-0003: Deterministic event runtime

- **Status:** Superseded by [ADR-0008](0008-dataset-replay-and-scheduler-ordering.md)
- **Date:** 2026-07-12
- **Scope:** V1A ingest, replay, simulation, and recovery

## Context

Replay and backtest evidence is trustworthy only when identical inputs produce identical decisions, fills, journal entries, balances, P&L, and hashes regardless of goroutine scheduling or wall-clock pacing.

## Decision

All decision-relevant events use an immutable canonical envelope with stable ID, connection generation, source sequence/time when available, local monotonic/UTC receipt evidence, pre-fan-out ingest ordinal, schema/parser versions, and payload hash.

Project-owned clocks and schedulers control logical time and timers. Canonical ordering uses scheduled/replay time, exchange time, valid source sequence, ingest ordinal, then stable event ID, with ingest ordinal the dataset-wide tie-breaker. Parallel work commits through deterministic reducers. Random models use keyed substreams derived from stable run/model/decision/order/event identities and a root seed. Canonical serialization and run manifests are versioned.

Backtest, replay, paper, and shadow reuse the same strategy, allocator, risk, execution, and accounting code. Checkpoints contain the logical cursor and all state needed for exact continuation.

## Consequences

- Results are auditable, reproducible, and comparable across supported runtimes.
- Ambient time, map iteration, select races, shared RNG consumption, and worker completion order cannot authorize behavior.
- Envelope, scheduler, serialization, and model versioning add metadata and compatibility obligations.
- Replay speed affects wall time only.

## Rejected alternatives

- Best-effort ordering from channels/goroutines: nondeterministic.
- Wall-clock timers inside strategy or simulation: not replayable.
- One global seeded RNG: unrelated work changes draw consumption.
- Separate simplified backtest logic: allows mode drift and false evidence.

## Validation

Ten identical runs must have byte-identical canonical hashes. Golden/property tests cover ordering ties, logical clocks, pause/step/seek, keyed randomness, canonical decimal encoding, concurrency settings, checkpoint restart, and supported schema compatibility.

## Revisit when

A new data source, parallel algorithm, architecture, or model cannot satisfy the current deterministic contract. Any replacement requires compatibility evidence and a superseding ADR.
