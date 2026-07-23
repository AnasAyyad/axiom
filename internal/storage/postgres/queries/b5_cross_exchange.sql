-- name: RegisterB5ClaimResource :exec
SELECT register_b5_claim_resource($1,$2,$3,$4,$5,$6,$7);

-- name: InsertCrossExchangeCandidate :one
INSERT INTO cross_exchange_candidates (
  decision_id, strategy_version_id, configuration_id, coherent_view_id,
  instrument_id, buy_exchange_id, sell_exchange_id, direction,
  buy_ownership_account_id, sell_ownership_account_id, quote_budget,
  base_quantity, gross_spread, buy_fee, sell_fee, spread_depth_cost,
  latency_deterioration, recovery_allowance, expected_execution_pnl,
  maximum_one_leg_loss, marginal_inventory_replacement, natural_reversal_cost,
  advisory_rebalancing_cost, exchange_concentration_penalty,
  usdt_venue_concentration_penalty, expected_closed_cycle_profit,
  worst_closed_cycle_profit, restoration_delay_nanos,
  first_detected_offset_nanos, decision_offset_nanos, expires_offset_nanos,
  configuration_hash, instrument_metadata_set_hash, risk_evaluation_id,
  pricing_model_version_id, claim_model_version_id, fee_model_version_id,
  latency_model_version_id, recovery_model_version_id,
  inventory_shadow_model_version_id, concentration_model_version_id,
  correlation_id, causation_id, canonical_hash, recorded_at
) VALUES (
  $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,
  $21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34,$35,$36,$37,$38,
  $39,$40,$41,$42,$43,$44,$45
)
RETURNING *;

-- name: InsertCrossExchangeCandidateMember :one
INSERT INTO cross_exchange_candidate_members (
  decision_id, coherent_view_id, member_ordinal, exchange_id, instrument_id,
  book_version, connection_generation, receive_monotonic_nanos, receive_utc,
  receive_utc_unix_nanos, ingest_ordinal, clock_offset_nanos,
  clock_uncertainty_nanos, clock_interval_start, clock_interval_end,
  state_hash, collector_instance, collector_region
) VALUES (
  $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18
)
RETURNING *;

-- name: InsertCrossExchangeCandidateLeg :one
INSERT INTO cross_exchange_candidate_legs (
  decision_id, leg_index, exchange_id, ownership_account_id, instrument_id,
  instrument_metadata_id, side, input_quantity, trade_quantity, gross_output,
  net_output, source_dust, fee_asset, fee_quantity, fee_quote_equivalent,
  notional, vwap, spread_depth_cost, book_version, connection_generation
) VALUES (
  $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20
)
RETURNING *;

-- name: InsertCrossExchangeInventorySnapshot :one
INSERT INTO cross_exchange_inventory_snapshots (
  decision_id, snapshot_role, ownership_account_id, exchange_id, base_asset,
  owner_label, ownership_revision, base_before, base_after, total_eligible_base,
  base_share_before, usdt_before, usdt_after, total_eligible_usdt,
  usdt_share_before, band_state, natural_reverse_preferred
) VALUES (
  $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17
)
RETURNING *;

-- name: ClaimB5Resources :exec
SELECT claim_b5_resources(
  sqlc.arg(group_id), sqlc.arg(decision_id), sqlc.arg(fencing_token),
  sqlc.arg(correlation_id), sqlc.arg(causation_id),
  sqlc.arg(resource_ids)::text[], sqlc.arg(quantities)::numeric[],
  sqlc.arg(recorded_at)
);

-- name: SettleB5ClaimGroup :exec
SELECT settle_b5_claim_group(
  sqlc.arg(group_id), sqlc.arg(expected_revision), sqlc.arg(fencing_token),
  sqlc.arg(resource_ids)::text[], sqlc.arg(consumed)::numeric[],
  sqlc.arg(final), sqlc.arg(recorded_at)
);

-- name: CloseB5ClaimGroup :exec
SELECT close_b5_claim_group($1,$2,$3,$4,$5);

-- name: InsertCrossExchangeSimulationOutcome :one
INSERT INTO cross_exchange_simulation_outcomes (
  decision_id, plan_id, outcome, actual_usdt_net, verification_completed,
  retry_attempted, retry_succeeded, unwind_attempted, unwind_succeeded,
  quarantined, final_disposition, recovery_loss, latency_model_version_id,
  canonical_hash, correlation_id, causation_id, recorded_at
) VALUES (
  $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17
)
RETURNING *;

-- name: InsertCrossExchangeSimulationLeg :one
INSERT INTO cross_exchange_simulation_legs (
  decision_id, leg_index, exchange_id, arrival_offset_nanos, initial_state,
  verified_state, final_state, input_quantity, filled_quantity,
  verification_count, retry_count
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
RETURNING *;

-- name: InsertCrossExchangeRebalancingNeed :one
INSERT INTO cross_exchange_rebalancing_needs (
  decision_id, required, asset_symbol, depleted_exchange_id,
  overweight_exchange_id, preferred_action, estimated_cost,
  estimated_delay_nanos, advisory_only, recorded_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
RETURNING *;

-- name: InsertCrossExchangeJournalLink :one
INSERT INTO cross_exchange_journal_links (decision_id, transaction_id, category)
VALUES ($1,$2,$3)
RETURNING *;

-- name: GetCrossExchangeCandidate :one
SELECT * FROM cross_exchange_candidates WHERE decision_id = $1;

-- name: ListCrossExchangeCandidateMembers :many
SELECT * FROM cross_exchange_candidate_members
WHERE decision_id = $1 ORDER BY member_ordinal;

-- name: ListCrossExchangeCandidateLegs :many
SELECT * FROM cross_exchange_candidate_legs
WHERE decision_id = $1 ORDER BY leg_index;

-- name: ListCrossExchangeInventorySnapshots :many
SELECT * FROM cross_exchange_inventory_snapshots
WHERE decision_id = $1 ORDER BY snapshot_role;

-- name: GetB5ClaimGroup :one
SELECT * FROM b5_claim_groups WHERE id = $1;

-- name: ListB5ClaimItems :many
SELECT * FROM b5_claim_items WHERE group_id = $1 ORDER BY resource_id;

-- name: GetCrossExchangeSimulationOutcome :one
SELECT * FROM cross_exchange_simulation_outcomes WHERE decision_id = $1;

-- name: ListCrossExchangeSimulationLegs :many
SELECT * FROM cross_exchange_simulation_legs
WHERE decision_id = $1 ORDER BY leg_index;
