# ADR-0004: PostgreSQL journal, inbox, outbox, jobs, and leases

- **Status:** Accepted
- **Date:** 2026-07-12
- **Scope:** V1A transactional persistence and durable coordination

## Context

Virtual balances, reservations, simulated orders/fills, accounting, commands, ownership, and API events must survive crashes without double spending, duplicate effects, missing journal facts, or unaudited control shortcuts. V1A does not justify an additional durable broker.

## Decision

Use PostgreSQL 18 as the transactional source of truth for the immutable multi-commodity journal, rebuildable projections, reservations, simulated order aggregates, commands/jobs, inbox identities, outbox events, consumer cursors, and execution leases/fencing epochs.

One transaction atomically couples fill/order reduction, journal posting, balance/position projection, reservation change, inbox identity, and outbox item when they are one business transition. Unique identities make redelivery idempotent. Administrative commands always use the durable command/inbox path. Engine events are consumed from monotonically revised outbox rows. `LISTEN/NOTIFY` is only a wake-up hint. Raw high-volume depth remains in Parquet, referenced by PostgreSQL manifests.

## Consequences

- Transactional constraints enforce journal balance, ownership, idempotency, and durable state/event coupling.
- API and engine can restart and resume from durable cursors.
- PostgreSQL availability and capacity are safety dependencies; critical-write loss pauses/locks new work.
- Outbox/inbox cleanup, partitions/indexes, lag monitoring, and backup/restore require explicit operations.

## Rejected alternatives

- Direct in-memory API-to-engine commands: unaudited and lost on restart.
- Publishing an event after a separate database commit: creates state/event dual-write gaps.
- Redis/Kafka/NATS: adds an unnecessary second durability/operations model for V1A.
- Every raw market delta in PostgreSQL: mismatches the high-volume storage design.

## Validation

Integration/property/kill-point tests must prove balanced journal transactions, no concurrent double reservation, inbox/outbox idempotency, monotonic fencing, stale-owner rejection, projection rebuild, redelivery, backup/restore, and zero loss after acknowledged critical commits.

## Revisit when

Measured throughput, retention, or availability requirements exceed a tuned PostgreSQL design. A superseding ADR must define atomicity, ordering, replay, migration, recovery, and operations evidence across any new boundary.
