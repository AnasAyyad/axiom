SET TIME ZONE 'UTC';

-- Live shadow must retain the exact semantic strategy input and decision even
-- when an older strategy-specific qualification table describes a different
-- evidence scope. In particular, a selected-venue Mean Reversion shadow must
-- not fabricate a two-exchange B2 coherent view merely to satisfy the
-- historical mean_reversion_decisions contract.
CREATE TABLE shadow_strategy_decision_evidence (
  decision_id text PRIMARY KEY REFERENCES decisions(id),
  strategy_version_id text NOT NULL REFERENCES strategy_versions(id),
  input_kind text NOT NULL CHECK (input_kind IN (
    'trend_input','mean_reversion_input','triangular_input','cross_exchange_input'
  )),
  input_hash sha256_hex NOT NULL,
  canonical_input bytea NOT NULL,
  decision_hash sha256_hex NOT NULL,
  canonical_decision bytea NOT NULL,
  correlation_id text NOT NULL,
  causation_id text NOT NULL,
  recorded_at timestamptz NOT NULL
);

CREATE FUNCTION enforce_shadow_strategy_decision_reference() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE
  parent_strategy text;
  parent_causation text;
BEGIN
  SELECT strategy_version_id, causation_id
    INTO parent_strategy, parent_causation
    FROM decisions WHERE id = NEW.decision_id;
  IF parent_strategy IS DISTINCT FROM NEW.strategy_version_id OR
     parent_causation IS DISTINCT FROM NEW.causation_id THEN
    RAISE EXCEPTION 'shadow_strategy_decision_parent_mismatch';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER shadow_strategy_decision_reference_guard
  BEFORE INSERT OR UPDATE ON shadow_strategy_decision_evidence
  FOR EACH ROW EXECUTE FUNCTION enforce_shadow_strategy_decision_reference();

CREATE TRIGGER shadow_strategy_decision_evidence_immutable
  BEFORE UPDATE OR DELETE ON shadow_strategy_decision_evidence
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();

CREATE INDEX shadow_strategy_decision_evidence_strategy_idx
  ON shadow_strategy_decision_evidence(strategy_version_id, recorded_at);
