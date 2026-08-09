SET TIME ZONE 'UTC';

-- Rebuildable current-state projections for the immutable sandbox journal.
-- The journal remains authoritative. These rows are transactionally replaced
-- from the complete ordered fill history and carry a hash that can be checked
-- by recovery, reconciliation, risk, and owner-facing reads.
ALTER TABLE sandbox_accounting_transactions
  ADD CONSTRAINT sandbox_accounting_transaction_projection_identity
  UNIQUE (id,strategy_session_id,account_id,account_epoch,instrument);

CREATE TABLE sandbox_accounting_positions (
  strategy_session_id text NOT NULL,
  account_id text NOT NULL,
  account_epoch bigint NOT NULL CHECK (account_epoch > 0),
  instrument text NOT NULL CHECK (instrument IN ('BTCUSDT','ETHUSDT')),
  base_asset text NOT NULL REFERENCES assets(symbol),
  quote_asset text NOT NULL REFERENCES assets(symbol),
  quantity financial_amount NOT NULL CHECK (quantity >= 0),
  total_cost financial_amount NOT NULL CHECK (total_cost >= 0),
  weighted_average_cost financial_amount NOT NULL CHECK (weighted_average_cost >= 0),
  realized_pnl signed_financial_amount NOT NULL,
  valuation_state text NOT NULL CHECK (valuation_state IN (
    'complete','unvalued_fee_asset'
  )),
  source_transaction_count bigint NOT NULL CHECK (source_transaction_count > 0),
  last_transaction_id text NOT NULL,
  last_occurred_at timestamptz NOT NULL,
  projection_hash sha256_hex NOT NULL,
  revision bigint NOT NULL CHECK (revision > 0),
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (strategy_session_id,account_id,account_epoch,instrument),
  FOREIGN KEY (strategy_session_id,account_id)
    REFERENCES sandbox_strategy_session_accounts(strategy_session_id,account_id),
  FOREIGN KEY (
    last_transaction_id,strategy_session_id,account_id,account_epoch,instrument
  ) REFERENCES sandbox_accounting_transactions(
    id,strategy_session_id,account_id,account_epoch,instrument
  ),
  CHECK (
    (instrument='BTCUSDT' AND base_asset='BTC' AND quote_asset='USDT') OR
    (instrument='ETHUSDT' AND base_asset='ETH' AND quote_asset='USDT')
  ),
  CHECK (
    (quantity=0 AND total_cost=0 AND weighted_average_cost=0) OR
    (quantity>0 AND total_cost>0 AND weighted_average_cost>0)
  ),
  CHECK (updated_at >= last_occurred_at)
);

CREATE TABLE sandbox_accounting_position_fees (
  strategy_session_id text NOT NULL,
  account_id text NOT NULL,
  account_epoch bigint NOT NULL CHECK (account_epoch > 0),
  instrument text NOT NULL CHECK (instrument IN ('BTCUSDT','ETHUSDT')),
  asset_symbol text NOT NULL REFERENCES assets(symbol),
  fee_quantity financial_amount NOT NULL CHECK (fee_quantity >= 0),
  rebate_quantity financial_amount NOT NULL CHECK (rebate_quantity >= 0),
  revision bigint NOT NULL CHECK (revision > 0),
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (
    strategy_session_id,account_id,account_epoch,instrument,asset_symbol
  ),
  FOREIGN KEY (strategy_session_id,account_id,account_epoch,instrument)
    REFERENCES sandbox_accounting_positions(
      strategy_session_id,account_id,account_epoch,instrument
    ) ON DELETE CASCADE
);

CREATE INDEX sandbox_accounting_positions_account_idx
  ON sandbox_accounting_positions(account_id,account_epoch,instrument);
