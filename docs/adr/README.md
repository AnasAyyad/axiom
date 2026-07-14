# Architecture decision records

ADRs record durable architectural, dependency, security, and safety decisions for Axiom. The authoritative product specification remains `crypto_bot_v1_codex_spec.md`; an ADR may clarify implementation choices but may not weaken product scope or a safety invariant.

## Index

| ADR | Status | Decision |
|---|---|---|
| [0001](0001-modular-monolith.md) | Accepted | V1A uses a Go modular monolith with an in-process decision hot path. |
| [0002](0002-decimal-wrappers-over-apd.md) | Accepted | Financial types are project wrappers over `cockroachdb/apd`. |
| [0003](0003-deterministic-runtime.md) | Superseded by [0008](0008-dataset-replay-and-scheduler-ordering.md) | Original deterministic-runtime contract; replay and scheduler ordering were clarified by ADR-0008. |
| [0004](0004-postgresql-journal-inbox-outbox.md) | Accepted | PostgreSQL owns the journal and durable coordination/inbox/outbox. |
| [0005](0005-server-sent-events.md) | Accepted | Browser live updates use resumable SSE; REST snapshots are authoritative. |
| [0006](0006-v1a-execution-prohibition.md) | Accepted | V1A cannot make any external order side effect. |
| [0007](0007-v1a-exchange-exposure-basis.md) | Accepted | V1A exchange exposure excludes uninvested local virtual USDT. |
| [0008](0008-dataset-replay-and-scheduler-ordering.md) | Accepted | Recorded datasets preserve logical-time/ingest order; source validation and scheduler tie-breaking are separate. |
| [0009](0009-compose-file-secret-groups.md) | Accepted | File-backed Compose secrets use narrowly pinned read-only consumer groups. |
| [0010](0010-a6-public-contract-emulator-boundary.md) | Accepted | A6 exposes public contracts and keeps its deterministic emulator test-only. |

## Naming and lifecycle

Use `NNNN-short-kebab-title.md`. Numbers are never reused. Allowed statuses are `Proposed`, `Accepted`, `Superseded by ADR-NNNN`, and `Deprecated`. Accepted ADRs are immutable except for typo/link corrections; a changed decision requires a new ADR that supersedes the old one.

Every ADR must identify its scope, consequences, rejected alternatives, validation obligations, and revisit conditions. Safety, accounting, deterministic-replay, and production-order-lock decisions cannot be waived by an ADR.

## Template

```markdown
# ADR-NNNN: Decision title

- **Status:** Proposed
- **Date:** YYYY-MM-DD
- **Scope:** Release/phase or subsystem

## Context

What requirement or problem requires a durable decision?

## Decision

State the decision and its normative constraints.

## Consequences

Describe benefits, costs, risks, and operational effects.

## Rejected alternatives

- Alternative: why it was not selected.

## Validation

List tests, benchmarks, drills, scans, or evidence required to keep the decision accepted.

## Revisit when

List objective triggers. Reconsideration creates a superseding ADR; it does not silently edit this decision.
```
