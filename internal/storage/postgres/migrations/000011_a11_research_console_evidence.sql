SET TIME ZONE 'UTC';

ALTER TABLE shadow_sessions
  ADD COLUMN model_namespace_id text REFERENCES model_namespaces(id),
  ADD COLUMN slippage_model_id text REFERENCES model_versions(id),
  ADD COLUMN gap_model_id text REFERENCES model_versions(id);

CREATE OR REPLACE FUNCTION protect_shadow_session() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN RAISE EXCEPTION 'immutable_shadow_session'; END IF;
  IF (to_jsonb(NEW) - 'run_id' - 'decision_dataset_id' - 'model_namespace_id' - 'slippage_model_id' - 'gap_model_id' -
      'state' - 'revision' - 'entries_enabled' - 'started_at' - 'stopped_at' - 'failure_code' -
      'claim_owner' - 'claim_epoch' - 'claim_expires_at') IS DISTINCT FROM
     (to_jsonb(OLD) - 'run_id' - 'decision_dataset_id' - 'model_namespace_id' - 'slippage_model_id' - 'gap_model_id' -
      'state' - 'revision' - 'entries_enabled' - 'started_at' - 'stopped_at' - 'failure_code' -
      'claim_owner' - 'claim_epoch' - 'claim_expires_at') THEN
    RAISE EXCEPTION 'immutable_shadow_session_identity';
  END IF;
  IF OLD.run_id IS NOT NULL AND NEW.run_id IS DISTINCT FROM OLD.run_id THEN
    RAISE EXCEPTION 'immutable_shadow_run';
  END IF;
  IF NEW.decision_dataset_id IS DISTINCT FROM OLD.decision_dataset_id AND
     (OLD.state NOT IN ('PAUSED','RUNNING','CANCEL_REQUESTED') OR
      NEW.state NOT IN ('PAUSED','RUNNING','CANCEL_REQUESTED')) THEN
    RAISE EXCEPTION 'immutable_shadow_dataset';
  END IF;
  IF (NEW.model_namespace_id IS DISTINCT FROM OLD.model_namespace_id OR
      NEW.slippage_model_id IS DISTINCT FROM OLD.slippage_model_id OR
      NEW.gap_model_id IS DISTINCT FROM OLD.gap_model_id) AND
     (OLD.model_namespace_id IS NOT NULL OR OLD.slippage_model_id IS NOT NULL OR OLD.gap_model_id IS NOT NULL OR
      NEW.model_namespace_id IS NULL OR NEW.slippage_model_id IS NULL OR NEW.gap_model_id IS NULL OR
      NEW.state <> 'PAUSED' OR NEW.claim_owner IS NULL) THEN
    RAISE EXCEPTION 'immutable_shadow_models';
  END IF;
  IF NEW.state <> OLD.state THEN
    IF NEW.revision <> OLD.revision + 1 OR NOT (
      (OLD.state='QUEUED' AND NEW.state IN ('PAUSED','RUNNING','CANCELED','FAILED')) OR
      (OLD.state='PAUSED' AND NEW.state IN ('RUNNING','CANCEL_REQUESTED','FAILED')) OR
      (OLD.state='RUNNING' AND NEW.state IN ('PAUSED','CANCEL_REQUESTED','FAILED')) OR
      (OLD.state='CANCEL_REQUESTED' AND NEW.state IN ('CANCELED','FAILED'))
    ) THEN RAISE EXCEPTION 'invalid_shadow_transition'; END IF;
  ELSIF NEW.revision <> OLD.revision THEN
    RAISE EXCEPTION 'invalid_shadow_revision';
  END IF;
  IF NEW.entries_enabled AND NEW.state <> 'RUNNING' THEN
    RAISE EXCEPTION 'invalid_shadow_entries';
  END IF;
  IF NEW.claim_epoch IS NOT NULL AND OLD.claim_epoch IS NOT NULL AND NEW.claim_epoch < OLD.claim_epoch THEN
    RAISE EXCEPTION 'invalid_shadow_claim';
  END IF;
  RETURN NEW;
END;
$$;

ALTER TABLE orders
  ADD COLUMN requested_limit_price financial_amount CHECK (requested_limit_price > 0),
  ADD COLUMN simulation_latency_ms bigint CHECK (simulation_latency_ms >= 0);

ALTER TABLE orders ADD CONSTRAINT shadow_order_assumptions_complete CHECK (
  (requested_limit_price IS NULL AND simulation_latency_ms IS NULL) OR
  (requested_limit_price IS NOT NULL AND simulation_latency_ms IS NOT NULL)
);
