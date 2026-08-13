-- Durable server-owned evaluation workflow. No table in this migration stores
-- credentials, exchange private data, or a real-order instruction.

INSERT INTO assets(symbol)
VALUES ('USDT'), ('BTC'), ('ETH')
ON CONFLICT DO NOTHING;

INSERT INTO instruments(id,base_asset,quote_asset,product)
VALUES
  ('instrument-BTC-USDT','BTC','USDT','spot'),
  ('instrument-ETH-USDT','ETH','USDT','spot'),
  ('instrument-ETH-BTC','ETH','BTC','spot')
ON CONFLICT (base_asset,quote_asset,product) DO NOTHING;

CREATE TABLE evaluation_campaigns (
  id text PRIMARY KEY,
  preset text NOT NULL CHECK (preset='balanced_full_v1'),
  state text NOT NULL CHECK (state IN ('PENDING','RUNNING','PAUSED_RECOVERABLE','COMPLETED','PARTIAL','BLOCKED','CANCELED')),
  current_stage text CHECK (current_stage IS NULL OR current_stage IN ('HISTORICAL_IMPORT','EXISTING_DATA_AUDIT','RECORDER_ROTATION','RECORDER_QUALIFICATION','BACKTEST_MATRIX','REPLAY_MATRIX','CANDIDATE_SELECTION','COMBINED_SHADOW','FINAL_REPORT')),
  completed_stages text[] NOT NULL DEFAULT '{}'::text[],
  valid_recording_seconds bigint NOT NULL DEFAULT 0 CHECK (valid_recording_seconds >= 0),
  valid_shadow_seconds bigint NOT NULL DEFAULT 0 CHECK (valid_shadow_seconds >= 0),
  reason_code text,
  combined_configuration_id text REFERENCES configuration_versions(id),
  campaign_storage_baseline_bytes bigint NOT NULL DEFAULT 0 CHECK (campaign_storage_baseline_bytes >= 0),
  campaign_recorded_bytes bigint NOT NULL DEFAULT 0 CHECK (campaign_recorded_bytes >= 0 AND campaign_recorded_bytes <= 214748364800),
  measured_bytes_per_hour bigint CHECK (measured_bytes_per_hour IS NULL OR measured_bytes_per_hour >= 0),
  shadow_reserved_bytes bigint CHECK (shadow_reserved_bytes IS NULL OR shadow_reserved_bytes >= 0),
  recording_last_valid_at timestamptz,
  shadow_last_valid_at timestamptz,
  claim_owner text,
  claim_epoch bigint NOT NULL DEFAULT 0 CHECK (claim_epoch >= 0),
  claim_expires_at timestamptz,
  revision bigint NOT NULL DEFAULT 0 CHECK (revision >= 0),
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  CHECK ((state IN ('RUNNING','PAUSED_RECOVERABLE')) = (current_stage IS NOT NULL)),
  CHECK ((claim_owner IS NULL) = (claim_expires_at IS NULL))
);

CREATE UNIQUE INDEX evaluation_campaigns_one_active
  ON evaluation_campaigns ((1)) WHERE state IN ('PENDING','RUNNING','PAUSED_RECOVERABLE');

CREATE TABLE evaluation_campaign_events (
  campaign_id text NOT NULL REFERENCES evaluation_campaigns(id),
  ordinal bigint NOT NULL CHECK (ordinal >= 0),
  event_type text NOT NULL,
  stage text,
  reason_code text,
  summary text,
  occurred_at timestamptz NOT NULL,
  PRIMARY KEY (campaign_id, ordinal)
);

CREATE TABLE evaluation_campaign_stages (
  campaign_id text NOT NULL REFERENCES evaluation_campaigns(id),
  stage text NOT NULL CHECK (stage IN ('HISTORICAL_IMPORT','EXISTING_DATA_AUDIT','RECORDER_ROTATION','RECORDER_QUALIFICATION','BACKTEST_MATRIX','REPLAY_MATRIX','CANDIDATE_SELECTION','COMBINED_SHADOW','FINAL_REPORT')),
  ordinal smallint NOT NULL CHECK (ordinal BETWEEN 1 AND 9),
  state text NOT NULL CHECK (state IN ('PENDING','RUNNING','PAUSED_RECOVERABLE','COMPLETED','BLOCKED','CANCELED')),
  attempt integer NOT NULL DEFAULT 0 CHECK (attempt >= 0),
  linked_resource_type text,
  linked_resource_id text,
  checkpoint_payload bytea,
  checkpoint_hash bytea CHECK (checkpoint_hash IS NULL OR octet_length(checkpoint_hash)=32),
  reason_code text,
  started_at timestamptz,
  completed_at timestamptz,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (campaign_id, stage),
  UNIQUE (campaign_id, ordinal),
  CHECK ((checkpoint_payload IS NULL) = (checkpoint_hash IS NULL))
);

CREATE TABLE evaluation_campaign_members (
  campaign_id text NOT NULL REFERENCES evaluation_campaigns(id),
  id text NOT NULL,
  strategy_id text NOT NULL CHECK (strategy_id IN ('trend-following','mean-reversion','triangular-arbitrage','cross-exchange-arbitrage','inventory-rebalancing')),
  configuration_key text NOT NULL,
  mode text NOT NULL CHECK (mode IN ('backtest','replay','shadow','advisory')),
  capital_micros bigint NOT NULL CHECK (capital_micros IN (500000000,1000000000,1500000000,2000000000,10000000000)),
  repeat_ordinal smallint NOT NULL CHECK (repeat_ordinal BETWEEN 0 AND 2),
  cost_stress_bps integer NOT NULL CHECK (cost_stress_bps IN (10000,15000,20000)),
  state text NOT NULL CHECK (state IN ('PENDING','QUEUED','RUNNING','SUCCEEDED','FAILED','EXCLUDED','CANCELED')),
  linked_run_id text,
  result_hash bytea CHECK (result_hash IS NULL OR octet_length(result_hash)=32),
  metrics_payload bytea,
  verdict text CHECK (verdict IS NULL OR verdict IN ('CONTINUE','IMPROVE','REJECT','BLOCKED')),
  reason_code text,
  configuration_id text REFERENCES configuration_versions(id),
  dataset_id text REFERENCES dataset_manifests(id),
  research_generation_id text REFERENCES research_generations(id),
  linked_job_id text UNIQUE REFERENCES jobs(id),
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (campaign_id, id),
  UNIQUE (campaign_id, strategy_id, configuration_key, mode, capital_micros, repeat_ordinal, cost_stress_bps)
);

CREATE TABLE evaluation_campaign_commands (
  id text PRIMARY KEY,
  actor_id text NOT NULL,
  idempotency_key text NOT NULL,
  request_hash bytea NOT NULL CHECK (octet_length(request_hash)=32),
  target_id text NOT NULL,
  state text NOT NULL CHECK (state IN ('accepted','completed')),
  response_payload bytea NOT NULL,
  created_at timestamptz NOT NULL,
  UNIQUE (actor_id, idempotency_key)
);

CREATE TABLE evaluation_campaign_datasets (
  campaign_id text NOT NULL REFERENCES evaluation_campaigns(id),
  strategy_id text NOT NULL CHECK (strategy_id IN ('trend-following','mean-reversion','triangular-arbitrage','cross-exchange-arbitrage','inventory-rebalancing')),
  dataset_id text NOT NULL REFERENCES dataset_manifests(id),
  manifest_hash bytea NOT NULL CHECK (octet_length(manifest_hash)=32),
  first_ordinal bigint NOT NULL CHECK (first_ordinal > 0),
  last_ordinal bigint NOT NULL CHECK (last_ordinal >= first_ordinal),
  split_ordinal bigint NOT NULL CHECK (split_ordinal >= first_ordinal AND split_ordinal <= last_ordinal),
  classified_at timestamptz NOT NULL,
  PRIMARY KEY (campaign_id,strategy_id)
);

CREATE TABLE evaluation_campaign_dataset_members (
  campaign_id text NOT NULL,
  strategy_id text NOT NULL,
  member_ordinal smallint NOT NULL CHECK (member_ordinal BETWEEN 0 AND 7),
  dataset_id text NOT NULL REFERENCES dataset_manifests(id),
  evidence_role text NOT NULL CHECK (evidence_role IN ('historical_candles','public_market')),
  created_at timestamptz NOT NULL,
  PRIMARY KEY (campaign_id,strategy_id,member_ordinal),
  UNIQUE (campaign_id,strategy_id,dataset_id),
  FOREIGN KEY (campaign_id,strategy_id)
    REFERENCES evaluation_campaign_datasets(campaign_id,strategy_id)
);

CREATE TABLE evaluation_campaign_metadata (
  campaign_id text NOT NULL REFERENCES evaluation_campaigns(id),
  exchange_id text NOT NULL CHECK (exchange_id IN ('binance','bybit')),
  instrument_id text NOT NULL REFERENCES instruments(id),
  metadata_id text NOT NULL REFERENCES instrument_metadata_versions(id),
  created_at timestamptz NOT NULL,
  PRIMARY KEY (campaign_id,exchange_id,instrument_id),
  UNIQUE (campaign_id,metadata_id)
);

-- The selected configuration is frozen before any final-window job is
-- created. A strategy with insufficient validation evidence is locked as
-- BLOCKED without opening the final 20 percent.
CREATE TABLE evaluation_campaign_candidate_locks (
  campaign_id text NOT NULL REFERENCES evaluation_campaigns(id),
  strategy_id text NOT NULL CHECK (strategy_id IN ('trend-following','mean-reversion','triangular-arbitrage','cross-exchange-arbitrage','inventory-rebalancing')),
  state text NOT NULL CHECK (state IN ('SELECTED','BLOCKED')),
  configuration_key text,
  configuration_id text REFERENCES configuration_versions(id),
  dataset_id text REFERENCES dataset_manifests(id),
  validation_result_hash bytea CHECK (validation_result_hash IS NULL OR octet_length(validation_result_hash)=32),
  reason_code text,
  locked_at timestamptz NOT NULL,
  PRIMARY KEY (campaign_id,strategy_id),
  CHECK ((state='SELECTED') = (configuration_key IS NOT NULL)),
  CHECK ((state='SELECTED') = (configuration_id IS NOT NULL)),
  CHECK ((state='SELECTED') = (dataset_id IS NOT NULL)),
  CHECK ((state='SELECTED') = (validation_result_hash IS NOT NULL)),
  CHECK ((state='BLOCKED') = (reason_code IS NOT NULL))
);

CREATE TABLE evaluation_campaign_reports (
  campaign_id text PRIMARY KEY REFERENCES evaluation_campaigns(id),
  state text NOT NULL CHECK (state IN ('final','partial')),
  verdict text CHECK (verdict IN ('CONTINUE','IMPROVE','REJECT','BLOCKED')),
  reason_code text,
  summary text,
  report_hash bytea NOT NULL CHECK (octet_length(report_hash)=32),
  canonical_payload bytea NOT NULL,
  generated_at timestamptz NOT NULL
);

CREATE TABLE evaluation_campaign_stress_results (
  campaign_id text NOT NULL REFERENCES evaluation_campaigns(id),
  scenario text NOT NULL CHECK (scenario IN ('delayed_data','data_gap','restart_recovery','rejection','partial_fill','cancel_fill_race','unknown_result','persistence_failure')),
  ordinal smallint NOT NULL CHECK (ordinal BETWEEN 1 AND 8),
  state text NOT NULL CHECK (state IN ('PASSED','FAILED')),
  reason_code text,
  canonical_payload bytea NOT NULL,
  evidence_hash bytea NOT NULL CHECK (octet_length(evidence_hash)=32),
  completed_at timestamptz NOT NULL,
  PRIMARY KEY (campaign_id,scenario),
  UNIQUE (campaign_id,ordinal)
);

CREATE TABLE evaluation_data_audits (
  id text PRIMARY KEY,
  campaign_id text REFERENCES evaluation_campaigns(id),
  state text NOT NULL CHECK (state IN ('PENDING','RUNNING','COMPLETED','BLOCKED')),
  reason_code text,
  baseline_at timestamptz NOT NULL,
  claim_owner text,
  claim_epoch bigint NOT NULL DEFAULT 0 CHECK (claim_epoch >= 0),
  claim_expires_at timestamptz,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  completed_at timestamptz,
  UNIQUE (campaign_id),
  CHECK ((claim_owner IS NULL)=(claim_expires_at IS NULL))
);

CREATE TABLE evaluation_data_audit_findings (
  audit_id text NOT NULL REFERENCES evaluation_data_audits(id),
  ordinal bigint NOT NULL CHECK (ordinal >= 0),
  dataset_id text NOT NULL,
  exchange_id text,
  instrument_id text,
  eligibility text NOT NULL CHECK (eligibility IN ('eligible','ineligible','blocked')),
  reason_code text NOT NULL,
  manifest_hash bytea CHECK (manifest_hash IS NULL OR octet_length(manifest_hash)=32),
  segment_count bigint NOT NULL DEFAULT 0 CHECK (segment_count >= 0),
  byte_count bigint NOT NULL DEFAULT 0 CHECK (byte_count >= 0),
  gap_count bigint NOT NULL DEFAULT 0 CHECK (gap_count >= 0),
  duplicate_count bigint NOT NULL DEFAULT 0 CHECK (duplicate_count >= 0),
  created_at timestamptz NOT NULL,
  PRIMARY KEY (audit_id, ordinal),
  UNIQUE (audit_id,dataset_id)
);

CREATE TABLE evaluation_historical_imports (
  id text PRIMARY KEY,
  campaign_id text NOT NULL REFERENCES evaluation_campaigns(id),
  exchange_id text NOT NULL CHECK (exchange_id IN ('binance','bybit')),
  instrument text NOT NULL CHECK (instrument IN ('BTC/USDT','ETH/USDT')),
  interval text NOT NULL CHECK (interval IN ('15m','1h','4h')),
  window_start timestamptz NOT NULL,
  window_end timestamptz NOT NULL,
  state text NOT NULL CHECK (state IN ('PENDING','RUNNING','COMPLETED','BLOCKED')),
  checkpoint_time timestamptz NOT NULL,
  session_id text NOT NULL,
  recorder_dataset_id text NOT NULL,
  manifest_revision bigint NOT NULL DEFAULT 0 CHECK (manifest_revision >= 0),
  manifest_hash bytea CHECK (manifest_hash IS NULL OR octet_length(manifest_hash)=32),
  manifest_path text,
  last_ordinal bigint NOT NULL DEFAULT 0 CHECK (last_ordinal >= 0),
  raw_hash bytea CHECK (raw_hash IS NULL OR octet_length(raw_hash)=32),
  normalized_hash bytea CHECK (normalized_hash IS NULL OR octet_length(normalized_hash)=32),
  raw_segment_id text,
  normalized_dataset_id text,
  row_count bigint NOT NULL DEFAULT 0 CHECK (row_count >= 0),
  byte_count bigint NOT NULL DEFAULT 0 CHECK (byte_count >= 0),
  gap_count bigint NOT NULL DEFAULT 0 CHECK (gap_count >= 0),
  source_metadata bytea,
  reason_code text,
  retry_count integer NOT NULL DEFAULT 0 CHECK (retry_count >= 0),
  retry_at timestamptz,
  claim_owner text,
  claim_epoch bigint NOT NULL DEFAULT 0 CHECK (claim_epoch >= 0),
  claim_expires_at timestamptz,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  UNIQUE (campaign_id, exchange_id, instrument, interval),
  UNIQUE (session_id),
  UNIQUE (recorder_dataset_id),
  CHECK (window_start='2023-08-01 00:00:00+00'::timestamptz),
  CHECK (window_end='2026-08-01 00:00:00+00'::timestamptz),
  CHECK (checkpoint_time>=window_start AND checkpoint_time<=window_end),
  CHECK ((state='COMPLETED')=(checkpoint_time=window_end)),
  CHECK ((claim_owner IS NULL)=(claim_expires_at IS NULL)),
  CHECK ((manifest_revision=0)=(manifest_hash IS NULL)),
  CHECK ((manifest_revision=0)=(manifest_path IS NULL))
);

CREATE TABLE evaluation_historical_import_segments (
  import_id text NOT NULL REFERENCES evaluation_historical_imports(id),
  page_start timestamptz NOT NULL,
  kind text NOT NULL CHECK (kind IN ('wire','canonical')),
  segment_id text NOT NULL UNIQUE,
  manifest_payload bytea NOT NULL,
  created_at timestamptz NOT NULL,
  PRIMARY KEY (import_id,page_start,kind)
);

CREATE TABLE evaluation_recorder_requests (
  campaign_id text PRIMARY KEY REFERENCES evaluation_campaigns(id),
  desired_session_id text NOT NULL UNIQUE,
  state text NOT NULL CHECK (state IN ('REQUESTED','FINALIZED','ACTIVATING','ACTIVE','PAUSED','FINALIZING','BLOCKED','COMPLETED')),
  previous_session_id text,
  binance_session_id text NOT NULL,
  bybit_session_id text NOT NULL,
  storage_baseline_bytes bigint NOT NULL CHECK (storage_baseline_bytes >= 0),
  recorded_bytes bigint NOT NULL DEFAULT 0 CHECK (recorded_bytes >= 0 AND recorded_bytes <= 214748364800),
  valid_recording_seconds bigint NOT NULL DEFAULT 0 CHECK (valid_recording_seconds >= 0),
  last_valid_at timestamptz,
  measured_bytes_per_hour bigint CHECK (measured_bytes_per_hour IS NULL OR measured_bytes_per_hour >= 0),
  shadow_reserved_bytes bigint CHECK (shadow_reserved_bytes IS NULL OR shadow_reserved_bytes >= 0),
  reason_code text,
  requested_at timestamptz NOT NULL,
  finalized_at timestamptz,
  activated_at timestamptz,
  completed_at timestamptz,
  updated_at timestamptz NOT NULL,
  CHECK (bybit_session_id=desired_session_id OR bybit_session_id=desired_session_id||'-bybit'),
  CHECK (binance_session_id=desired_session_id)
);

CREATE UNIQUE INDEX evaluation_recorder_one_unfinished
  ON evaluation_recorder_requests ((1)) WHERE state NOT IN ('BLOCKED','COMPLETED');

CREATE TABLE evaluation_campaign_recording_segments (
  campaign_id text NOT NULL REFERENCES evaluation_campaigns(id),
  segment_id text NOT NULL REFERENCES market_data_segments(id),
  byte_count bigint NOT NULL CHECK (byte_count > 0),
  recorded_at timestamptz NOT NULL,
  PRIMARY KEY (campaign_id,segment_id)
);

CREATE TABLE evaluation_recorder_observations (
  campaign_id text NOT NULL REFERENCES evaluation_campaigns(id),
  ordinal bigint NOT NULL CHECK (ordinal >= 1),
  session_id text NOT NULL,
  observed_at timestamptz NOT NULL,
  interval_start timestamptz,
  interval_valid boolean NOT NULL,
  valid_interval_seconds bigint NOT NULL DEFAULT 0 CHECK (valid_interval_seconds >= 0),
  all_collectors_eligible boolean NOT NULL,
  persistence_healthy boolean NOT NULL,
  message_count bigint NOT NULL CHECK (message_count >= 0),
  queue_drop_count bigint NOT NULL CHECK (queue_drop_count >= 0),
  gap_count bigint NOT NULL CHECK (gap_count >= 0),
  decoder_error_count bigint NOT NULL CHECK (decoder_error_count >= 0),
  recorded_bytes bigint NOT NULL CHECK (recorded_bytes >= 0 AND recorded_bytes <= 214748364800),
  PRIMARY KEY (campaign_id,ordinal)
);

CREATE TABLE evaluation_recorder_instrument_observations (
  campaign_id text NOT NULL,
  observation_ordinal bigint NOT NULL,
  exchange_id text NOT NULL CHECK (exchange_id IN ('binance','bybit')),
  instrument text NOT NULL CHECK (instrument IN ('BTCUSDT','ETHUSDT','ETHBTC')),
  eligible boolean NOT NULL,
  book_fresh boolean NOT NULL,
  clock_eligible boolean NOT NULL,
  latest_event_at timestamptz NOT NULL,
  message_count bigint NOT NULL CHECK (message_count >= 0),
  queue_drop_count bigint NOT NULL CHECK (queue_drop_count >= 0),
  gap_count bigint NOT NULL CHECK (gap_count >= 0),
  decoder_error_count bigint NOT NULL CHECK (decoder_error_count >= 0),
  PRIMARY KEY (campaign_id,observation_ordinal,exchange_id,instrument),
  FOREIGN KEY (campaign_id,observation_ordinal)
    REFERENCES evaluation_recorder_observations(campaign_id,ordinal)
);

-- One combined, simulation-only shadow runtime owns all selected members. The
-- capital constants deliberately leave a protected 2,000 USDT reserve and
-- prevent any member from escaping its 2,000 USDT ceiling.
CREATE TABLE evaluation_shadow_sessions (
  campaign_id text PRIMARY KEY REFERENCES evaluation_campaigns(id),
  recorder_session_id text NOT NULL,
  state text NOT NULL CHECK (state IN ('PENDING','RUNNING','PAUSED_RECOVERABLE','COMPLETED','BLOCKED','CANCELED')),
  start_ordinal bigint NOT NULL CHECK (start_ordinal >= 0),
  last_processed_ordinal bigint NOT NULL DEFAULT 0 CHECK (last_processed_ordinal >= start_ordinal),
  valid_seconds bigint NOT NULL DEFAULT 0 CHECK (valid_seconds >= 0),
  shared_capital_micros bigint NOT NULL DEFAULT 10000000000 CHECK (shared_capital_micros=10000000000),
  protected_reserve_micros bigint NOT NULL DEFAULT 2000000000 CHECK (protected_reserve_micros=2000000000),
  member_ceiling_micros bigint NOT NULL DEFAULT 2000000000 CHECK (member_ceiling_micros=2000000000),
  checkpoint_payload bytea,
  checkpoint_hash bytea CHECK (checkpoint_hash IS NULL OR octet_length(checkpoint_hash)=32),
  input_manifest_hash bytea CHECK (input_manifest_hash IS NULL OR octet_length(input_manifest_hash)=32),
  reason_code text,
  started_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  completed_at timestamptz,
  CHECK ((checkpoint_payload IS NULL)=(checkpoint_hash IS NULL)),
  CHECK (protected_reserve_micros + 4*member_ceiling_micros = shared_capital_micros)
);

CREATE TABLE evaluation_shadow_member_checkpoints (
  campaign_id text NOT NULL REFERENCES evaluation_shadow_sessions(campaign_id),
  member_id text NOT NULL,
  strategy_id text NOT NULL CHECK (strategy_id IN ('trend-following','mean-reversion','triangular-arbitrage','cross-exchange-arbitrage')),
  state text NOT NULL CHECK (state IN ('PENDING','RUNNING','SUCCEEDED','FAILED','EXCLUDED','CANCELED')),
  last_processed_ordinal bigint NOT NULL DEFAULT 0 CHECK (last_processed_ordinal >= 0),
  metrics_payload bytea NOT NULL DEFAULT '{}'::bytea,
  result_hash bytea CHECK (result_hash IS NULL OR octet_length(result_hash)=32),
  reason_code text,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (campaign_id,member_id),
  FOREIGN KEY (campaign_id,member_id) REFERENCES evaluation_campaign_members(campaign_id,id)
);

-- Only decision/fill outcomes are retained here; observation-only public
-- inputs remain in the immutable recorder dataset and are referenced by
-- ordinal and manifest hash from the session checkpoint.
CREATE TABLE evaluation_shadow_decisions (
  campaign_id text NOT NULL REFERENCES evaluation_shadow_sessions(campaign_id),
  member_id text NOT NULL,
  input_ordinal bigint NOT NULL CHECK (input_ordinal > 0),
  strategy_id text NOT NULL CHECK (strategy_id IN ('trend-following','mean-reversion','triangular-arbitrage','cross-exchange-arbitrage')),
  decision_hash bytea NOT NULL CHECK (octet_length(decision_hash)=32),
  result_hash bytea NOT NULL CHECK (octet_length(result_hash)=32),
  canonical_payload bytea NOT NULL,
  occurred_at timestamptz NOT NULL,
  PRIMARY KEY (campaign_id,member_id,input_ordinal),
  FOREIGN KEY (campaign_id,member_id) REFERENCES evaluation_campaign_members(campaign_id,id)
);

CREATE FUNCTION protect_evaluation_immutable_evidence() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'evaluation_evidence_immutable';
END;
$$;

CREATE TRIGGER evaluation_campaign_events_immutable
  BEFORE UPDATE OR DELETE ON evaluation_campaign_events
  FOR EACH ROW EXECUTE FUNCTION protect_evaluation_immutable_evidence();
CREATE TRIGGER evaluation_campaign_reports_immutable
  BEFORE UPDATE OR DELETE ON evaluation_campaign_reports
  FOR EACH ROW EXECUTE FUNCTION protect_evaluation_immutable_evidence();
CREATE TRIGGER evaluation_campaign_stress_results_immutable
  BEFORE UPDATE OR DELETE ON evaluation_campaign_stress_results
  FOR EACH ROW EXECUTE FUNCTION protect_evaluation_immutable_evidence();
CREATE TRIGGER evaluation_data_audit_findings_immutable
  BEFORE UPDATE OR DELETE ON evaluation_data_audit_findings
  FOR EACH ROW EXECUTE FUNCTION protect_evaluation_immutable_evidence();
CREATE TRIGGER evaluation_historical_import_segments_immutable
  BEFORE UPDATE OR DELETE ON evaluation_historical_import_segments
  FOR EACH ROW EXECUTE FUNCTION protect_evaluation_immutable_evidence();
CREATE TRIGGER evaluation_campaign_recording_segments_immutable
  BEFORE UPDATE OR DELETE ON evaluation_campaign_recording_segments
  FOR EACH ROW EXECUTE FUNCTION protect_evaluation_immutable_evidence();
CREATE TRIGGER evaluation_campaign_datasets_immutable
  BEFORE UPDATE OR DELETE ON evaluation_campaign_datasets
  FOR EACH ROW EXECUTE FUNCTION protect_evaluation_immutable_evidence();
CREATE TRIGGER evaluation_campaign_dataset_members_immutable
  BEFORE UPDATE OR DELETE ON evaluation_campaign_dataset_members
  FOR EACH ROW EXECUTE FUNCTION protect_evaluation_immutable_evidence();
CREATE TRIGGER evaluation_campaign_metadata_immutable
  BEFORE UPDATE OR DELETE ON evaluation_campaign_metadata
  FOR EACH ROW EXECUTE FUNCTION protect_evaluation_immutable_evidence();
CREATE TRIGGER evaluation_campaign_candidate_locks_immutable
  BEFORE UPDATE OR DELETE ON evaluation_campaign_candidate_locks
  FOR EACH ROW EXECUTE FUNCTION protect_evaluation_immutable_evidence();
CREATE TRIGGER evaluation_recorder_observations_immutable
  BEFORE UPDATE OR DELETE ON evaluation_recorder_observations
  FOR EACH ROW EXECUTE FUNCTION protect_evaluation_immutable_evidence();
CREATE TRIGGER evaluation_recorder_instrument_observations_immutable
  BEFORE UPDATE OR DELETE ON evaluation_recorder_instrument_observations
  FOR EACH ROW EXECUTE FUNCTION protect_evaluation_immutable_evidence();
CREATE TRIGGER evaluation_shadow_decisions_immutable
  BEFORE UPDATE OR DELETE ON evaluation_shadow_decisions
  FOR EACH ROW EXECUTE FUNCTION protect_evaluation_immutable_evidence();

REVOKE ALL ON FUNCTION protect_evaluation_immutable_evidence() FROM PUBLIC;
