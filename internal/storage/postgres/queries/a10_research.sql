-- name: InsertA10StrategyDefinition :one
INSERT INTO strategy_definitions (id, name, family)
VALUES ($1, $2, $3)
RETURNING *;

-- name: InsertA10StrategyVersion :one
INSERT INTO strategy_versions (
  id, strategy_id, version, implementation_hash, promotion_status, created_at,
  manifest_hash, canonical_manifest, code_commit, supported_modes, author, notes
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
RETURNING *;

-- name: InsertA10StrategyParameter :one
INSERT INTO strategy_parameters (
  strategy_version_id, parameter_name, decimal_value, unit, description, algorithm_version,
  minimum_value, maximum_value, minimum_inclusive, maximum_inclusive, decimal_scale,
  rounding, cadence, warm_up, mutability, model_dependencies, evaluation_timezone,
  change_behavior, approval_actor, approval_reference, approved_at, change_reason
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)
RETURNING *;

-- name: InsertA10ExperimentRegistration :one
INSERT INTO experiment_registrations (
  id, strategy_version_id, configuration_id, dataset_id, hypothesis, status, registered_at,
  generation, primary_metric, train_start, train_end, validation_start, validation_end,
  final_test_start, final_test_end, search_space, parameter_neighborhood, model_assumptions,
  benchmark_assumptions, minimum_samples, stopping_rule, rejection_rule, promotion_rule,
  registered_seed_hash
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24)
RETURNING *;

-- name: InsertResearchGeneration :one
INSERT INTO research_generations (
  id, experiment_id, generation, final_window_hash, registration_hash, registered_at
) VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ConsumeFinalTestGeneration :one
INSERT INTO experiment_final_test_consumptions (
  research_generation_id, consumed_by_run_id, consumption_hash, consumed_at
) VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: InsertTrendDecision :one
INSERT INTO trend_decisions (
  decision_id, explanation_hash, canonical_explanation, candle_view_id, candle_view_revision,
  market_view_id, market_view_revision, instrument_metadata_id, asset_eligibility_version,
  portfolio_revision, position_revision, fee_model_id, latency_model_id, fill_model_id,
  slippage_model_id, gap_model_id, correlation_id, causation_id, recorded_at
) VALUES (
  $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19
)
RETURNING *;

-- name: InsertResearchReport :one
INSERT INTO research_reports (
  id, research_generation_id, manifest_hash, artifact_hash, canonical_manifest,
  run_references, confidence_label, platform_correctness, strategy_evidence,
  viability_disposition, disclaimer_policy, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
RETURNING *;

-- name: GetResearchGeneration :one
SELECT * FROM research_generations WHERE id = $1;

-- name: GetFinalTestConsumption :one
SELECT * FROM experiment_final_test_consumptions WHERE research_generation_id = $1;

-- name: GetTrendDecision :one
SELECT * FROM trend_decisions WHERE decision_id = $1;

-- name: GetResearchReport :one
SELECT * FROM research_reports WHERE id = $1;
