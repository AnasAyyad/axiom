SET TIME ZONE 'UTC';

-- Immutable exact valuations behind automatic sandbox central-risk inputs.
-- A baseline row deliberately has no risk observation: the next evaluation
-- can measure loss and drawdown against durable history instead of assuming
-- that missing history means zero risk.
ALTER TABLE sandbox_accounting_positions
  ADD CONSTRAINT sandbox_accounting_position_risk_identity
  UNIQUE (
    strategy_session_id,account_id,account_epoch,instrument,projection_hash
  );

CREATE TABLE sandbox_strategy_risk_valuations (
  id text PRIMARY KEY,
  purpose text NOT NULL CHECK (purpose IN ('baseline','evaluated')),
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
  accounting_state text NOT NULL CHECK (accounting_state IN ('no_fills','complete')),
  accounting_evidence_hash sha256_hex NOT NULL,
  accounting_projection_hash sha256_hex,
  mark_price financial_amount NOT NULL CHECK (mark_price > 0),
  account_equity financial_amount NOT NULL CHECK (account_equity > 0),
  volatile_asset_value financial_amount NOT NULL CHECK (volatile_asset_value >= 0),
  combined_volatile_value financial_amount NOT NULL CHECK (combined_volatile_value >= 0),
  committed_buy_value financial_amount NOT NULL CHECK (committed_buy_value >= 0),
  exchange_risk_value financial_amount NOT NULL CHECK (exchange_risk_value >= 0),
  reserve_value financial_amount NOT NULL CHECK (reserve_value >= 0),
  reserved_value financial_amount NOT NULL CHECK (reserved_value >= 0),
  strategy_position_quantity financial_amount NOT NULL CHECK (strategy_position_quantity >= 0),
  strategy_position_value financial_amount NOT NULL CHECK (strategy_position_value >= 0),
  strategy_total_cost financial_amount NOT NULL CHECK (strategy_total_cost >= 0),
  strategy_realized_pnl signed_financial_amount NOT NULL,
  strategy_unrealized_pnl signed_financial_amount NOT NULL,
  strategy_total_pnl signed_financial_amount NOT NULL,
  account_peak_equity financial_amount NOT NULL CHECK (account_peak_equity > 0),
  utc_day_baseline_equity financial_amount NOT NULL CHECK (utc_day_baseline_equity > 0),
  rolling_24_hour_baseline_equity financial_amount NOT NULL CHECK (rolling_24_hour_baseline_equity > 0),
  strategy_peak_pnl signed_financial_amount NOT NULL,
  open_orders integer NOT NULL CHECK (open_orders >= 0),
  slippage financial_amount NOT NULL CHECK (slippage >= 0),
  reconciliation_id text NOT NULL REFERENCES v1c_reconciliations(id),
  reconciliation_hash sha256_hex NOT NULL,
  storage_revision bigint NOT NULL CHECK (storage_revision > 0),
  storage_observed_at timestamptz NOT NULL,
  engine_startup_cycle bigint NOT NULL CHECK (engine_startup_cycle > 0),
  admission_hash sha256_hex NOT NULL,
  risk_observation_id text UNIQUE REFERENCES sandbox_strategy_risk_observations(id),
  observed_at timestamptz NOT NULL,
  recorded_at timestamptz NOT NULL CHECK (recorded_at >= observed_at),
  evidence_hash sha256_hex NOT NULL UNIQUE,
  FOREIGN KEY (strategy_session_id,account_id)
    REFERENCES sandbox_strategy_session_accounts(strategy_session_id,account_id),
  FOREIGN KEY (account_id,account_epoch,snapshot_hash)
    REFERENCES v1c_account_snapshots(account_id,account_epoch,snapshot_hash),
  FOREIGN KEY (policy_id,policy_version)
    REFERENCES risk_policies(id,version),
  FOREIGN KEY (
    strategy_session_id,account_id,account_epoch,instrument,
    accounting_projection_hash
  ) REFERENCES sandbox_accounting_positions(
    strategy_session_id,account_id,account_epoch,instrument,projection_hash
  ),
  UNIQUE (
    purpose,strategy_session_id,account_id,account_epoch,strategy_revision,
    snapshot_hash,market_hash,policy_id,policy_version
  ),
  CHECK (
    (accounting_state='no_fills' AND accounting_projection_hash IS NULL) OR
    (accounting_state='complete' AND
      accounting_projection_hash=accounting_evidence_hash)
  ),
  CHECK (
    (purpose='baseline' AND risk_observation_id IS NULL) OR
    (purpose='evaluated' AND risk_observation_id IS NOT NULL)
  ),
  CHECK (combined_volatile_value >= volatile_asset_value),
  CHECK (exchange_risk_value=combined_volatile_value+committed_buy_value),
  CHECK (account_peak_equity >= account_equity),
  CHECK (storage_observed_at <= observed_at)
);

CREATE FUNCTION enforce_sandbox_strategy_risk_valuation_references()
RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
BEGIN
  IF NEW.accounting_state='no_fills' AND EXISTS(
    SELECT 1 FROM sandbox_accounting_transactions accounting_transaction
    WHERE accounting_transaction.strategy_session_id=NEW.strategy_session_id
      AND accounting_transaction.account_id=NEW.account_id
      AND accounting_transaction.account_epoch=NEW.account_epoch
      AND accounting_transaction.instrument=NEW.instrument
  ) THEN
    RAISE EXCEPTION 'sandbox_strategy_risk_accounting_projection_missing';
  END IF;

  IF NOT EXISTS(
    SELECT 1 FROM v1c_reconciliations reconciliation
    WHERE reconciliation.id=NEW.reconciliation_id
      AND reconciliation.account_id=NEW.account_id
      AND reconciliation.account_epoch=NEW.account_epoch
      AND reconciliation.state='clean'
      AND reconciliation.evidence_hash=NEW.reconciliation_hash
      AND reconciliation.reconciled_at<=NEW.observed_at
      AND reconciliation.reconciled_at>NEW.observed_at-interval '60 seconds'
  ) THEN
    RAISE EXCEPTION 'sandbox_strategy_risk_reconciliation_stale';
  END IF;

  IF NOT EXISTS(
    SELECT 1 FROM v1d_storage_pressure_state pressure
    WHERE pressure.scope_id='market-data'
      AND pressure.level='NORMAL'
      AND pressure.revision=NEW.storage_revision
      AND pressure.observed_at=NEW.storage_observed_at
      AND pressure.observed_at<=NEW.observed_at
      AND pressure.observed_at>NEW.observed_at-interval '30 seconds'
  ) THEN
    RAISE EXCEPTION 'sandbox_strategy_risk_storage_stale';
  END IF;

  IF NEW.purpose='evaluated' AND NOT EXISTS(
    SELECT 1 FROM sandbox_strategy_risk_observations observation
    WHERE observation.id=NEW.risk_observation_id
      AND observation.strategy_session_id=NEW.strategy_session_id
      AND observation.account_id=NEW.account_id
      AND observation.account_epoch=NEW.account_epoch
      AND observation.strategy_revision=NEW.strategy_revision
      AND observation.instrument=NEW.instrument
      AND observation.snapshot_hash=NEW.snapshot_hash
      AND observation.market_hash=NEW.market_hash
      AND observation.policy_id=NEW.policy_id
      AND observation.policy_version=NEW.policy_version
      AND observation.policy_hash=NEW.policy_hash
      AND observation.observed_at=NEW.observed_at
  ) THEN
    RAISE EXCEPTION 'sandbox_strategy_risk_observation_mismatch';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER sandbox_strategy_risk_valuations_reference_guard
  BEFORE INSERT ON sandbox_strategy_risk_valuations
  FOR EACH ROW EXECUTE FUNCTION enforce_sandbox_strategy_risk_valuation_references();

CREATE TRIGGER sandbox_strategy_risk_valuations_immutable
  BEFORE UPDATE OR DELETE ON sandbox_strategy_risk_valuations
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();

CREATE INDEX sandbox_strategy_risk_valuations_account_history
  ON sandbox_strategy_risk_valuations(account_id,account_epoch,observed_at,id);

CREATE INDEX sandbox_strategy_risk_valuations_strategy_history
  ON sandbox_strategy_risk_valuations(
    strategy_session_id,account_id,account_epoch,observed_at,id
  );
