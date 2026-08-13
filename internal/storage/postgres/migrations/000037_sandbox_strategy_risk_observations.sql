SET TIME ZONE 'UTC';

-- Complete central-risk inputs for one automatic strategy evaluation. These
-- rows contain only redacted measurements and immutable hashes; credentials,
-- private provider payloads, and arbitrary logs are forbidden by shape.
CREATE TABLE sandbox_strategy_risk_observations (
  id text PRIMARY KEY,
  strategy_session_id text NOT NULL REFERENCES sandbox_strategy_sessions(id),
  account_id text NOT NULL,
  account_epoch bigint NOT NULL CHECK (account_epoch > 0),
  strategy_revision bigint NOT NULL CHECK (strategy_revision > 0),
  instrument text NOT NULL CHECK (instrument IN ('BTCUSDT','ETHUSDT')),
  snapshot_hash sha256_hex NOT NULL,
  market_hash sha256_hex NOT NULL,
  policy_id text NOT NULL,
  policy_version bigint NOT NULL CHECK (policy_version > 0),
  policy_hash sha256_hex NOT NULL,
  account_drawdown financial_amount NOT NULL,
  utc_day_loss financial_amount NOT NULL,
  rolling_24_hour_loss financial_amount NOT NULL,
  strategy_loss financial_amount NOT NULL,
  asset_exposure financial_amount NOT NULL,
  combined_exposure financial_amount NOT NULL,
  exchange_exposure financial_amount NOT NULL,
  reserve financial_amount NOT NULL,
  reserved_capital financial_amount NOT NULL,
  spread financial_amount NOT NULL,
  slippage financial_amount NOT NULL,
  open_orders integer NOT NULL CHECK (open_orders >= 0),
  book_age_nanoseconds bigint NOT NULL CHECK (book_age_nanoseconds >= 0),
  queue_lag_nanoseconds bigint NOT NULL CHECK (queue_lag_nanoseconds >= 0),
  clock_drift_nanoseconds bigint NOT NULL,
  quality_score integer NOT NULL CHECK (quality_score BETWEEN 0 AND 100),
  gap boolean NOT NULL,
  stale_data boolean NOT NULL,
  reconciliation_fault boolean NOT NULL,
  accounting_fault boolean NOT NULL,
  unknown_order boolean NOT NULL,
  persistence_fault boolean NOT NULL,
  disk_fault boolean NOT NULL,
  api_error boolean NOT NULL,
  lease_lost boolean NOT NULL,
  observed_at timestamptz NOT NULL,
  recorded_at timestamptz NOT NULL CHECK (recorded_at >= observed_at),
  evidence_hash sha256_hex NOT NULL UNIQUE,
  UNIQUE (
    strategy_session_id,account_id,account_epoch,strategy_revision,instrument,
    snapshot_hash,market_hash,policy_id,policy_version
  ),
  FOREIGN KEY (strategy_session_id,account_id)
    REFERENCES sandbox_strategy_session_accounts(strategy_session_id,account_id),
  FOREIGN KEY (account_id,account_epoch,snapshot_hash)
    REFERENCES v1c_account_snapshots(account_id,account_epoch,snapshot_hash),
  FOREIGN KEY (policy_id,policy_version)
    REFERENCES risk_policies(id,version)
);

CREATE FUNCTION enforce_sandbox_strategy_risk_observation_references()
RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE
  stored_policy_hash text;
  stored_risk_state text;
  snapshot_observed_at timestamptz;
  session_matches boolean;
BEGIN
  SELECT policy_hash INTO stored_policy_hash
    FROM risk_policies
    WHERE id=NEW.policy_id AND version=NEW.policy_version
      AND scope_kind='global' AND scope_id='platform'
      AND state='NORMAL' AND effective_at<=NEW.observed_at
      AND NOT EXISTS(
        SELECT 1 FROM risk_policies newer
        WHERE newer.scope_kind='global' AND newer.scope_id='platform'
          AND newer.state='NORMAL' AND newer.effective_at<=NEW.observed_at
          AND newer.version>NEW.policy_version
      );
  IF stored_policy_hash IS DISTINCT FROM NEW.policy_hash THEN
    RAISE EXCEPTION 'sandbox_strategy_risk_policy_mismatch';
  END IF;

  SELECT coalesce(
    (SELECT next_state FROM risk_state_events ORDER BY entity_revision DESC LIMIT 1),
    'PAUSED'
  ) INTO stored_risk_state;
  IF stored_risk_state<>'NORMAL' THEN
    RAISE EXCEPTION 'sandbox_strategy_risk_state_not_normal';
  END IF;

  SELECT observed_at INTO snapshot_observed_at
    FROM v1c_account_snapshots
    WHERE account_id=NEW.account_id
      AND account_epoch=NEW.account_epoch
      AND snapshot_hash=NEW.snapshot_hash;
  IF snapshot_observed_at IS NULL OR snapshot_observed_at > NEW.observed_at OR
     NEW.observed_at-snapshot_observed_at > interval '250 milliseconds' THEN
    RAISE EXCEPTION 'sandbox_strategy_risk_snapshot_stale';
  END IF;

  SELECT EXISTS(
    SELECT 1
    FROM sandbox_strategy_sessions strategy_session
    JOIN sandbox_strategy_session_accounts membership
      ON membership.strategy_session_id=strategy_session.id
    WHERE strategy_session.id=NEW.strategy_session_id
      AND membership.account_id=NEW.account_id
      AND membership.account_epoch=NEW.account_epoch
      AND strategy_session.revision=NEW.strategy_revision
      AND strategy_session.instrument=NEW.instrument
      AND strategy_session.state='running'
  ) INTO session_matches;
  IF NOT session_matches THEN
    RAISE EXCEPTION 'sandbox_strategy_risk_session_mismatch';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER sandbox_strategy_risk_observations_reference_guard
  BEFORE INSERT ON sandbox_strategy_risk_observations
  FOR EACH ROW EXECUTE FUNCTION enforce_sandbox_strategy_risk_observation_references();

CREATE TRIGGER sandbox_strategy_risk_observations_immutable
  BEFORE UPDATE OR DELETE ON sandbox_strategy_risk_observations
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();

CREATE INDEX sandbox_strategy_risk_observations_lookup
  ON sandbox_strategy_risk_observations(
    strategy_session_id,account_id,account_epoch,observed_at DESC,id
  );
