# Mean Reversion V1B strategy

## Scope and authority

`mean-reversion.v1b.1` is the B3 baseline: long-only Spot research over
completed UTC-aligned 1-hour candles, with a completed 4-hour regime view. The
Go implementation in `internal/strategies/meanreversion` is authoritative in
backtest, replay, paper, and public-data shadow modes. Python under `research/`
is an independent offline indicator and report checker. It cannot authorize a
decision and is absent from the runtime image.

The strategy emits a desired change and immutable explanation. It cannot
reserve capital, approve risk, mutate balances, write journals, or submit an
order. Accepted candidates follow the shared path: mean reversion → A9
allocator → central risk → A8 planner → simulator → reducer/accounting.
Ordinary no-change and rejected decisions remain canonical replay evidence.

## Immutable input and admission

The reviewed `axiom.config.v1b.2` graph records 27 parameters with stable IDs,
algorithm versions, exact decimal defaults and ranges, inclusivity, scale,
rounding, cadence, UTC evaluation, warm-up, mutability, existing-position
behavior, model dependencies, approval, timestamp, reason, and configuration
identity. Older V1A and V1B.1 schemas retain their original interpretation and
canonical hashes.

One evaluation references exact strategy/configuration, primary and higher
candle views, B2 coherent version vector, arrival market view, portfolio
ownership, portfolio/position revisions, instrument metadata, asset
eligibility, central-risk policy, fee, latency, fill, slippage, gap, and
correlation models, plus correlation/causation IDs. Missing or mismatched
evidence rejects.

Admission requires at least 28 chronological completed 1-hour candles and 210
chronological completed 4-hour candles. Missing slots, regressions, conflicting
duplicates, incomplete candles, stale publication, misaligned timeframes, or
an incoherent view reject; identical retries deduplicate. A signal is eligible
two seconds after final publication and only while its evaluation age is
strictly below five seconds. Arrival-book age is strictly below 250 ms.

## Indicators, entry, and exits

Exact decimal arithmetic uses scale 18 and half-even intermediate rounding.
The z-score is `(close - population_mean) / population_stddev` over the final
20 primary closes; zero deviation rejects. ATR14 and ADX14 use Wilder
smoothing. EMA200 uses `alpha = 2 / (period + 1)` with a simple-mean seed.
“Strongly declining” is an inclusive 0.50% EMA200 decline across the previous
10 completed higher-timeframe candles.

Entry requires all of the following:

1. z-score `<= -2.0`;
2. ADX14 strictly `< 25`;
3. higher-timeframe price is not below a strongly declining EMA200;
4. spread `<= 0.001`, healthy/fresh market data, passing quality, and no
   exchange risk pause;
5. no open mean-reversion position and no active protective-loss cooldown.

Exit precedence is deliberately adverse: an ATR stop touched by the intrabar
low wins, then z-score `<= -3.5`, then normalization at `>= -0.25`, then the
12th completed holding candle. Protective ATR/z-score losses start a
three-candle cooldown; the fourth eligible completed candle may enter. Stops
are triggers, never guaranteed execution prices.

## Stressed sizing and execution

The baseline calculation is:

```text
risk_budget = current_mean_reversion_equity * 0.0025
nominal_stop = min(entry_price - 2.5 * ATR14, mean20 - 3.5 * population_stddev20)
stressed_exit = nominal_stop - gap_allowance - slippage_allowance
unit_risk = entry_price - stressed_exit + entry_fee + exit_fee
quantity = risk_budget / unit_risk
```

Sizing uses the first executable post-latency price, never the signal close.
It rejects nonpositive or uncomputable unit risk and clips conservatively by
owned available USDT, the 75-USDT strategy cap, 10% allocation cap, supplied
reserve/global/asset/exchange/strategy/correlation limits, marketable-limit
slippage, and instrument tick/step/minimum filters. Risk and quantity round
down; stressed loss rounds against the strategy. Sells cannot exceed owned
inventory.

The allocator enforces strategy ownership transactionally and refuses any buy
while the same mean-reversion portfolio owns a positive position. Central risk
is authoritative and releases both funds and displayed-liquidity claims on
rejection. Only an approved intent reaches the single-leg, five-second
marketable-limit simulated plan. Fill, partial fill, miss, expiry, fee, spread,
slippage, latency, gap, and journal/accounting evidence use the shared V1A
engine.

## Research governance and reporting

B3 has a separate `mean-reversion-report.v1` contract without weakening the
immutable Trend report. An experiment generation preregisters its hypothesis,
primary metric, chronological train/validation/untouched-final windows,
walk-forward design, parameter neighborhood, capacity, stress, benchmark,
model, seed, stopping, rejection, and promotion rules. A final window can be
consumed once; reuse requires a disclosed new generation.

Reports include serial block-bootstrap intervals, parameter-neighborhood
stability, capacity, fee/spread/slippage/latency/gap/missed-fill stress,
cash/buy-and-hold/static-inventory benchmarks, and breakdowns by asset, regime,
holding period, fast-decline failure, maximum adverse excursion, trend-filter
comparison, and drawdown, with exact rejection/failure counts. Platform
correctness, strategy evidence, and viability are separate fields. No B3
result is evidence or a guarantee of production profitability.

## Limitations

The optional 15-minute challenger is outside B3 and requires a new immutable
strategy version. B3 has no production order, authenticated exchange, testnet,
demo, short, leverage, margin, transfer, withdrawal, borrowing, lending, or
staking capability. Formal acceptance remains subject to predecessor,
deferred soak, and Product/Security/QA/SRE gates.
