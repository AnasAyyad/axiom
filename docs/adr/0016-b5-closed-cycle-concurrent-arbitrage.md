# ADR-0016: Closed-cycle economics and concurrent cross-exchange simulation

- **Status:** Accepted
- **Date:** 2026-07-23
- **Scope:** V1B B5 cross-exchange evaluation, ownership, concurrency, and
  recovery

## Context

B5 compares independently collected Binance and Bybit books. Treating two fresh
books as automatically comparable can create false opportunities, while
treating an immediate two-leg USDT gain as profit can hide depletion of scarce
sell inventory. Concurrent virtual legs also create unknown and one-leg states
that cannot be resolved safely through timing assumptions.

## Decision

Every candidate binds its two executable books to one complete B2 coherent
view. The binding includes version, connection generation, all receive and
ingest ordering facts, clock intervals, collector identity and region, state
hashes, and the canonical version-vector identity. B5 independently evaluates
BTC-USDT and ETH-USDT in both venue directions and rejects missing, future,
stale, skewed, gapped, or incomparable inputs.

Admission uses exact full-depth conversions and requires positive expected and
worst profit after the complete inventory-restoration cycle. Buy and sell fees,
spread/depth, latency, recovery, maximum one-leg exposure, marginal replacement,
natural reversal, advisory rebalancing, exchange concentration, and USDT venue
concentration remain separate facts.

The sell account must own the base asset and the buy account must own the USDT.
The reviewed 30/50/70 bands pause, reduce, permit, or prefer a direction.
Central risk approval precedes one all-or-nothing seven-resource claim covering
both balances, both fee buffers, both depth slices, and recovery capacity.

Simulation schedules the two virtual legs concurrently from deterministic
latency samples. Unknown state must be verified before a single
risk-authorized retry. Otherwise a protected virtual unwind is attempted, and
unresolved exposure and claims are quarantined. Rebalancing output is advisory
evidence only and has no execution surface.

## Consequences

- Cross-exchange decisions reproduce one explicit as-of comparison rather than
  reconstructing a view from independently fresh books.
- A candidate cannot profit by silently consuming scarce inventory or omitting
  the cost and delay of returning to the target distribution.
- Concurrent outcomes remain deterministic across goroutine scheduling,
  restart, and map iteration.
- Eleven independent balanced journal categories preserve execution, inventory,
  cost, restoration, and combined portfolio economics.
- B5 can describe a need for rebalancing without implementing B6 or creating a
  transfer, withdrawal, or private exchange capability.

## Rejected alternatives

- Latest-book comparison without a coherent view: rejected because freshness
  alone does not prove temporal comparability.
- Immediate two-leg P&L as the ranking metric: rejected because it externalizes
  inventory replacement and concentration costs.
- Short selling on the expensive venue: rejected because V1 is Spot-only and
  may sell only owned inventory.
- Independent claims per leg: rejected because one-sided acquisition can
  double-spend balances or displayed liquidity.
- Unlimited retry or time-based inference for unknown state: rejected because
  ownership and loss become timing-dependent.
- Automatic rebalancing: rejected because B5 is advisory-only and V1 has no
  transfer or withdrawal capability.

## Validation

- Both-instrument, both-direction, exact economics, false-profit, coherent-view,
  inventory-band, permutation, race, fuzz, and declared-p99 tests.
- All concurrent result classes, unknown verification, bounded retry,
  protected unwind, quarantine, restart, and tamper tests.
- Atomic contention, fencing, seven-resource shape, ownership, advisory-only
  rebalancing, and eleven-category balanced journal tests.
- PostgreSQL 18 clean install and exact B4-to-B5 upgrade qualification,
  immutable aggregates, generated-query checks, and least-privilege role
  assertions.

## Revisit when

- B6 adds a reviewed advisory optimizer; its outputs remain data and cannot
  inherit execution authority from B5.
- The venue or instrument universe expands beyond the exact approved set.
- V1C considers authenticated sandbox execution; external side effects require
  a separate architecture and safety decision.
