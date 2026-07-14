SET TIME ZONE 'UTC';

CREATE TABLE jobs (
  id text PRIMARY KEY,
  job_type text NOT NULL,
  idempotency_key text NOT NULL UNIQUE,
  state text NOT NULL CHECK (state IN ('queued','claimed','running','completed','failed','cancelled')),
  claim_owner text,
  claim_epoch bigint CHECK (claim_epoch > 0),
  claim_expires_at timestamptz,
  payload_hash sha256_hex NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL
);

CREATE TABLE command_requests (
  id text PRIMARY KEY,
  deduplication_key text NOT NULL UNIQUE,
  payload_hash sha256_hex NOT NULL,
  configuration_id text NOT NULL REFERENCES configuration_versions(id),
  state text NOT NULL CHECK (state IN ('pending','applied','rejected','failed')),
  created_at timestamptz NOT NULL,
  applied_at timestamptz
);

CREATE TABLE inbox_events (
  consumer text NOT NULL,
  message_id text NOT NULL,
  payload_hash sha256_hex NOT NULL,
  consumed_at timestamptz NOT NULL,
  PRIMARY KEY (consumer, message_id)
);

CREATE TABLE outbox_events (
  revision bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  id text NOT NULL UNIQUE,
  topic text NOT NULL,
  payload_hash sha256_hex NOT NULL,
  created_at timestamptz NOT NULL,
  published_at timestamptz
);

CREATE TABLE consumer_cursors (
  consumer text PRIMARY KEY,
  outbox_revision bigint NOT NULL CHECK (outbox_revision >= 0),
  updated_at timestamptz NOT NULL
);

CREATE TABLE execution_lease_epochs (
  resource text PRIMARY KEY,
  last_fencing_token bigint NOT NULL CHECK (last_fencing_token >= 0),
  UNIQUE (resource, last_fencing_token)
);

CREATE TABLE execution_leases (
  resource text PRIMARY KEY,
  owner text NOT NULL,
  fencing_token bigint NOT NULL CHECK (fencing_token > 0),
  acquired_at timestamptz NOT NULL,
  expires_at timestamptz NOT NULL,
  CHECK (expires_at > acquired_at),
  UNIQUE (resource, fencing_token),
  FOREIGN KEY (resource, fencing_token)
    REFERENCES execution_lease_epochs(resource, last_fencing_token)
    DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE market_data_segments (
  id text PRIMARY KEY,
  recorder_session text NOT NULL,
  exchange_id text NOT NULL REFERENCES exchanges(id),
  instrument_id text REFERENCES instruments(id),
  event_type text NOT NULL,
  schema_version text NOT NULL,
  parser_version text NOT NULL,
  normalization_version text NOT NULL,
  compression text NOT NULL CHECK (compression = 'zstd'),
  path text NOT NULL UNIQUE CHECK (path !~ '(^|/)\.\.(/|$)'),
  checksum sha256_hex NOT NULL,
  ordered_content_hash sha256_hex NOT NULL,
  record_count bigint NOT NULL CHECK (record_count > 0),
  first_ordinal bigint NOT NULL CHECK (first_ordinal > 0),
  last_ordinal bigint NOT NULL CHECK (last_ordinal >= first_ordinal),
  first_source_sequence text,
  last_source_sequence text,
  started_at timestamptz NOT NULL,
  ended_at timestamptz NOT NULL,
  state text NOT NULL CHECK (state IN ('ready','quarantined','deleted')),
  finalized_at timestamptz NOT NULL,
  CHECK (ended_at >= started_at),
  CHECK ((first_source_sequence IS NULL) = (last_source_sequence IS NULL))
);

CREATE TABLE dataset_manifests (
  id text PRIMARY KEY,
  dataset_hash sha256_hex NOT NULL UNIQUE,
  schema_compatibility text NOT NULL,
  coverage_start timestamptz NOT NULL,
  coverage_end timestamptz NOT NULL,
  state text NOT NULL CHECK (state IN ('building','ready','qualified','rejected','deleted')),
  created_at timestamptz NOT NULL,
  CHECK (coverage_end >= coverage_start)
);

CREATE TABLE dataset_segments (
  dataset_id text NOT NULL REFERENCES dataset_manifests(id),
  segment_id text NOT NULL REFERENCES market_data_segments(id),
  ordinal integer NOT NULL CHECK (ordinal >= 0),
  PRIMARY KEY (dataset_id, ordinal),
  UNIQUE (dataset_id, segment_id)
);

CREATE TABLE dataset_gaps (
  id text PRIMARY KEY,
  dataset_id text NOT NULL REFERENCES dataset_manifests(id),
  first_ordinal bigint NOT NULL CHECK (first_ordinal > 0),
  last_ordinal bigint NOT NULL CHECK (last_ordinal >= first_ordinal),
  reason_code text NOT NULL,
  detected_at timestamptz NOT NULL,
  UNIQUE (dataset_id, first_ordinal, last_ordinal)
);

ALTER TABLE experiment_registrations
  ADD CONSTRAINT experiment_dataset_fk FOREIGN KEY (dataset_id) REFERENCES dataset_manifests(id);
ALTER TABLE runs
  ADD CONSTRAINT runs_dataset_fk FOREIGN KEY (dataset_id) REFERENCES dataset_manifests(id);

CREATE TABLE data_quality_events (
  id text PRIMARY KEY,
  dataset_id text REFERENCES dataset_manifests(id),
  segment_id text REFERENCES market_data_segments(id),
  event_type text NOT NULL,
  severity text NOT NULL CHECK (severity IN ('info','warning','error','critical')),
  reason_code text NOT NULL,
  first_ordinal bigint,
  last_ordinal bigint,
  occurred_at timestamptz NOT NULL
);

CREATE TABLE incidents (
  id text PRIMARY KEY,
  severity text NOT NULL CHECK (severity IN ('warning','error','critical')),
  state text NOT NULL CHECK (state IN ('open','acknowledged','resolved')),
  reason_code text NOT NULL,
  opened_at timestamptz NOT NULL,
  resolved_at timestamptz
);

ALTER TABLE reconciliation_cases
  ADD CONSTRAINT reconciliation_incident_fk FOREIGN KEY (incident_id) REFERENCES incidents(id);

CREATE TABLE alerts (
  id text PRIMARY KEY,
  incident_id text REFERENCES incidents(id),
  alert_type text NOT NULL,
  state text NOT NULL CHECK (state IN ('open','acknowledged','resolved')),
  created_at timestamptz NOT NULL,
  acknowledged_at timestamptz,
  resolved_at timestamptz
);

CREATE TABLE alert_acknowledgements (
  alert_id text NOT NULL REFERENCES alerts(id),
  revision bigint NOT NULL CHECK (revision > 0),
  actor text NOT NULL,
  reason text NOT NULL,
  acknowledged_at timestamptz NOT NULL,
  PRIMARY KEY (alert_id, revision)
);

CREATE TABLE audit_events (
  id text PRIMARY KEY,
  event_type text NOT NULL,
  actor text NOT NULL,
  causation_id text NOT NULL,
  correlation_id text NOT NULL,
  configuration_id text REFERENCES configuration_versions(id),
  event_hash sha256_hex NOT NULL,
  recorded_at timestamptz NOT NULL
);

CREATE FUNCTION reject_immutable_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'immutable_history';
END;
$$;

CREATE FUNCTION protect_journal_transaction() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'immutable_journal_transaction';
  END IF;
  IF OLD.sealed OR NOT NEW.sealed OR pg_trigger_depth() < 2 OR
     (to_jsonb(NEW) - 'sealed') IS DISTINCT FROM (to_jsonb(OLD) - 'sealed') THEN
    RAISE EXCEPTION 'immutable_journal_transaction';
  END IF;
  RETURN NEW;
END;
$$;

CREATE FUNCTION reject_sealed_journal_line() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM journal_transactions WHERE id = NEW.transaction_id AND sealed
  ) THEN
    RAISE EXCEPTION 'sealed_journal_transaction';
  END IF;
  RETURN NEW;
END;
$$;

CREATE FUNCTION protect_strategy_version() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'immutable_strategy_version';
  END IF;
  IF NEW.id <> OLD.id OR NEW.strategy_id <> OLD.strategy_id OR
     NEW.version <> OLD.version OR NEW.implementation_hash <> OLD.implementation_hash OR
     NEW.created_at <> OLD.created_at OR OLD.used_at IS NOT NULL THEN
    RAISE EXCEPTION 'immutable_strategy_version';
  END IF;
  IF NEW.used_at IS NOT NULL AND NEW.used_at < NEW.created_at THEN
    RAISE EXCEPTION 'invalid_strategy_use_time';
  END IF;
  IF NEW.promotion_status <> OLD.promotion_status AND NOT (
    (OLD.promotion_status = 'research' AND NEW.promotion_status IN ('candidate','retired')) OR
    (OLD.promotion_status = 'candidate' AND NEW.promotion_status IN ('locked_test','retired')) OR
    (OLD.promotion_status = 'locked_test' AND NEW.promotion_status IN ('promoted','retired')) OR
    (OLD.promotion_status = 'promoted' AND NEW.promotion_status = 'retired')
  ) THEN
    RAISE EXCEPTION 'invalid_strategy_promotion';
  END IF;
  RETURN NEW;
END;
$$;

CREATE FUNCTION protect_model_version() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'immutable_model_version';
  END IF;
  IF NEW.id <> OLD.id OR NEW.model_type <> OLD.model_type OR NEW.version <> OLD.version OR
     NEW.model_hash <> OLD.model_hash OR NEW.canonical_payload <> OLD.canonical_payload OR
     NEW.created_at <> OLD.created_at OR OLD.used_at IS NOT NULL OR
     (NEW.used_at IS NOT NULL AND NEW.used_at < NEW.created_at) THEN
    RAISE EXCEPTION 'immutable_model_version';
  END IF;
  RETURN NEW;
END;
$$;

CREATE FUNCTION enforce_asset_screening_sequence() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE prior asset_screening_versions%ROWTYPE;
BEGIN
  PERFORM 1 FROM assets WHERE symbol = NEW.asset_symbol FOR UPDATE;
  SELECT * INTO prior FROM asset_screening_versions
    WHERE asset_symbol = NEW.asset_symbol
    ORDER BY version DESC LIMIT 1;
  IF NOT FOUND THEN
    IF NEW.version <> 1 OR NEW.prior_status IS NOT NULL THEN
      RAISE EXCEPTION 'invalid_asset_screening_sequence';
    END IF;
  ELSIF NEW.version <> prior.version + 1 OR NEW.prior_status IS DISTINCT FROM prior.status OR
        NEW.effective_at < prior.effective_at OR NEW.recorded_at < prior.recorded_at THEN
    RAISE EXCEPTION 'invalid_asset_screening_sequence';
  END IF;
  RETURN NEW;
END;
$$;

CREATE FUNCTION protect_market_data_segment() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.state <> 'ready' THEN
      RAISE EXCEPTION 'invalid_market_data_segment_initial_state';
    END IF;
    RETURN NEW;
  END IF;
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'immutable_market_data_segment';
  END IF;
  IF NEW.id <> OLD.id OR NEW.recorder_session <> OLD.recorder_session OR
     NEW.exchange_id <> OLD.exchange_id OR NEW.instrument_id IS DISTINCT FROM OLD.instrument_id OR
     NEW.event_type <> OLD.event_type OR NEW.schema_version <> OLD.schema_version OR
     NEW.parser_version <> OLD.parser_version OR NEW.normalization_version <> OLD.normalization_version OR
     NEW.compression <> OLD.compression OR NEW.path <> OLD.path OR NEW.checksum <> OLD.checksum OR
     NEW.ordered_content_hash <> OLD.ordered_content_hash OR NEW.record_count <> OLD.record_count OR
     NEW.first_ordinal <> OLD.first_ordinal OR NEW.last_ordinal <> OLD.last_ordinal OR
     NEW.first_source_sequence IS DISTINCT FROM OLD.first_source_sequence OR
     NEW.last_source_sequence IS DISTINCT FROM OLD.last_source_sequence OR
     NEW.started_at <> OLD.started_at OR NEW.ended_at <> OLD.ended_at OR NEW.finalized_at <> OLD.finalized_at THEN
    RAISE EXCEPTION 'immutable_market_data_segment';
  END IF;
  IF NEW.state <> OLD.state AND NOT (
    (OLD.state = 'ready' AND NEW.state IN ('quarantined','deleted')) OR
    (OLD.state = 'quarantined' AND NEW.state = 'deleted')
  ) THEN
    RAISE EXCEPTION 'invalid_market_data_segment_transition';
  END IF;
  RETURN NEW;
END;
$$;

CREATE FUNCTION protect_dataset_manifest() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.state <> 'building' THEN
      RAISE EXCEPTION 'invalid_dataset_manifest_initial_state';
    END IF;
    RETURN NEW;
  END IF;
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'immutable_dataset_manifest';
  END IF;
  IF NEW.id <> OLD.id OR NEW.dataset_hash <> OLD.dataset_hash OR
     NEW.schema_compatibility <> OLD.schema_compatibility OR NEW.coverage_start <> OLD.coverage_start OR
     NEW.coverage_end <> OLD.coverage_end OR NEW.created_at <> OLD.created_at THEN
    RAISE EXCEPTION 'immutable_dataset_manifest';
  END IF;
  IF NEW.state <> OLD.state AND NOT (
    (OLD.state = 'building' AND NEW.state IN ('ready','rejected','deleted')) OR
    (OLD.state = 'ready' AND NEW.state IN ('qualified','rejected','deleted')) OR
    (OLD.state = 'qualified' AND NEW.state = 'deleted') OR
    (OLD.state = 'rejected' AND NEW.state = 'deleted')
  ) THEN
    RAISE EXCEPTION 'invalid_dataset_manifest_transition';
  END IF;
  RETURN NEW;
END;
$$;

CREATE FUNCTION enforce_dataset_gap_nonoverlap() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  PERFORM 1 FROM dataset_manifests WHERE id = NEW.dataset_id FOR UPDATE;
  IF EXISTS (
    SELECT 1 FROM dataset_gaps
    WHERE dataset_id = NEW.dataset_id
      AND first_ordinal <= NEW.last_ordinal AND last_ordinal >= NEW.first_ordinal
  ) THEN
    RAISE EXCEPTION 'overlapping_dataset_gap';
  END IF;
  RETURN NEW;
END;
$$;

CREATE FUNCTION protect_run_identity() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.state <> 'created' OR NEW.started_at IS NOT NULL OR NEW.completed_at IS NOT NULL THEN
      RAISE EXCEPTION 'invalid_run_initial_state';
    END IF;
    RETURN NEW;
  END IF;
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'immutable_run_identity';
  END IF;
  IF (to_jsonb(NEW) - 'state' - 'started_at' - 'completed_at') IS DISTINCT FROM
     (to_jsonb(OLD) - 'state' - 'started_at' - 'completed_at') THEN
    RAISE EXCEPTION 'immutable_run_identity';
  END IF;
  IF NEW.state = OLD.state THEN
    IF NEW.started_at IS DISTINCT FROM OLD.started_at OR NEW.completed_at IS DISTINCT FROM OLD.completed_at THEN
      RAISE EXCEPTION 'invalid_run_lifecycle';
    END IF;
    RETURN NEW;
  END IF;
  IF NOT (
    (OLD.state = 'created' AND NEW.state IN ('running','failed','cancelled')) OR
    (OLD.state = 'running' AND NEW.state IN ('paused','completed','failed','cancelled')) OR
    (OLD.state = 'paused' AND NEW.state IN ('running','failed','cancelled'))
  ) THEN
    RAISE EXCEPTION 'invalid_run_transition';
  END IF;
  IF OLD.started_at IS NOT NULL AND NEW.started_at IS DISTINCT FROM OLD.started_at THEN
    RAISE EXCEPTION 'invalid_run_lifecycle';
  END IF;
  IF NEW.started_at IS NOT NULL AND NEW.started_at < NEW.created_at THEN
    RAISE EXCEPTION 'invalid_run_lifecycle';
  END IF;
  IF NEW.state IN ('running','paused') AND (NEW.started_at IS NULL OR NEW.completed_at IS NOT NULL) THEN
    RAISE EXCEPTION 'invalid_run_lifecycle';
  END IF;
  IF NEW.state IN ('completed','failed','cancelled') AND
     (NEW.completed_at IS NULL OR NEW.completed_at < coalesce(NEW.started_at, NEW.created_at)) THEN
    RAISE EXCEPTION 'invalid_run_lifecycle';
  END IF;
  RETURN NEW;
END;
$$;

CREATE FUNCTION enforce_fencing_increase() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.resource <> OLD.resource OR NEW.last_fencing_token <= OLD.last_fencing_token THEN
    RAISE EXCEPTION 'nonmonotonic_fencing_token';
  END IF;
  RETURN NEW;
END;
$$;

CREATE FUNCTION enforce_order_transition() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.state <> 'created' OR NEW.revision <> 1 OR NEW.updated_at < NEW.created_at THEN
      RAISE EXCEPTION 'invalid_order_initial_state';
    END IF;
    RETURN NEW;
  END IF;
  IF (to_jsonb(NEW) - 'state' - 'revision' - 'updated_at') IS DISTINCT FROM
     (to_jsonb(OLD) - 'state' - 'revision' - 'updated_at') THEN
    RAISE EXCEPTION 'immutable_order_identity';
  END IF;
  IF NEW.state = OLD.state THEN
    IF NEW.revision <> OLD.revision OR NEW.updated_at <> OLD.updated_at THEN
      RAISE EXCEPTION 'invalid_order_revision';
    END IF;
    RETURN NEW;
  END IF;
  IF NOT (
    (OLD.state = 'created' AND NEW.state IN ('scheduled','rejected')) OR
    (OLD.state = 'scheduled' AND NEW.state IN ('open','partially_filled','filled','rejected','expired','unknown')) OR
    (OLD.state = 'open' AND NEW.state IN ('partially_filled','filled','cancel_pending','cancelled','expired','unknown')) OR
    (OLD.state = 'partially_filled' AND NEW.state IN ('filled','cancel_pending','cancelled','expired','unknown','recovery_required')) OR
    (OLD.state = 'cancel_pending' AND NEW.state IN ('cancelled','partially_filled','filled','open','unknown','recovery_required')) OR
    (OLD.state = 'unknown' AND NEW.state IN ('open','partially_filled','filled','cancelled','rejected','expired','recovery_required')) OR
    (OLD.state = 'recovery_required' AND NEW.state IN ('open','partially_filled','filled','cancelled','rejected','expired'))
  ) THEN
    RAISE EXCEPTION 'invalid_order_transition';
  END IF;
  IF NEW.revision <> OLD.revision + 1 OR NEW.updated_at < OLD.updated_at THEN
    RAISE EXCEPTION 'invalid_order_revision';
  END IF;
  RETURN NEW;
END;
$$;

CREATE FUNCTION enforce_reservation_transition() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.state <> 'active' OR NEW.revision <> 1 OR NEW.updated_at < NEW.created_at THEN
      RAISE EXCEPTION 'invalid_reservation_initial_state';
    END IF;
    RETURN NEW;
  END IF;
  IF (to_jsonb(NEW) - 'state' - 'revision' - 'updated_at') IS DISTINCT FROM
     (to_jsonb(OLD) - 'state' - 'revision' - 'updated_at') THEN
    RAISE EXCEPTION 'immutable_reservation_identity';
  END IF;
  IF NEW.state = OLD.state THEN
    IF NEW.revision <> OLD.revision OR NEW.updated_at <> OLD.updated_at THEN
      RAISE EXCEPTION 'invalid_reservation_revision';
    END IF;
    RETURN NEW;
  END IF;
  IF OLD.state <> 'active' OR NEW.state NOT IN ('consumed','released','expired','quarantined') OR
     NEW.revision <> OLD.revision + 1 OR NEW.updated_at < OLD.updated_at THEN
    RAISE EXCEPTION 'invalid_reservation_transition';
  END IF;
  RETURN NEW;
END;
$$;

CREATE FUNCTION enforce_job_transition() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.state <> 'queued' OR NEW.claim_owner IS NOT NULL OR NEW.claim_epoch IS NOT NULL OR
       NEW.claim_expires_at IS NOT NULL OR NEW.updated_at < NEW.created_at THEN
      RAISE EXCEPTION 'invalid_job_initial_state';
    END IF;
    RETURN NEW;
  END IF;
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'immutable_job_identity';
  END IF;
  IF (to_jsonb(NEW) - 'state' - 'claim_owner' - 'claim_epoch' - 'claim_expires_at' - 'updated_at') IS DISTINCT FROM
     (to_jsonb(OLD) - 'state' - 'claim_owner' - 'claim_epoch' - 'claim_expires_at' - 'updated_at') OR
     NEW.updated_at < OLD.updated_at THEN
    RAISE EXCEPTION 'immutable_job_identity';
  END IF;
  IF NEW.state = OLD.state THEN
    IF NEW.state NOT IN ('claimed','running') OR NEW.claim_owner <> OLD.claim_owner OR
       NEW.claim_epoch <> OLD.claim_epoch OR NEW.claim_expires_at <= OLD.claim_expires_at OR
       NEW.claim_expires_at <= NEW.updated_at THEN
      RAISE EXCEPTION 'invalid_job_renewal';
    END IF;
  ELSIF NEW.state = 'claimed' THEN
    IF NEW.claim_owner IS NULL OR NEW.claim_epoch IS NULL OR NEW.claim_expires_at <= NEW.updated_at OR NOT (
      OLD.state = 'queued' OR
      (OLD.state IN ('claimed','running') AND OLD.claim_expires_at <= NEW.updated_at AND NEW.claim_epoch > OLD.claim_epoch)
    ) THEN
      RAISE EXCEPTION 'invalid_job_claim';
    END IF;
  ELSIF NEW.state = 'running' THEN
    IF OLD.state <> 'claimed' OR NEW.claim_owner <> OLD.claim_owner OR NEW.claim_epoch <> OLD.claim_epoch OR
       NEW.claim_expires_at < OLD.claim_expires_at OR NEW.claim_expires_at <= NEW.updated_at THEN
      RAISE EXCEPTION 'invalid_job_start';
    END IF;
  ELSIF NEW.state IN ('completed','failed','cancelled') THEN
    IF NOT (OLD.state IN ('claimed','running') OR (OLD.state = 'queued' AND NEW.state = 'cancelled')) OR
       NEW.claim_expires_at IS NOT NULL THEN
      RAISE EXCEPTION 'invalid_job_completion';
    END IF;
  ELSE
    RAISE EXCEPTION 'invalid_job_transition';
  END IF;
  RETURN NEW;
END;
$$;

CREATE FUNCTION protect_command_request() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.state <> 'pending' OR NEW.applied_at IS NOT NULL THEN
      RAISE EXCEPTION 'invalid_command_initial_state';
    END IF;
    RETURN NEW;
  END IF;
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'invalid_command_transition';
  END IF;
  IF (to_jsonb(NEW) - 'state' - 'applied_at') IS DISTINCT FROM
     (to_jsonb(OLD) - 'state' - 'applied_at') OR OLD.state <> 'pending' OR
     NEW.state NOT IN ('applied','rejected','failed') OR NEW.applied_at IS NULL OR NEW.applied_at < NEW.created_at THEN
    RAISE EXCEPTION 'invalid_command_transition';
  END IF;
  RETURN NEW;
END;
$$;

CREATE FUNCTION protect_outbox_event() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.published_at IS NOT NULL THEN
      RAISE EXCEPTION 'invalid_outbox_initial_state';
    END IF;
    RETURN NEW;
  END IF;
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'immutable_outbox_event';
  END IF;
  IF (to_jsonb(NEW) - 'published_at') IS DISTINCT FROM
     (to_jsonb(OLD) - 'published_at') OR OLD.published_at IS NOT NULL OR
     NEW.published_at IS NULL OR NEW.published_at < NEW.created_at THEN
    RAISE EXCEPTION 'immutable_outbox_event';
  END IF;
  RETURN NEW;
END;
$$;

CREATE FUNCTION enforce_consumer_cursor() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    RETURN NEW;
  END IF;
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'nonmonotonic_consumer_cursor';
  END IF;
  IF NEW.consumer <> OLD.consumer OR
     NEW.outbox_revision <= OLD.outbox_revision OR NEW.updated_at < OLD.updated_at THEN
    RAISE EXCEPTION 'nonmonotonic_consumer_cursor';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER configuration_versions_immutable BEFORE UPDATE OR DELETE ON configuration_versions
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER configuration_activations_immutable BEFORE UPDATE OR DELETE ON configuration_activations
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER asset_screening_immutable BEFORE UPDATE OR DELETE ON asset_screening_versions
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER instrument_metadata_immutable BEFORE UPDATE OR DELETE ON instrument_metadata_versions
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER exchange_capabilities_immutable BEFORE UPDATE OR DELETE ON exchange_capabilities
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER strategy_versions_protected BEFORE UPDATE OR DELETE ON strategy_versions
  FOR EACH ROW EXECUTE FUNCTION protect_strategy_version();
CREATE TRIGGER strategy_parameters_immutable BEFORE UPDATE OR DELETE ON strategy_parameters
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER model_versions_protected BEFORE UPDATE OR DELETE ON model_versions
  FOR EACH ROW EXECUTE FUNCTION protect_model_version();
CREATE TRIGGER decisions_immutable BEFORE UPDATE OR DELETE ON decisions
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER decision_inputs_immutable BEFORE UPDATE OR DELETE ON decision_inputs
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER opportunities_immutable BEFORE UPDATE OR DELETE ON opportunities
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER risk_evaluations_immutable BEFORE UPDATE OR DELETE ON risk_evaluations
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER fills_immutable BEFORE UPDATE OR DELETE ON fills
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER journal_transactions_immutable BEFORE UPDATE OR DELETE ON journal_transactions
  FOR EACH ROW EXECUTE FUNCTION protect_journal_transaction();
CREATE TRIGGER ledger_entries_immutable BEFORE UPDATE OR DELETE ON ledger_entries
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER ledger_entries_require_open_journal BEFORE INSERT ON ledger_entries
  FOR EACH ROW EXECUTE FUNCTION reject_sealed_journal_line();
CREATE TRIGGER order_events_immutable BEFORE UPDATE OR DELETE ON order_events
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER audit_events_immutable BEFORE UPDATE OR DELETE ON audit_events
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER account_snapshots_immutable BEFORE UPDATE OR DELETE ON account_snapshots
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER run_checkpoints_immutable BEFORE UPDATE OR DELETE ON run_checkpoints
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER run_results_immutable BEFORE UPDATE OR DELETE ON run_results
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER inbox_events_immutable BEFORE UPDATE OR DELETE ON inbox_events
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER alert_acknowledgements_immutable BEFORE UPDATE OR DELETE ON alert_acknowledgements
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER dataset_gaps_immutable BEFORE UPDATE OR DELETE ON dataset_gaps
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER dataset_segments_immutable BEFORE UPDATE OR DELETE ON dataset_segments
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER data_quality_events_immutable BEFORE UPDATE OR DELETE ON data_quality_events
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER asset_screening_sequence BEFORE INSERT ON asset_screening_versions
  FOR EACH ROW EXECUTE FUNCTION enforce_asset_screening_sequence();
CREATE TRIGGER market_data_segments_protected BEFORE INSERT OR UPDATE OR DELETE ON market_data_segments
  FOR EACH ROW EXECUTE FUNCTION protect_market_data_segment();
CREATE TRIGGER dataset_manifests_protected BEFORE INSERT OR UPDATE OR DELETE ON dataset_manifests
  FOR EACH ROW EXECUTE FUNCTION protect_dataset_manifest();
CREATE TRIGGER dataset_gaps_nonoverlap BEFORE INSERT ON dataset_gaps
  FOR EACH ROW EXECUTE FUNCTION enforce_dataset_gap_nonoverlap();
CREATE TRIGGER runs_identity_immutable BEFORE INSERT OR UPDATE OR DELETE ON runs
  FOR EACH ROW EXECUTE FUNCTION protect_run_identity();
CREATE TRIGGER fencing_tokens_increase BEFORE UPDATE ON execution_lease_epochs
  FOR EACH ROW EXECUTE FUNCTION enforce_fencing_increase();
CREATE TRIGGER orders_follow_state_machine BEFORE INSERT OR UPDATE ON orders
  FOR EACH ROW EXECUTE FUNCTION enforce_order_transition();
CREATE TRIGGER reservations_follow_state_machine BEFORE INSERT OR UPDATE ON reservations
  FOR EACH ROW EXECUTE FUNCTION enforce_reservation_transition();
CREATE TRIGGER jobs_follow_state_machine BEFORE INSERT OR UPDATE OR DELETE ON jobs
  FOR EACH ROW EXECUTE FUNCTION enforce_job_transition();
CREATE TRIGGER command_requests_protected BEFORE INSERT OR UPDATE OR DELETE ON command_requests
  FOR EACH ROW EXECUTE FUNCTION protect_command_request();
CREATE TRIGGER outbox_events_protected BEFORE INSERT OR UPDATE OR DELETE ON outbox_events
  FOR EACH ROW EXECUTE FUNCTION protect_outbox_event();
CREATE TRIGGER consumer_cursors_increase BEFORE INSERT OR UPDATE OR DELETE ON consumer_cursors
  FOR EACH ROW EXECUTE FUNCTION enforce_consumer_cursor();

CREATE FUNCTION enforce_journal_asset_balance() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE unbalanced integer;
BEGIN
  SELECT count(*) INTO unbalanced FROM (
    SELECT asset_symbol
    FROM ledger_entries
    WHERE transaction_id = NEW.id
    GROUP BY asset_symbol
    HAVING sum(CASE direction WHEN 'debit' THEN quantity ELSE -quantity END) <> 0
  ) differences;
  IF unbalanced <> 0 OR NOT EXISTS (
    SELECT 1 FROM ledger_entries WHERE transaction_id = NEW.id
  ) THEN
    RAISE EXCEPTION 'unbalanced_journal_transaction';
  END IF;
  UPDATE journal_transactions SET sealed = true WHERE id = NEW.id AND NOT sealed;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'journal_seal_failed';
  END IF;
  RETURN NEW;
END;
$$;

CREATE FUNCTION enforce_journal_reversal() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.reversal_of IS NULL THEN
    RETURN NEW;
  END IF;
  IF EXISTS (
    SELECT 1 FROM (
      (SELECT account_class,account_owner,asset_symbol,
          CASE direction WHEN 'debit' THEN 'credit' ELSE 'debit' END AS direction,
          quantity,functional_value,lot_reference,rounding_metadata
        FROM ledger_entries WHERE transaction_id=NEW.reversal_of
       EXCEPT ALL
       SELECT account_class,account_owner,asset_symbol,direction,
          quantity,functional_value,lot_reference,rounding_metadata
        FROM ledger_entries WHERE transaction_id=NEW.id)
      UNION ALL
      (SELECT account_class,account_owner,asset_symbol,direction,
          quantity,functional_value,lot_reference,rounding_metadata
        FROM ledger_entries WHERE transaction_id=NEW.id
       EXCEPT ALL
       SELECT account_class,account_owner,asset_symbol,
          CASE direction WHEN 'debit' THEN 'credit' ELSE 'debit' END AS direction,
          quantity,functional_value,lot_reference,rounding_metadata
        FROM ledger_entries WHERE transaction_id=NEW.reversal_of)
    ) mismatches
  ) THEN
    RAISE EXCEPTION 'journal_reversal_mismatch';
  END IF;
  RETURN NEW;
END;
$$;

CREATE CONSTRAINT TRIGGER journal_balanced_on_commit
AFTER INSERT ON journal_transactions
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_journal_asset_balance();

CREATE CONSTRAINT TRIGGER journal_reversal_on_commit
AFTER INSERT ON journal_transactions
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_journal_reversal();

CREATE INDEX jobs_claim_idx ON jobs(state, claim_expires_at, created_at);
CREATE INDEX outbox_unpublished_idx ON outbox_events(revision) WHERE published_at IS NULL;
CREATE INDEX segments_coverage_idx ON market_data_segments(exchange_id, instrument_id, started_at, ended_at);
CREATE INDEX quality_dataset_idx ON data_quality_events(dataset_id, occurred_at);
CREATE INDEX audit_recorded_idx ON audit_events(recorded_at, id);
