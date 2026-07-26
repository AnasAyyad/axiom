SET TIME ZONE 'UTC';

CREATE TABLE rebalancing_fact_sets (
  id text PRIMARY KEY CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9_.:-]{0,191}$'),
  configuration_id text NOT NULL REFERENCES configuration_versions(id),
  configuration_hash sha256_hex NOT NULL,
  fact_schema_version text NOT NULL CHECK (fact_schema_version = 'rebalancing-fact.v1'),
  cost_model_version text NOT NULL CHECK (cost_model_version = 'rebalancing-cost.v1'),
  canonical_hash sha256_hex NOT NULL UNIQUE,
  recorded_at timestamptz NOT NULL,
  UNIQUE (id, canonical_hash)
);

CREATE TABLE rebalancing_route_facts (
  fact_set_id text NOT NULL REFERENCES rebalancing_fact_sets(id),
  fact_id text NOT NULL CHECK (fact_id ~ '^[A-Za-z0-9][A-Za-z0-9_.:-]{0,191}$'),
  logical_key text NOT NULL CHECK (
    length(logical_key) BETWEEN 1 AND 512 AND
    logical_key ~ '^[A-Za-z0-9][A-Za-z0-9_.:@/|-]+$'
  ),
  fact_version bigint NOT NULL CHECK (fact_version > 0),
  fact_kind text NOT NULL CHECK (fact_kind IN ('trade','transfer')),
  from_exchange_id text NOT NULL REFERENCES exchanges(id),
  from_asset_symbol text NOT NULL REFERENCES assets(symbol),
  to_exchange_id text NOT NULL REFERENCES exchanges(id),
  to_asset_symbol text NOT NULL REFERENCES assets(symbol),
  instrument_id text REFERENCES instruments(id),
  network text,
  source_chain text,
  destination_chain text,
  minimum_quantity financial_amount NOT NULL,
  available boolean NOT NULL,
  withdrawal_available boolean NOT NULL,
  deposit_available boolean NOT NULL,
  compatible boolean NOT NULL,
  ambiguous boolean NOT NULL,
  fee_cost financial_amount NOT NULL,
  spread_cost financial_amount NOT NULL,
  depth_cost financial_amount NOT NULL,
  delay_cost financial_amount NOT NULL,
  network_fee_cost financial_amount NOT NULL,
  compatibility_cost financial_amount NOT NULL,
  volatility_risk_cost financial_amount NOT NULL,
  operational_risk_cost financial_amount NOT NULL,
  minimum_duration_nanos bigint NOT NULL CHECK (minimum_duration_nanos > 0),
  maximum_duration_nanos bigint NOT NULL CHECK (maximum_duration_nanos >= minimum_duration_nanos),
  risk_score financial_amount NOT NULL CHECK (risk_score <= 1),
  warnings text[] NOT NULL,
  manual_checklist text[] NOT NULL,
  source text NOT NULL CHECK (source ~ '^[A-Za-z0-9][A-Za-z0-9_.:/-]{0,191}$'),
  observer text NOT NULL CHECK (observer ~ '^[A-Za-z0-9][A-Za-z0-9_.:/-]{0,191}$'),
  observed_at timestamptz NOT NULL,
  expires_at timestamptz NOT NULL,
  confidence financial_amount NOT NULL CHECK (confidence <= 1),
  approved boolean NOT NULL,
  approval_actor text,
  approval_reference text,
  approved_at timestamptz,
  provenance_hash sha256_hex NOT NULL UNIQUE,
  PRIMARY KEY (fact_set_id, fact_id),
  UNIQUE (fact_set_id, logical_key, fact_version),
  CHECK (from_exchange_id <> to_exchange_id OR from_asset_symbol <> to_asset_symbol),
  CHECK (expires_at > observed_at),
  CHECK (
    (approved AND approval_actor IS NOT NULL AND approval_reference IS NOT NULL AND
      approved_at IS NOT NULL AND approved_at >= observed_at AND approved_at <= expires_at) OR
    (NOT approved AND approval_actor IS NULL AND approval_reference IS NULL AND approved_at IS NULL)
  ),
  CHECK (
    (fact_kind = 'trade' AND from_exchange_id = to_exchange_id AND
      from_asset_symbol <> to_asset_symbol AND instrument_id IS NOT NULL AND
      network IS NULL AND source_chain IS NULL AND destination_chain IS NULL) OR
    (fact_kind = 'transfer' AND from_exchange_id <> to_exchange_id AND
      from_asset_symbol = to_asset_symbol AND instrument_id IS NULL AND
      network IS NOT NULL AND source_chain IS NOT NULL AND destination_chain IS NOT NULL)
  )
);

CREATE TABLE rebalancing_recommendations (
  id text PRIMARY KEY CHECK (id ~ '^b6-[0-9a-f]{24}$'),
  request_id text NOT NULL UNIQUE CHECK (request_id ~ '^[A-Za-z0-9][A-Za-z0-9_.:-]{0,191}$'),
  configuration_id text NOT NULL REFERENCES configuration_versions(id),
  configuration_hash sha256_hex NOT NULL,
  fact_set_id text NOT NULL,
  fact_set_hash sha256_hex NOT NULL,
  source_b5_decision_id text REFERENCES cross_exchange_candidates(decision_id),
  method text NOT NULL CHECK (method IN ('natural_reverse_arbitrage','reviewed_graph_route')),
  source_exchange_id text NOT NULL REFERENCES exchanges(id),
  source_asset_symbol text NOT NULL REFERENCES assets(symbol),
  destination_exchange_id text NOT NULL REFERENCES exchanges(id),
  destination_asset_symbol text NOT NULL REFERENCES assets(symbol),
  quantity financial_amount NOT NULL CHECK (quantity > 0),
  fee_cost financial_amount NOT NULL,
  spread_cost financial_amount NOT NULL,
  depth_cost financial_amount NOT NULL,
  delay_cost financial_amount NOT NULL,
  network_fee_cost financial_amount NOT NULL,
  compatibility_cost financial_amount NOT NULL,
  volatility_risk_cost financial_amount NOT NULL,
  operational_risk_cost financial_amount NOT NULL,
  total_cost financial_amount NOT NULL CHECK (total_cost <= 25),
  minimum_duration_nanos bigint NOT NULL CHECK (minimum_duration_nanos > 0),
  maximum_duration_nanos bigint NOT NULL CHECK (
    maximum_duration_nanos >= minimum_duration_nanos AND
    maximum_duration_nanos <= 604800000000000
  ),
  risk_score financial_amount NOT NULL CHECK (risk_score <= 1),
  warnings text[] NOT NULL,
  advisory_only boolean NOT NULL CHECK (advisory_only),
  canonical_hash sha256_hex NOT NULL UNIQUE,
  recorded_at timestamptz NOT NULL,
  FOREIGN KEY (fact_set_id, fact_set_hash)
    REFERENCES rebalancing_fact_sets(id, canonical_hash),
  CHECK (source_exchange_id <> destination_exchange_id),
  CHECK (source_asset_symbol = destination_asset_symbol),
  CHECK (
    total_cost = fee_cost + spread_cost + depth_cost + delay_cost +
      network_fee_cost + compatibility_cost + volatility_risk_cost +
      operational_risk_cost
  ),
  CHECK (
    (method = 'natural_reverse_arbitrage' AND source_b5_decision_id IS NOT NULL) OR
    (method = 'reviewed_graph_route' AND source_b5_decision_id IS NULL)
  )
);

CREATE TABLE rebalancing_recommendation_steps (
  recommendation_id text NOT NULL REFERENCES rebalancing_recommendations(id),
  step_index integer NOT NULL CHECK (step_index BETWEEN 0 AND 5),
  role text NOT NULL CHECK (role IN ('route','sell_overweight_inventory','buy_depleted_inventory')),
  fact_set_id text NOT NULL,
  fact_id text NOT NULL,
  fact_version bigint NOT NULL CHECK (fact_version > 0),
  provenance_hash sha256_hex NOT NULL,
  PRIMARY KEY (recommendation_id, step_index),
  UNIQUE (recommendation_id, fact_id),
  FOREIGN KEY (fact_set_id, fact_id)
    REFERENCES rebalancing_route_facts(fact_set_id, fact_id)
);

CREATE TABLE rebalancing_checklist_steps (
  recommendation_id text NOT NULL REFERENCES rebalancing_recommendations(id),
  step_index integer NOT NULL CHECK (step_index BETWEEN 0 AND 63),
  instruction text NOT NULL CHECK (
    length(instruction) BETWEEN 1 AND 512 AND
    instruction ~ '^[A-Za-z0-9][A-Za-z0-9_.: /-]+$'
  ),
  PRIMARY KEY (recommendation_id, step_index)
);

CREATE FUNCTION enforce_rebalancing_fact_set_reference() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE
  registered_hash sha256_hex;
BEGIN
  SELECT configuration_hash INTO registered_hash
    FROM configuration_versions WHERE id = NEW.configuration_id;
  IF registered_hash IS DISTINCT FROM NEW.configuration_hash THEN
    RAISE EXCEPTION 'rebalancing_fact_set_configuration_mismatch';
  END IF;
  RETURN NEW;
END;
$$;

CREATE FUNCTION enforce_rebalancing_recommendation_reference() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE
  registered_hash sha256_hex;
  set_configuration_id text;
  set_configuration_hash sha256_hex;
BEGIN
  SELECT configuration_hash INTO registered_hash
    FROM configuration_versions WHERE id = NEW.configuration_id;
  SELECT configuration_id, configuration_hash
    INTO set_configuration_id, set_configuration_hash
    FROM rebalancing_fact_sets WHERE id = NEW.fact_set_id;
  IF registered_hash IS DISTINCT FROM NEW.configuration_hash OR
     set_configuration_id IS DISTINCT FROM NEW.configuration_id OR
     set_configuration_hash IS DISTINCT FROM NEW.configuration_hash THEN
    RAISE EXCEPTION 'rebalancing_recommendation_configuration_mismatch';
  END IF;
  RETURN NEW;
END;
$$;

CREATE FUNCTION enforce_rebalancing_recommendation_complete() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE
  target_id text;
  recommendation rebalancing_recommendations%ROWTYPE;
  step_count integer;
  checklist_count integer;
  summed_fee numeric;
  summed_spread numeric;
  summed_depth numeric;
  summed_delay numeric;
  summed_network_fee numeric;
  summed_compatibility numeric;
  summed_volatility numeric;
  summed_operational numeric;
  summed_minimum_duration numeric;
  summed_maximum_duration numeric;
  summed_risk numeric;
BEGIN
  IF TG_TABLE_NAME = 'rebalancing_recommendations' THEN
    target_id := NEW.id;
  ELSE
    target_id := NEW.recommendation_id;
  END IF;
  SELECT * INTO recommendation
    FROM rebalancing_recommendations WHERE id = target_id;
  IF NOT FOUND THEN
    RETURN NEW;
  END IF;
  SELECT count(*) INTO step_count
    FROM rebalancing_recommendation_steps WHERE recommendation_id = target_id;
  SELECT count(*) INTO checklist_count
    FROM rebalancing_checklist_steps WHERE recommendation_id = target_id;
  IF step_count < 1 OR step_count > 6 OR checklist_count < 4 OR
     (SELECT min(step_index) FROM rebalancing_recommendation_steps
       WHERE recommendation_id = target_id) <> 0 OR
     (SELECT max(step_index) FROM rebalancing_recommendation_steps
       WHERE recommendation_id = target_id) <> step_count - 1 OR
     (SELECT min(step_index) FROM rebalancing_checklist_steps
       WHERE recommendation_id = target_id) <> 0 OR
     (SELECT max(step_index) FROM rebalancing_checklist_steps
       WHERE recommendation_id = target_id) <> checklist_count - 1 THEN
    RAISE EXCEPTION 'rebalancing_recommendation_incomplete';
  END IF;
  IF EXISTS (
    SELECT 1
    FROM rebalancing_recommendation_steps selected
    JOIN rebalancing_route_facts fact
      ON fact.fact_set_id = selected.fact_set_id AND fact.fact_id = selected.fact_id
    WHERE selected.recommendation_id = target_id AND (
      selected.fact_set_id <> recommendation.fact_set_id OR
      selected.fact_version <> fact.fact_version OR
      selected.provenance_hash <> fact.provenance_hash OR
      NOT fact.available OR NOT fact.compatible OR fact.ambiguous OR
      NOT fact.approved OR fact.confidence < 0.80 OR
      fact.minimum_quantity > recommendation.quantity OR
      recommendation.recorded_at < fact.observed_at OR
      recommendation.recorded_at >= fact.expires_at OR
      fact.approved_at > recommendation.recorded_at OR
      (fact.fact_kind = 'transfer' AND (
        NOT fact.withdrawal_available OR NOT fact.deposit_available OR
        fact.network <> fact.source_chain OR fact.network <> fact.destination_chain
      ))
    )
  ) THEN
    RAISE EXCEPTION 'rebalancing_selected_fact_ineligible';
  END IF;
  SELECT
    sum(fact.fee_cost), sum(fact.spread_cost), sum(fact.depth_cost),
    sum(fact.delay_cost), sum(fact.network_fee_cost), sum(fact.compatibility_cost),
    sum(fact.volatility_risk_cost), sum(fact.operational_risk_cost),
    sum(fact.minimum_duration_nanos), sum(fact.maximum_duration_nanos), sum(fact.risk_score)
  INTO
    summed_fee, summed_spread, summed_depth, summed_delay, summed_network_fee,
    summed_compatibility, summed_volatility, summed_operational,
    summed_minimum_duration, summed_maximum_duration, summed_risk
  FROM rebalancing_recommendation_steps selected
  JOIN rebalancing_route_facts fact
    ON fact.fact_set_id = selected.fact_set_id AND fact.fact_id = selected.fact_id
  WHERE selected.recommendation_id = target_id;
  IF ROW(
    summed_fee, summed_spread, summed_depth, summed_delay, summed_network_fee,
    summed_compatibility, summed_volatility, summed_operational,
    summed_minimum_duration, summed_maximum_duration, summed_risk
  ) IS DISTINCT FROM ROW(
    recommendation.fee_cost, recommendation.spread_cost, recommendation.depth_cost,
    recommendation.delay_cost, recommendation.network_fee_cost,
    recommendation.compatibility_cost, recommendation.volatility_risk_cost,
    recommendation.operational_risk_cost, recommendation.minimum_duration_nanos,
    recommendation.maximum_duration_nanos, recommendation.risk_score
  ) THEN
    RAISE EXCEPTION 'rebalancing_recommendation_evidence_mismatch';
  END IF;
  IF recommendation.method = 'natural_reverse_arbitrage' THEN
    IF step_count <> 2 OR EXISTS (
      SELECT 1
      FROM rebalancing_recommendation_steps selected
      JOIN rebalancing_route_facts fact
        ON fact.fact_set_id = selected.fact_set_id AND fact.fact_id = selected.fact_id
      WHERE selected.recommendation_id = target_id AND (
        fact.fact_kind <> 'trade' OR
        (selected.step_index = 0 AND (
          selected.role <> 'sell_overweight_inventory' OR
          fact.from_exchange_id <> recommendation.source_exchange_id OR
          fact.from_asset_symbol <> recommendation.source_asset_symbol OR
          fact.to_exchange_id <> recommendation.source_exchange_id OR
          fact.to_asset_symbol <> 'USDT'
        )) OR
        (selected.step_index = 1 AND (
          selected.role <> 'buy_depleted_inventory' OR
          fact.from_exchange_id <> recommendation.destination_exchange_id OR
          fact.from_asset_symbol <> 'USDT' OR
          fact.to_exchange_id <> recommendation.destination_exchange_id OR
          fact.to_asset_symbol <> recommendation.destination_asset_symbol
        ))
      )
    ) THEN
      RAISE EXCEPTION 'rebalancing_natural_reverse_mismatch';
    END IF;
  ELSE
    IF EXISTS (
      SELECT 1 FROM (
        SELECT selected.step_index, selected.role,
          fact.from_exchange_id, fact.from_asset_symbol,
          fact.to_exchange_id, fact.to_asset_symbol,
          lag(fact.to_exchange_id) OVER (ORDER BY selected.step_index) AS prior_exchange,
          lag(fact.to_asset_symbol) OVER (ORDER BY selected.step_index) AS prior_asset
        FROM rebalancing_recommendation_steps selected
        JOIN rebalancing_route_facts fact
          ON fact.fact_set_id = selected.fact_set_id AND fact.fact_id = selected.fact_id
        WHERE selected.recommendation_id = target_id
      ) route
      WHERE route.role <> 'route' OR
        (route.step_index = 0 AND ROW(route.from_exchange_id, route.from_asset_symbol)
          IS DISTINCT FROM ROW(recommendation.source_exchange_id, recommendation.source_asset_symbol)) OR
        (route.step_index > 0 AND ROW(route.from_exchange_id, route.from_asset_symbol)
          IS DISTINCT FROM ROW(route.prior_exchange, route.prior_asset))
    ) OR EXISTS (
      SELECT 1
      FROM rebalancing_recommendation_steps selected
      JOIN rebalancing_route_facts fact
        ON fact.fact_set_id = selected.fact_set_id AND fact.fact_id = selected.fact_id
      WHERE selected.recommendation_id = target_id AND
        selected.step_index = step_count - 1 AND
        ROW(fact.to_exchange_id, fact.to_asset_symbol)
          IS DISTINCT FROM ROW(
            recommendation.destination_exchange_id,
            recommendation.destination_asset_symbol
          )
    ) THEN
      RAISE EXCEPTION 'rebalancing_graph_route_mismatch';
    END IF;
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER rebalancing_fact_sets_reference_guard
  BEFORE INSERT OR UPDATE ON rebalancing_fact_sets
  FOR EACH ROW EXECUTE FUNCTION enforce_rebalancing_fact_set_reference();
CREATE TRIGGER rebalancing_recommendations_reference_guard
  BEFORE INSERT OR UPDATE ON rebalancing_recommendations
  FOR EACH ROW EXECUTE FUNCTION enforce_rebalancing_recommendation_reference();

CREATE TRIGGER rebalancing_fact_sets_immutable
  BEFORE UPDATE OR DELETE ON rebalancing_fact_sets
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER rebalancing_route_facts_immutable
  BEFORE UPDATE OR DELETE ON rebalancing_route_facts
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER rebalancing_recommendations_immutable
  BEFORE UPDATE OR DELETE ON rebalancing_recommendations
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER rebalancing_recommendation_steps_immutable
  BEFORE UPDATE OR DELETE ON rebalancing_recommendation_steps
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER rebalancing_checklist_steps_immutable
  BEFORE UPDATE OR DELETE ON rebalancing_checklist_steps
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();

CREATE CONSTRAINT TRIGGER rebalancing_recommendations_complete
  AFTER INSERT ON rebalancing_recommendations DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION enforce_rebalancing_recommendation_complete();
CREATE CONSTRAINT TRIGGER rebalancing_recommendation_steps_complete
  AFTER INSERT ON rebalancing_recommendation_steps DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION enforce_rebalancing_recommendation_complete();
CREATE CONSTRAINT TRIGGER rebalancing_checklist_steps_complete
  AFTER INSERT ON rebalancing_checklist_steps DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION enforce_rebalancing_recommendation_complete();

CREATE INDEX rebalancing_route_facts_route_idx
  ON rebalancing_route_facts(fact_set_id, from_exchange_id, from_asset_symbol);
CREATE INDEX rebalancing_recommendations_recorded_idx
  ON rebalancing_recommendations(recorded_at, source_asset_symbol);
