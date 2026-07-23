SET TIME ZONE 'UTC';

ALTER TABLE model_versions DROP CONSTRAINT model_versions_model_type_check;
ALTER TABLE model_versions ADD CONSTRAINT model_versions_model_type_check CHECK (model_type IN (
  'fee','latency','spread','slippage','fill','cost_basis','impact',
  'adverse_selection','maker_queue','gap','correlation','depth','recovery','claim',
  'inventory_shadow','concentration'
));

CREATE TABLE cross_exchange_candidates (
  decision_id text PRIMARY KEY REFERENCES decisions(id),
  strategy_version_id text NOT NULL REFERENCES strategy_versions(id),
  configuration_id text NOT NULL REFERENCES configuration_versions(id),
  coherent_view_id sha256_hex NOT NULL REFERENCES cross_market_view_headers(id),
  instrument_id text NOT NULL REFERENCES instruments(id),
  buy_exchange_id text NOT NULL REFERENCES exchanges(id),
  sell_exchange_id text NOT NULL REFERENCES exchanges(id),
  direction text NOT NULL CHECK (direction IN (
    'buy_binance_sell_bybit','buy_bybit_sell_binance'
  )),
  buy_ownership_account_id text NOT NULL REFERENCES portfolio_ownership(account_id),
  sell_ownership_account_id text NOT NULL REFERENCES portfolio_ownership(account_id),
  quote_budget financial_amount NOT NULL CHECK (quote_budget > 0 AND quote_budget <= 100),
  base_quantity financial_amount NOT NULL CHECK (base_quantity > 0),
  gross_spread signed_financial_amount NOT NULL,
  buy_fee financial_amount NOT NULL,
  sell_fee financial_amount NOT NULL,
  spread_depth_cost financial_amount NOT NULL,
  latency_deterioration financial_amount NOT NULL,
  recovery_allowance financial_amount NOT NULL CHECK (recovery_allowance > 0),
  expected_execution_pnl signed_financial_amount NOT NULL,
  maximum_one_leg_loss financial_amount NOT NULL,
  marginal_inventory_replacement financial_amount NOT NULL,
  natural_reversal_cost financial_amount NOT NULL,
  advisory_rebalancing_cost financial_amount NOT NULL,
  exchange_concentration_penalty financial_amount NOT NULL,
  usdt_venue_concentration_penalty financial_amount NOT NULL,
  expected_closed_cycle_profit signed_financial_amount NOT NULL
    CHECK (expected_closed_cycle_profit > 0),
  worst_closed_cycle_profit signed_financial_amount NOT NULL
    CHECK (worst_closed_cycle_profit > 0),
  restoration_delay_nanos bigint NOT NULL CHECK (restoration_delay_nanos > 0),
  first_detected_offset_nanos bigint NOT NULL CHECK (first_detected_offset_nanos > 0),
  decision_offset_nanos bigint NOT NULL CHECK (decision_offset_nanos >= first_detected_offset_nanos),
  expires_offset_nanos bigint NOT NULL CHECK (expires_offset_nanos >= decision_offset_nanos),
  configuration_hash sha256_hex NOT NULL,
  instrument_metadata_set_hash sha256_hex NOT NULL,
  risk_evaluation_id text NOT NULL REFERENCES risk_evaluations(id),
  pricing_model_version_id text NOT NULL REFERENCES model_versions(id),
  claim_model_version_id text NOT NULL REFERENCES model_versions(id),
  fee_model_version_id text NOT NULL REFERENCES model_versions(id),
  latency_model_version_id text NOT NULL REFERENCES model_versions(id),
  recovery_model_version_id text NOT NULL REFERENCES model_versions(id),
  inventory_shadow_model_version_id text NOT NULL REFERENCES model_versions(id),
  concentration_model_version_id text NOT NULL REFERENCES model_versions(id),
  correlation_id text NOT NULL,
  causation_id text NOT NULL,
  canonical_hash sha256_hex NOT NULL UNIQUE,
  recorded_at timestamptz NOT NULL,
  CHECK (buy_exchange_id <> sell_exchange_id),
  CHECK (buy_ownership_account_id <> sell_ownership_account_id),
  CHECK (worst_closed_cycle_profit <= expected_closed_cycle_profit),
  CHECK (expected_closed_cycle_profit < expected_execution_pnl),
  CHECK (expires_offset_nanos - first_detected_offset_nanos = 250000000),
  CHECK (
    (direction = 'buy_binance_sell_bybit' AND buy_exchange_id = 'binance' AND sell_exchange_id = 'bybit') OR
    (direction = 'buy_bybit_sell_binance' AND buy_exchange_id = 'bybit' AND sell_exchange_id = 'binance')
  )
);

CREATE TABLE cross_exchange_candidate_members (
  decision_id text NOT NULL REFERENCES cross_exchange_candidates(decision_id),
  coherent_view_id sha256_hex NOT NULL,
  member_ordinal integer NOT NULL CHECK (member_ordinal BETWEEN 0 AND 1),
  exchange_id text NOT NULL REFERENCES exchanges(id),
  instrument_id text NOT NULL REFERENCES instruments(id),
  book_version bigint NOT NULL CHECK (book_version > 0),
  connection_generation bigint NOT NULL CHECK (connection_generation > 0),
  receive_monotonic_nanos bigint NOT NULL CHECK (receive_monotonic_nanos > 0),
  receive_utc timestamptz NOT NULL,
  receive_utc_unix_nanos bigint NOT NULL,
  ingest_ordinal bigint NOT NULL CHECK (ingest_ordinal > 0),
  clock_offset_nanos bigint NOT NULL,
  clock_uncertainty_nanos bigint NOT NULL CHECK (clock_uncertainty_nanos >= 0),
  clock_interval_start timestamptz NOT NULL,
  clock_interval_end timestamptz NOT NULL,
  state_hash sha256_hex NOT NULL,
  collector_instance text NOT NULL CHECK (collector_instance ~ '^[A-Za-z0-9_.:-]{1,128}$'),
  collector_region text NOT NULL CHECK (collector_region ~ '^[A-Za-z0-9_.:-]{1,128}$'),
  PRIMARY KEY (decision_id, member_ordinal),
  UNIQUE (decision_id, exchange_id),
  FOREIGN KEY (coherent_view_id, exchange_id, instrument_id)
    REFERENCES cross_market_view_members(cross_market_view_id, exchange_id, instrument_id),
  CHECK (clock_interval_end >= clock_interval_start)
);

CREATE TABLE cross_exchange_candidate_legs (
  decision_id text NOT NULL REFERENCES cross_exchange_candidates(decision_id),
  leg_index integer NOT NULL CHECK (leg_index BETWEEN 0 AND 1),
  exchange_id text NOT NULL REFERENCES exchanges(id),
  ownership_account_id text NOT NULL REFERENCES portfolio_ownership(account_id),
  instrument_id text NOT NULL REFERENCES instruments(id),
  instrument_metadata_id text NOT NULL REFERENCES instrument_metadata_versions(id),
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
  UNIQUE (decision_id, exchange_id)
);

CREATE TABLE cross_exchange_inventory_snapshots (
  decision_id text NOT NULL REFERENCES cross_exchange_candidates(decision_id),
  snapshot_role text NOT NULL CHECK (snapshot_role IN ('buy_venue','sell_venue')),
  ownership_account_id text NOT NULL REFERENCES portfolio_ownership(account_id),
  exchange_id text NOT NULL REFERENCES exchanges(id),
  base_asset text NOT NULL REFERENCES assets(symbol),
  owner_label text NOT NULL,
  ownership_revision bigint NOT NULL CHECK (ownership_revision > 0),
  base_before financial_amount NOT NULL,
  base_after financial_amount NOT NULL,
  total_eligible_base financial_amount NOT NULL CHECK (total_eligible_base > 0),
  base_share_before financial_amount NOT NULL CHECK (base_share_before BETWEEN 0 AND 1),
  usdt_before financial_amount NOT NULL,
  usdt_after financial_amount NOT NULL,
  total_eligible_usdt financial_amount NOT NULL CHECK (total_eligible_usdt > 0),
  usdt_share_before financial_amount NOT NULL CHECK (usdt_share_before BETWEEN 0 AND 1),
  band_state text NOT NULL CHECK (band_state IN (
    'paused_depleted','reduced','normal','preferred_natural_reverse'
  )),
  natural_reverse_preferred boolean NOT NULL,
  PRIMARY KEY (decision_id, snapshot_role),
  UNIQUE (decision_id, exchange_id),
  CHECK (base_before <= total_eligible_base),
  CHECK (usdt_before <= total_eligible_usdt),
  CHECK (base_share_before = base_before / total_eligible_base),
  CHECK (usdt_share_before = usdt_before / total_eligible_usdt)
);

CREATE FUNCTION enforce_cross_exchange_candidate_references() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE
  parent_strategy text;
  parent_configuration text;
  parent_scope text;
  parent_view text;
  registered_configuration_hash sha256_hex;
  strategy_family text;
  buy_strategy text;
  sell_strategy text;
  buy_exchange text;
  sell_exchange text;
  buy_portfolio text;
  sell_portfolio text;
BEGIN
  SELECT strategy_version_id, configuration_id, decision_market_scope, cross_market_view_id
    INTO parent_strategy, parent_configuration, parent_scope, parent_view
    FROM decisions WHERE id = NEW.decision_id;
  IF parent_strategy IS DISTINCT FROM NEW.strategy_version_id OR
     parent_configuration IS DISTINCT FROM NEW.configuration_id OR
     parent_scope IS DISTINCT FROM 'cross_market' OR
     parent_view IS DISTINCT FROM NEW.coherent_view_id THEN
    RAISE EXCEPTION 'cross_exchange_candidate_parent_mismatch';
  END IF;
  SELECT configuration_hash INTO registered_configuration_hash
    FROM configuration_versions WHERE id = NEW.configuration_id;
  IF registered_configuration_hash IS DISTINCT FROM NEW.configuration_hash THEN
    RAISE EXCEPTION 'cross_exchange_candidate_configuration_hash_mismatch';
  END IF;
  SELECT definition.family INTO strategy_family
    FROM strategy_versions version
    JOIN strategy_definitions definition ON definition.id = version.strategy_id
    WHERE version.id = NEW.strategy_version_id;
  SELECT strategy_key, exchange_id, portfolio_id
    INTO buy_strategy, buy_exchange, buy_portfolio
    FROM portfolio_ownership WHERE account_id = NEW.buy_ownership_account_id;
  SELECT strategy_key, exchange_id, portfolio_id
    INTO sell_strategy, sell_exchange, sell_portfolio
    FROM portfolio_ownership WHERE account_id = NEW.sell_ownership_account_id;
  IF strategy_family IS DISTINCT FROM 'cross_exchange' OR
     buy_strategy IS DISTINCT FROM 'cross_exchange' OR
     sell_strategy IS DISTINCT FROM 'cross_exchange' OR
     buy_exchange IS DISTINCT FROM NEW.buy_exchange_id OR
     sell_exchange IS DISTINCT FROM NEW.sell_exchange_id OR
     buy_portfolio IS DISTINCT FROM sell_portfolio THEN
    RAISE EXCEPTION 'cross_exchange_candidate_ownership_mismatch';
  END IF;
  IF (SELECT decision_id FROM risk_evaluations WHERE id = NEW.risk_evaluation_id)
       IS DISTINCT FROM NEW.decision_id OR
     (SELECT outcome FROM risk_evaluations WHERE id = NEW.risk_evaluation_id)
       IS DISTINCT FROM 'approved' THEN
    RAISE EXCEPTION 'cross_exchange_candidate_risk_mismatch';
  END IF;
  IF (SELECT member_count FROM cross_market_view_headers WHERE id = NEW.coherent_view_id)
       IS DISTINCT FROM 2 THEN
    RAISE EXCEPTION 'cross_exchange_candidate_view_membership_mismatch';
  END IF;
  IF (SELECT model_type FROM model_versions WHERE id = NEW.pricing_model_version_id)
       IS DISTINCT FROM 'depth' OR
     (SELECT model_type FROM model_versions WHERE id = NEW.claim_model_version_id)
       IS DISTINCT FROM 'claim' OR
     (SELECT model_type FROM model_versions WHERE id = NEW.fee_model_version_id)
       IS DISTINCT FROM 'fee' OR
     (SELECT model_type FROM model_versions WHERE id = NEW.latency_model_version_id)
       IS DISTINCT FROM 'latency' OR
     (SELECT model_type FROM model_versions WHERE id = NEW.recovery_model_version_id)
       IS DISTINCT FROM 'recovery' OR
     (SELECT model_type FROM model_versions WHERE id = NEW.inventory_shadow_model_version_id)
       IS DISTINCT FROM 'inventory_shadow' OR
     (SELECT model_type FROM model_versions WHERE id = NEW.concentration_model_version_id)
       IS DISTINCT FROM 'concentration' THEN
    RAISE EXCEPTION 'cross_exchange_candidate_model_type_mismatch';
  END IF;
  RETURN NEW;
END;
$$;

CREATE FUNCTION enforce_cross_exchange_candidate_complete() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE
  candidate_id text;
  candidate cross_exchange_candidates%ROWTYPE;
  member_count integer;
  leg_count integer;
  inventory_count integer;
BEGIN
  candidate_id := NEW.decision_id;
  SELECT * INTO candidate FROM cross_exchange_candidates WHERE decision_id = candidate_id;
  SELECT count(*) INTO member_count FROM cross_exchange_candidate_members
    WHERE decision_id = candidate_id;
  SELECT count(*) INTO leg_count FROM cross_exchange_candidate_legs
    WHERE decision_id = candidate_id;
  SELECT count(*) INTO inventory_count FROM cross_exchange_inventory_snapshots
    WHERE decision_id = candidate_id;
  IF member_count <> 2 OR leg_count <> 2 OR inventory_count <> 2 THEN
    RAISE EXCEPTION 'cross_exchange_candidate_incomplete';
  END IF;
  IF EXISTS (
    SELECT 1 FROM cross_exchange_candidate_members copied
    JOIN cross_market_view_members source
      ON source.cross_market_view_id = copied.coherent_view_id AND
         source.exchange_id = copied.exchange_id AND source.instrument_id = copied.instrument_id
    WHERE copied.decision_id = candidate_id AND (
      copied.coherent_view_id <> candidate.coherent_view_id OR
      copied.instrument_id <> candidate.instrument_id OR
      copied.member_ordinal <> source.member_ordinal OR
      copied.book_version <> source.book_version OR
      copied.connection_generation <> source.connection_generation OR
      copied.receive_monotonic_nanos <> source.receive_monotonic_nanos OR
      copied.receive_utc <> source.receive_utc OR
      copied.receive_utc_unix_nanos <> source.receive_utc_unix_nanos OR
      copied.ingest_ordinal <> source.ingest_ordinal OR
      copied.clock_offset_nanos <> source.clock_offset_nanos OR
      copied.clock_uncertainty_nanos <> source.clock_uncertainty_nanos OR
      copied.clock_interval_start <> source.clock_interval_start OR
      copied.clock_interval_end <> source.clock_interval_end OR
      copied.state_hash <> source.state_hash OR
      copied.collector_instance <> source.collector_instance OR
      copied.collector_region <> source.collector_region
    )
  ) THEN
    RAISE EXCEPTION 'cross_exchange_candidate_member_evidence_mismatch';
  END IF;
  IF EXISTS (
    SELECT 1 FROM cross_exchange_candidate_legs leg
    JOIN cross_exchange_candidate_members member
      ON member.decision_id = leg.decision_id AND member.exchange_id = leg.exchange_id
    JOIN instrument_metadata_versions metadata ON metadata.id = leg.instrument_metadata_id
    WHERE leg.decision_id = candidate_id AND (
      leg.instrument_id <> candidate.instrument_id OR
      member.book_version <> leg.book_version OR
      member.connection_generation <> leg.connection_generation OR
      metadata.exchange_id <> leg.exchange_id OR metadata.instrument_id <> leg.instrument_id OR
      (leg.leg_index = 0 AND (
        leg.side <> 'buy' OR leg.exchange_id <> candidate.buy_exchange_id OR
        leg.ownership_account_id <> candidate.buy_ownership_account_id
      )) OR
      (leg.leg_index = 1 AND (
        leg.side <> 'sell' OR leg.exchange_id <> candidate.sell_exchange_id OR
        leg.ownership_account_id <> candidate.sell_ownership_account_id
      ))
    )
  ) THEN
    RAISE EXCEPTION 'cross_exchange_candidate_leg_mismatch';
  END IF;
  IF EXISTS (
    SELECT 1 FROM cross_exchange_inventory_snapshots inventory
    WHERE inventory.decision_id = candidate_id AND (
      inventory.base_asset <> (SELECT base_asset FROM instruments WHERE id = candidate.instrument_id) OR
      (inventory.snapshot_role = 'buy_venue' AND (
        inventory.exchange_id <> candidate.buy_exchange_id OR
        inventory.ownership_account_id <> candidate.buy_ownership_account_id OR
        inventory.base_after < inventory.base_before OR inventory.usdt_after > inventory.usdt_before
      )) OR
      (inventory.snapshot_role = 'sell_venue' AND (
        inventory.exchange_id <> candidate.sell_exchange_id OR
        inventory.ownership_account_id <> candidate.sell_ownership_account_id OR
        inventory.base_after > inventory.base_before OR inventory.usdt_after < inventory.usdt_before
      )) OR
      (inventory.base_share_before <= 0.30 AND inventory.band_state <> 'paused_depleted') OR
      (inventory.base_share_before > 0.30 AND inventory.base_share_before < 0.50 AND
        inventory.band_state <> 'reduced') OR
      (inventory.base_share_before BETWEEN 0.50 AND 0.70 AND inventory.band_state <> 'normal') OR
      (inventory.base_share_before > 0.70 AND inventory.band_state <> 'preferred_natural_reverse')
    )
  ) THEN
    RAISE EXCEPTION 'cross_exchange_candidate_inventory_mismatch';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER cross_exchange_candidates_reference_guard
  BEFORE INSERT OR UPDATE ON cross_exchange_candidates
  FOR EACH ROW EXECUTE FUNCTION enforce_cross_exchange_candidate_references();
CREATE TRIGGER cross_exchange_candidates_immutable
  BEFORE UPDATE OR DELETE ON cross_exchange_candidates
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER cross_exchange_candidate_members_immutable
  BEFORE UPDATE OR DELETE ON cross_exchange_candidate_members
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER cross_exchange_candidate_legs_immutable
  BEFORE UPDATE OR DELETE ON cross_exchange_candidate_legs
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER cross_exchange_inventory_snapshots_immutable
  BEFORE UPDATE OR DELETE ON cross_exchange_inventory_snapshots
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE CONSTRAINT TRIGGER cross_exchange_candidates_complete
  AFTER INSERT ON cross_exchange_candidates DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION enforce_cross_exchange_candidate_complete();
CREATE CONSTRAINT TRIGGER cross_exchange_candidate_members_complete
  AFTER INSERT ON cross_exchange_candidate_members DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION enforce_cross_exchange_candidate_complete();
CREATE CONSTRAINT TRIGGER cross_exchange_candidate_legs_complete
  AFTER INSERT ON cross_exchange_candidate_legs DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION enforce_cross_exchange_candidate_complete();
CREATE CONSTRAINT TRIGGER cross_exchange_inventory_snapshots_complete
  AFTER INSERT ON cross_exchange_inventory_snapshots DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION enforce_cross_exchange_candidate_complete();

CREATE TABLE b5_claim_resources (
  id text PRIMARY KEY,
  account_id text NOT NULL REFERENCES virtual_accounts(id),
  exchange_id text NOT NULL,
  resource_kind text NOT NULL CHECK (resource_kind IN (
    'balance','fee_buffer','liquidity','recovery'
  )),
  resource_key text NOT NULL,
  available_quantity financial_amount NOT NULL,
  held_quantity financial_amount NOT NULL,
  revision bigint NOT NULL CHECK (revision > 0),
  updated_at timestamptz NOT NULL,
  UNIQUE (account_id, exchange_id, resource_kind, resource_key)
);

CREATE TABLE b5_claim_groups (
  id text PRIMARY KEY,
  decision_id text NOT NULL UNIQUE REFERENCES cross_exchange_candidates(decision_id),
  state text NOT NULL CHECK (state IN (
    'active','consumed','released','expired','quarantined'
  )),
  fencing_token bigint NOT NULL CHECK (fencing_token > 0),
  revision bigint NOT NULL CHECK (revision > 0),
  correlation_id text NOT NULL,
  causation_id text NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL
);

CREATE TABLE b5_claim_items (
  group_id text NOT NULL REFERENCES b5_claim_groups(id),
  resource_id text NOT NULL REFERENCES b5_claim_resources(id),
  requested_quantity financial_amount NOT NULL CHECK (requested_quantity > 0),
  remaining_quantity financial_amount NOT NULL,
  PRIMARY KEY (group_id, resource_id),
  CHECK (remaining_quantity <= requested_quantity)
);

CREATE FUNCTION register_b5_claim_resource(
  p_id text, p_account_id text, p_exchange_id text, p_resource_kind text,
  p_resource_key text, p_available financial_amount, p_recorded_at timestamptz
) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
BEGIN
  IF p_available < 0 OR p_exchange_id !~ '^[a-z0-9_-]{1,32}$' OR
     p_resource_kind NOT IN ('balance','fee_buffer','liquidity','recovery') THEN
    RAISE EXCEPTION 'b5_claim_resource_invalid';
  END IF;
  INSERT INTO b5_claim_resources (
    id, account_id, exchange_id, resource_kind, resource_key,
    available_quantity, held_quantity, revision, updated_at
  ) VALUES (
    p_id, p_account_id, p_exchange_id, p_resource_kind, p_resource_key,
    p_available, 0, 1, p_recorded_at
  )
  ON CONFLICT (id) DO UPDATE SET
    available_quantity = EXCLUDED.available_quantity,
    revision = b5_claim_resources.revision + 1,
    updated_at = EXCLUDED.updated_at
  WHERE b5_claim_resources.account_id = EXCLUDED.account_id AND
        b5_claim_resources.exchange_id = EXCLUDED.exchange_id AND
        b5_claim_resources.resource_kind = EXCLUDED.resource_kind AND
        b5_claim_resources.resource_key = EXCLUDED.resource_key AND
        b5_claim_resources.held_quantity = 0;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'b5_claim_resource_registration_rejected';
  END IF;
END;
$$;

CREATE FUNCTION claim_b5_resources(
  p_group_id text, p_decision_id text, p_fencing_token bigint,
  p_correlation_id text, p_causation_id text, p_resource_ids text[],
  p_quantities numeric[], p_recorded_at timestamptz
) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE
  requested record;
  resource_available financial_amount;
  balance_count integer;
  fee_count integer;
  liquidity_count integer;
  recovery_count integer;
BEGIN
  IF cardinality(p_resource_ids) <> 7 OR
     cardinality(p_resource_ids) <> cardinality(p_quantities) OR
     p_fencing_token <= 0 OR EXISTS (
       SELECT 1 FROM unnest(p_resource_ids, p_quantities) AS item(resource_id, quantity)
       WHERE item.quantity <= 0
     ) OR cardinality(p_resource_ids) <> (
       SELECT count(DISTINCT item) FROM unnest(p_resource_ids) AS item
     ) THEN
    RAISE EXCEPTION 'b5_claim_request_invalid';
  END IF;
  SELECT
    count(*) FILTER (WHERE resource_kind = 'balance'),
    count(*) FILTER (WHERE resource_kind = 'fee_buffer'),
    count(*) FILTER (WHERE resource_kind = 'liquidity'),
    count(*) FILTER (WHERE resource_kind = 'recovery')
    INTO balance_count, fee_count, liquidity_count, recovery_count
    FROM b5_claim_resources WHERE id = ANY(p_resource_ids);
  IF balance_count <> 2 OR fee_count <> 2 OR liquidity_count <> 2 OR recovery_count <> 1 THEN
    RAISE EXCEPTION 'b5_claim_shape_invalid';
  END IF;
  INSERT INTO b5_claim_groups (
    id, decision_id, state, fencing_token, revision,
    correlation_id, causation_id, created_at, updated_at
  ) VALUES (
    p_group_id, p_decision_id, 'active', p_fencing_token, 1,
    p_correlation_id, p_causation_id, p_recorded_at, p_recorded_at
  );
  FOR requested IN
    SELECT item.resource_id, item.quantity
    FROM unnest(p_resource_ids, p_quantities) AS item(resource_id, quantity)
    ORDER BY item.resource_id
  LOOP
    SELECT available_quantity INTO resource_available
      FROM b5_claim_resources WHERE id = requested.resource_id FOR UPDATE;
    IF NOT FOUND OR resource_available < requested.quantity THEN
      RAISE EXCEPTION 'b5_claim_resource_unavailable';
    END IF;
    UPDATE b5_claim_resources SET
      available_quantity = available_quantity - requested.quantity,
      held_quantity = held_quantity + requested.quantity,
      revision = revision + 1, updated_at = p_recorded_at
      WHERE id = requested.resource_id;
    INSERT INTO b5_claim_items (
      group_id, resource_id, requested_quantity, remaining_quantity
    ) VALUES (
      p_group_id, requested.resource_id, requested.quantity, requested.quantity
    );
  END LOOP;
END;
$$;

CREATE FUNCTION settle_b5_claim_group(
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
    FROM b5_claim_groups WHERE id = p_group_id FOR UPDATE;
  IF claim_state IS DISTINCT FROM 'active' OR claim_fence IS DISTINCT FROM p_fencing_token OR
     claim_revision IS DISTINCT FROM p_expected_revision OR cardinality(p_resource_ids) = 0 OR
     cardinality(p_resource_ids) <> cardinality(p_consumed) OR
     cardinality(p_resource_ids) <> (
       SELECT count(DISTINCT item) FROM unnest(p_resource_ids) AS item
     ) THEN
    RAISE EXCEPTION 'b5_claim_settlement_rejected';
  END IF;
  FOR consumed IN
    SELECT item.resource_id, item.quantity
    FROM unnest(p_resource_ids, p_consumed) AS item(resource_id, quantity)
    ORDER BY item.resource_id
  LOOP
    SELECT remaining_quantity INTO remaining FROM b5_claim_items
      WHERE group_id = p_group_id AND resource_id = consumed.resource_id FOR UPDATE;
    IF NOT FOUND OR consumed.quantity <= 0 OR consumed.quantity > remaining THEN
      RAISE EXCEPTION 'b5_claim_settlement_rejected';
    END IF;
    UPDATE b5_claim_items SET remaining_quantity = remaining_quantity - consumed.quantity
      WHERE group_id = p_group_id AND resource_id = consumed.resource_id;
    UPDATE b5_claim_resources SET held_quantity = held_quantity - consumed.quantity,
      revision = revision + 1, updated_at = p_recorded_at
      WHERE id = consumed.resource_id;
  END LOOP;
  IF p_final THEN
    FOR release IN
      SELECT resource_id, remaining_quantity FROM b5_claim_items
      WHERE group_id = p_group_id AND remaining_quantity > 0 ORDER BY resource_id FOR UPDATE
    LOOP
      UPDATE b5_claim_resources SET
        available_quantity = available_quantity + release.remaining_quantity,
        held_quantity = held_quantity - release.remaining_quantity,
        revision = revision + 1, updated_at = p_recorded_at
        WHERE id = release.resource_id;
      UPDATE b5_claim_items SET remaining_quantity = 0
        WHERE group_id = p_group_id AND resource_id = release.resource_id;
    END LOOP;
  END IF;
  UPDATE b5_claim_groups SET state = CASE WHEN p_final THEN 'consumed' ELSE 'active' END,
    revision = revision + 1, updated_at = p_recorded_at WHERE id = p_group_id;
END;
$$;

CREATE FUNCTION close_b5_claim_group(
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
    FROM b5_claim_groups WHERE id = p_group_id FOR UPDATE;
  IF claim_state IS DISTINCT FROM 'active' OR claim_fence IS DISTINCT FROM p_fencing_token OR
     claim_revision IS DISTINCT FROM p_expected_revision OR
     p_next_state NOT IN ('released','expired','quarantined') THEN
    RAISE EXCEPTION 'b5_claim_transition_rejected';
  END IF;
  IF p_next_state <> 'quarantined' THEN
    FOR release IN
      SELECT resource_id, remaining_quantity FROM b5_claim_items
      WHERE group_id = p_group_id AND remaining_quantity > 0 ORDER BY resource_id FOR UPDATE
    LOOP
      UPDATE b5_claim_resources SET
        available_quantity = available_quantity + release.remaining_quantity,
        held_quantity = held_quantity - release.remaining_quantity,
        revision = revision + 1, updated_at = p_recorded_at
        WHERE id = release.resource_id;
      UPDATE b5_claim_items SET remaining_quantity = 0
        WHERE group_id = p_group_id AND resource_id = release.resource_id;
    END LOOP;
  END IF;
  UPDATE b5_claim_groups SET state = p_next_state, revision = revision + 1,
    updated_at = p_recorded_at WHERE id = p_group_id;
END;
$$;

CREATE TABLE cross_exchange_simulation_outcomes (
  decision_id text PRIMARY KEY REFERENCES cross_exchange_candidates(decision_id),
  plan_id text NOT NULL UNIQUE REFERENCES execution_plans(id),
  outcome text NOT NULL CHECK (outcome IN (
    'both_filled','buy_only','sell_only','partial_buy','partial_sell',
    'partial_both','both_missed','negative_before_arrival','delayed_unknown'
  )),
  actual_usdt_net signed_financial_amount NOT NULL,
  verification_completed boolean NOT NULL,
  retry_attempted boolean NOT NULL,
  retry_succeeded boolean NOT NULL,
  unwind_attempted boolean NOT NULL,
  unwind_succeeded boolean NOT NULL,
  quarantined boolean NOT NULL,
  final_disposition text NOT NULL,
  recovery_loss financial_amount NOT NULL,
  latency_model_version_id text NOT NULL REFERENCES model_versions(id),
  canonical_hash sha256_hex NOT NULL UNIQUE,
  correlation_id text NOT NULL,
  causation_id text NOT NULL,
  recorded_at timestamptz NOT NULL,
  CHECK (NOT retry_succeeded OR retry_attempted),
  CHECK (NOT unwind_succeeded OR unwind_attempted),
  CHECK ((outcome = 'delayed_unknown') = quarantined)
);

CREATE TABLE cross_exchange_simulation_legs (
  decision_id text NOT NULL REFERENCES cross_exchange_simulation_outcomes(decision_id),
  leg_index integer NOT NULL CHECK (leg_index BETWEEN 0 AND 1),
  exchange_id text NOT NULL REFERENCES exchanges(id),
  arrival_offset_nanos bigint NOT NULL CHECK (arrival_offset_nanos > 0),
  initial_state text NOT NULL,
  verified_state text NOT NULL,
  final_state text NOT NULL,
  input_quantity financial_amount NOT NULL,
  filled_quantity financial_amount NOT NULL,
  verification_count integer NOT NULL CHECK (verification_count BETWEEN 0 AND 1),
  retry_count integer NOT NULL CHECK (retry_count BETWEEN 0 AND 1),
  PRIMARY KEY (decision_id, leg_index),
  UNIQUE (decision_id, exchange_id),
  CHECK (NOT (initial_state = 'UNKNOWN') OR verification_count = 1),
  CHECK (retry_count = 0 OR verification_count = 1)
);

CREATE TABLE cross_exchange_rebalancing_needs (
  decision_id text PRIMARY KEY REFERENCES cross_exchange_candidates(decision_id),
  required boolean NOT NULL,
  asset_symbol text NOT NULL REFERENCES assets(symbol),
  depleted_exchange_id text NOT NULL REFERENCES exchanges(id),
  overweight_exchange_id text NOT NULL REFERENCES exchanges(id),
  preferred_action text NOT NULL CHECK (preferred_action IN (
    'none','prefer_natural_reverse_candidate','operator_review_only'
  )),
  estimated_cost financial_amount NOT NULL,
  estimated_delay_nanos bigint NOT NULL CHECK (estimated_delay_nanos > 0),
  advisory_only boolean NOT NULL CHECK (advisory_only),
  recorded_at timestamptz NOT NULL,
  CHECK (depleted_exchange_id <> overweight_exchange_id)
);

CREATE TABLE cross_exchange_journal_links (
  decision_id text NOT NULL REFERENCES cross_exchange_candidates(decision_id),
  transaction_id text NOT NULL UNIQUE REFERENCES journal_transactions(id),
  category text NOT NULL CHECK (category IN (
    'execution_pnl','btc_inventory_market_pnl','eth_inventory_market_pnl',
    'stablecoin_valuation','fees','spread','slippage','latency','recovery',
    'inventory_restoration','combined_pnl'
  )),
  PRIMARY KEY (decision_id, category)
);

CREATE TRIGGER cross_exchange_simulation_outcomes_immutable
  BEFORE UPDATE OR DELETE ON cross_exchange_simulation_outcomes
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER cross_exchange_simulation_legs_immutable
  BEFORE UPDATE OR DELETE ON cross_exchange_simulation_legs
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER cross_exchange_rebalancing_needs_immutable
  BEFORE UPDATE OR DELETE ON cross_exchange_rebalancing_needs
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER cross_exchange_journal_links_immutable
  BEFORE UPDATE OR DELETE ON cross_exchange_journal_links
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();

CREATE INDEX cross_exchange_candidates_view_recorded_idx
  ON cross_exchange_candidates(coherent_view_id, recorded_at);
CREATE INDEX cross_exchange_candidates_strategy_recorded_idx
  ON cross_exchange_candidates(strategy_version_id, recorded_at);
CREATE INDEX cross_exchange_candidate_members_book_idx
  ON cross_exchange_candidate_members(exchange_id, instrument_id, book_version, connection_generation);
CREATE INDEX b5_claim_resources_owner_idx
  ON b5_claim_resources(account_id, exchange_id, resource_kind, resource_key);
CREATE INDEX b5_claim_groups_active_idx
  ON b5_claim_groups(updated_at) WHERE state IN ('active','quarantined');

REVOKE EXECUTE ON FUNCTION register_b5_claim_resource(
  text,text,text,text,text,financial_amount,timestamptz
) FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION claim_b5_resources(
  text,text,bigint,text,text,text[],numeric[],timestamptz
) FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION settle_b5_claim_group(
  text,bigint,bigint,text[],numeric[],boolean,timestamptz
) FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION close_b5_claim_group(
  text,bigint,bigint,text,timestamptz
) FROM PUBLIC;
