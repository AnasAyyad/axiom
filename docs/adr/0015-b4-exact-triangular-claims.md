# ADR-0015: Exact triangular evaluation and atomic multi-resource claims

- **Status:** Accepted
- **Date:** 2026-07-23
- **Scope:** V1B B4 triangular-arbitrage evaluation, allocation, and recovery

## Context

B4 must evaluate two three-leg Spot cycles without false profit from floating
point, partial depth, fees, filters, rounding, stale books, or reuse of the same
virtual capital and displayed liquidity. A sequential simulated cycle can fail
after acquiring BTC or ETH, so allocation and recovery must remain deterministic
across contention and restart.

## Decision

The authoritative evaluator exhaustively checks the two approved USDT/BTC/ETH
cycles at every reviewed and dynamically clipped size. Every edge is an exact
native-orientation conversion through complete executable depth with explicit
instrument filters, fee asset, dust, VWAP, book version, and connection
generation. No logarithmic or binary floating-point discovery path participates
in B4.

A candidate proceeds only after central risk approval and one atomic claim
acquires its settlement balance, per-leg displayed liquidity, fee buffers, and
recovery allowance. The in-process and PostgreSQL claim implementations share
all-or-nothing semantics, canonical resource ordering, fencing tokens,
monotonic revisions, exact settlement, and explicit release, expiry, or
quarantine. A failed acquisition leaves no partial hold. A quarantined recovery
resource remains held until explicit reconciliation.

Simulation dispatches three legs sequentially. Each next leg receives the
previous leg's actual rounded net output and reads the deterministic future book
at arrival. One immediate USDT recovery attempt is permitted after an incomplete
cycle; failed recovery quarantines the unresolved BTC or ETH exposure. This
boundary records virtual outcomes only and has no external order capability.

## Consequences

- Candidate economics and retained evidence are reproducible from exact books,
  metadata, fee/model/configuration identities, and causation references.
- Displayed liquidity is a claimable resource rather than an advisory capacity,
  preventing concurrent candidates from spending the same depth.
- Sequential execution may expose intermediate virtual inventory; this is
  visible as recovery loss or quarantined inventory, never hidden in P&L.
- The claim primitive is intentionally strategy-neutral so B5 can reuse it
  without weakening B4's single-exchange rules.

## Rejected alternatives

- Floating-point logarithmic cycle search: rejected because B4's universe is
  small and exact exhaustive enumeration avoids exclusion and rounding error.
- Independent per-resource reservations: rejected because a partial acquisition
  can strand capital or double-count displayed depth.
- Treating all three legs as simultaneous: rejected because it would hide
  arrival latency, actual output chaining, and recovery exposure.
- Unlimited recovery retries: rejected because outcomes and loss would become
  timing-dependent and could retain unbounded virtual exposure.

## Validation

- Exact conversion, orientation, fee-asset, rounding, filter, depth, size, and
  permutation tests plus graph-cycle fuzzing.
- Central-risk, candidate-expiry, atomic contention, fencing, settlement,
  quarantine, restart, and tamper tests.
- Sequential arrival, actual-output chaining, all terminal outcomes, recovery,
  journal balance, and opportunity-lifetime tests.
- PostgreSQL 18 clean install and exact B3-to-B4 upgrade qualification,
  generated-query check, least-privilege role assertions, race detection, and a
  declared evaluator p99 at or below 25 ms.

## Revisit when

- The approved asset graph expands enough to justify a discovery accelerator.
- A venue introduces fee or filter semantics the exact conversion contract
  cannot represent.
- V1C adds authenticated sandbox execution; external side effects require a
  separate decision and cannot inherit simulation authorization.
