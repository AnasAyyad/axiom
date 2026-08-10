SET TIME ZONE 'UTC';

-- A multi-leg live-shadow decision is not representable as the one-order
-- execution projection used by Trend and Mean Reversion. Preserve the exact
-- approved saga, terminal simulator result, reduction evidence, and projected
-- balances together without pretending that any leg reached a private venue.
CREATE TABLE shadow_multileg_execution_evidence (
  decision_id text PRIMARY KEY REFERENCES shadow_strategy_decision_evidence(decision_id),
  strategy_version_id text NOT NULL REFERENCES strategy_versions(id),
  candidate_id text NOT NULL CHECK (candidate_id <> ''),
  outcome text NOT NULL CHECK (outcome <> ''),
  execution_plan_hash sha256_hex NOT NULL,
  canonical_execution_plan bytea NOT NULL CHECK (octet_length(canonical_execution_plan) > 1),
  simulation_hash sha256_hex NOT NULL,
  canonical_simulation bytea NOT NULL CHECK (octet_length(canonical_simulation) > 1),
  reduction_hash sha256_hex NOT NULL,
  canonical_reduction bytea NOT NULL CHECK (octet_length(canonical_reduction) > 1),
  projected_balances_hash sha256_hex NOT NULL,
  canonical_projected_balances bytea NOT NULL CHECK (octet_length(canonical_projected_balances) > 1),
  recorded_at timestamptz NOT NULL
);

CREATE FUNCTION enforce_shadow_multileg_execution_reference() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE
  parent_strategy text;
  parent_outcome text;
BEGIN
  SELECT evidence.strategy_version_id, decision.outcome
    INTO parent_strategy, parent_outcome
    FROM public.shadow_strategy_decision_evidence evidence
    JOIN public.decisions decision ON decision.id = evidence.decision_id
    WHERE evidence.decision_id = NEW.decision_id;
  IF parent_strategy IS DISTINCT FROM NEW.strategy_version_id OR
     parent_strategy NOT IN ('triangular-v1b-1','cross-exchange-v1b-1') OR
     parent_outcome IS DISTINCT FROM 'accepted' THEN
    RAISE EXCEPTION 'shadow_multileg_execution_parent_mismatch';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER shadow_multileg_execution_reference_guard
  BEFORE INSERT OR UPDATE ON shadow_multileg_execution_evidence
  FOR EACH ROW EXECUTE FUNCTION enforce_shadow_multileg_execution_reference();

CREATE TRIGGER shadow_multileg_execution_evidence_immutable
  BEFORE UPDATE OR DELETE ON shadow_multileg_execution_evidence
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();

CREATE INDEX shadow_multileg_execution_strategy_idx
  ON shadow_multileg_execution_evidence(strategy_version_id, recorded_at);
