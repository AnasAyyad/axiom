SET TIME ZONE 'UTC';

-- Preserve every complete automatic strategy evaluation, including the
-- no-order decisions needed to reproduce trailing stops, holding duration,
-- and cooldowns. Accepted decisions are written in the same transaction as
-- their durable plan; rejected or blocked-at-allocation decisions retain a
-- NULL plan reference. This table contains public canonical strategy facts
-- only and is immutable once recorded.
CREATE TABLE sandbox_strategy_decisions (
  id text PRIMARY KEY,
  strategy_session_id text NOT NULL REFERENCES sandbox_strategy_sessions(id),
  plan_id text REFERENCES v1c_submission_plans(id),
  account_id text NOT NULL,
  account_epoch bigint NOT NULL CHECK (account_epoch > 0),
  strategy_revision bigint NOT NULL CHECK (strategy_revision > 0),
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
  occurred_at timestamptz NOT NULL,
  UNIQUE (strategy_session_id,account_id,account_epoch,decision_id),
  UNIQUE (strategy_session_id,account_id,account_epoch,event_ordinal)
);

CREATE UNIQUE INDEX sandbox_strategy_decisions_plan_unique
  ON sandbox_strategy_decisions(plan_id) WHERE plan_id IS NOT NULL;

CREATE INDEX sandbox_strategy_decisions_projection_lookup
  ON sandbox_strategy_decisions(
    strategy_session_id,account_id,account_epoch,event_ordinal,occurred_at
  );

CREATE TRIGGER sandbox_strategy_decisions_immutable
  BEFORE UPDATE OR DELETE ON sandbox_strategy_decisions
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
