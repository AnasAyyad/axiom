-- name: InsertB7ExperimentPreregistration :one
INSERT INTO b7_experiment_preregistrations (
  id, research_generation_id, strategy_version_id, registration_hash,
  canonical_registration, minimum_samples, minimum_trades,
  minimum_shadow_duration_nanos, minimum_deflated_sharpe_probability,
  registered_at, final_test_start, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
RETURNING *;

-- name: InsertB7ValidationSuite :one
INSERT INTO b7_validation_suites (
  id, preregistration_id, research_generation_id, strategy_version_id,
  manifest_hash, canonical_manifest, evidence_hash,
  final_test_consumption_hash, primary_modes, primary_dataset_tier,
  primary_confidence_label, has_integration_only_primary,
  eligible_maturities, confidence_label, viability_disposition,
  disclaimer_policy, created_at
) VALUES (
  $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17
)
RETURNING *;

-- name: InsertB7ChampionChallengerReport :one
INSERT INTO b7_champion_challenger_reports (
  id, champion_strategy_version_id, challenger_strategy_version_id,
  champion_suite_id, challenger_suite_id, champion_evidence_hash,
  challenger_evidence_hash, manifest_hash, canonical_manifest,
  disposition, disclaimer_policy, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
RETURNING *;

-- name: GetB7ExperimentPreregistration :one
SELECT * FROM b7_experiment_preregistrations WHERE id = $1;

-- name: GetB7ValidationSuite :one
SELECT * FROM b7_validation_suites WHERE id = $1;

-- name: GetB7MaturityState :one
SELECT * FROM strategy_maturity_states WHERE strategy_version_id = $1;
