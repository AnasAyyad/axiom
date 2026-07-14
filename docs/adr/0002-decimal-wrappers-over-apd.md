# ADR-0002: Project financial wrappers over cockroachdb/apd

- **Status:** Accepted
- **Date:** 2026-07-12
- **Scope:** V1A financial domain

## Context

Prices, quantities, balances, fees, notional, rates, percentages, allocations, risk limits, and P&L must be exact, explicitly rounded, reproducible, and free of binary floating-point authority. The product specification originally required comparing reviewed decimal implementations; the approved V1A plan subsequently locked `cockroachdb/apd`.

## Decision

Implement project-owned `Price`, `Quantity`, `Money`, `Rate`, `Percent`, `Fee`, `Notional`, balance, and P&L types over `cockroachdb/apd`. Third-party types never cross a domain API.

Each operation uses a named immutable `apd.Context` with explicit precision, exponent bounds, traps, scale, and rounding. Parsing and serialization are lossless/canonical. Exchange filters and every risk-sensitive rounding direction are explicit and test-covered. A sell quantity is rounded so it never exceeds owned inventory. Unexpected inexact, overflow, underflow, invalid, or division conditions return typed errors and fail closed.

The apd selection is locked by the user-approved implementation plan. The A2 decimal benchmark remains required validation evidence for hot-path capacity and regression baselines; it is not a library-selection gate and cannot silently replace apd.

## Consequences

- Exact decimal behavior and contexts are centralized and auditable.
- Domain code cannot accidentally mix units or inherit library defaults.
- Wrappers and conversions add code and allocations must be measured.
- Database/API/Parquet mappings must preserve canonical decimal text or exact numeric scale.

## Rejected alternatives

- `float32`/`float64`: prohibited for authoritative finance and risk.
- Exposing `apd.Decimal`: leaks dependency semantics and permits uncontrolled contexts.
- `govalues/decimal` as the selected engine: superseded by the locked V1A plan; it may appear only in an isolated benchmark comparison if useful.
- Unbounded ad hoc integers: do not provide the required checked scale/context API without rebuilding a decimal library.

## Validation

A2 requires table, property, fuzz, serialization, database round-trip, exchange-rounding, overflow/trap, and concurrency tests plus decimal benchmarks on the declared reference machine. CI scans must reject binary floating-point financial fields and leaked third-party decimal types.

## Revisit when

Only an explicit plan/specification change and superseding ADR may replace apd. Benchmark results may drive wrapper/context optimization but not an unapproved dependency change.
