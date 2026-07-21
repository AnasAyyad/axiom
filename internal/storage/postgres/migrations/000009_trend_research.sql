SET TIME ZONE 'UTC';

ALTER TABLE model_versions DROP CONSTRAINT model_versions_model_type_check;
ALTER TABLE model_versions ADD CONSTRAINT model_versions_model_type_check CHECK (model_type IN (
  'fee','latency','spread','slippage','fill','cost_basis','impact',
  'adverse_selection','maker_queue','gap'
));

ALTER TABLE strategy_versions
  ADD COLUMN manifest_hash sha256_hex,
  ADD COLUMN canonical_manifest bytea,
  ADD COLUMN code_commit text,
  ADD COLUMN supported_modes text[],
  ADD COLUMN author text,
  ADD COLUMN notes text;

ALTER TABLE strategy_parameters
  ADD COLUMN description text,
  ADD COLUMN algorithm_version text,
  ADD COLUMN minimum_value text,
  ADD COLUMN maximum_value text,
  ADD COLUMN minimum_inclusive boolean,
  ADD COLUMN maximum_inclusive boolean,
  ADD COLUMN decimal_scale integer CHECK (decimal_scale BETWEEN 0 AND 18),
  ADD COLUMN rounding text CHECK (rounding IN ('down','ceiling','floor','half_even')),
  ADD COLUMN cadence text,
  ADD COLUMN warm_up text,
  ADD COLUMN mutability text CHECK (mutability IN ('immutable_per_run','new_decisions','restart_only')),
  ADD COLUMN model_dependencies jsonb;

ALTER TABLE experiment_registrations
  ADD COLUMN generation integer CHECK (generation > 0),
  ADD COLUMN primary_metric text,
  ADD COLUMN train_start timestamptz,
  ADD COLUMN train_end timestamptz,
  ADD COLUMN validation_start timestamptz,
  ADD COLUMN validation_end timestamptz,
  ADD COLUMN final_test_start timestamptz,
  ADD COLUMN final_test_end timestamptz,
  ADD COLUMN search_space jsonb,
  ADD COLUMN parameter_neighborhood jsonb,
  ADD COLUMN model_assumptions jsonb,
  ADD COLUMN benchmark_assumptions jsonb,
  ADD COLUMN minimum_samples bigint CHECK (minimum_samples > 0),
  ADD COLUMN stopping_rule text,
  ADD COLUMN rejection_rule text,
  ADD COLUMN promotion_rule text,
  ADD COLUMN registered_seed_hash sha256_hex;

CREATE UNIQUE INDEX experiment_generation_idx
  ON experiment_registrations(strategy_version_id, generation)
  WHERE generation IS NOT NULL;

CREATE TABLE research_generations (
  id text PRIMARY KEY,
  experiment_id text NOT NULL REFERENCES experiment_registrations(id),
  generation integer NOT NULL CHECK (generation > 0),
  final_window_hash sha256_hex NOT NULL,
  registration_hash sha256_hex NOT NULL,
  registered_at timestamptz NOT NULL,
  UNIQUE (experiment_id, generation)
);

CREATE TABLE experiment_final_test_consumptions (
  research_generation_id text PRIMARY KEY REFERENCES research_generations(id),
  consumed_by_run_id text NOT NULL REFERENCES runs(id),
  consumption_hash sha256_hex NOT NULL UNIQUE,
  consumed_at timestamptz NOT NULL
);

CREATE TABLE trend_decisions (
  decision_id text PRIMARY KEY REFERENCES decisions(id),
  explanation_hash sha256_hex NOT NULL,
  canonical_explanation bytea NOT NULL,
  candle_view_id text NOT NULL,
  candle_view_revision bigint NOT NULL CHECK (candle_view_revision > 0),
  market_view_id text NOT NULL,
  market_view_revision bigint NOT NULL CHECK (market_view_revision > 0),
  instrument_metadata_id text NOT NULL REFERENCES instrument_metadata_versions(id),
  asset_eligibility_version bigint NOT NULL CHECK (asset_eligibility_version > 0),
  portfolio_revision bigint NOT NULL CHECK (portfolio_revision > 0),
  position_revision bigint NOT NULL CHECK (position_revision > 0),
  fee_model_id text NOT NULL REFERENCES model_versions(id),
  latency_model_id text NOT NULL REFERENCES model_versions(id),
  fill_model_id text NOT NULL REFERENCES model_versions(id),
  slippage_model_id text NOT NULL REFERENCES model_versions(id),
  gap_model_id text NOT NULL REFERENCES model_versions(id),
  correlation_id text NOT NULL,
  causation_id text NOT NULL,
  recorded_at timestamptz NOT NULL
);

CREATE TABLE research_reports (
  id text PRIMARY KEY,
  research_generation_id text NOT NULL REFERENCES research_generations(id),
  manifest_hash sha256_hex NOT NULL UNIQUE,
  artifact_hash sha256_hex NOT NULL,
  canonical_manifest bytea NOT NULL,
  run_references jsonb NOT NULL,
  confidence_label text NOT NULL CHECK (confidence_label IN ('local_tier_b','formal_tier_a','insufficient','rejected')),
  platform_correctness text NOT NULL,
  strategy_evidence text NOT NULL,
  viability_disposition text NOT NULL CHECK (viability_disposition IN ('undetermined','viable_for_more_research','rejected')),
  disclaimer_policy text NOT NULL CHECK (disclaimer_policy = 'no_production_profitability_claim'),
  created_at timestamptz NOT NULL
);

CREATE TRIGGER research_generations_immutable BEFORE UPDATE OR DELETE ON research_generations
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER final_test_consumptions_immutable BEFORE UPDATE OR DELETE ON experiment_final_test_consumptions
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER trend_decisions_immutable BEFORE UPDATE OR DELETE ON trend_decisions
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER research_reports_immutable BEFORE UPDATE OR DELETE ON research_reports
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();

CREATE INDEX trend_decisions_market_revision_idx ON trend_decisions(market_view_id, market_view_revision);
CREATE INDEX research_reports_generation_idx ON research_reports(research_generation_id, created_at);
CREATE INDEX research_generations_window_idx ON research_generations(final_window_hash, generation);
