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
| [0011](0011-a7-resynchronization-attribution.md) | Accepted | A7 retains the 15-second all-sample resynchronization gate and records objective bounded fault attribution separately. |
| [0012](0012-trend-version-and-research-generation.md) | Accepted | Trend decisions and final-test use are locked to immutable versions and visible generations. |
| [0013](0013-a11-authentication-and-session-policy.md) | Accepted | A11 uses file-bootstrapped Argon2id credentials and hashed opaque sessions. |
| [0014](0014-v1b-public-multi-exchange-recording.md) | Accepted | B1 uses compiled credential-free Bybit routes, common recording facts, and separate immutable per-exchange datasets. |
| [0015](0015-b4-exact-triangular-claims.md) | Accepted | B4 exhaustively evaluates exact cycles and fences every required resource in one restart-safe atomic claim group. |
| [0016](0016-b5-closed-cycle-concurrent-arbitrage.md) | Accepted | B5 binds concurrent two-venue simulation to coherent views and complete closed-cycle inventory economics. |
| [0017](0017-b6-reviewed-advisory-rebalancing.md) | Accepted | B6 deterministically ranks immutable reviewed route facts while retaining a compiled advisory-only boundary. |
| [0018](0018-b7-preregistered-research-promotion.md) | Accepted | B7 separates preregistered statistical evidence from explicit authenticated research-maturity promotion. |
| [0019](0019-b8-generic-multi-exchange-console.md) | Accepted | B8 exposes generic multi-exchange evidence and deterministic simulation commands without external execution authority. |
| [0020 V1C](0020-v1c-closed-authenticated-boundary.md) | Accepted | V1C authenticated Testnet/Demo clients are closed, isolated, and production-private incapable. |
| [0020 B2](0020-b2-formal-coherent-qualification.md) | Accepted | B2 has an independent coherent formal market-data qualification contract. |
| [0021 V1C](0021-v1c-durable-sandbox-dispatch.md) | Accepted | Sandbox dispatch persists intent before I/O and reconciles ambiguous outcomes. |
| [0021 D1](0021-v1d-d1-control-plane.md) | Accepted | D1 exposes compatible durable control-plane contracts. |
| [0022 Bybit](0022-bybit-demo-unified-key-bundle.md) | Accepted | Bybit Demo uses the reviewed unified key bundle boundary. |
| [0022 D2](0022-v1d-d2-command-center.md) | Accepted | D2 provides the role-aware, safety-labelled command center. |
| [0023](0023-v1d-d3-research-labs.md) | Accepted | D3 labs preserve immutable reproducibility and research claims. |
| [0024](0024-v1d-d4-operational-evidence.md) | Accepted | D4 reports, incidents, alerts, audit, and evidence use one durable model. |
| [0025](0025-v1d-d5-operational-readiness.md) | Accepted | D5 uses fail-closed pressure, independent recovery, and terminal readiness evidence. |
| [0026](0026-v1d-d6-cumulative-certification.md) | Accepted | D6 requires exact signed cumulative evidence and never promotes local/CI results into certification. |
| [0027](0027-owner-console-semantic-runtime.md) | Accepted | Owner console uses one semantic owner and unified run workflows. |
| [0028](0028-same-exchange-triangular-asof-coherence.md) | Accepted | Triangular shadow evaluation joins three same-exchange committed books at ETH/BTC events under a 100 ms ceiling. |

## Naming and lifecycle

Use `NNNN-short-kebab-title.md`. Numbers are never reused. Allowed statuses are `Proposed`, `Accepted`, `Superseded by ADR-NNNN`, and `Deprecated`. Accepted ADRs are immutable except for typo/link corrections; a changed decision requires a new ADR that supersedes the old one.

Every ADR must identify its scope, consequences, rejected alternatives, validation obligations, and revisit conditions. Safety, accounting, deterministic-replay, and production-order-lock decisions cannot be waived by an ADR.

Current V1D certification decision: [ADR-0026](0026-v1d-d6-cumulative-certification.md).

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
