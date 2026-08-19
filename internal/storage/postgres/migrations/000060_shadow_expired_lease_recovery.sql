SET TIME ZONE 'UTC';

-- A database interruption may return an expired shadow lease to the queue only
-- when the durable session was already paused, entries were disabled, a
-- checkpoint and account snapshot exist, and no uncertain execution state is
-- present. The old lease tuple remains intact until the next claim advances the
-- fencing epoch. Every other expired lease remains terminal.
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
      (OLD.state='CANCEL_REQUESTED' AND NEW.state IN ('CANCELED','FAILED')) OR
      (OLD.state IN ('PAUSED','RUNNING') AND NEW.state='QUEUED' AND NOT NEW.entries_enabled AND
       OLD.claim_owner IS NOT NULL AND NEW.claim_owner=OLD.claim_owner AND
       NEW.claim_epoch=OLD.claim_epoch AND NEW.claim_expires_at=OLD.claim_expires_at AND
       EXISTS (SELECT 1 FROM run_checkpoints checkpoint WHERE checkpoint.run_id=OLD.run_id) AND (
         NEW.failure_code IS NOT DISTINCT FROM OLD.failure_code OR
         (OLD.state='PAUSED' AND NOT OLD.entries_enabled AND
          NEW.failure_code='shadow_lease_recovery_pending' AND
          EXISTS (SELECT 1 FROM account_snapshots snapshot JOIN virtual_accounts account
            ON account.id=snapshot.account_id WHERE account.run_id=OLD.run_id) AND
          NOT EXISTS (SELECT 1 FROM orders order_row JOIN virtual_accounts account
            ON account.id=order_row.account_id WHERE account.run_id=OLD.run_id
            AND order_row.state IN ('created','scheduled','open','partially_filled','cancel_pending','unknown','recovery_required')) AND
          NOT EXISTS (SELECT 1 FROM reservations reservation JOIN virtual_accounts account
            ON account.id=reservation.account_id WHERE account.run_id=OLD.run_id
            AND reservation.state IN ('active','quarantined')) AND
          NOT EXISTS (SELECT 1 FROM execution_plans plan JOIN decisions decision
            ON decision.id=plan.decision_id WHERE decision.run_id=OLD.run_id
            AND plan.state IN ('planned','active','recovery_required','quarantined'))
         )
       ))
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
