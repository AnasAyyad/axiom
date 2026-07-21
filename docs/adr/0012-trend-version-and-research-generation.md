# ADR-0012: Lock Trend decisions and final-test use to immutable generations

- **Status:** Accepted
- **Date:** 2026-07-16
- **Scope:** A10 Trend and research governance

## Context

A10 needs one deterministic Trend baseline and useful research tooling without
allowing parameter drift, final-test reuse, or research results to imply
production profitability.

## Decision

The complete 16-parameter `trend.v1a.1` graph is immutable per run and hashed
with the strategy version. Go owns every decision in all supported modes. A
research experiment registers its windows, seed, assumptions, rules, and
parameter neighborhood before final-test consumption. One final-test window is
consumed once by one research generation; further use requires a new visible
generation. Reports are immutable manifests with artifact/run hashes and
separate platform-correctness, strategy-evidence, viability, confidence, and
no-production-profitability fields.

Python remains a locked offline independent checker. It is excluded from the
runtime image and shadow hot path.

## Consequences

- Historical positions, orders, decisions, explanations, and reports never
  change when a later strategy version is introduced.
- Tuning and untouched evidence cannot be silently blended.
- A rejected or inconclusive strategy remains a valid platform-correctness
  result.
- A future 1-hour challenger creates a new strategy version and experiment
  generation instead of modifying this baseline.

## Rejected alternatives

- Mutable strategy parameters: they would make historical decisions and
  explanations irreproducible.
- Reusing one untouched window after tuning: it would turn the final test into
  validation data without visible evidence.
- Python in the hot path: it would introduce a second decision authority and
  a process boundary.

## Validation

Configuration completeness/hash tests, cross-mode byte comparisons,
one-consumption database constraints, immutable mutation tests, independent
Go/Python indicator goldens, report-claim lint, and runtime-image boundary
inspection keep this decision accepted.

## Revisit when

A new immutable strategy version, a formally approved dataset-generation
policy, or a later product phase requires a different research protocol. A
change requires a superseding ADR.
