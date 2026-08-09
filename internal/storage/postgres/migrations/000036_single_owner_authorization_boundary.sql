SET TIME ZONE 'UTC';

-- Role and permission rows predate the one-owner product. They remain
-- available as immutable historical evidence only; no current authority may
-- be created, changed, or removed through them.
CREATE OR REPLACE FUNCTION reject_legacy_authorization_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'legacy_authorization_records_are_historical';
END;
$$;

CREATE TRIGGER authorization_permissions_historical
  BEFORE INSERT OR UPDATE OR DELETE ON authorization_permissions
  FOR EACH ROW EXECUTE FUNCTION reject_legacy_authorization_mutation();
CREATE TRIGGER authorization_roles_historical
  BEFORE INSERT OR UPDATE OR DELETE ON authorization_roles
  FOR EACH ROW EXECUTE FUNCTION reject_legacy_authorization_mutation();
CREATE TRIGGER role_permissions_historical
  BEFORE INSERT OR UPDATE OR DELETE ON role_permissions
  FOR EACH ROW EXECUTE FUNCTION reject_legacy_authorization_mutation();
CREATE TRIGGER user_roles_historical
  BEFORE INSERT OR UPDATE OR DELETE ON user_roles
  FOR EACH ROW EXECUTE FUNCTION reject_legacy_authorization_mutation();

-- The singleton owner relation is established only once. It may be inserted
-- by first bootstrap when the database contains no owner, but cannot be
-- reassigned or deleted by a running product process.
CREATE OR REPLACE FUNCTION protect_owner_account() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'owner_account_immutable';
END;
$$;
CREATE TRIGGER owner_accounts_immutable
  BEFORE UPDATE OR DELETE ON owner_accounts
  FOR EACH ROW EXECUTE FUNCTION protect_owner_account();

-- Recreate the durable maturity command boundary with the same atomic,
-- idempotent policy, but bind a request to the singleton owner account rather
-- than any historical role or permission grant.
CREATE OR REPLACE FUNCTION apply_b7_maturity_promotion(
  p_command_id text,
  p_strategy_version_id text,
  p_evidence_id text,
  p_evidence_hash sha256_hex,
  p_target_maturity text,
  p_expected_revision bigint,
  p_actor_user_id text,
  p_session_id text,
  p_idempotency_key text,
  p_payload_hash sha256_hex,
  p_reason text,
  p_command_time timestamptz
) RETURNS TABLE (
  command_id text,
  outcome text,
  maturity text,
  revision bigint,
  failure_code text
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
DECLARE
  existing strategy_maturity_commands%ROWTYPE;
  current_state strategy_maturity_states%ROWTYPE;
  suite b7_validation_suites%ROWTYPE;
  failure text;
  authenticated boolean;
  strategy_known boolean;
  transition_valid boolean;
BEGIN
  SELECT EXISTS (
    SELECT 1 FROM strategy_versions WHERE id = p_strategy_version_id
  ) INTO strategy_known;
  SELECT EXISTS (
    SELECT 1
    FROM owner_accounts owner
    JOIN users user_record ON user_record.id = owner.user_id
    JOIN sessions session_record ON session_record.user_id = user_record.id
    WHERE owner.singleton
      AND owner.user_id = p_actor_user_id
      AND user_record.status = 'active'
      AND session_record.id = p_session_id
      AND session_record.revoked_at IS NULL
      AND session_record.expires_at > p_command_time
      AND session_record.idle_expires_at > p_command_time
      AND session_record.reauthenticated_at <= p_command_time
      AND session_record.reauthenticated_at >= p_command_time - interval '10 minutes'
      AND p_command_time >= statement_timestamp() - interval '1 minute'
      AND p_command_time <= statement_timestamp() + interval '5 seconds'
  ) INTO authenticated;

  IF authenticated THEN
    SELECT * INTO existing
    FROM strategy_maturity_commands command
    WHERE command.actor_user_id = p_actor_user_id
      AND command.idempotency_key = p_idempotency_key;
    IF FOUND THEN
      IF existing.payload_hash <> p_payload_hash THEN
        RETURN QUERY SELECT existing.id, 'rejected'::text, existing.prior_maturity,
          existing.resulting_revision, 'promotion_idempotency_conflict'::text;
      ELSE
        RETURN QUERY SELECT existing.id, existing.outcome,
          CASE WHEN existing.outcome = 'applied'
            THEN existing.target_maturity ELSE existing.prior_maturity END,
          existing.resulting_revision, existing.failure_code;
      END IF;
      RETURN;
    END IF;
  END IF;

  SELECT * INTO current_state
  FROM strategy_maturity_states state
  WHERE state.strategy_version_id = p_strategy_version_id
  FOR UPDATE;
  IF NOT FOUND THEN
    current_state.strategy_version_id := p_strategy_version_id;
    current_state.maturity := 'EXPERIMENTAL';
    current_state.revision := 1;
    current_state.updated_at := p_command_time;
    IF strategy_known AND authenticated THEN
      INSERT INTO strategy_maturity_states(
        strategy_version_id, maturity, revision, updated_at
      ) VALUES (
        p_strategy_version_id, 'EXPERIMENTAL', 1, p_command_time
      ) ON CONFLICT (strategy_version_id) DO NOTHING;
      SELECT * INTO current_state
      FROM strategy_maturity_states state
      WHERE state.strategy_version_id = p_strategy_version_id
      FOR UPDATE;
    END IF;
  END IF;

  IF NOT strategy_known THEN
    RETURN QUERY SELECT p_command_id, 'rejected'::text, 'EXPERIMENTAL'::text,
      1::bigint, 'promotion_strategy_unknown'::text;
    RETURN;
  END IF;
  IF p_target_maturity NOT IN (
    'EXPERIMENTAL','BACKTEST_VALIDATED','REPLAY_VALIDATED','SHADOW_VALIDATED',
    'SANDBOX_INTEGRATION_VALIDATED','REJECTED'
  ) THEN
    RETURN QUERY SELECT p_command_id, 'rejected'::text, current_state.maturity,
      current_state.revision, 'promotion_target_invalid'::text;
    RETURN;
  END IF;
  IF NOT authenticated THEN
    SELECT * INTO existing
    FROM strategy_maturity_commands command
    WHERE command.actor_user_id = p_actor_user_id
      AND command.idempotency_key = p_idempotency_key;
    IF FOUND THEN
      RETURN QUERY SELECT p_command_id, 'rejected'::text, current_state.maturity,
        current_state.revision, 'promotion_unauthorized'::text;
      RETURN;
    END IF;
  END IF;

  SELECT * INTO suite
  FROM b7_validation_suites validation
  WHERE validation.id = p_evidence_id;

  failure := NULL;
  IF NOT authenticated THEN
    failure := 'promotion_unauthorized';
  ELSIF p_expected_revision <> current_state.revision THEN
    failure := 'promotion_revision_conflict';
  ELSIF suite.id IS NULL OR suite.strategy_version_id <> p_strategy_version_id OR
        suite.manifest_hash <> p_evidence_hash THEN
    failure := 'promotion_evidence_mismatch';
  ELSIF p_target_maturity = 'SANDBOX_INTEGRATION_VALIDATED' THEN
    failure := 'promotion_target_unavailable_v1b';
  END IF;

  transition_valid := p_target_maturity = 'REJECTED' AND current_state.maturity <> 'REJECTED';
  transition_valid := transition_valid OR (
    current_state.maturity = 'EXPERIMENTAL' AND
    p_target_maturity = 'BACKTEST_VALIDATED'
  ) OR (
    current_state.maturity = 'BACKTEST_VALIDATED' AND
    p_target_maturity = 'REPLAY_VALIDATED'
  ) OR (
    current_state.maturity = 'REPLAY_VALIDATED' AND
    p_target_maturity = 'SHADOW_VALIDATED'
  );
  IF failure IS NULL AND NOT transition_valid THEN
    failure := 'promotion_transition_invalid';
  ELSIF failure IS NULL AND p_target_maturity <> 'REJECTED' AND
        NOT (p_target_maturity = ANY(suite.eligible_maturities)) THEN
    failure := 'promotion_evidence_ineligible';
  END IF;

  INSERT INTO strategy_maturity_commands(
    id, strategy_version_id, evidence_id, evidence_hash, actor_user_id,
    session_id, idempotency_key, payload_hash, expected_revision,
    prior_maturity, target_maturity, outcome, failure_code, reason,
    command_time, resulting_revision
  ) VALUES (
    p_command_id, p_strategy_version_id, p_evidence_id, p_evidence_hash,
    p_actor_user_id, p_session_id, p_idempotency_key, p_payload_hash,
    p_expected_revision, current_state.maturity, p_target_maturity,
    CASE WHEN failure IS NULL THEN 'applied' ELSE 'rejected' END,
    failure, p_reason, p_command_time,
    CASE WHEN failure IS NULL THEN current_state.revision + 1
      ELSE current_state.revision END
  );

  IF failure IS NOT NULL THEN
    RETURN QUERY SELECT p_command_id, 'rejected'::text, current_state.maturity,
      current_state.revision, failure;
    RETURN;
  END IF;

  UPDATE strategy_maturity_states
  SET maturity = p_target_maturity,
      revision = current_state.revision + 1,
      updated_at = p_command_time
  WHERE strategy_version_id = p_strategy_version_id;
  INSERT INTO strategy_maturity_events(
    command_id, strategy_version_id, evidence_id, prior_maturity,
    target_maturity, revision, event_hash, actor_user_id, occurred_at
  ) VALUES (
    p_command_id, p_strategy_version_id, p_evidence_id,
    current_state.maturity, p_target_maturity, current_state.revision + 1,
    p_payload_hash, p_actor_user_id, p_command_time
  );
  RETURN QUERY SELECT p_command_id, 'applied'::text, p_target_maturity,
    current_state.revision + 1, NULL::text;
END;
$$;

REVOKE ALL ON FUNCTION apply_b7_maturity_promotion(
  text,text,text,sha256_hex,text,bigint,text,text,text,sha256_hex,text,timestamptz
) FROM PUBLIC;
