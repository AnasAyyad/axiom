-- name: InsertMeanReversionDecision :one
INSERT INTO mean_reversion_decisions (
  decision_id, strategy_version_id, configuration_id, explanation_hash, canonical_explanation,
  primary_candle_view_id, primary_candle_view_revision, higher_candle_view_id,
  higher_candle_view_revision, market_view_id, market_view_revision, coherent_view_id,
  coherent_version_vector_hash, portfolio_ownership_account_id, instrument_metadata_id,
  asset_eligibility_version, portfolio_revision, position_revision, risk_policy_id, risk_policy_version,
  risk_policy_hash, fee_model_id, latency_model_id, fill_model_id, slippage_model_id,
  gap_model_id, correlation_model_id, correlation_id, causation_id, recorded_at
) VALUES (
  $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,
  $21,$22,$23,$24,$25,$26,$27,$28,$29,$30
)
RETURNING *;

-- name: GetMeanReversionDecision :one
SELECT * FROM mean_reversion_decisions WHERE decision_id = $1;
