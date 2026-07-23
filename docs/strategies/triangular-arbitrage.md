# Triangular arbitrage V1B strategy

## Scope and authority

`triangular.v1b.1` is the B4 baseline: exact, exhaustive, single-exchange Spot
simulation over the two approved paths:

```text
USDT -> BTC -> ETH -> USDT
USDT -> ETH -> BTC -> USDT
```

The Go implementation in `internal/strategies/triangular` is authoritative in
backtest, replay, and public-data shadow modes. It consumes three healthy,
fresh, approved books for BTC-USDT, ETH-USDT, and ETH-BTC or the native inverse
orientation. It emits candidates and virtual simulation/accounting evidence;
it cannot submit an external order.

## Exact conversion and sizing

`internal/strategies/arbitrage` converts each source asset into its target using
the native instrument side and complete executable book depth. It applies price
tick and quantity-step rounding, minimum and maximum quantity, minimum
notional, spread/depth cost, VWAP, source/target/third-asset taker fees, and
source dust. Prices, quantities, balances, fees, P&L, and edges use project
decimal wrappers; binary floating point is absent.

Every evaluation checks both paths at the reviewed 10, 25, 50, and 100 USDT
ladder points plus the exact dynamically clipped capacity. Capacity is the
smallest applicable owned settlement balance, triangular strategy budget, and
100 USDT cap after the global reserve and recovery allowance. Required fee
balances must already be owned. A size can pass while a larger size fails
because its full-depth economics or filters differ.

## Admission and claims

The reviewed `axiom.config.v1b.3` graph records `triangular.v1b.1`,
`triangular-exact-depth.v1`, sequential dispatch, the two exact paths, and 18
immutable parameter contracts. Older configuration schemas retain their
original interpretation.

A candidate exists only when all three books are active, generation-valid,
sequence-healthy, and no older than 250 ms; every conversion is filter-valid
and fully executable; expected net and worst-case net are positive; and the
worst-case edge is strictly greater than the additional 15 bps safety margin.
The reviewed deterioration applies a conservative per-leg haircut. Candidate
lifetime is 250 ms from first detection.

Central risk approval precedes allocation. One all-or-nothing claim group
acquires settlement capital, fee buffers, each exact displayed-liquidity slice,
and the recovery allowance. Canonical resource ordering, fencing tokens, and
monotonic revisions prevent partial acquisition and concurrent reuse. Partial
and final settlement consume exact amounts. Release and expiry return unused
capacity; quarantine deliberately retains unresolved capacity for
reconciliation. Both the in-process checkpoint and PostgreSQL representation
validate their canonical restart hash.

## Sequential simulation and recovery

The simulator uses the shared execution saga with three dependent legs. At each
configured arrival it reads the deterministic future book, recalculates the
exact fill, and passes the actual rounded net output to the next leg. It records
`full_success`, `partial_cycle`, `missed_leg`,
`negative_after_latency`, or `stranded_asset`.

After a failure following one or two legs, B4 permits one immediate exact
conversion back to USDT. A successful recovery records its output and loss. An
unavailable or invalid recovery book quarantines the BTC or ETH exposure and
keeps it visible in saga, claim, inventory, and accounting evidence. Restart
rebuilds the same terminal state and canonical hash.

## Accounting and opportunity lifetime

Every durable outcome links balanced double-entry transactions with separate
categories for trade economics, fees, spread/depth, rounding dust, latency,
recovery/unwind, stranded inventory, and explicit reconciliation adjustment.
Recovery and stranded inventory are never folded invisibly into strategy P&L.

The bounded deterministic lifetime tracker retains first detection, last
profitable observation, peak edge, edge at simulated arrival, total lifetime,
and survival at the configured p50, p95, and p99 latency thresholds.

## Limitations

B4 is simulation-only and makes no profitability claim. It has no authenticated
exchange transport, production order, testnet/demo order, margin, derivative,
leverage, short, transfer, withdrawal, borrowing, lending, or staking
capability. Formal acceptance remains subject to predecessor, deferred soak,
and Product/Security/QA/SRE gates.
