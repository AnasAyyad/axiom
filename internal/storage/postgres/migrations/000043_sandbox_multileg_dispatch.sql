SET TIME ZONE 'UTC';

-- A sequential three-leg triangular plan owns one dispatchable order slot at
-- a time. Later immutable legs remain WAITING until the prior leg is fully
-- filled and its accounting fact is durable. Cross-exchange plans retain two
-- concurrent, separately fenced legs.
ALTER TABLE v1c_submission_plans
  DROP CONSTRAINT v1c_submission_plans_leg_count_check;
ALTER TABLE v1c_submission_plans
  ADD CONSTRAINT v1c_submission_plans_leg_count_check
  CHECK (leg_count BETWEEN 1 AND 3);
ALTER TABLE v1c_submission_plans
  ADD COLUMN execution_expires_at timestamptz,
  ADD CONSTRAINT v1c_submission_plans_execution_expiry_check
  CHECK (execution_expires_at IS NULL OR (
    execution_expires_at > approved_at AND
    execution_expires_at <= approved_at + interval '250 milliseconds'
  ));

CREATE OR REPLACE FUNCTION protect_v1c_submission_plan() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP='DELETE' OR
     NEW.id IS DISTINCT FROM OLD.id OR
     NEW.sandbox_session_id IS DISTINCT FROM OLD.sandbox_session_id OR
     NEW.arm_id IS DISTINCT FROM OLD.arm_id OR
     NEW.approval_hash IS DISTINCT FROM OLD.approval_hash OR
     NEW.intent_kind IS DISTINCT FROM OLD.intent_kind OR
     NEW.intent_hash IS DISTINCT FROM OLD.intent_hash OR
     NEW.allocator_hash IS DISTINCT FROM OLD.allocator_hash OR
     NEW.risk_hash IS DISTINCT FROM OLD.risk_hash OR
     NEW.planner_hash IS DISTINCT FROM OLD.planner_hash OR
     NEW.asset_approval_hash IS DISTINCT FROM OLD.asset_approval_hash OR
     NEW.policy_hash IS DISTINCT FROM OLD.policy_hash OR
     NEW.configuration_id IS DISTINCT FROM OLD.configuration_id OR
     NEW.leg_count IS DISTINCT FROM OLD.leg_count OR
     NEW.dispatch_policy IS DISTINCT FROM OLD.dispatch_policy OR
     NEW.execution_expires_at IS DISTINCT FROM OLD.execution_expires_at OR
     NEW.approved_at IS DISTINCT FROM OLD.approved_at OR
     NEW.revision<>OLD.revision+1 OR
     NEW.saga_revision<>OLD.saga_revision+1 OR
     OLD.state IN ('COMPLETED','FAILED','QUARANTINED') OR
     NOT (
       NEW.state=OLD.state OR
       (OLD.state='APPROVED' AND NEW.state IN (
         'ACTIVE','FAILED','RECOVERY_REQUIRED','QUARANTINED'
       )) OR
       (OLD.state='ACTIVE' AND NEW.state IN (
         'COMPLETED','FAILED','RECOVERY_REQUIRED','QUARANTINED'
       )) OR
       (OLD.state='RECOVERY_REQUIRED' AND NEW.state IN (
         'ACTIVE','COMPLETED','FAILED','QUARANTINED'
       ))
     ) THEN
    RAISE EXCEPTION 'v1c_submission_plan_mutation_rejected';
  END IF;
  RETURN NEW;
END;
$$;

-- A future sequential leg does not own its planned input before the prior
-- authoritative fill creates that asset. WAITING is a dependent claim, not
-- spendable ownership; only the fill transaction may activate it.
ALTER TABLE v1c_sandbox_reservations
  DROP CONSTRAINT v1c_sandbox_reservations_state_check;
ALTER TABLE v1c_sandbox_reservations
  DROP CONSTRAINT v1c_sandbox_reservations_check;
ALTER TABLE v1c_sandbox_reservations
  ADD CONSTRAINT v1c_sandbox_reservations_state_check
  CHECK (state IN ('WAITING','ACTIVE','CONSUMED','RELEASED','QUARANTINED')),
  ADD CONSTRAINT v1c_sandbox_reservations_lifecycle_check
  CHECK (
    (state IN ('WAITING','ACTIVE','QUARANTINED') AND
      released_at IS NULL AND release_reason IS NULL) OR
    (state IN ('CONSUMED','RELEASED') AND
      released_at IS NOT NULL AND release_reason IS NOT NULL)
  );

CREATE OR REPLACE FUNCTION protect_v1c_reservation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP='DELETE' OR
     NEW.id IS DISTINCT FROM OLD.id OR
     NEW.plan_id IS DISTINCT FROM OLD.plan_id OR
     NEW.account_id IS DISTINCT FROM OLD.account_id OR
     NEW.account_epoch IS DISTINCT FROM OLD.account_epoch OR
     NEW.order_id IS DISTINCT FROM OLD.order_id OR
     NEW.asset_symbol IS DISTINCT FROM OLD.asset_symbol OR
     NEW.quantity IS DISTINCT FROM OLD.quantity OR
     NEW.created_at IS DISTINCT FROM OLD.created_at OR
     NOT (
       (OLD.state='WAITING' AND NEW.state IN (
         'ACTIVE','RELEASED','QUARANTINED'
       )) OR
       (OLD.state='ACTIVE' AND NEW.state IN (
         'CONSUMED','RELEASED','QUARANTINED'
       ))
     ) THEN
    RAISE EXCEPTION 'v1c_reservation_mutation_rejected';
  END IF;
  RETURN NEW;
END;
$$;

DROP TRIGGER v1c_plan_eligibility_immutable ON v1c_plan_eligibility;
ALTER TABLE v1c_plan_eligibility ADD COLUMN instrument text;
UPDATE v1c_plan_eligibility
SET instrument=snapshot->>'instrument';
ALTER TABLE v1c_plan_eligibility ALTER COLUMN instrument SET NOT NULL;
ALTER TABLE v1c_plan_eligibility
  ADD CONSTRAINT v1c_plan_eligibility_instrument_check
  CHECK (
    instrument IN ('BTCUSDT','ETHUSDT','ETHBTC') AND
    snapshot->>'instrument'=instrument
  );
ALTER TABLE v1c_plan_eligibility DROP CONSTRAINT v1c_plan_eligibility_pkey;
ALTER TABLE v1c_plan_eligibility
  ADD PRIMARY KEY (plan_id,exchange,instrument);
CREATE TRIGGER v1c_plan_eligibility_immutable
  BEFORE UPDATE OR DELETE ON v1c_plan_eligibility
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();

DROP TRIGGER v1c_submission_outbox_protected ON v1c_submission_outbox;
DROP INDEX v1c_one_nonterminal_per_account;
ALTER TABLE v1c_submission_outbox
  DROP CONSTRAINT v1c_submission_outbox_state_check;
ALTER TABLE v1c_submission_outbox
  ADD COLUMN leg_index integer,
  ADD COLUMN depends_on_leg_index integer;
WITH indexed AS (
  SELECT id,row_number() OVER (PARTITION BY plan_id ORDER BY id)-1 AS leg_index
  FROM v1c_submission_outbox
)
UPDATE v1c_submission_outbox outbox
SET leg_index=indexed.leg_index
FROM indexed
WHERE indexed.id=outbox.id;
ALTER TABLE v1c_submission_outbox ALTER COLUMN leg_index SET NOT NULL;
ALTER TABLE v1c_submission_outbox
  ADD CONSTRAINT v1c_submission_outbox_leg_index_check
  CHECK (leg_index BETWEEN 0 AND 2),
  ADD CONSTRAINT v1c_submission_outbox_dependency_check
  CHECK (depends_on_leg_index IS NULL OR (
    depends_on_leg_index >= 0 AND depends_on_leg_index < leg_index
  )),
  ADD CONSTRAINT v1c_submission_outbox_plan_leg_unique
  UNIQUE (plan_id,leg_index),
  ADD CONSTRAINT v1c_submission_outbox_state_check
  CHECK (state IN ('WAITING','PENDING','CLAIMED','ACKNOWLEDGED','UNKNOWN','TERMINAL'));
CREATE UNIQUE INDEX v1c_one_nonterminal_per_account
  ON v1c_submission_outbox(account_id)
  WHERE state IN ('PENDING','CLAIMED','ACKNOWLEDGED','UNKNOWN');

CREATE OR REPLACE FUNCTION protect_v1c_submission_outbox() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP='DELETE' OR
     NEW.id IS DISTINCT FROM OLD.id OR
     NEW.plan_id IS DISTINCT FROM OLD.plan_id OR
     NEW.account_id IS DISTINCT FROM OLD.account_id OR
     NEW.account_epoch IS DISTINCT FROM OLD.account_epoch OR
     NEW.order_id IS DISTINCT FROM OLD.order_id OR
     NEW.client_order_id IS DISTINCT FROM OLD.client_order_id OR
     NEW.strategy_id IS DISTINCT FROM OLD.strategy_id OR
     NEW.instrument IS DISTINCT FROM OLD.instrument OR
     NEW.side IS DISTINCT FROM OLD.side OR
     NEW.quantity IS DISTINCT FROM OLD.quantity OR
     NEW.limit_price IS DISTINCT FROM OLD.limit_price OR
     NEW.reserved_notional IS DISTINCT FROM OLD.reserved_notional OR
     NEW.order_style IS DISTINCT FROM OLD.order_style OR
     NEW.intent_action IS DISTINCT FROM OLD.intent_action OR
     NEW.request_hash IS DISTINCT FROM OLD.request_hash OR
     NEW.policy_hash IS DISTINCT FROM OLD.policy_hash OR
     NEW.approved_at IS DISTINCT FROM OLD.approved_at OR
     NEW.leg_index IS DISTINCT FROM OLD.leg_index OR
     NEW.depends_on_leg_index IS DISTINCT FROM OLD.depends_on_leg_index OR
     NEW.attempt<OLD.attempt OR NEW.updated_at<OLD.updated_at OR
     (OLD.fencing_token IS NOT NULL AND
      NEW.fencing_token IS NOT NULL AND NEW.fencing_token<OLD.fencing_token) OR
     OLD.state='TERMINAL' OR
     NOT (
       NEW.state=OLD.state OR
       (OLD.state='WAITING' AND NEW.state IN ('PENDING','UNKNOWN','TERMINAL')) OR
       (OLD.state='PENDING' AND NEW.state IN ('CLAIMED','UNKNOWN')) OR
       (OLD.state='CLAIMED' AND NEW.state IN ('ACKNOWLEDGED','UNKNOWN','TERMINAL')) OR
       (OLD.state='ACKNOWLEDGED' AND NEW.state IN ('UNKNOWN','TERMINAL')) OR
       (OLD.state='UNKNOWN' AND NEW.state IN ('ACKNOWLEDGED','TERMINAL'))
     ) OR
     NOT (
       NEW.order_state=OLD.order_state OR
       NEW.order_state='RECOVERY_REQUIRED' OR
       (OLD.order_state='APPROVED' AND NEW.order_state IN (
         'SUBMITTING','REJECTED','EXPIRED'
       )) OR
       (OLD.order_state='SUBMITTING' AND NEW.order_state IN (
         'ACKNOWLEDGED','PARTIALLY_FILLED','FILLED','REJECTED','EXPIRED','UNKNOWN'
       )) OR
       (OLD.order_state='ACKNOWLEDGED' AND NEW.order_state IN (
         'PARTIALLY_FILLED','FILLED','CANCEL_PENDING','CANCELED','EXPIRED','UNKNOWN'
       )) OR
       (OLD.order_state='PARTIALLY_FILLED' AND NEW.order_state IN (
         'FILLED','CANCEL_PENDING','CANCELED','EXPIRED','UNKNOWN'
       )) OR
       (OLD.order_state='CANCEL_PENDING' AND NEW.order_state IN (
         'PARTIALLY_FILLED','FILLED','CANCELED','UNKNOWN'
       )) OR
       (OLD.order_state IN ('UNKNOWN','RECOVERY_REQUIRED') AND
        NEW.order_state='CANCEL_PENDING') OR
       (OLD.order_state='UNKNOWN' AND NEW.order_state IN (
         'ACKNOWLEDGED','PARTIALLY_FILLED','FILLED','CANCELED','REJECTED','EXPIRED'
       )) OR
       (OLD.order_state IN ('CANCELED','EXPIRED') AND NEW.order_state IN (
         'PARTIALLY_FILLED','FILLED'
       ))
     ) THEN
    RAISE EXCEPTION 'v1c_submission_outbox_mutation_rejected';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER v1c_submission_outbox_protected
  BEFORE UPDATE OR DELETE ON v1c_submission_outbox
  FOR EACH ROW EXECUTE FUNCTION protect_v1c_submission_outbox();
