# ADR-0018: Preregistered evidence and explicit research promotion

- **Status:** Accepted
- **Date:** 2026-07-24
- **Scope:** V1B B7 multi-strategy research validation and maturity governance

## Context

Repeated parameter search can make a weak strategy appear convincing if the
search space, final window, statistical family, stopping rule, or promotion
threshold changes after results are observed. Demo and testnet activity can
also prove an integration path while providing weak or misleading statistical
evidence. A strategy label must therefore be reproducible from evidence fixed
before final-test consumption, and changing that label must remain a separate
authenticated governance action.

## Decision

B7 stores a canonical immutable preregistration before the final-test boundary.
It fixes the research generation, strategy version, hypothesis, primary metric,
bounded parameter search, chronological split, model and benchmark versions,
minimum samples and trades, minimum shadow duration, minimum deflated-Sharpe
probability, stopping rule, rejection rule, promotion rule, and seed hash.
Every final-test generation remains single-use through the A10 consumption
boundary.

A canonical validation suite binds that preregistration to immutable backtest,
replay, shadow, paper, demo, or testnet run evidence. It includes walk-forward
folds, block-bootstrap intervals, parameter-neighborhood stability, capacity,
registered stresses, benchmarks, regimes, Benjamini-Hochberg false-discovery
adjustment, probabilistic Sharpe, deflated Sharpe, measurable criteria, and a
mandatory non-profitability disclaimer. The standard-library Python research
environment independently recalculates the statistical outputs and eligibility;
it is not present in the runtime image.

Only primary Tier A evidence carrying `formal_tier_a` confidence can qualify a
maturity. Primary modes are limited to backtest, replay, and shadow. Paper,
demo, testnet, Tier B, low-confidence, insufficient, and integration-only
evidence may be retained as supplemental facts but cannot qualify promotion.
Eligible maturities are derived in order:
`BACKTEST_VALIDATED`, `REPLAY_VALIDATED`, then `SHADOW_VALIDATED`.
`SANDBOX_INTEGRATION_VALIDATED` remains unavailable in V1B.

Promotion is never implied by producing a report. It requires an explicit
authenticated command with `research.promote`, recent reauthentication,
expected revision, idempotency key, evidence identity and hash, and reason.
PostgreSQL independently rechecks session state, permission, evidence
eligibility, transition order, optimistic concurrency, and idempotency in one
`SECURITY DEFINER` function. Applied and rejected commands are audited;
successful state changes append an immutable maturity event.

Research maturity is not release acceptance and is never evidence or a
guarantee of production profitability. B7 adds no exchange credential, broker,
order, transfer, withdrawal, accounting, or portfolio mutation capability.

## Consequences

- Final-test evidence can be tied to the exact research question and rules that
  existed before it was observed.
- Multiple comparisons and independent trials remain visible instead of being
  silently collapsed into a favorable point estimate.
- Champion/challenger reports can recommend a disposition without changing
  maturity.
- A stale browser, replayed command, concurrent operator, or application bug
  cannot bypass database-side authorization and revision checks.
- Rejected commands become durable audit evidence, while evidence manifests and
  maturity events remain immutable.
- Statistical calculations use binary floating-point only in the non-authoritative
  cold research path; prices, quantities, balances, fees, P&L, allocation, risk,
  accounting, and execution remain exact-decimal domains.

## Rejected alternatives

- Automatic promotion when a report passes: rejected because evidence
  production and governance authorization are separate responsibilities.
- Treating demo or testnet results as primary statistical evidence: rejected
  because integration behavior does not establish representative economics.
- An uncorrected best Sharpe from all tried variants: rejected because it hides
  multiple testing and selection bias.
- A mutable registry row: rejected because later edits would make prior final
  results impossible to interpret or reproduce.
- Application-only permission checks: rejected because direct or faulty
  callers must still fail closed at PostgreSQL.

## Validation

- Canonicalization, preregistration-time, tamper, one-use final generation,
  multiple-testing, Sharpe, eligibility, low-confidence, demo, supplemental,
  incomplete-suite, and champion/challenger tests.
- Independent Go and Python comparison against one committed statistics golden.
- Authentication, recent-reauthentication, permission, idempotency, sequential
  transition, stale revision, concurrency, audit, and rejection tests.
- PostgreSQL 18 clean install and exact B6-to-B7 upgrade qualification with
  sentinel preservation, immutable evidence, database-side auth, and
  least-privilege role assertions.
- Runtime-image and source boundaries proving the Python checker and external
  execution capabilities are absent.

## Revisit when

- Research adopts a different registered multiple-testing family or
  uncertainty estimator that requires a new versioned evidence contract.
- A formally reviewed V1C sandbox integration phase defines a distinct
  `SANDBOX_INTEGRATION_VALIDATED` authority.
- The number or dependence structure of trials requires a stronger registered
  effective-trials estimator.
