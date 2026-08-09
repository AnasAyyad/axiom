SET TIME ZONE 'UTC';

-- Triangular sandbox execution uses the approved BTC/USDT, ETH/BTC, and
-- ETH/USDT spot cycle.  The immutable journal must retain the exact middle
-- market instead of rejecting an exchange-confirmed fill.  Its current-state
-- projection is session-portfolio scoped and remains stored under the
-- strategy session's primary instrument, so the final transaction may belong
-- to a different market.
ALTER TABLE sandbox_accounting_transactions
  DROP CONSTRAINT sandbox_accounting_transactions_instrument_check,
  ADD CONSTRAINT sandbox_accounting_transactions_instrument_check
  CHECK (instrument IN ('BTCUSDT','ETHUSDT','ETHBTC'));

ALTER TABLE sandbox_accounting_transactions
  ADD CONSTRAINT sandbox_accounting_transaction_session_identity
  UNIQUE (id,strategy_session_id,account_id,account_epoch);

ALTER TABLE sandbox_accounting_positions
  DROP CONSTRAINT sandbox_accounting_positions_last_transaction_id_strategy__fkey,
  ADD CONSTRAINT sandbox_accounting_positions_last_transaction_session_fkey
  FOREIGN KEY (last_transaction_id,strategy_session_id,account_id,account_epoch)
  REFERENCES sandbox_accounting_transactions(
    id,strategy_session_id,account_id,account_epoch
  );

-- An exchange-confirmed fill is never discarded because valuation cannot close a
-- cross-asset lot.  Incomplete or unresolved projections are durable and
-- explicit; the central-risk reader accepts only `complete`, so another entry
-- decision still fails closed until the cycle closes or recovery reconciles it.
ALTER TABLE sandbox_accounting_positions
  DROP CONSTRAINT sandbox_accounting_positions_valuation_state_check,
  ADD CONSTRAINT sandbox_accounting_positions_valuation_state_check
  CHECK (valuation_state IN (
    'complete','unvalued_fee_asset','cross_asset_open','inventory_unresolved'
  ));
