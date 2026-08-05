# Research validation policy

## Separation of claims

Strategy viability and platform readiness are independent. A valid negative
strategy result is acceptable and must not be hidden. Backtest, replay, shadow,
testnet, and demo results never prove production profitability.

Every result records mode, data-confidence tier, valuation basis, fee/latency/
fill assumptions, sample size, uncertainty, maturity, exact source/build,
strategy/model/configuration/dataset identities, seed, timestamps, and result
hash. Final-test data is not used to tune a challenger. Promotion is explicit,
audited, exact-revision, and cannot weaken allocator/risk controls.

## Validation sequence

Use version-controlled hypotheses and presets; collect/partition data; train or
tune only on allowed partitions; validate out of sample; deterministically
replay recorded books and approved faults; run public-data shadow comparison;
then, only in separately authorized work, validate plumbing on Binance Spot
Testnet or Bybit Demo. Compare champions/challengers with confidence intervals,
multiple-testing controls, fee/latency sensitivity, and documented stop rules.

Reproducibility requires immutable input manifests, stable event ordering,
explicit clocks/seeds/rounding, hash-sealed outputs, and safe exports. A replay
may resume only from a matching checkpoint revision. Missing datasets, stale or
low-confidence data, changed models, invalid partitions, or hash mismatches fail
closed.

Tests cover deterministic runs, prior-schema fixtures, parameter/result diffs,
pause/cancel/checkpoint behavior, export redaction, maturity labels, and
separation of platform/strategy claims. Public books cannot model hidden
liquidity, production queue position, impact, or latency exactly.
