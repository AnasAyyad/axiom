# A9 local implementation validation

**Date:** 2026-07-16

**Candidate branch:** `a9-portfolio-risk`

**Sequencing:** owner-authorized speculative local implementation from the
locally validated A8 commit while A7 formal qualification remains pending. This
evidence does not satisfy the A7 or formal A8 prerequisites and does not
authorize merge or formal A9 acceptance.

## Outcome

A9 is implemented and locally validated. Formal acceptance remains blocked by
A7 and formal A8 acceptance.
Formal acceptance also remains blocked by A7 and formal A8 acceptance. The
implementation adds the exact isolated Trend
portfolio, exclusive allocation and displayed-liquidity ownership, weighted-
average cost basis and exact P&L/exposure projections, central hierarchical risk
policy, complete risk state/intent controls, circuit breakers, asset-eligibility
checks at all three execution boundaries, virtual reconciliation/quarantine,
ordered locked startup recovery, forward-only PostgreSQL persistence, and real
A9 adapters for the shared A8 pipeline.

The portfolio initializes exactly 500 USDT, zero BTC, and zero ETH under explicit
Trend/portfolio/account/Binance ownership. USDT is the only functional
numeraire; no USD valuation or display source was introduced.

## Retained local checks

- Complete cumulative `make verify` passed with Go 1.26.5, Node 24.18.0,
  pnpm 11.12.0, all backend/frontend and race tests, fuzz smoke, builds, all 128
  Compose profile renders, documentation/traceability validation, security and
  binary capability scans, and vulnerability analysis.
- `make a9-model-qualify` passed the exact portfolio, reservation, risk,
  reconciliation, and shared A8-pipeline models.
- `make a9-postgres-qualify` passed against a fresh disposable PostgreSQL 18.4
  database after the partial-fill remediation. A8 migration `000007`, A9
  migration `000008`, and generated
  sqlc repositories passed role
  permissions, exact initialization evidence, 32-way reservation/liquidity
  contention, stale-CAS atomic rollback, risk-policy/evaluation/breaker history,
  reconciliation suspense/quarantine history, and all 14 ordered startup-
  recovery evidence stages ending `ready_paused`, durable two-stage partial
  fills, balanced reservation journals, and atomic balance, position, projection,
  checkpoint, and outbox updates.
- In-memory contention admits only one claimant to one displayed-liquidity unit
  while maintaining exact cash/inventory projections. Release, consume, expire,
  and quarantine require current revisions and fences; rejected settlement or
  closure changes neither claim.
- Two-step partial-fill tests retain exact remaining cash and liquidity claims,
  survive canonical restart, settle exact fee-inclusive cost basis, and prevent
  a first fill from consuming later-fill ownership. Risk rejection releases
  both claims; uncertain submit or durable-stage failure quarantines them.
- Boundary tests cover every conservative default, inclusivity edge, mandatory
  input, all policy scopes including the exchange-account scope required by the
  product specification, state/intent permissions, cautious controls, five-
  minute hysteresis, manual paused/locked recovery, and every required breaker
  with audit/alert assertions.
- Canonical restart tests reproduce portfolio ownership, balances,
  reservations, positions, cost basis, journal projections, and hashes. The
  cumulative A8 restore tests retain canonical orders/checkpoints, and startup
  recovery remains locked until every persisted stage completes, then remains
  paused.
- Asset transitions away from `approved` are rejected before allocation, before
  plan construction, and at the simulated broker boundary. Unowned sells are
  rejected at allocation and broker authorization.
- The current `axiom:a9-local` image built successfully. `make compose-smoke
  IMAGE=axiom:a9-local` passed the full application and observability profile
  with PostgreSQL, migration, API, recorder, shadow engine, backtest worker,
  Prometheus, and Grafana healthy.

## Persistence added

The forward-only A9 migration and reviewed queries persist explicit ownership,
allocation score components, remaining funds and liquidity claims, balanced
reservation movements, atomic fill/balance/position/projection updates, portfolio snapshot
hashes, versioned policy limits, contributing policy identities, risk-state and
breaker events, reconciliation differences/suspense/quarantine, and ordered
startup-recovery evidence. Immutable evidence tables reject update/delete, and
runtime, recorder, and reporting roles retain closed least-privilege matrices.

## Safety and remaining gate

The candidate is spot-only, credential-free simulation. It adds no signing,
private exchange route, external order transport, withdrawal, transfer, margin,
derivative, leverage, borrowing, lending, staking, short selling, implicit
ownership transfer, unowned sale, testnet/demo, or real-money capability.

A9 must remain unmerged. After A7 and A8 are formally accepted, the candidate
must be rebased onto accepted A8 and rerun through the cumulative formal gate.
All phase prerequisite and completion checkboxes remain unchecked until then.
