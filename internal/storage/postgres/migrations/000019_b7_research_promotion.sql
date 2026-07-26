SET TIME ZONE 'UTC';

INSERT INTO authorization_permissions(id, description)
VALUES ('research.promote', 'Promote preregistered strategy research maturity')
ON CONFLICT (id) DO NOTHING;

INSERT INTO role_permissions(role_id, permission_id, granted_at)
VALUES ('owner', 'research.promote', CURRENT_TIMESTAMP)
ON CONFLICT DO NOTHING;

CREATE TABLE b7_experiment_preregistrations (
  id text PRIMARY KEY,
  research_generation_id text NOT NULL UNIQUE REFERENCES research_generations(id),
  strategy_version_id text NOT NULL REFERENCES strategy_versions(id),
  registration_hash sha256_hex NOT NULL UNIQUE,
  canonical_registration bytea NOT NULL,
  minimum_samples bigint NOT NULL CHECK (minimum_samples > 0),
  minimum_trades bigint NOT NULL CHECK (minimum_trades > 0),
  minimum_shadow_duration_nanos bigint NOT NULL CHECK (minimum_shadow_duration_nanos > 0),
  minimum_deflated_sharpe_probability numeric(18,17) NOT NULL
    CHECK (minimum_deflated_sharpe_probability BETWEEN 0 AND 1),
  registered_at timestamptz NOT NULL,
  final_test_start timestamptz NOT NULL,
  created_at timestamptz NOT NULL,
  CHECK (registered_at < final_test_start),
  CHECK (created_at >= registered_at)
);

CREATE FUNCTION validate_b7_preregistration() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  registered_experiment experiment_registrations%ROWTYPE;
  registered_generation research_generations%ROWTYPE;
BEGIN
  SELECT * INTO registered_generation
  FROM research_generations
  WHERE id = NEW.research_generation_id;
  SELECT * INTO registered_experiment
  FROM experiment_registrations
  WHERE id = registered_generation.experiment_id;
  IF NOT FOUND OR registered_experiment.strategy_version_id <> NEW.strategy_version_id OR
     registered_generation.registration_hash <> NEW.registration_hash OR
     registered_experiment.final_test_start IS NULL OR
     registered_experiment.final_test_start <> NEW.final_test_start OR
     registered_experiment.registered_at <> NEW.registered_at THEN
    RAISE EXCEPTION 'b7_preregistration_identity_mismatch';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER b7_preregistration_validated
  BEFORE INSERT ON b7_experiment_preregistrations
  FOR EACH ROW EXECUTE FUNCTION validate_b7_preregistration();

CREATE TABLE b7_validation_suites (
  id text PRIMARY KEY,
  preregistration_id text NOT NULL REFERENCES b7_experiment_preregistrations(id),
  research_generation_id text NOT NULL REFERENCES research_generations(id),
  strategy_version_id text NOT NULL REFERENCES strategy_versions(id),
  manifest_hash sha256_hex NOT NULL UNIQUE,
  canonical_manifest bytea NOT NULL,
  evidence_hash sha256_hex NOT NULL,
  final_test_consumption_hash sha256_hex NOT NULL,
  primary_modes text[] NOT NULL CHECK (
    primary_modes <@ ARRAY['backtest','replay','shadow','paper','testnet','demo']::text[]
  ),
  primary_dataset_tier text NOT NULL CHECK (
    primary_dataset_tier IN ('tier_a','tier_b','low_confidence','integration_only')
  ),
  primary_confidence_label text NOT NULL CHECK (
    primary_confidence_label IN ('formal_tier_a','local_tier_b','insufficient')
  ),
  has_integration_only_primary boolean NOT NULL,
  eligible_maturities text[] NOT NULL CHECK (
    eligible_maturities <@ ARRAY[
      'BACKTEST_VALIDATED','REPLAY_VALIDATED','SHADOW_VALIDATED'
    ]::text[]
  ),
  confidence_label text NOT NULL CHECK (
    confidence_label IN ('formal_tier_a','local_tier_b','insufficient','rejected')
  ),
  viability_disposition text NOT NULL CHECK (
    viability_disposition IN ('undetermined','viable_for_more_research','rejected')
  ),
  disclaimer_policy text NOT NULL CHECK (
    disclaimer_policy = 'no_production_profitability_claim'
  ),
  created_at timestamptz NOT NULL,
  CHECK (
    cardinality(eligible_maturities) = 0 OR (
      primary_dataset_tier = 'tier_a' AND
      primary_confidence_label = 'formal_tier_a' AND
      NOT has_integration_only_primary AND
      confidence_label = 'formal_tier_a' AND
      viability_disposition = 'viable_for_more_research'
    )
  )
);

CREATE FUNCTION validate_b7_validation_suite() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  registration b7_experiment_preregistrations%ROWTYPE;
  consumption experiment_final_test_consumptions%ROWTYPE;
BEGIN
  SELECT * INTO registration
  FROM b7_experiment_preregistrations
  WHERE id = NEW.preregistration_id;
  SELECT * INTO consumption
  FROM experiment_final_test_consumptions
  WHERE research_generation_id = NEW.research_generation_id;
  IF registration.id IS NULL OR consumption.research_generation_id IS NULL OR
     registration.research_generation_id <> NEW.research_generation_id OR
     registration.strategy_version_id <> NEW.strategy_version_id OR
     consumption.consumption_hash <> NEW.final_test_consumption_hash OR
     NEW.created_at < consumption.consumed_at THEN
    RAISE EXCEPTION 'b7_validation_suite_identity_mismatch';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER b7_validation_suite_validated
  BEFORE INSERT ON b7_validation_suites
  FOR EACH ROW EXECUTE FUNCTION validate_b7_validation_suite();

CREATE TABLE b7_champion_challenger_reports (
  id text PRIMARY KEY,
  champion_strategy_version_id text NOT NULL REFERENCES strategy_versions(id),
  challenger_strategy_version_id text NOT NULL REFERENCES strategy_versions(id),
  champion_suite_id text NOT NULL REFERENCES b7_validation_suites(id),
  challenger_suite_id text NOT NULL REFERENCES b7_validation_suites(id),
  champion_evidence_hash sha256_hex NOT NULL,
  challenger_evidence_hash sha256_hex NOT NULL,
  manifest_hash sha256_hex NOT NULL UNIQUE,
  canonical_manifest bytea NOT NULL,
  disposition text NOT NULL CHECK (
    disposition IN ('retain_champion','recommend_challenger','reject_challenger')
  ),
  disclaimer_policy text NOT NULL CHECK (
    disclaimer_policy = 'no_production_profitability_claim'
  ),
  created_at timestamptz NOT NULL,
  CHECK (champion_strategy_version_id <> challenger_strategy_version_id),
  CHECK (champion_suite_id <> challenger_suite_id)
);

CREATE FUNCTION validate_b7_champion_challenger() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  champion b7_validation_suites%ROWTYPE;
  challenger b7_validation_suites%ROWTYPE;
BEGIN
  SELECT * INTO champion
  FROM b7_validation_suites WHERE id = NEW.champion_suite_id;
  SELECT * INTO challenger
  FROM b7_validation_suites WHERE id = NEW.challenger_suite_id;
  IF champion.id IS NULL OR challenger.id IS NULL OR
     champion.strategy_version_id <> NEW.champion_strategy_version_id OR
     challenger.strategy_version_id <> NEW.challenger_strategy_version_id OR
     champion.manifest_hash <> NEW.champion_evidence_hash OR
     challenger.manifest_hash <> NEW.challenger_evidence_hash THEN
    RAISE EXCEPTION 'b7_champion_challenger_identity_mismatch';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER b7_champion_challenger_validated
  BEFORE INSERT ON b7_champion_challenger_reports
  FOR EACH ROW EXECUTE FUNCTION validate_b7_champion_challenger();

CREATE TABLE strategy_maturity_states (
  strategy_version_id text PRIMARY KEY REFERENCES strategy_versions(id),
  maturity text NOT NULL CHECK (maturity IN (
    'EXPERIMENTAL','BACKTEST_VALIDATED','REPLAY_VALIDATED','SHADOW_VALIDATED',
    'SANDBOX_INTEGRATION_VALIDATED','REJECTED'
  )),
  revision bigint NOT NULL CHECK (revision > 0),
  updated_at timestamptz NOT NULL
);

CREATE TABLE strategy_maturity_commands (
  id text PRIMARY KEY,
  strategy_version_id text NOT NULL REFERENCES strategy_versions(id),
  evidence_id text NOT NULL,
  evidence_hash sha256_hex NOT NULL,
  actor_user_id text NOT NULL,
  session_id text NOT NULL,
  idempotency_key text NOT NULL,
  payload_hash sha256_hex NOT NULL,
  expected_revision bigint NOT NULL CHECK (expected_revision > 0),
  prior_maturity text NOT NULL,
  target_maturity text NOT NULL,
  outcome text NOT NULL CHECK (outcome IN ('applied','rejected')),
  failure_code text,
  reason text NOT NULL,
  command_time timestamptz NOT NULL,
  recorded_at timestamptz NOT NULL DEFAULT statement_timestamp(),
  resulting_revision bigint NOT NULL CHECK (resulting_revision > 0),
  UNIQUE (actor_user_id, idempotency_key),
  CHECK (btrim(reason) <> ''),
  CHECK (prior_maturity IN (
    'EXPERIMENTAL','BACKTEST_VALIDATED','REPLAY_VALIDATED','SHADOW_VALIDATED',
    'SANDBOX_INTEGRATION_VALIDATED','REJECTED'
  )),
  CHECK (target_maturity IN (
    'EXPERIMENTAL','BACKTEST_VALIDATED','REPLAY_VALIDATED','SHADOW_VALIDATED',
    'SANDBOX_INTEGRATION_VALIDATED','REJECTED'
  )),
  CHECK (
    (outcome = 'applied' AND failure_code IS NULL) OR
    (outcome = 'rejected' AND failure_code IS NOT NULL)
  )
);

CREATE TABLE strategy_maturity_events (
  command_id text PRIMARY KEY REFERENCES strategy_maturity_commands(id),
  strategy_version_id text NOT NULL REFERENCES strategy_versions(id),
  evidence_id text NOT NULL REFERENCES b7_validation_suites(id),
  prior_maturity text NOT NULL,
  target_maturity text NOT NULL,
  revision bigint NOT NULL CHECK (revision > 1),
  event_hash sha256_hex NOT NULL,
  actor_user_id text NOT NULL,
  occurred_at timestamptz NOT NULL,
  UNIQUE (strategy_version_id, revision)
);

CREATE TRIGGER b7_preregistrations_immutable
  BEFORE UPDATE OR DELETE ON b7_experiment_preregistrations
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER b7_validation_suites_immutable
  BEFORE UPDATE OR DELETE ON b7_validation_suites
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER b7_champion_challenger_immutable
  BEFORE UPDATE OR DELETE ON b7_champion_challenger_reports
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER strategy_maturity_commands_immutable
  BEFORE UPDATE OR DELETE ON strategy_maturity_commands
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER strategy_maturity_events_immutable
  BEFORE UPDATE OR DELETE ON strategy_maturity_events
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();

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
    FROM users user_record
    JOIN sessions session_record
      ON session_record.user_id = user_record.id
    JOIN user_roles assignment
      ON assignment.user_id = user_record.id
    JOIN role_permissions granted
      ON granted.role_id = assignment.role_id
    WHERE user_record.id = p_actor_user_id
      AND user_record.status = 'active'
      AND session_record.id = p_session_id
      AND session_record.revoked_at IS NULL
      AND session_record.expires_at > p_command_time
      AND session_record.idle_expires_at > p_command_time
      AND session_record.reauthenticated_at <= p_command_time
      AND session_record.reauthenticated_at >= p_command_time - interval '10 minutes'
      AND p_command_time >= statement_timestamp() - interval '1 minute'
      AND p_command_time <= statement_timestamp() + interval '5 seconds'
      AND granted.permission_id = 'research.promote'
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

CREATE INDEX b7_validation_generation_idx
  ON b7_validation_suites(research_generation_id, created_at);
CREATE INDEX b7_maturity_command_strategy_idx
  ON strategy_maturity_commands(strategy_version_id, command_time);
