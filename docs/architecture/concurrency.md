# V1A concurrency, ownership, and fencing

**Status:** Normative A0 architecture contract

## Goals

V1A may process independent instruments and offline jobs concurrently, but concurrency must not change results, duplicate ownership, overspend virtual balances, reuse combined-portfolio liquidity, lose critical events, or make shutdown unbounded.

## Ownership model

Mutable state has one declared owner:

| State | Ownership/serialization key |
|---|---|
| Order book and candle builder | exchange + instrument + connection generation |
| Market-data ingest ordinal | recorder/session |
| Strategy state | run + strategy + instrument |
| Reservation and balance | virtual account/portfolio + asset |
| Simulated liquidity | combined run + market-view version + price level |
| Order aggregate | order ID |
| Execution plan/saga | plan ID |
| Risk state | applicable platform/exchange/instrument/strategy/portfolio scope |
| Journal/projection commit | transaction plus affected account/asset rows |
| Shadow engine | protected shadow session/portfolio resource |
| Offline job | job ID/claim epoch |

One goroutine serially mutates each in-memory partition. Cross-partition coordination uses immutable messages, versioned views, deterministic barriers/schedulers where required, and database transactions for shared durable invariants. Concurrent readers never receive a mutable order book or configuration object.

## Bounded execution

All goroutines, worker pools, channels, retry loops, and staging areas have configured limits and cancellation. Each queue exposes capacity, depth, oldest age, saturation duration, coalesced/dropped counts, and recovery count without identifier labels.

Critical reservation, risk, order, fill, journal, and command events are never dropped. Capacity failure rejects new entry work and pauses/locks the affected scope. Book deltas are not sampled: saturation invalidates the whole unsafe generation and forces a snapshot rebuild. Only replaceable dashboard/current-state updates may coalesce.

## Deterministic concurrency

Concurrency is an optimization, not an ordering source. A dataset cursor admits
recorded events by `(recorded logical time, unique ingest_ordinal)`. Per-book
source sequence validates continuity in that order and never re-sorts recorded
input. After the cursor selects an input, eligible timers and derived/synthetic
work use the separate five-part deterministic scheduler tuple described in
[deterministic replay](deterministic-replay.md#scheduler-tie-breaking).
Exchange time, source sequence, and stable ID can therefore serve their
evidence, validation, and scheduler roles without rewriting recorded ingest.

Map iteration, channel select timing, goroutine scheduling, wall-clock timing, shared random streams, and process-local arrival races must not affect a decision or result checksum.

Parallel work emits results tagged with deterministic keys. A deterministic reducer commits them in canonical order. Random models use keyed substreams derived from stable run/model/decision/order/event identifiers, so adding unrelated work cannot consume another order's random draws. Combined strategies share one deterministic simulated-order scheduler and liquidity-consumption view.

## Lease acquisition

PostgreSQL holds one active lease per protected resource. Acquisition is an atomic compare-and-set transaction that:

1. rejects a currently valid owner;
2. assigns the requesting instance and expiry;
3. increments and returns a fencing token/epoch;
4. persists acquisition identity and UTC timestamps for audit.

A process is not an owner merely because it started first or still has a local timer. It is an owner only while its database lease and fencing token are current.

## Fenced mutations

Every protected write includes the current resource and fencing token in its database predicate or is executed inside an equivalent verified ownership transaction. Tokens only increase. A stale owner cannot renew, checkpoint, claim a command, mutate protected virtual execution state, or release a newer owner's lease.

In-memory work is rechecked at its commit boundary. A valid token at evaluation start is not sufficient if the lease is lost before commit. Database uniqueness and transaction constraints independently enforce one active owner, idempotency, nonnegative balances, exclusive reservations, and unique order/fill identities.

## Renewal and ownership loss

Renewal runs before expiry with bounded deadlines and jitter that cannot affect business determinism. On renewal uncertainty, database durability loss, fencing rejection, or confirmed replacement:

- atomically stop intake of new entry intents and execution plans;
- move the protected runtime to `LOCKED` and readiness false;
- cancel pending noncritical work;
- allow only bounded reconciliation, checkpoint, cancellation, or explicitly approved risk-reducing recovery that can prove current ownership;
- do not release reservations or report success based on uncertain ownership;
- emit a critical alert/audit event and begin the shutdown/recovery path.

If ownership cannot be proven, the old instance waits for termination and never attempts an unfenced final write. A clean owner releases its lease last using an owner-and-token conditional update. On an unclean exit, it leaves the lease to expire; the next owner receives a higher token and performs full recovery.

## Reservation and journal concurrency

Reservations use exact decimal amounts, row/version locks, and database constraints so concurrent candidates cannot reserve the same available funds or owned inventory. Expiry/release is compare-and-set against reservation state, order/plan state, and fencing token; active, unknown, cancel-pending, or recovery-required orders retain their reservation.

Fill reduction, journal posting, balances/positions, reservation consumption, and the corresponding outbox item commit atomically. Retry is idempotent by stable inbox/event identity. A conflicting or impossible transition creates an incident; it is never resolved by last-write-wins.

## Cancellation and shutdown

`context.Context` propagates cancellation and deadlines. Shutdown first closes entry intake, then drains/checkpoints critical queues in dependency order within the global 60-second maximum. Producers stop before consumers; the lease renewer continues until protected state is safe, and lease release occurs last. See [Lifecycle](lifecycle.md).

## Required evidence

- race-detector and stress tests for partitions, queues, leases, books, reservations, and projections;
- identical replay results across different goroutine schedules and configured worker counts;
- overlapping startup, lease expiry, stale-token, database loss, and kill-point tests;
- property/model tests proving no double reservation, negative balance, duplicate fill, stale order transition, or shared-liquidity reuse;
- overload tests proving a safe pause/rebuild rather than stale execution;
- goroutine-leak, bounded-memory, and shutdown-at-or-below-60-seconds tests.

## Known limitation

Fencing enforces single-writer safety on the supported single-server deployment. It does not turn V1A into an HA system or guarantee continued availability during PostgreSQL or host failure.
