# Trend V1A strategy

## Scope and authority

`trend.v1a.1` is the only V1A strategy version. It is a long-only Spot research
strategy over completed, UTC-aligned 4-hour candles. The Go implementation in
`internal/strategies/trend` is authoritative in backtest, replay, paper, and
public-data shadow modes. Python under `research/` is an independent offline
checker and report-validation aid; it never makes a strategy decision and is
not copied into the runtime image.

The strategy returns a desired change. It cannot reserve capital, approve
risk, mutate balances, write journals, or submit an order. The shared pipeline
orders those capabilities as Trend → A9 allocator → A9 central risk → A8
planner → A8 simulation broker → reducer/accounting.

## Inputs and admission

One input identifies the exact candle view, market view, instrument metadata,
asset-eligibility revision, configuration and strategy versions, portfolio and
position revisions, model versions, and correlation/causation IDs. Input is
rejected unless it contains at least 200 chronological completed 4-hour
candles with no missing slot, regression, or conflicting duplicate. An
identical duplicate is deduplicated. The latest candle becomes eligible two
seconds after recorded final publication and remains eligible only while the
evaluation age is strictly below five seconds. Arrival-book age is strictly
below 250 ms.

## Indicators and entry

All intermediate indicator arithmetic uses scale 18 and half-even rounding.
EMA has `alpha = 2 / (period + 1)` and a simple-mean seed. ATR uses exact true
range and Wilder smoothing with an initial simple mean.

Entry requires every comparison to be strict:

1. completed close > EMA 200;
2. EMA 50 > EMA 200;
3. completed close > the greatest high of the prior 20 completed candles,
   excluding the signal candle.

Equality is a rejection. One finalized candle has one deterministic decision
identity. An open position or active three-candle protective-loss cooldown
prevents entry; there is no averaging down or adverse-move increase.

## Sizing and rounding

The baseline calculation is:

```text
risk_budget = current_trend_equity * 0.005
nominal_stop = entry_reference - 2.5 * ATR14
stressed_exit = nominal_stop - gap_allowance - latency_deterioration
unit_risk = entry_reference - stressed_exit + entry_fee + exit_fee
raw_quantity = risk_budget / unit_risk
```

The quantity is clipped by owned available cash/inventory, the 150-USDT Trend
cap, the 15% reserve, supplied global/asset/exchange/strategy limits,
instrument filters, one-position/order policy, A9 reservations, and central
risk. Risk and quantity round down; stressed unit risk rounds up. Buy
marketable-limit prices round upward to the tick without crossing 50 bps.
Sell/protective prices round down, and sells cannot exceed owned inventory.
Missing, stale, zero, negative, uncomputable, or filter-invalid values reject.

## Stops and execution semantics

The initial stop is `actual simulated entry - 2.5 * signal ATR`. The trailing
stop is `highest favorable completed close - 3 * current ATR`; it can only
tighten. A completed close below EMA 50 is secondary. If several exits are
eligible together, initial/trailing protection wins. Candle-only ambiguity is
ordered adversely. A gap exits at the first executable post-latency
observation, never at an assumed stop price. A protective loss blocks the next
three completed candles; the fourth is eligible.

The planner creates a single five-second marketable-limit simulated order only
after allocation and risk approval. The simulator reads market state at or
after modeled arrival, never the signal close, and records fill, partial fill,
miss, expiry, and cancellation with fee, spread, slippage, latency, gap, fill,
and model attribution.

## Research governance and evidence

An experiment generation is registered before final-test access. Its
hypothesis, primary metric, chronological train/validation/untouched-test
windows, search neighborhood, model and benchmark assumptions, minimum sample,
seed, stopping, rejection, and promotion rules are immutable. A final window
can be consumed once per registered generation; reuse is a visible new
generation.

Reports include chronological and expanding walk-forward results, registered
block-bootstrap intervals, parameter-neighborhood stability, capacity curves,
fee/spread/slippage/latency/gap/missed-fill stress, cash/buy-and-hold/static
inventory benchmarks, asset/regime/holding-period/false-breakout/drawdown
breakdowns, and failure counts. Reports separately label platform
correctness/reproducibility, strategy evidence, and viability disposition.
Backtest, replay, paper, and shadow results are research evidence only and are
not evidence or a guarantee of production profitability.

Ignored A7 recordings may support local Tier B engineering checks without
export. Formal Tier A evidence remains blocked until the A7 dataset and all
predecessor gates are accepted.

## Limitations

V1A is one long-only BTC/USDT or ETH/USDT Spot position per isolated Trend
portfolio. It has no production orders, authenticated exchange client,
testnet/demo trading, shorting, leverage, margin, transfers, or withdrawals.
The optional one-hour challenger requires a new immutable strategy version and
is outside A10.
