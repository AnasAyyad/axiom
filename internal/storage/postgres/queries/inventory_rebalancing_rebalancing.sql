-- name: InsertRebalancingFactSet :one
INSERT INTO rebalancing_fact_sets (
  id, configuration_id, configuration_hash, fact_schema_version,
  cost_model_version, canonical_hash, recorded_at
) VALUES ($1,$2,$3,$4,$5,$6,$7)
RETURNING *;

-- name: InsertRebalancingRouteFact :one
INSERT INTO rebalancing_route_facts (
  fact_set_id, fact_id, logical_key, fact_version, fact_kind,
  from_exchange_id, from_asset_symbol, to_exchange_id, to_asset_symbol,
  instrument_id, network, source_chain, destination_chain, minimum_quantity,
  available, withdrawal_available, deposit_available, compatible, ambiguous,
  fee_cost, spread_cost, depth_cost, delay_cost, network_fee_cost,
  compatibility_cost, volatility_risk_cost, operational_risk_cost,
  minimum_duration_nanos, maximum_duration_nanos, risk_score,
  warnings, manual_checklist, source, observer, observed_at, expires_at,
  confidence, approved, approval_actor, approval_reference, approved_at,
  provenance_hash
) VALUES (
  $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,
  $21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34,$35,$36,$37,$38,
  $39,$40,$41,$42
)
RETURNING *;

-- name: InsertRebalancingRecommendation :one
INSERT INTO rebalancing_recommendations (
  id, request_id, configuration_id, configuration_hash, fact_set_id,
  fact_set_hash, source_cross_exchange_arbitrage_decision_id, method, source_exchange_id,
  source_asset_symbol, destination_exchange_id, destination_asset_symbol,
  quantity, fee_cost, spread_cost, depth_cost, delay_cost, network_fee_cost,
  compatibility_cost, volatility_risk_cost, operational_risk_cost, total_cost,
  minimum_duration_nanos, maximum_duration_nanos, risk_score, warnings,
  advisory_only, canonical_hash, recorded_at
) VALUES (
  $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,
  $21,$22,$23,$24,$25,$26,$27,$28,$29
)
RETURNING *;

-- name: InsertRebalancingRecommendationStep :one
INSERT INTO rebalancing_recommendation_steps (
  recommendation_id, step_index, role, fact_set_id, fact_id,
  fact_version, provenance_hash
) VALUES ($1,$2,$3,$4,$5,$6,$7)
RETURNING *;

-- name: InsertRebalancingChecklistStep :one
INSERT INTO rebalancing_checklist_steps (
  recommendation_id, step_index, instruction
) VALUES ($1,$2,$3)
RETURNING *;

-- name: GetRebalancingFactSet :one
SELECT * FROM rebalancing_fact_sets WHERE id = $1;

-- name: ListRebalancingRouteFacts :many
SELECT * FROM rebalancing_route_facts
WHERE fact_set_id = $1 ORDER BY logical_key, fact_version DESC, fact_id;

-- name: GetRebalancingRecommendation :one
SELECT * FROM rebalancing_recommendations WHERE id = $1;

-- name: ListRebalancingRecommendationSteps :many
SELECT * FROM rebalancing_recommendation_steps
WHERE recommendation_id = $1 ORDER BY step_index;

-- name: ListRebalancingChecklistSteps :many
SELECT * FROM rebalancing_checklist_steps
WHERE recommendation_id = $1 ORDER BY step_index;
