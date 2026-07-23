# Cross-exchange arbitrage V1B strategy

## Scope and authority

`cross-exchange.v1b.1` is the B5 simulation-only Spot strategy for BTC-USDT and
ETH-USDT across Binance and Bybit. It exhaustively evaluates both approved
directions:

```text
buy Binance / sell owned inventory on Bybit
buy Bybit / sell owned inventory on Binance
```

The implementation in `internal/strategies/crossarb` is authoritative for
backtest, replay, and public-data shadow decisions. It consumes B2 coherent
views and owned virtual inventory, then emits candidates, virtual execution
outcomes, and accounting evidence. It cannot submit authenticated or production
orders.

## Coherent views and exact economics

Each instrument is evaluated from exactly one two-member B2 coherent view. The
executable books must match the retained exchange, instrument, connection
generation, book version, receive monotonic time, receive UTC and Unix time,
ingest ordinal, state hash, collector instance, collector region, clock
uncertainty interval, and version-vector identity. Missing members, gaps,
future data, incompatible regions, clock uncertainty, book age above 250 ms,
or inter-book skew above 250 ms fail closed.

The shared exact conversion engine uses complete executable ask or bid depth,
instrument tick and step filters, minimum quantity and notional, VWAP, fees,
spread/depth cost, and dust. Binary floating point is absent from prices,
quantities, balances, fees, costs, inventory shares, and P&L.

An immediate two-leg USDT gain is not sufficient. Admission separately charges:

- buy and sell fees;
- spread and executable-depth cost;
- latency deterioration and recovery allowance;
- maximum one-leg loss;
- marginal inventory replacement;
- natural-reversal and advisory rebalancing cost;
- exchange concentration and USDT venue-concentration penalties.

Both expected and worst closed-inventory-cycle profit must remain strictly
positive after every cost. The model, configuration, metadata, risk, ownership,
and coherent-view identities are retained with the decision.

## Inventory policy and atomic claims

The sell venue must own the base asset before evaluation, and the buy venue must
own the required USDT. Strategy-owned BTC or ETH share is evaluated separately
from USDT distribution:

- at or below 30 percent, the depleted sell direction pauses;
- above 30 and below 50 percent, its notional is reduced to at most 50 USDT;
- from 50 through 70 percent, the normal 100 USDT cap applies;
- above 70 percent, the direction is preferred as a natural reversal.

The strategy can emit a read-only rebalancing need, but B5 has no route,
transfer, withdrawal, or B6 executor.

Central risk approval precedes allocation. One fenced atomic group exclusively
claims the buy-side USDT balance, sell-side owned base balance, two fee buffers,
two exact displayed-depth slices, and recovery capacity. Contention cannot
leave a partial hold. Release, expiry, settlement, and quarantine require the
current revision and fence; unresolved ownership deliberately remains held for
reconciliation.

## Concurrent simulation and recovery

Both virtual legs are scheduled concurrently from deterministic
exchange-specific latency samples. Each arrival reads its future public book
and reprices the exact leg. Evidence distinguishes:

- both filled;
- buy only or sell only;
- partial buy, partial sell, or both partial;
- both missed;
- economics negative before arrival;
- delayed or unresolved unknown state.

Unknown state is verified before any retry or unwind. Central risk may authorize
at most one bounded retry. Otherwise the simulator performs one protected
virtual unwind; an unresolved position is quarantined. Saga state, exposure,
loss, latency version, and canonical checkpoint hash make the result
restart-comparable and tamper-evident.

## Accounting and persistence

Every outcome requires eleven independently balanced double-entry transactions:
execution P&L, BTC inventory market P&L, ETH inventory market P&L, stablecoin
valuation, fees, spread, slippage, latency, recovery, inventory restoration,
and combined P&L. Inventory losses cannot be hidden inside an arbitrage-profit
label.

Migration 000017 stores immutable candidate, exact B2 member, leg, inventory,
claim, simulation, advisory rebalancing, and journal-link evidence. Deferred
constraints require complete two-member, two-leg, two-inventory, and
eleven-journal aggregates. Security-definer claim functions use a fixed search
path, revoke public execution, and are granted only to the reviewed roles.

## Limitations

B5 proves deterministic platform behavior, not strategy viability or
profitability. It uses public market data and virtual owned inventory only. It
has no authenticated exchange transport, private endpoint, external order,
testnet/demo order, margin, derivative, leverage, short sale, transfer,
withdrawal, borrowing, lending, staking, or rebalancing execution capability.
Formal acceptance remains subject to predecessor, deferred soak, and
Product/Security/QA/SRE gates.
