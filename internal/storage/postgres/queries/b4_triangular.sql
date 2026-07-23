-- name: RegisterB4ClaimResource :exec
SELECT register_b4_claim_resource($1,$2,$3,$4,$5,$6,$7);

-- name: InsertTriangularCandidate :one
INSERT INTO triangular_candidates (
  decision_id, strategy_version_id, configuration_id, portfolio_ownership_account_id,
  exchange_id, cycle, start_quantity, expected_final_quantity, worst_final_quantity,
  expected_net, worst_net, expected_edge, worst_edge, additional_safety_margin,
  first_detected_offset_nanos, decision_offset_nanos, expires_offset_nanos,
  configuration_hash, model_version_id, instrument_metadata_set_hash,
  risk_evaluation_id, claim_model_version_id, fee_model_version_id,
  latency_model_version_id, recovery_model_version_id, correlation_id,
  causation_id, canonical_hash, recorded_at
) VALUES (
  $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,
  $21,$22,$23,$24,$25,$26,$27,$28,$29
)
RETURNING *;

-- name: InsertTriangularCandidateLeg :one
INSERT INTO triangular_candidate_legs (
  decision_id, leg_index, instrument_id, instrument_metadata_id,
  source_asset, target_asset, side, input_quantity, trade_quantity,
  gross_output, net_output, source_dust, fee_asset, fee_quantity,
  fee_quote_equivalent, notional, vwap, spread_depth_cost,
  book_version, connection_generation
) VALUES (
  $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20
)
RETURNING *;

-- name: ClaimB4Resources :exec
SELECT claim_b4_resources(
  sqlc.arg(group_id), sqlc.arg(decision_id), sqlc.arg(account_id),
  sqlc.arg(fencing_token), sqlc.arg(correlation_id), sqlc.arg(causation_id),
  sqlc.arg(resource_ids)::text[], sqlc.arg(quantities)::numeric[],
  sqlc.arg(recorded_at)
);

-- name: SettleB4ClaimGroup :exec
SELECT settle_b4_claim_group(
  sqlc.arg(group_id), sqlc.arg(expected_revision), sqlc.arg(fencing_token),
  sqlc.arg(resource_ids)::text[], sqlc.arg(consumed)::numeric[],
  sqlc.arg(final), sqlc.arg(recorded_at)
);

-- name: CloseB4ClaimGroup :exec
SELECT close_b4_claim_group($1,$2,$3,$4,$5);

-- name: InsertTriangularSimulationOutcome :one
INSERT INTO triangular_simulation_outcomes (
  decision_id, plan_id, outcome, actual_final_usdt, latency_model_version_id,
  recovery_attempted, recovery_succeeded, quarantined, stranded_asset,
  stranded_quantity, recovery_loss, canonical_hash, correlation_id,
  causation_id, recorded_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
RETURNING *;

-- name: InsertTriangularOpportunityLifetime :one
INSERT INTO triangular_opportunity_lifetimes (
  decision_id, first_detection_nanos, last_profitable_nanos, peak_edge,
  edge_at_arrival, total_lifetime_nanos, survived_p50, survived_p95,
  survived_p99, metric_window, correlation_id, causation_id, recorded_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
RETURNING *;

-- name: InsertTriangularJournalLink :one
INSERT INTO triangular_journal_links (decision_id, transaction_id, category)
VALUES ($1,$2,$3)
RETURNING *;

-- name: GetTriangularCandidate :one
SELECT * FROM triangular_candidates WHERE decision_id = $1;

-- name: ListTriangularCandidateLegs :many
SELECT * FROM triangular_candidate_legs
WHERE decision_id = $1 ORDER BY leg_index;

-- name: GetB4ClaimGroup :one
SELECT * FROM b4_claim_groups WHERE id = $1;

-- name: ListB4ClaimItems :many
SELECT * FROM b4_claim_items WHERE group_id = $1 ORDER BY resource_id;

-- name: GetTriangularSimulationOutcome :one
SELECT * FROM triangular_simulation_outcomes WHERE decision_id = $1;

-- name: GetTriangularOpportunityLifetime :one
SELECT * FROM triangular_opportunity_lifetimes WHERE decision_id = $1;
