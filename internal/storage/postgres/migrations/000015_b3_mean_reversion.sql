SET TIME ZONE 'UTC';

ALTER TABLE model_versions DROP CONSTRAINT model_versions_model_type_check;
ALTER TABLE model_versions ADD CONSTRAINT model_versions_model_type_check CHECK (model_type IN (
  'fee','latency','spread','slippage','fill','cost_basis','impact',
  'adverse_selection','maker_queue','gap','correlation'
));

ALTER TABLE strategy_parameters
  ADD COLUMN evaluation_timezone text,
  ADD COLUMN change_behavior text,
  ADD COLUMN approval_actor text,
  ADD COLUMN approval_reference text,
  ADD COLUMN approved_at timestamptz,
  ADD COLUMN change_reason text;

CREATE TABLE mean_reversion_decisions (
  decision_id text PRIMARY KEY REFERENCES decisions(id),
  strategy_version_id text NOT NULL REFERENCES strategy_versions(id),
  configuration_id text NOT NULL REFERENCES configuration_versions(id),
  explanation_hash sha256_hex NOT NULL,
  canonical_explanation bytea NOT NULL,
  primary_candle_view_id text NOT NULL,
  primary_candle_view_revision bigint NOT NULL CHECK (primary_candle_view_revision > 0),
  higher_candle_view_id text NOT NULL,
  higher_candle_view_revision bigint NOT NULL CHECK (higher_candle_view_revision > 0),
  market_view_id text NOT NULL,
  market_view_revision bigint NOT NULL CHECK (market_view_revision > 0),
  coherent_view_id sha256_hex NOT NULL REFERENCES cross_market_view_headers(id),
  coherent_version_vector_hash sha256_hex NOT NULL,
  portfolio_ownership_account_id text NOT NULL REFERENCES portfolio_ownership(account_id),
  instrument_metadata_id text NOT NULL REFERENCES instrument_metadata_versions(id),
  asset_eligibility_version bigint NOT NULL CHECK (asset_eligibility_version > 0),
  portfolio_revision bigint NOT NULL CHECK (portfolio_revision > 0),
  position_revision bigint NOT NULL CHECK (position_revision > 0),
  risk_policy_id text NOT NULL,
  risk_policy_version bigint NOT NULL CHECK (risk_policy_version > 0),
  risk_policy_hash sha256_hex NOT NULL,
  fee_model_id text NOT NULL REFERENCES model_versions(id),
  latency_model_id text NOT NULL REFERENCES model_versions(id),
  fill_model_id text NOT NULL REFERENCES model_versions(id),
  slippage_model_id text NOT NULL REFERENCES model_versions(id),
  gap_model_id text NOT NULL REFERENCES model_versions(id),
  correlation_model_id text NOT NULL REFERENCES model_versions(id),
  correlation_id text NOT NULL,
  causation_id text NOT NULL,
  recorded_at timestamptz NOT NULL,
  CHECK (coherent_view_id = coherent_version_vector_hash),
  FOREIGN KEY (risk_policy_id, risk_policy_version) REFERENCES risk_policies(id, version)
);

CREATE FUNCTION enforce_mean_reversion_decision_references() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE
  parent_strategy text;
  parent_configuration text;
  parent_scope text;
  parent_coherent_view text;
  owner_strategy_version text;
  owner_strategy_key text;
  strategy_family text;
  stored_risk_hash text;
BEGIN
  SELECT strategy_version_id, configuration_id, decision_market_scope, cross_market_view_id
    INTO parent_strategy, parent_configuration, parent_scope, parent_coherent_view
    FROM decisions WHERE id = NEW.decision_id;
  IF parent_strategy IS DISTINCT FROM NEW.strategy_version_id OR
     parent_configuration IS DISTINCT FROM NEW.configuration_id OR
     parent_scope IS DISTINCT FROM 'cross_market' OR
     parent_coherent_view IS DISTINCT FROM NEW.coherent_view_id THEN
    RAISE EXCEPTION 'mean_reversion_decision_parent_mismatch';
  END IF;
  SELECT ownership.strategy_version_id, ownership.strategy_key
    INTO owner_strategy_version, owner_strategy_key
    FROM portfolio_ownership ownership
    WHERE ownership.account_id = NEW.portfolio_ownership_account_id;
  SELECT definition.family INTO strategy_family
    FROM strategy_versions version
    JOIN strategy_definitions definition ON definition.id = version.strategy_id
    WHERE version.id = NEW.strategy_version_id;
  IF owner_strategy_version IS DISTINCT FROM NEW.strategy_version_id OR
     owner_strategy_key IS DISTINCT FROM 'mean_reversion' OR
     strategy_family IS DISTINCT FROM 'mean_reversion' THEN
    RAISE EXCEPTION 'mean_reversion_ownership_strategy_mismatch';
  END IF;
  SELECT policy_hash INTO stored_risk_hash FROM risk_policies
    WHERE id = NEW.risk_policy_id AND version = NEW.risk_policy_version;
  IF stored_risk_hash IS DISTINCT FROM NEW.risk_policy_hash THEN
    RAISE EXCEPTION 'mean_reversion_risk_policy_mismatch';
  END IF;
  IF (SELECT model_type FROM model_versions WHERE id = NEW.fee_model_id) IS DISTINCT FROM 'fee' OR
     (SELECT model_type FROM model_versions WHERE id = NEW.latency_model_id) IS DISTINCT FROM 'latency' OR
     (SELECT model_type FROM model_versions WHERE id = NEW.fill_model_id) IS DISTINCT FROM 'fill' OR
     (SELECT model_type FROM model_versions WHERE id = NEW.slippage_model_id) IS DISTINCT FROM 'slippage' OR
     (SELECT model_type FROM model_versions WHERE id = NEW.gap_model_id) IS DISTINCT FROM 'gap' OR
     (SELECT model_type FROM model_versions WHERE id = NEW.correlation_model_id) IS DISTINCT FROM 'correlation' THEN
    RAISE EXCEPTION 'mean_reversion_model_type_mismatch';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER mean_reversion_decisions_reference_guard
  BEFORE INSERT OR UPDATE ON mean_reversion_decisions
  FOR EACH ROW EXECUTE FUNCTION enforce_mean_reversion_decision_references();
CREATE TRIGGER mean_reversion_decisions_immutable
  BEFORE UPDATE OR DELETE ON mean_reversion_decisions
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();

CREATE INDEX mean_reversion_decisions_coherent_view_idx
  ON mean_reversion_decisions(coherent_view_id, recorded_at);
CREATE INDEX mean_reversion_decisions_strategy_configuration_idx
  ON mean_reversion_decisions(strategy_version_id, configuration_id, recorded_at);
