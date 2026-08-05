# Journal and valuation

## Purpose and model

Axiom uses exact decimal/fixed-point domain values and an immutable per-commodity
double-entry journal for virtual portfolios. Binary floating point is forbidden
for prices, balances, quantities, fees, allocations, risk, and P&L.

Every transaction contains a unique ID, run/portfolio/strategy causation,
configuration identity, deterministic event time, and balanced lines. Debits and
credits balance independently for each asset; BTC cannot numerically balance a
USDT difference. Corrections append one exact inverse reversal and never mutate
history.

## Reservations and settlement

An order first reserves exact owned inventory or quote balance. Available and
reserved values change atomically with a revision. Partial fills consume only
the settled portion; cancel/reject releases only known-safe remainder. An
ambiguous or quarantined order keeps uncertain funds reserved until
reconciliation proves the outcome. A sell greater than available plus safely
reserved owned base is rejected.

## Cost and P&L formulas

Weighted-average unit cost is `total inventory cost / owned quantity`, using
explicit decimal scale and rounding. A buy adds quote cost and applicable fees
to inventory cost. A sell removes `sold quantity * prior weighted-average unit
cost`; realized inventory P&L is net proceeds minus removed cost and fees.
Unrealized P&L is current marked value minus remaining inventory cost.
Arbitrage execution loss, fees, slippage, and latency attribution use separate
journal account classes and reports; they are never merged into inventory P&L.

Valuation records price source, timestamp/freshness, base/reference currency,
confidence, depeg policy, model version, and configuration hash. Missing, stale,
crossed, or unsupported marks yield an explicit unavailable/degraded valuation,
not an invented value.

## Verification and limitations

Tests cover commodity balance, duplicate/reversal rejection, reservations,
partial fills, quarantine, restart rebuild hashes, oversells, weighted cost,
realized/unrealized separation, and reconciliation invariants. Independent D6
accounting review is still required. Virtual accounting and models are research
results, not custodial balances or profitability evidence.
