# Multi-strategy validation and promotion

## Purpose and boundary

B7 provides a common evidence and maturity-governance layer for Trend, Mean
Reversion, Triangular Arbitrage, Cross-Exchange Arbitrage, and later registered
strategies. It answers two separate questions:

1. Does an immutable research suite satisfy the criteria registered before its
   final test?
2. Has an authorized operator explicitly approved the next research-maturity
   label?

Neither answer authorizes a real or simulated order. Strategy maturity is not a
release state, formal approval, or profitability claim.

## Preregister before final-test use

`research-preregistration.v1` locks the following values before the final-test
window begins:

- research generation, immutable strategy version, hypothesis, and primary
  metric;
- bounded parameter values and the chronological train, validation, and final
  windows;
- fee, spread, slippage, latency, gap, and missed-fill model identities;
- cash, buy-and-hold, and static-inventory benchmarks;
- minimum observations, trades, shadow duration, and deflated-Sharpe
  probability;
- stopping, rejection, promotion, and deterministic-seed rules.

Canonical JSON bytes are hashed and stored immutably. The A10 final-test
generation is consumed once and is linked to the suite, preventing a later
search from reusing a supposedly locked final window.

## Required evidence

`multi-strategy-validation.v1` retains:

- exact immutable run sources and their dataset tier, confidence, result hash,
  mode, and primary/supplemental role; external demo or testnet observations
  use the generic non-executable `integration` evidence mode;
- chronological walk-forward folds and a seeded block-bootstrap interval;
- registered-neighborhood stability;
- capacity points, market regimes, and all six execution/cost stresses;
- all three required benchmarks;
- the registered false-discovery family and adjusted p-values;
- probabilistic and deflated Sharpe inputs and results;
- measurable promotion criteria, sample/trade counts, shadow duration,
  platform-correctness statement, strategy-evidence statement, viability
  disposition, and the mandatory research disclaimer.

Go produces the canonical artifact. The dependency-free Python 3.12.3 checker
independently recomputes Benjamini-Hochberg adjustment, probabilistic and
deflated Sharpe evidence, neighborhood stability, and eligible labels against a
shared committed golden. The checker is cold-path only and is excluded from the
runtime image.

## Eligibility policy

Global eligibility requires all of the following:

- confidence is `formal_tier_a`;
- viability is only `viable_for_more_research`, not a profitability claim;
- registered sample, trade, criterion, bootstrap-lower-bound, stability,
  primary-hypothesis adjusted-significance, and deflated-Sharpe thresholds pass;
- every primary source is Tier A with `formal_tier_a` confidence;
- every primary source mode is backtest, replay, or shadow.

Paper, demo, and testnet modes and Tier B, low-confidence, insufficient, and
integration-only labels may be retained only as supplemental facts. They cannot
qualify a maturity. Labels are cumulative and sequential:

1. `EXPERIMENTAL` → `BACKTEST_VALIDATED` requires qualified backtest evidence.
2. `BACKTEST_VALIDATED` → `REPLAY_VALIDATED` additionally requires qualified
   replay evidence.
3. `REPLAY_VALIDATED` → `SHADOW_VALIDATED` additionally requires qualified
   shadow evidence meeting the registered minimum duration.

`SANDBOX_INTEGRATION_VALIDATED` is intentionally unavailable in V1B.
`REJECTED` remains an explicit terminal research disposition.

## Champion/challenger evidence

An immutable champion/challenger report compares exact evidence hashes,
overall registered slices, and regime slices. Its disposition can keep the
champion, recommend the challenger, or reject the challenger. The report is
evidence only: creating it does not update either strategy version.

## Explicit promotion command

The operator must be authenticated, hold `research.promote`, and have
reauthenticated within ten minutes. The request supplies the strategy version,
validation-suite ID and hash, target, expected revision, idempotency key, and
reason. Application code creates a canonical actor-bound payload hash.

PostgreSQL then independently verifies the active user and session, recent
reauthentication, permission, immutable evidence, eligibility, sequential
transition, expected revision, and idempotency. One concurrent command wins;
the stale command is rejected and audited. A retry with the same key and exact
payload returns the stored outcome, while a mismatched reuse fails closed.
Runtime roles cannot directly update maturity state or strategy-version rows.

## Operational interpretation

Retain the preregistration, final-consumption identity, suite, comparison,
promotion command, audit event, and maturity event together. A passed local B7
gate establishes implementation correctness only. It does not close V1A/A7,
B1/B2 soak, predecessor, Product, Security, QA, or SRE holds and is not evidence
or a guarantee of production profitability.
