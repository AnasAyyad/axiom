SET TIME ZONE 'UTC';

-- Preserve the exact public decision input and accepted strategy decision
-- which caused an automatic one-leg plan. These are needed to reconstruct
-- strategy-owned protective state after private fills; account-wide balance
-- snapshots cannot supply that provenance.
CREATE TABLE v1c_strategy_plan_decisions (
  plan_id text PRIMARY KEY REFERENCES v1c_submission_plans(id),
  sandbox_session_id text NOT NULL REFERENCES v1c_sandbox_sessions(id),
  account_id text NOT NULL,
  account_epoch bigint NOT NULL CHECK (account_epoch > 0),
  strategy text NOT NULL CHECK (strategy IN ('trend','mean-reversion')),
  instrument text NOT NULL CHECK (instrument IN ('BTCUSDT','ETHUSDT')),
  decision_id text NOT NULL,
  event_ordinal bigint NOT NULL CHECK (event_ordinal > 0),
  event_logical_time bigint NOT NULL CHECK (event_logical_time > 0),
  input_hash sha256_hex NOT NULL,
  decision_hash sha256_hex NOT NULL,
  canonical_input bytea NOT NULL CHECK (
    jsonb_typeof(convert_from(canonical_input,'UTF8')::jsonb) IS NOT NULL
  ),
  canonical_decision bytea NOT NULL CHECK (
    jsonb_typeof(convert_from(canonical_decision,'UTF8')::jsonb) IS NOT NULL
  ),
  UNIQUE (sandbox_session_id,account_id,account_epoch,decision_id)
);

CREATE INDEX v1c_strategy_plan_decisions_session_lookup
  ON v1c_strategy_plan_decisions(sandbox_session_id,account_id,account_epoch,strategy,instrument,event_ordinal);

CREATE TRIGGER v1c_strategy_plan_decisions_immutable
  BEFORE UPDATE OR DELETE ON v1c_strategy_plan_decisions
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
