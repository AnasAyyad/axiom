BEGIN;

CREATE TABLE v1c_engine_runtime_events (
  id text PRIMARY KEY CHECK (char_length(id) BETWEEN 1 AND 128),
  account_id text NOT NULL REFERENCES v1c_exchange_accounts(id),
  account_epoch bigint NOT NULL CHECK (account_epoch > 0),
  exchange text NOT NULL CHECK (exchange IN ('binance','bybit')),
  startup_cycle bigint NOT NULL CHECK (startup_cycle > 0),
  kind text NOT NULL CHECK (kind IN (
    'PRIVATE_RECONNECT','UNKNOWN_RECOVERY','RECONCILIATION'
  )),
  duration_ms bigint NOT NULL CHECK (duration_ms >= 0),
  succeeded boolean NOT NULL,
  evidence_hash sha256_hex NOT NULL,
  occurred_at timestamptz NOT NULL,
  FOREIGN KEY (account_id,account_epoch)
    REFERENCES v1c_account_epochs(account_id,epoch)
);

CREATE VIEW v1c_c6_order_observations
WITH (security_barrier=true) AS
SELECT
  account.exchange,
  outbox.approved_at,
  outbox.updated_at,
  outbox.state,
  outbox.order_state,
  (outbox.reserved_notional*1000000)::bigint
    AS reserved_notional_microunits,
  count(fill.native_fill_id_hash)::bigint AS persisted_fill_count,
  (
    outbox.order_state IN ('PARTIALLY_FILLED','FILLED')
    AND count(fill.native_fill_id_hash)=0
  ) AS lost_fill,
  coalesce((
    SELECT sum(duplicates.duplicate_count-1)::bigint
    FROM (
      SELECT count(*)::bigint AS duplicate_count
      FROM v1c_exchange_fills candidate
      WHERE candidate.account_id=outbox.account_id
        AND candidate.account_epoch=outbox.account_epoch
        AND candidate.order_id=outbox.order_id
      GROUP BY candidate.canonical_fill,candidate.occurred_at
      HAVING count(*)>1
    ) duplicates
  ),0)::bigint AS double_posted_fills
FROM v1c_submission_outbox outbox
JOIN v1c_exchange_accounts account
  ON account.id=outbox.account_id
LEFT JOIN v1c_exchange_fills fill
  ON fill.account_id=outbox.account_id
 AND fill.account_epoch=outbox.account_epoch
 AND fill.order_id=outbox.order_id
GROUP BY
  outbox.id,
  account.exchange,
  outbox.approved_at,
  outbox.updated_at,
  outbox.state,
  outbox.order_state,
  outbox.reserved_notional,
  outbox.account_id,
  outbox.account_epoch,
  outbox.order_id;

CREATE TABLE v1c_c6_qualification_runs (
  id text PRIMARY KEY CHECK (char_length(id) BETWEEN 1 AND 128),
  mode text NOT NULL CHECK (mode IN ('smoke','formal')),
  state text NOT NULL CHECK (state IN (
    'PENDING','RUNNING','SMOKE_PASSED','PASSED','FAILED'
  )),
  commit_sha text NOT NULL CHECK (commit_sha ~ '^[0-9a-f]{40}$'),
  build_hash sha256_hex NOT NULL,
  executable_hash sha256_hex NOT NULL,
  image_hash text CHECK (
    image_hash IS NULL OR image_hash ~ '^sha256:[0-9a-f]{64}$'
  ),
  CHECK (mode<>'formal' OR image_hash IS NOT NULL),
  configuration_hash sha256_hex NOT NULL,
  source_dirty boolean NOT NULL,
  required_duration_seconds bigint NOT NULL CHECK (
    (mode='smoke' AND required_duration_seconds BETWEEN 1 AND 900) OR
    (mode='formal' AND required_duration_seconds=259200)
  ),
  observed_duration_seconds bigint NOT NULL DEFAULT 0
    CHECK (observed_duration_seconds >= 0),
  profitability_evidence boolean NOT NULL CHECK (NOT profitability_evidence),
  qualified boolean NOT NULL CHECK (
    (state='PASSED' AND mode='formal' AND qualified) OR
    (state<>'PASSED' AND NOT qualified)
  ),
  started_at timestamptz,
  ended_at timestamptz,
  evidence_hash sha256_hex,
  revision bigint NOT NULL CHECK (revision > 0),
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  CHECK (
    (state='PENDING' AND started_at IS NULL AND ended_at IS NULL
      AND evidence_hash IS NULL AND observed_duration_seconds=0) OR
    (state='RUNNING' AND started_at IS NOT NULL AND ended_at IS NULL
      AND evidence_hash IS NULL) OR
    (state IN ('SMOKE_PASSED','PASSED','FAILED')
      AND started_at IS NOT NULL AND ended_at IS NOT NULL
      AND ended_at >= started_at AND evidence_hash IS NOT NULL)
  ),
  CHECK (
    state<>'SMOKE_PASSED' OR
    (mode='smoke' AND NOT qualified
      AND observed_duration_seconds>=required_duration_seconds)
  ),
  CHECK (
    state<>'PASSED' OR
    (mode='formal' AND NOT source_dirty
      AND observed_duration_seconds>=259200)
  )
);

CREATE TABLE v1c_c6_qualification_accounts (
  run_id text NOT NULL REFERENCES v1c_c6_qualification_runs(id),
  account_id text NOT NULL REFERENCES v1c_exchange_accounts(id),
  exchange text NOT NULL CHECK (exchange IN ('binance','bybit')),
  environment text NOT NULL CHECK (
    (exchange='binance' AND environment='spot_testnet') OR
    (exchange='bybit' AND environment='demo')
  ),
  account_epoch bigint NOT NULL CHECK (account_epoch > 0),
  credential_generation bigint NOT NULL CHECK (credential_generation > 0),
  configuration_hash sha256_hex NOT NULL,
  PRIMARY KEY (run_id,account_id),
  FOREIGN KEY (account_id,account_epoch)
    REFERENCES v1c_account_epochs(account_id,epoch)
);

CREATE TABLE v1c_c6_qualification_samples (
  run_id text NOT NULL REFERENCES v1c_c6_qualification_runs(id),
  sample_ordinal bigint NOT NULL CHECK (sample_ordinal > 0),
  observed_at timestamptz NOT NULL,
  orders_acknowledged bigint NOT NULL CHECK (orders_acknowledged >= 0),
  duplicate_creates bigint NOT NULL CHECK (duplicate_creates >= 0),
  lost_fills bigint NOT NULL CHECK (lost_fills >= 0),
  double_posted_fills bigint NOT NULL CHECK (double_posted_fills >= 0),
  unknown_orders bigint NOT NULL CHECK (unknown_orders >= 0),
  oldest_unknown_seconds bigint NOT NULL CHECK (oldest_unknown_seconds >= 0),
  reconciliation_mismatches bigint NOT NULL
    CHECK (reconciliation_mismatches >= 0),
  suspense_items bigint NOT NULL CHECK (suspense_items >= 0),
  reconnects bigint NOT NULL CHECK (reconnects >= 0),
  restarts bigint NOT NULL CHECK (restarts >= 0),
  recovery_duration_ms bigint NOT NULL CHECK (recovery_duration_ms >= 0),
  critical_alert_latency_ms bigint NOT NULL
    CHECK (critical_alert_latency_ms >= 0),
  resident_memory_bytes bigint NOT NULL CHECK (resident_memory_bytes >= 0),
  daily_submitted_microunits bigint NOT NULL
    CHECK (daily_submitted_microunits >= 0),
  largest_order_microunits bigint NOT NULL
    CHECK (largest_order_microunits >= 0),
  maximum_account_open bigint NOT NULL CHECK (maximum_account_open >= 0),
  global_open bigint NOT NULL CHECK (global_open >= 0),
  all_accounts_fresh boolean NOT NULL,
  all_leases_held boolean NOT NULL,
  persistence_healthy boolean NOT NULL,
  restart_safe boolean NOT NULL,
  entry_safe boolean NOT NULL,
  production_target_observed boolean NOT NULL,
  PRIMARY KEY (run_id,sample_ordinal)
);

CREATE TABLE v1c_c6_qualification_failures (
  id text PRIMARY KEY CHECK (char_length(id) BETWEEN 1 AND 128),
  run_id text NOT NULL REFERENCES v1c_c6_qualification_runs(id),
  reason text NOT NULL CHECK (reason IN (
    'duplicate_create','lost_fill','double_posted_fill','unresolved_unknown',
    'reconciliation_mismatch','suspense','stale_data','lease_loss',
    'persistence_failure','unsafe_restart','production_target',
    'cap_violation','memory_leak','critical_alert_slo','operator_abort',
    'evidence_failure'
  )),
  evidence_hash sha256_hex NOT NULL,
  occurred_at timestamptz NOT NULL,
  UNIQUE (run_id,id)
);

CREATE TABLE v1c_c6_chaos_events (
  id text PRIMARY KEY CHECK (char_length(id) BETWEEN 1 AND 128),
  run_id text REFERENCES v1c_c6_qualification_runs(id),
  scenario text NOT NULL CHECK (scenario IN (
    'websocket_disconnect','rest_timeout','database_slowdown',
    'database_failure','process_kill','fencing_loss','duplicate_event',
    'out_of_order_event','partial_fill','late_fill','cancel_fill_race',
    'ambiguous_timeout','account_reset','stream_snapshot_recovery'
  )),
  outcome text NOT NULL CHECK (outcome IN ('PASSED','FAILED')),
  deterministic_seed_hash sha256_hex NOT NULL,
  evidence_hash sha256_hex NOT NULL,
  occurred_at timestamptz NOT NULL
);

CREATE FUNCTION protect_v1c_c6_qualification_run() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP='DELETE' OR
     NEW.id IS DISTINCT FROM OLD.id OR
     NEW.mode IS DISTINCT FROM OLD.mode OR
     NEW.commit_sha IS DISTINCT FROM OLD.commit_sha OR
     NEW.build_hash IS DISTINCT FROM OLD.build_hash OR
     NEW.executable_hash IS DISTINCT FROM OLD.executable_hash OR
     NEW.image_hash IS DISTINCT FROM OLD.image_hash OR
     NEW.configuration_hash IS DISTINCT FROM OLD.configuration_hash OR
     NEW.source_dirty IS DISTINCT FROM OLD.source_dirty OR
     NEW.required_duration_seconds IS DISTINCT FROM
       OLD.required_duration_seconds OR
     NEW.profitability_evidence IS DISTINCT FROM
       OLD.profitability_evidence OR
     NEW.created_at IS DISTINCT FROM OLD.created_at OR
     NEW.updated_at < OLD.updated_at OR
     NEW.revision <> OLD.revision+1 OR
     NOT (
       (OLD.state='PENDING' AND NEW.state='RUNNING'
         AND NEW.started_at IS NOT NULL AND NEW.ended_at IS NULL
         AND NEW.evidence_hash IS NULL) OR
       (OLD.state='RUNNING'
         AND NEW.state IN ('SMOKE_PASSED','PASSED','FAILED')
         AND NEW.started_at=OLD.started_at AND NEW.ended_at IS NOT NULL
         AND NEW.evidence_hash IS NOT NULL)
     ) THEN
    RAISE EXCEPTION 'v1c_c6_qualification_mutation_rejected';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER v1c_c6_qualification_run_protected
  BEFORE UPDATE OR DELETE ON v1c_c6_qualification_runs
  FOR EACH ROW EXECUTE FUNCTION protect_v1c_c6_qualification_run();
CREATE TRIGGER v1c_engine_runtime_events_immutable
  BEFORE UPDATE OR DELETE ON v1c_engine_runtime_events
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER v1c_c6_qualification_accounts_immutable
  BEFORE UPDATE OR DELETE ON v1c_c6_qualification_accounts
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER v1c_c6_qualification_samples_immutable
  BEFORE UPDATE OR DELETE ON v1c_c6_qualification_samples
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER v1c_c6_qualification_failures_immutable
  BEFORE UPDATE OR DELETE ON v1c_c6_qualification_failures
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER v1c_c6_chaos_events_immutable
  BEFORE UPDATE OR DELETE ON v1c_c6_chaos_events
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();

COMMIT;
