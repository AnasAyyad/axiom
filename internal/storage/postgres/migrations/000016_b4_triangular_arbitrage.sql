SET TIME ZONE 'UTC';

ALTER TABLE model_versions DROP CONSTRAINT model_versions_model_type_check;
ALTER TABLE model_versions ADD CONSTRAINT model_versions_model_type_check CHECK (model_type IN (
  'fee','latency','spread','slippage','fill','cost_basis','impact',
  'adverse_selection','maker_queue','gap','correlation','depth','recovery','claim'
));

CREATE TABLE triangular_candidates (
  decision_id text PRIMARY KEY REFERENCES decisions(id),
  strategy_version_id text NOT NULL REFERENCES strategy_versions(id),
  configuration_id text NOT NULL REFERENCES configuration_versions(id),
  portfolio_ownership_account_id text NOT NULL REFERENCES portfolio_ownership(account_id),
  exchange_id text NOT NULL REFERENCES exchanges(id),
  cycle text NOT NULL CHECK (cycle IN ('USDT-BTC-ETH-USDT','USDT-ETH-BTC-USDT')),
  start_quantity financial_amount NOT NULL CHECK (start_quantity > 0 AND start_quantity <= 100),
  expected_final_quantity financial_amount NOT NULL CHECK (expected_final_quantity > start_quantity),
  worst_final_quantity financial_amount NOT NULL CHECK (worst_final_quantity > start_quantity),
  expected_net signed_financial_amount NOT NULL CHECK (expected_net > 0),
  worst_net signed_financial_amount NOT NULL CHECK (worst_net > 0),
  expected_edge financial_amount NOT NULL CHECK (expected_edge > 0),
  worst_edge financial_amount NOT NULL CHECK (worst_edge > 0.0015),
  additional_safety_margin financial_amount NOT NULL CHECK (additional_safety_margin = 0.0015),
  first_detected_offset_nanos bigint NOT NULL CHECK (first_detected_offset_nanos > 0),
  decision_offset_nanos bigint NOT NULL CHECK (decision_offset_nanos >= first_detected_offset_nanos),
  expires_offset_nanos bigint NOT NULL CHECK (expires_offset_nanos >= decision_offset_nanos),
  configuration_hash sha256_hex NOT NULL,
  model_version_id text NOT NULL REFERENCES model_versions(id),
  instrument_metadata_set_hash sha256_hex NOT NULL,
  risk_evaluation_id text NOT NULL REFERENCES risk_evaluations(id),
  claim_model_version_id text NOT NULL REFERENCES model_versions(id),
  fee_model_version_id text NOT NULL REFERENCES model_versions(id),
  latency_model_version_id text NOT NULL REFERENCES model_versions(id),
  recovery_model_version_id text NOT NULL REFERENCES model_versions(id),
  correlation_id text NOT NULL,
  causation_id text NOT NULL,
  canonical_hash sha256_hex NOT NULL UNIQUE,
  recorded_at timestamptz NOT NULL,
  CHECK (expires_offset_nanos - first_detected_offset_nanos = 250000000)
);

CREATE TABLE triangular_candidate_legs (
  decision_id text NOT NULL REFERENCES triangular_candidates(decision_id),
  leg_index integer NOT NULL CHECK (leg_index BETWEEN 0 AND 2),
  instrument_id text NOT NULL REFERENCES instruments(id),
  instrument_metadata_id text NOT NULL REFERENCES instrument_metadata_versions(id),
  source_asset text NOT NULL REFERENCES assets(symbol),
  target_asset text NOT NULL REFERENCES assets(symbol),
  side text NOT NULL CHECK (side IN ('buy','sell')),
  input_quantity financial_amount NOT NULL CHECK (input_quantity > 0),
  trade_quantity financial_amount NOT NULL CHECK (trade_quantity > 0),
  gross_output financial_amount NOT NULL CHECK (gross_output > 0),
  net_output financial_amount NOT NULL CHECK (net_output > 0),
  source_dust financial_amount NOT NULL,
  fee_asset text NOT NULL REFERENCES assets(symbol),
  fee_quantity financial_amount NOT NULL,
  fee_quote_equivalent financial_amount NOT NULL,
  notional financial_amount NOT NULL CHECK (notional > 0),
  vwap financial_amount NOT NULL CHECK (vwap > 0),
  spread_depth_cost financial_amount NOT NULL,
  book_version bigint NOT NULL CHECK (book_version > 0),
  connection_generation bigint NOT NULL CHECK (connection_generation > 0),
  PRIMARY KEY (decision_id, leg_index),
  CHECK (source_asset <> target_asset)
);

CREATE FUNCTION enforce_triangular_candidate_references() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE
  parent_strategy text;
  parent_configuration text;
  parent_scope text;
  registered_configuration_hash sha256_hex;
  owner_strategy_version text;
  owner_strategy_key text;
  owner_exchange text;
  strategy_family text;
BEGIN
  SELECT strategy_version_id, configuration_id, decision_market_scope
    INTO parent_strategy, parent_configuration, parent_scope
    FROM decisions WHERE id = NEW.decision_id;
  IF parent_strategy IS DISTINCT FROM NEW.strategy_version_id OR
     parent_configuration IS DISTINCT FROM NEW.configuration_id OR
     parent_scope IS DISTINCT FROM 'single_market' THEN
    RAISE EXCEPTION 'triangular_candidate_parent_mismatch';
  END IF;
  SELECT configuration_hash INTO registered_configuration_hash
    FROM configuration_versions WHERE id = NEW.configuration_id;
  IF registered_configuration_hash IS DISTINCT FROM NEW.configuration_hash THEN
    RAISE EXCEPTION 'triangular_candidate_configuration_hash_mismatch';
  END IF;
  SELECT ownership.strategy_version_id, ownership.strategy_key, ownership.exchange_id
    INTO owner_strategy_version, owner_strategy_key, owner_exchange
    FROM portfolio_ownership ownership
    WHERE ownership.account_id = NEW.portfolio_ownership_account_id;
  SELECT definition.family INTO strategy_family
    FROM strategy_versions version
    JOIN strategy_definitions definition ON definition.id = version.strategy_id
    WHERE version.id = NEW.strategy_version_id;
  IF owner_strategy_version IS DISTINCT FROM NEW.strategy_version_id OR
     owner_strategy_key IS DISTINCT FROM 'triangular' OR
     strategy_family IS DISTINCT FROM 'triangular' OR
     owner_exchange IS DISTINCT FROM NEW.exchange_id THEN
    RAISE EXCEPTION 'triangular_candidate_ownership_mismatch';
  END IF;
  IF (SELECT decision_id FROM risk_evaluations WHERE id = NEW.risk_evaluation_id)
       IS DISTINCT FROM NEW.decision_id OR
     (SELECT outcome FROM risk_evaluations WHERE id = NEW.risk_evaluation_id)
       IS DISTINCT FROM 'approved' THEN
    RAISE EXCEPTION 'triangular_candidate_risk_mismatch';
  END IF;
  IF (SELECT model_type FROM model_versions WHERE id = NEW.model_version_id)
       IS DISTINCT FROM 'depth' OR
     (SELECT model_type FROM model_versions WHERE id = NEW.claim_model_version_id)
       IS DISTINCT FROM 'claim' OR
     (SELECT model_type FROM model_versions WHERE id = NEW.fee_model_version_id)
       IS DISTINCT FROM 'fee' OR
     (SELECT model_type FROM model_versions WHERE id = NEW.latency_model_version_id)
       IS DISTINCT FROM 'latency' OR
     (SELECT model_type FROM model_versions WHERE id = NEW.recovery_model_version_id)
       IS DISTINCT FROM 'recovery' THEN
    RAISE EXCEPTION 'triangular_candidate_model_type_mismatch';
  END IF;
  RETURN NEW;
END;
$$;

CREATE FUNCTION enforce_triangular_candidate_complete() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE
  candidate_id text;
  path text[];
  actual_legs integer;
  first_index integer;
  last_index integer;
BEGIN
  candidate_id := CASE WHEN TG_TABLE_NAME = 'triangular_candidates'
    THEN NEW.decision_id ELSE NEW.decision_id END;
  SELECT count(*), min(leg_index), max(leg_index)
    INTO actual_legs, first_index, last_index
    FROM triangular_candidate_legs WHERE decision_id = candidate_id;
  IF actual_legs <> 3 OR first_index <> 0 OR last_index <> 2 THEN
    RAISE EXCEPTION 'triangular_candidate_incomplete';
  END IF;
  SELECT array_agg(source_asset ORDER BY leg_index) ||
         ARRAY[(array_agg(target_asset ORDER BY leg_index))[3]]
    INTO path FROM triangular_candidate_legs WHERE decision_id = candidate_id;
  IF path NOT IN (
    ARRAY['USDT','BTC','ETH','USDT']::text[],
    ARRAY['USDT','ETH','BTC','USDT']::text[]
  ) OR array_to_string(path, '-') IS DISTINCT FROM
       (SELECT cycle FROM triangular_candidates WHERE decision_id = candidate_id) THEN
    RAISE EXCEPTION 'triangular_candidate_path_mismatch';
  END IF;
  IF EXISTS (
    SELECT 1 FROM triangular_candidate_legs current
    JOIN triangular_candidate_legs prior
      ON prior.decision_id = current.decision_id AND prior.leg_index = current.leg_index - 1
    WHERE current.decision_id = candidate_id AND (
      current.input_quantity <> prior.net_output OR current.source_asset <> prior.target_asset
    )
  ) THEN
    RAISE EXCEPTION 'triangular_candidate_output_chain_mismatch';
  END IF;
  IF EXISTS (
    SELECT 1 FROM triangular_candidate_legs leg
    JOIN instruments instrument ON instrument.id = leg.instrument_id
    JOIN instrument_metadata_versions metadata ON metadata.id = leg.instrument_metadata_id
    JOIN triangular_candidates candidate ON candidate.decision_id = leg.decision_id
    WHERE leg.decision_id = candidate_id AND (
      metadata.instrument_id <> leg.instrument_id OR
      metadata.exchange_id <> candidate.exchange_id OR
      NOT (
        (leg.side = 'buy' AND leg.source_asset = instrument.quote_asset AND
         leg.target_asset = instrument.base_asset) OR
        (leg.side = 'sell' AND leg.source_asset = instrument.base_asset AND
         leg.target_asset = instrument.quote_asset)
      )
    )
  ) THEN
    RAISE EXCEPTION 'triangular_candidate_instrument_mismatch';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER triangular_candidates_reference_guard
  BEFORE INSERT OR UPDATE ON triangular_candidates
  FOR EACH ROW EXECUTE FUNCTION enforce_triangular_candidate_references();
CREATE TRIGGER triangular_candidates_immutable
  BEFORE UPDATE OR DELETE ON triangular_candidates
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER triangular_candidate_legs_immutable
  BEFORE UPDATE OR DELETE ON triangular_candidate_legs
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE CONSTRAINT TRIGGER triangular_candidates_complete
  AFTER INSERT ON triangular_candidates DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION enforce_triangular_candidate_complete();
CREATE CONSTRAINT TRIGGER triangular_candidate_legs_complete
  AFTER INSERT ON triangular_candidate_legs DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION enforce_triangular_candidate_complete();

CREATE TABLE b4_claim_resources (
  id text PRIMARY KEY,
  account_id text NOT NULL REFERENCES virtual_accounts(id),
  exchange_id text NOT NULL REFERENCES exchanges(id),
  resource_kind text NOT NULL CHECK (resource_kind IN ('balance','fee_buffer','liquidity','recovery')),
  resource_key text NOT NULL,
  available_quantity financial_amount NOT NULL,
  held_quantity financial_amount NOT NULL,
  revision bigint NOT NULL CHECK (revision > 0),
  updated_at timestamptz NOT NULL,
  UNIQUE (account_id, exchange_id, resource_kind, resource_key)
);

CREATE TABLE b4_claim_groups (
  id text PRIMARY KEY,
  decision_id text NOT NULL UNIQUE REFERENCES triangular_candidates(decision_id),
  account_id text NOT NULL REFERENCES virtual_accounts(id),
  state text NOT NULL CHECK (state IN ('active','consumed','released','expired','quarantined')),
  fencing_token bigint NOT NULL CHECK (fencing_token > 0),
  revision bigint NOT NULL CHECK (revision > 0),
  correlation_id text NOT NULL,
  causation_id text NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL
);

CREATE TABLE b4_claim_items (
  group_id text NOT NULL REFERENCES b4_claim_groups(id),
  resource_id text NOT NULL REFERENCES b4_claim_resources(id),
  requested_quantity financial_amount NOT NULL CHECK (requested_quantity > 0),
  remaining_quantity financial_amount NOT NULL,
  PRIMARY KEY (group_id, resource_id),
  CHECK (remaining_quantity <= requested_quantity)
);

CREATE FUNCTION register_b4_claim_resource(
  p_id text, p_account_id text, p_exchange_id text, p_resource_kind text,
  p_resource_key text, p_available financial_amount, p_recorded_at timestamptz
) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
BEGIN
  IF p_available < 0 OR p_resource_kind NOT IN (
    'balance','fee_buffer','liquidity','recovery'
  ) THEN
    RAISE EXCEPTION 'b4_claim_resource_invalid';
  END IF;
  INSERT INTO b4_claim_resources (
    id, account_id, exchange_id, resource_kind, resource_key,
    available_quantity, held_quantity, revision, updated_at
  ) VALUES (
    p_id, p_account_id, p_exchange_id, p_resource_kind, p_resource_key,
    p_available, 0, 1, p_recorded_at
  )
  ON CONFLICT (id) DO UPDATE SET
    available_quantity = EXCLUDED.available_quantity,
    revision = b4_claim_resources.revision + 1,
    updated_at = EXCLUDED.updated_at
  WHERE b4_claim_resources.account_id = EXCLUDED.account_id AND
        b4_claim_resources.exchange_id = EXCLUDED.exchange_id AND
        b4_claim_resources.resource_kind = EXCLUDED.resource_kind AND
        b4_claim_resources.resource_key = EXCLUDED.resource_key AND
        b4_claim_resources.held_quantity = 0;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'b4_claim_resource_registration_rejected';
  END IF;
END;
$$;

CREATE FUNCTION claim_b4_resources(
  p_group_id text, p_decision_id text, p_account_id text,
  p_fencing_token bigint, p_correlation_id text, p_causation_id text,
  p_resource_ids text[], p_quantities numeric[], p_recorded_at timestamptz
) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE
  requested record;
  resource_account text;
  resource_available financial_amount;
BEGIN
  IF cardinality(p_resource_ids) = 0 OR
     cardinality(p_resource_ids) <> cardinality(p_quantities) OR
     p_fencing_token <= 0 OR EXISTS (
       SELECT 1 FROM unnest(p_resource_ids, p_quantities) AS item(resource_id, quantity)
       WHERE item.quantity <= 0
     ) OR cardinality(p_resource_ids) <> (
       SELECT count(DISTINCT item) FROM unnest(p_resource_ids) AS item
     ) THEN
    RAISE EXCEPTION 'b4_claim_request_invalid';
  END IF;
  INSERT INTO b4_claim_groups (
    id, decision_id, account_id, state, fencing_token, revision,
    correlation_id, causation_id, created_at, updated_at
  ) VALUES (
    p_group_id, p_decision_id, p_account_id, 'active', p_fencing_token, 1,
    p_correlation_id, p_causation_id, p_recorded_at, p_recorded_at
  );
  FOR requested IN
    SELECT item.resource_id, item.quantity
    FROM unnest(p_resource_ids, p_quantities) AS item(resource_id, quantity)
    ORDER BY item.resource_id
  LOOP
    SELECT account_id, available_quantity
      INTO resource_account, resource_available
      FROM b4_claim_resources WHERE id = requested.resource_id FOR UPDATE;
    IF NOT FOUND OR resource_account IS DISTINCT FROM p_account_id OR
       resource_available < requested.quantity THEN
      RAISE EXCEPTION 'b4_claim_resource_unavailable';
    END IF;
    UPDATE b4_claim_resources SET
      available_quantity = available_quantity - requested.quantity,
      held_quantity = held_quantity + requested.quantity,
      revision = revision + 1, updated_at = p_recorded_at
      WHERE id = requested.resource_id;
    INSERT INTO b4_claim_items (
      group_id, resource_id, requested_quantity, remaining_quantity
    ) VALUES (
      p_group_id, requested.resource_id, requested.quantity, requested.quantity
    );
  END LOOP;
END;
$$;

CREATE FUNCTION settle_b4_claim_group(
  p_group_id text, p_expected_revision bigint, p_fencing_token bigint,
  p_resource_ids text[], p_consumed numeric[],
  p_final boolean, p_recorded_at timestamptz
) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE
  claim_state text;
  claim_fence bigint;
  claim_revision bigint;
  consumed record;
  remaining financial_amount;
  release record;
BEGIN
  SELECT state, fencing_token, revision INTO claim_state, claim_fence, claim_revision
    FROM b4_claim_groups WHERE id = p_group_id FOR UPDATE;
  IF claim_state IS DISTINCT FROM 'active' OR claim_fence IS DISTINCT FROM p_fencing_token OR
     claim_revision IS DISTINCT FROM p_expected_revision OR cardinality(p_resource_ids) = 0 OR
     cardinality(p_resource_ids) <> cardinality(p_consumed) OR
     cardinality(p_resource_ids) <> (
       SELECT count(DISTINCT item) FROM unnest(p_resource_ids) AS item
     ) THEN
    RAISE EXCEPTION 'b4_claim_settlement_rejected';
  END IF;
  FOR consumed IN
    SELECT item.resource_id, item.quantity
    FROM unnest(p_resource_ids, p_consumed) AS item(resource_id, quantity)
    ORDER BY item.resource_id
  LOOP
    SELECT remaining_quantity INTO remaining FROM b4_claim_items
      WHERE group_id = p_group_id AND resource_id = consumed.resource_id FOR UPDATE;
    IF NOT FOUND OR consumed.quantity <= 0 OR consumed.quantity > remaining THEN
      RAISE EXCEPTION 'b4_claim_settlement_rejected';
    END IF;
    UPDATE b4_claim_items SET remaining_quantity = remaining_quantity - consumed.quantity
      WHERE group_id = p_group_id AND resource_id = consumed.resource_id;
    UPDATE b4_claim_resources SET held_quantity = held_quantity - consumed.quantity,
      revision = revision + 1, updated_at = p_recorded_at
      WHERE id = consumed.resource_id;
  END LOOP;
  IF p_final THEN
    FOR release IN
      SELECT resource_id, remaining_quantity FROM b4_claim_items
      WHERE group_id = p_group_id AND remaining_quantity > 0 ORDER BY resource_id FOR UPDATE
    LOOP
      UPDATE b4_claim_resources SET
        available_quantity = available_quantity + release.remaining_quantity,
        held_quantity = held_quantity - release.remaining_quantity,
        revision = revision + 1, updated_at = p_recorded_at
        WHERE id = release.resource_id;
      UPDATE b4_claim_items SET remaining_quantity = 0
        WHERE group_id = p_group_id AND resource_id = release.resource_id;
    END LOOP;
  END IF;
  UPDATE b4_claim_groups SET state = CASE WHEN p_final THEN 'consumed' ELSE 'active' END,
    revision = revision + 1, updated_at = p_recorded_at WHERE id = p_group_id;
END;
$$;

CREATE FUNCTION close_b4_claim_group(
  p_group_id text, p_expected_revision bigint, p_fencing_token bigint,
  p_next_state text, p_recorded_at timestamptz
) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE
  claim_state text;
  claim_fence bigint;
  claim_revision bigint;
  release record;
BEGIN
  SELECT state, fencing_token, revision INTO claim_state, claim_fence, claim_revision
    FROM b4_claim_groups WHERE id = p_group_id FOR UPDATE;
  IF claim_state IS DISTINCT FROM 'active' OR claim_fence IS DISTINCT FROM p_fencing_token OR
     claim_revision IS DISTINCT FROM p_expected_revision OR
     p_next_state NOT IN ('released','expired','quarantined') THEN
    RAISE EXCEPTION 'b4_claim_transition_rejected';
  END IF;
  IF p_next_state <> 'quarantined' THEN
    FOR release IN
      SELECT resource_id, remaining_quantity FROM b4_claim_items
      WHERE group_id = p_group_id AND remaining_quantity > 0 ORDER BY resource_id FOR UPDATE
    LOOP
      UPDATE b4_claim_resources SET
        available_quantity = available_quantity + release.remaining_quantity,
        held_quantity = held_quantity - release.remaining_quantity,
        revision = revision + 1, updated_at = p_recorded_at
        WHERE id = release.resource_id;
      UPDATE b4_claim_items SET remaining_quantity = 0
        WHERE group_id = p_group_id AND resource_id = release.resource_id;
    END LOOP;
  END IF;
  UPDATE b4_claim_groups SET state = p_next_state, revision = revision + 1,
    updated_at = p_recorded_at WHERE id = p_group_id;
END;
$$;

CREATE TABLE triangular_simulation_outcomes (
  decision_id text PRIMARY KEY REFERENCES triangular_candidates(decision_id),
  plan_id text NOT NULL UNIQUE REFERENCES execution_plans(id),
  outcome text NOT NULL CHECK (outcome IN (
    'full_success','partial_cycle','missed_leg','negative_after_latency','stranded_asset'
  )),
  actual_final_usdt financial_amount,
  latency_model_version_id text NOT NULL REFERENCES model_versions(id),
  recovery_attempted boolean NOT NULL,
  recovery_succeeded boolean NOT NULL,
  quarantined boolean NOT NULL,
  stranded_asset text REFERENCES assets(symbol),
  stranded_quantity financial_amount,
  recovery_loss signed_financial_amount NOT NULL,
  canonical_hash sha256_hex NOT NULL UNIQUE,
  correlation_id text NOT NULL,
  causation_id text NOT NULL,
  recorded_at timestamptz NOT NULL,
  CHECK ((stranded_asset IS NULL) = (stranded_quantity IS NULL)),
  CHECK (NOT recovery_succeeded OR recovery_attempted),
  CHECK (NOT quarantined OR recovery_attempted),
  CHECK ((outcome = 'stranded_asset') = quarantined)
);

CREATE TABLE triangular_opportunity_lifetimes (
  decision_id text PRIMARY KEY REFERENCES triangular_candidates(decision_id),
  first_detection_nanos bigint NOT NULL CHECK (first_detection_nanos > 0),
  last_profitable_nanos bigint NOT NULL CHECK (last_profitable_nanos >= first_detection_nanos),
  peak_edge financial_amount NOT NULL,
  edge_at_arrival financial_amount NOT NULL,
  total_lifetime_nanos bigint NOT NULL CHECK (total_lifetime_nanos >= 0),
  survived_p50 boolean NOT NULL,
  survived_p95 boolean NOT NULL,
  survived_p99 boolean NOT NULL,
  metric_window integer NOT NULL CHECK (metric_window = 1000),
  correlation_id text NOT NULL,
  causation_id text NOT NULL,
  recorded_at timestamptz NOT NULL,
  CHECK (total_lifetime_nanos = last_profitable_nanos - first_detection_nanos)
);

CREATE TABLE triangular_journal_links (
  decision_id text NOT NULL REFERENCES triangular_candidates(decision_id),
  transaction_id text NOT NULL UNIQUE REFERENCES journal_transactions(id),
  category text NOT NULL CHECK (category IN (
    'trade_economics','fees','spread_depth','rounding_dust','latency',
    'recovery_unwind','stranded_inventory','reconciliation_adjustment'
  )),
  PRIMARY KEY (decision_id, category, transaction_id)
);

CREATE TRIGGER triangular_simulation_outcomes_immutable
  BEFORE UPDATE OR DELETE ON triangular_simulation_outcomes
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER triangular_opportunity_lifetimes_immutable
  BEFORE UPDATE OR DELETE ON triangular_opportunity_lifetimes
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER triangular_journal_links_immutable
  BEFORE UPDATE OR DELETE ON triangular_journal_links
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();

CREATE INDEX triangular_candidates_strategy_recorded_idx
  ON triangular_candidates(strategy_version_id, recorded_at);
CREATE INDEX triangular_candidate_legs_book_idx
  ON triangular_candidate_legs(instrument_id, book_version, connection_generation);
CREATE INDEX b4_claim_resources_owner_idx
  ON b4_claim_resources(account_id, exchange_id, resource_kind, resource_key);
CREATE INDEX b4_claim_groups_active_idx
  ON b4_claim_groups(account_id, updated_at) WHERE state IN ('active','quarantined');

REVOKE EXECUTE ON FUNCTION register_b4_claim_resource(
  text,text,text,text,text,financial_amount,timestamptz
) FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION claim_b4_resources(
  text,text,text,bigint,text,text,text[],numeric[],timestamptz
) FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION settle_b4_claim_group(
  text,bigint,bigint,text[],numeric[],boolean,timestamptz
) FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION close_b4_claim_group(
  text,bigint,bigint,text,timestamptz
) FROM PUBLIC;
