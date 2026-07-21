-- name: InsertPortfolioOwnership :one
INSERT INTO portfolio_ownership (
  account_id, portfolio_id, exchange_id, strategy_version_id, strategy_key,
  initialization_transaction_id, numeraire_asset, ownership_hash, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
RETURNING *;

-- name: InsertA9AccountSnapshot :one
INSERT INTO account_snapshots (
  id, account_id, revision, snapshot_hash, canonical_payload, recorded_at,
  ownership_hash, balances_hash, positions_hash, reservations_hash, risk_state_hash
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
RETURNING *;

-- name: InsertAllocationCandidate :one
INSERT INTO allocation_candidates (
  id, account_id, instrument_id, side, quantity, notional, aggregate_score,
  base_eligibility_version, quote_eligibility_version, state, reason_code,
  revision, created_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
RETURNING *;

-- name: InsertAllocationScoreComponent :one
INSERT INTO allocation_score_components (
  candidate_id, component_name, component_value, ordinal
) VALUES ($1,$2,$3,$4)
RETURNING *;

-- name: LockLiquidityDomain :one
SELECT * FROM liquidity_domains WHERE id=$1 FOR UPDATE;

-- name: ReserveLiquidityDomain :one
UPDATE liquidity_domains
SET available_quantity=available_quantity-$2, revision=revision+1, updated_at=$3
WHERE id=$1 AND revision=$4 AND available_quantity >= $2
RETURNING *;

-- name: InsertLiquidityReservation :one
INSERT INTO liquidity_reservations (
  id, candidate_id, domain_id, quantity, remaining_quantity, state,
  fencing_token, revision, created_at, updated_at
) VALUES ($1,$2,$3,$4,$4,'active',$5,1,$6,$6)
RETURNING *;

-- name: LinkAllocationReservations :one
INSERT INTO allocation_reservations (candidate_id, reservation_id, liquidity_reservation_id)
VALUES ($1,$2,$3)
RETURNING *;

-- name: CloseAllocationCandidate :one
UPDATE allocation_candidates
SET state=$2, reason_code=$3, revision=revision+1, updated_at=$4
WHERE id=$1 AND state='reserved' AND revision=$5
RETURNING *;

-- name: SettleAllocationCandidateFill :one
UPDATE allocation_candidates
SET state=CASE WHEN sqlc.arg(final_fill)::boolean THEN 'settled' ELSE 'reserved' END,
    reason_code=CASE WHEN sqlc.arg(final_fill)::boolean THEN 'filled' ELSE 'partially_filled' END,
    revision=revision+1,
    updated_at=sqlc.arg(updated_at)
WHERE id=sqlc.arg(id) AND state='reserved' AND revision=sqlc.arg(revision)
RETURNING *;

-- name: LockLiquidityReservation :one
SELECT * FROM liquidity_reservations WHERE id=$1 FOR UPDATE;

-- name: CloseLiquidityReservation :one
UPDATE liquidity_reservations
SET state=$2,
    remaining_quantity=CASE WHEN $2='quarantined' THEN remaining_quantity ELSE 0 END,
    revision=revision+1,
    updated_at=$3
WHERE id=$1 AND state='active' AND revision=$4 AND fencing_token=$5
RETURNING *;

-- name: SettleLiquidityReservationFill :one
UPDATE liquidity_reservations
SET state=CASE WHEN sqlc.arg(final_fill)::boolean THEN 'consumed' ELSE 'active' END,
    remaining_quantity=CASE WHEN sqlc.arg(final_fill)::boolean THEN remaining_quantity*0 ELSE remaining_quantity-sqlc.arg(fill_quantity) END,
    revision=revision+1,
    updated_at=sqlc.arg(updated_at)
WHERE id=sqlc.arg(id) AND state='active' AND revision=sqlc.arg(revision)
  AND fencing_token=sqlc.arg(fencing_token) AND sqlc.arg(fill_quantity)>remaining_quantity*0
  AND remaining_quantity>=sqlc.arg(fill_quantity)
  AND (sqlc.arg(final_fill)::boolean OR remaining_quantity>sqlc.arg(fill_quantity))
RETURNING *;

-- name: ReleaseLiquidityDomain :one
UPDATE liquidity_domains
SET available_quantity=available_quantity+$2, revision=revision+1, updated_at=$3
WHERE id=$1
RETURNING *;

-- name: UpdateLiquidityDomainProjection :one
UPDATE liquidity_domains
SET available_quantity=sqlc.arg(available_quantity), revision=revision+1, updated_at=sqlc.arg(updated_at)
WHERE id=sqlc.arg(id) AND revision=sqlc.arg(expected_revision)
RETURNING *;

-- name: InsertRiskPolicy :one
INSERT INTO risk_policies (
  id, version, scope_kind, scope_id, state, policy_hash, canonical_payload, effective_at, recorded_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
RETURNING *;

-- name: InsertRiskPolicyLimits :one
INSERT INTO risk_policy_limits (
  policy_id, policy_version, account_drawdown, utc_day_loss, rolling_24_hour_loss,
  strategy_loss, asset_exposure, combined_exposure, exchange_exposure,
  minimum_reserve, maximum_reserved_capital, maximum_spread, maximum_slippage,
  maximum_open_orders, maximum_book_age_microseconds, maximum_queue_lag_microseconds,
  maximum_clock_drift_microseconds, minimum_quality_score
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
RETURNING *;

-- name: InsertRiskStateEvent :one
INSERT INTO risk_state_events (
  id, prior_state, next_state, reason_code, actor, evidence_hash, occurred_at
) VALUES ($1,$2,$3,$4,$5,$6,$7)
RETURNING *;

-- name: InsertA9RiskEvaluation :one
INSERT INTO risk_evaluations (
  id, decision_id, policy_version, outcome, reason_code, evaluated_at,
  action, effective_state, observation_hash, canonical_payload
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
RETURNING *;

-- name: InsertRiskEvaluationPolicy :one
INSERT INTO risk_evaluation_policies (
  evaluation_id, policy_id, policy_version, precedence
) VALUES ($1,$2,$3,$4)
RETURNING *;

-- name: InsertCircuitBreakerEvent :one
INSERT INTO circuit_breaker_events (
  id, breaker_kind, scope_kind, scope_id, action, resulting_state, evidence_hash, occurred_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
RETURNING *;

-- name: InsertA9ReconciliationCase :one
INSERT INTO reconciliation_cases (
  id, account_id, classification, state, incident_id, opened_at,
  scope, expected_state_hash, actual_state_hash, case_hash
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
RETURNING *;

-- name: InsertReconciliationDifference :one
INSERT INTO reconciliation_differences (
  case_id, ordinal, category, classification, expected_hash, actual_hash,
  asset_symbol, quantity, critical, canonical_payload
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
RETURNING *;

-- name: InsertReconciliationSuspense :one
INSERT INTO reconciliation_suspense (case_id, asset_symbol, quantity, reason)
VALUES ($1,$2,$3,$4)
RETURNING *;

-- name: QuarantineScope :one
INSERT INTO quarantined_scopes (
  scope, reason_code, case_id, revision, quarantined_at
) VALUES ($1,$2,$3,1,$4)
RETURNING *;

-- name: InsertStartupRecoveryAttempt :one
INSERT INTO startup_recovery_attempts (
  id, run_id, state, build_hash, configuration_hash, started_at
) VALUES ($1,$2,'locked',$3,$4,$5)
RETURNING *;

-- name: InsertStartupRecoveryEvidence :one
INSERT INTO startup_recovery_evidence (
  attempt_id, ordinal, stage, evidence_hash, recorded_at
) VALUES ($1,$2,$3,$4,$5)
RETURNING *;

-- name: CompleteStartupRecoveryAttempt :one
UPDATE startup_recovery_attempts
SET state='ready_paused', completed_at=$2
WHERE id=$1 AND state='locked' AND completed_at IS NULL
RETURNING *;
