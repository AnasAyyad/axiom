# ADR-0008: Dataset replay and scheduler ordering

- **Status:** Accepted
- **Date:** 2026-07-12
- **Scope:** V1A recorded ingest, dataset replay, per-book validation, and deterministic scheduling
- **Supersedes:** [ADR-0003](0003-deterministic-runtime.md)

## Context

ADR-0003 used one five-part tuple to describe both replay input order and
deterministic scheduler order. That wording can be read to let exchange event
time or a source sequence reorder events whose receive order was already
recorded. The authoritative specification instead makes `ingest_ordinal` the
dataset-wide total-order tie-breaker and requires replay to sort by recorded
logical time and that ordinal.

Three related concerns therefore need separate contracts: preserving recorded
input order, validating one source's order-book sequence, and deterministically
ordering timers or derived/synthetic work.

## Decision

### Recorded dataset input

Within the ordering scope declared by a dataset manifest, every replayable
record has a present, unique `ingest_ordinal` assigned before concurrent
fan-out. The dataset reader admits recorded events in exactly this order:

1. recorded logical time;
2. `ingest_ordinal`.

Recorded logical time is the versioned logical/monotonic time stored by the
dataset schema. It is not recomputed from exchange UTC time during replay.
Missing or duplicate ordinals make the dataset invalid for deterministic
replay; stable event ID is not a fallback that hides an ordinal defect.

Exchange event time, source sequence, and stable event ID remain immutable
evidence and identity fields. They do not participate in sorting recorded
dataset input and cannot move a later-ingested record ahead of an
earlier-ingested record at the same logical time.

### Per-book source validation

A source sequence is interpreted only inside its exchange, instrument, source
connection, and connection generation. The single writer for that book
validates snapshot bridging, continuity, duplicates, regressions, and gaps in
recorded ingest order. Validation may reject a duplicate, invalidate a book
generation, pause decisions, or require a rebuild; it never repairs evidence
by sorting recorded events by source sequence.

Exchange event time is used for exchange semantics, completed-candle rules,
freshness and clock-uncertainty checks, and recorded as-of evidence. It is not a
globally trustworthy clock and does not define cross-source ingest order.

### Deterministic scheduler tie-breaking

After the dataset cursor has selected the next recorded input, the scheduler
orders eligible timers, delayed effects, simulated arrivals, faults, and other
derived or synthetic work with the implementation plan's five-part tuple:

1. replay or scheduled logical time;
2. causal exchange event time when applicable;
3. valid causal source sequence within its source/generation when applicable;
4. causal `ingest_ordinal` when applicable;
5. stable event or work ID.

The serialization/scheduler version defines canonical sentinels for fields
that do not apply. Source sequence participates only when the work carries a
validated source-generation context. Stable ID is the final defensive
tie-breaker for scheduled work, not a repair mechanism for malformed recorded
input.

The replay reader exposes recorded inputs through its strict two-key cursor; it
does not place multiple recorded inputs into the scheduler for the five-part
tuple to re-sort. A later recorded input becomes eligible only after the prior
input boundary and its explicitly defined consequences have been reduced.
Scheduler interleaving therefore remains deterministic without changing the
relative order of recorded ingest.

| Envelope field | Recorded dataset order | Per-book validation | Scheduler use |
|---|---|---|---|
| Recorded logical time | Primary key | Evidence | Logical-time key |
| Exchange event time | Never a sort key | Time/candle/freshness evidence | Optional causal tie-breaker |
| Source sequence | Never a sort key | Scoped continuity authority | Optional validated causal tie-breaker |
| `ingest_ordinal` | Unique secondary key | Preserved receive-order evidence | Optional causal tie-breaker |
| Stable event/work ID | Identity, deduplication, integrity | Identity | Final defensive tie-breaker |

The dataset ordering policy and scheduler comparator/sentinel policy are
independently versioned in the run manifest and canonical result identity.

## Consequences

- Replay preserves what the recorder observed instead of constructing a new
  history from exchange clocks or incomparable source sequences.
- Book sequencing remains strict and fail-closed without being mistaken for a
  dataset-wide merge order.
- The plan's five-part tuple still gives scheduled and synthetic work a total,
  reproducible order.
- Implementations need distinct dataset-cursor, source-validator, and scheduler
  comparator tests and versions.
- Runs produced under ADR-0003's ambiguous comparator cannot be declared
  compatible automatically; compatibility requires fixture evidence or an
  explicit deterministic translation.

## Rejected alternatives

- Apply the five-part tuple directly to all recorded rows: exchange time or
  source sequence could rewrite recorded ingest order.
- Treat source sequences as a cross-exchange total order: sequence scopes are
  source- and generation-specific and are not comparable globally.
- Use stable event ID when an ingest ordinal is missing or duplicated: this
  would make a corrupt or incompatible dataset appear valid.
- Order only by wall-clock or exchange time: clock correction, skew, and
  exchange timestamp semantics would make replay unfaithful.

## Validation

- Golden fixtures prove dataset records replay by `(recorded logical time,
  ingest_ordinal)` even when exchange times or source sequences would sort them
  differently.
- Missing and duplicate ordinal fixtures fail closed before strategy
  evaluation.
- Book fixtures prove valid bridging and continuity and prove that duplicate,
  regressing, out-of-order, and gapped sequences reject/invalidate rather than
  reorder input.
- Scheduler fixtures cover every five-part key, canonical missing-field
  sentinels, same-time causal consequences, stable-ID ties, and timers or
  synthetic work with no exchange fields.
- Repeated runs under different worker counts, replay speeds, and injected
  goroutine schedules produce byte-identical canonical results.
- Compatibility fixtures cover every retained dataset-ordering and scheduler
  version.

## Revisit when

A dataset cannot provide a unique ingest ordinal, a new scheduler work class
cannot be ordered by this contract, or a cross-partition barrier/watermark is
introduced. Any replacement requires a superseding ADR and replay-compatibility
evidence.
