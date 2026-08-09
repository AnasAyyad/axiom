SET TIME ZONE 'UTC';

-- Authenticated sandbox fills use the exchange account as the external source
-- of truth, but every strategy-owned asset movement must also be represented
-- by an immutable, balanced local journal.  This journal is deliberately
-- separate from historical/shadow run journals: it is bound to an account
-- epoch, automatic strategy session, durable plan, order, and native fill.
CREATE TABLE sandbox_accounting_transactions (
  id text PRIMARY KEY CHECK (char_length(id) BETWEEN 1 AND 128),
  strategy_session_id text NOT NULL REFERENCES sandbox_strategy_sessions(id),
  plan_id text NOT NULL REFERENCES v1c_submission_plans(id),
  transaction_type text NOT NULL CHECK (transaction_type='fill'),
  source_mode text NOT NULL CHECK (source_mode='exchange_sandbox'),
  account_id text NOT NULL,
  account_epoch bigint NOT NULL CHECK (account_epoch > 0),
  exchange text NOT NULL CHECK (exchange IN ('binance','bybit')),
  environment text NOT NULL CHECK (environment IN ('spot_testnet','demo')),
  configuration_id text NOT NULL REFERENCES configuration_versions(id),
  policy_hash sha256_hex NOT NULL,
  order_id text NOT NULL,
  client_order_id text NOT NULL CHECK (char_length(client_order_id) BETWEEN 1 AND 128),
  intent_action text NOT NULL CHECK (intent_action IN ('ENTRY','EXIT','RECOVERY')),
  native_fill_id_hash sha256_hex NOT NULL,
  fill_id text NOT NULL CHECK (char_length(fill_id) BETWEEN 1 AND 128),
  instrument text NOT NULL CHECK (instrument IN ('BTCUSDT','ETHUSDT')),
  side text NOT NULL CHECK (side IN ('buy','sell')),
  quantity financial_amount NOT NULL CHECK (quantity > 0),
  price financial_amount NOT NULL CHECK (price > 0),
  notional financial_amount NOT NULL CHECK (notional > 0),
  fee financial_amount NOT NULL CHECK (fee >= 0),
  rebate financial_amount NOT NULL CHECK (rebate >= 0),
  fee_asset text REFERENCES assets(symbol),
  fill_ordinal bigint NOT NULL CHECK (fill_ordinal > 0),
  occurred_at timestamptz NOT NULL,
  recorded_at timestamptz NOT NULL CHECK (recorded_at >= occurred_at),
  evidence_hash sha256_hex NOT NULL,
  sealed boolean NOT NULL DEFAULT false,
  FOREIGN KEY (strategy_session_id,account_id)
    REFERENCES sandbox_strategy_session_accounts(strategy_session_id,account_id),
  FOREIGN KEY (account_id,account_epoch,native_fill_id_hash)
    REFERENCES v1c_exchange_fills(account_id,account_epoch,native_fill_id_hash),
  UNIQUE (account_id,account_epoch,fill_id),
  CHECK (
    (exchange='binance' AND environment='spot_testnet') OR
    (exchange='bybit' AND environment='demo')
  ),
  CHECK ((fee=0 AND rebate=0) OR fee_asset IS NOT NULL)
);

CREATE TABLE sandbox_accounting_entries (
  transaction_id text NOT NULL REFERENCES sandbox_accounting_transactions(id),
  line_number integer NOT NULL CHECK (line_number > 0),
  account_class text NOT NULL CHECK (account_class IN (
    'external_equity','available_asset','reserved_asset',
    'exchange_inventory','strategy_inventory','strategy_cash','trade_cost_proceeds',
    'fee_expense','realized_pnl','unrealized_pnl','inventory_valuation',
    'spread_slippage_latency','rebalancing_expense','recovery_loss',
    'rounding_dust','reconciliation_suspense'
  )),
  account_owner text NOT NULL,
  asset_symbol text NOT NULL REFERENCES assets(symbol),
  direction text NOT NULL CHECK (direction IN ('debit','credit')),
  quantity financial_amount NOT NULL CHECK (quantity > 0),
  functional_value signed_financial_amount,
  lot_reference text,
  rounding_metadata text,
  PRIMARY KEY (transaction_id,line_number)
);

CREATE FUNCTION reject_sealed_sandbox_accounting_entry() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM sandbox_accounting_transactions
    WHERE id=NEW.transaction_id AND sealed
  ) THEN
    RAISE EXCEPTION 'sealed_sandbox_accounting_transaction';
  END IF;
  RETURN NEW;
END;
$$;

CREATE FUNCTION enforce_sandbox_accounting_balance() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE unbalanced integer;
BEGIN
  SELECT count(*) INTO unbalanced FROM (
    SELECT asset_symbol
    FROM sandbox_accounting_entries
    WHERE transaction_id=NEW.id
    GROUP BY asset_symbol
    HAVING sum(CASE direction WHEN 'debit' THEN quantity ELSE -quantity END)<>0
  ) differences;
  IF unbalanced<>0 OR NOT EXISTS (
    SELECT 1 FROM sandbox_accounting_entries WHERE transaction_id=NEW.id
  ) THEN
    RAISE EXCEPTION 'unbalanced_sandbox_accounting_transaction';
  END IF;
  UPDATE sandbox_accounting_transactions
  SET sealed=true WHERE id=NEW.id AND NOT sealed;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'sandbox_accounting_seal_failed';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER sandbox_accounting_transactions_immutable
  BEFORE UPDATE OR DELETE ON sandbox_accounting_transactions
  FOR EACH ROW EXECUTE FUNCTION protect_journal_transaction();

CREATE TRIGGER sandbox_accounting_entries_immutable
  BEFORE UPDATE OR DELETE ON sandbox_accounting_entries
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();

CREATE TRIGGER sandbox_accounting_entries_require_open_transaction
  BEFORE INSERT ON sandbox_accounting_entries
  FOR EACH ROW EXECUTE FUNCTION reject_sealed_sandbox_accounting_entry();

CREATE CONSTRAINT TRIGGER sandbox_accounting_balanced_on_commit
  AFTER INSERT ON sandbox_accounting_transactions
  DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION enforce_sandbox_accounting_balance();

CREATE INDEX sandbox_accounting_session_timeline_idx
  ON sandbox_accounting_transactions(
    strategy_session_id,account_id,account_epoch,occurred_at,id
  );
