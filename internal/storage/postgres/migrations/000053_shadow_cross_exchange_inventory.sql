SET TIME ZONE 'UTC';

-- A paired public-shadow run owns one isolated virtual account per venue.
-- Initialization is based on exact coherent public books and is immutable; it
-- grants no private exchange or transfer capability.
CREATE TABLE shadow_cross_exchange_inventory_initializations (
  session_id text NOT NULL REFERENCES shadow_sessions(id),
  exchange_id text NOT NULL REFERENCES exchanges(id),
  account_id text NOT NULL UNIQUE REFERENCES virtual_accounts(id),
  instrument_id text NOT NULL REFERENCES instruments(id),
  base_asset text NOT NULL REFERENCES assets(symbol),
  venue_capital financial_amount NOT NULL CHECK (venue_capital > 0),
  target_base_value financial_amount NOT NULL CHECK (target_base_value > 0),
  reference_price financial_amount NOT NULL CHECK (reference_price > 0),
  base_quantity financial_amount NOT NULL CHECK (base_quantity > 0),
  available_usdt financial_amount NOT NULL CHECK (available_usdt > 0),
  model_version text NOT NULL CHECK (model_version = 'cross-exchange-single-instrument-prefund.v1'),
  unselected_asset_rule text NOT NULL CHECK (
    unselected_asset_rule = 'retain_unselected_volatile_allocation_as_usdt'
  ),
  initialization_transaction_id text NOT NULL UNIQUE REFERENCES journal_transactions(id),
  canonical_hash sha256_hex NOT NULL UNIQUE,
  canonical_payload bytea NOT NULL CHECK (octet_length(canonical_payload) > 1),
  initialized_at timestamptz NOT NULL,
  PRIMARY KEY (session_id, exchange_id),
  CHECK (exchange_id IN ('binance','bybit')),
  CHECK ((base_asset = 'BTC' AND target_base_value = venue_capital * 0.25) OR
         (base_asset = 'ETH' AND target_base_value = venue_capital * 0.15))
);

CREATE FUNCTION enforce_shadow_cross_exchange_inventory_reference() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE
  session_strategy text;
  session_portfolio text;
  account_portfolio text;
  account_run text;
  instrument_base text;
  instrument_quote text;
BEGIN
  SELECT strategy_version_id,portfolio_id INTO session_strategy,session_portfolio
    FROM public.shadow_sessions WHERE id=NEW.session_id;
  SELECT portfolio_id,run_id INTO account_portfolio,account_run
    FROM public.virtual_accounts WHERE id=NEW.account_id;
  SELECT base_asset,quote_asset INTO instrument_base,instrument_quote
    FROM public.instruments WHERE id=NEW.instrument_id;
  IF session_strategy IS DISTINCT FROM 'cross-exchange-v1b-1' OR
     account_portfolio IS DISTINCT FROM session_portfolio OR
     account_run IS DISTINCT FROM NEW.session_id OR
     instrument_base IS DISTINCT FROM NEW.base_asset OR instrument_quote IS DISTINCT FROM 'USDT' THEN
    RAISE EXCEPTION 'shadow_cross_exchange_inventory_reference_mismatch';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER shadow_cross_exchange_inventory_reference_guard
  BEFORE INSERT OR UPDATE ON shadow_cross_exchange_inventory_initializations
  FOR EACH ROW EXECUTE FUNCTION enforce_shadow_cross_exchange_inventory_reference();

CREATE TRIGGER shadow_cross_exchange_inventory_initializations_immutable
  BEFORE UPDATE OR DELETE ON shadow_cross_exchange_inventory_initializations
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();

CREATE INDEX shadow_cross_exchange_inventory_session_idx
  ON shadow_cross_exchange_inventory_initializations(session_id, initialized_at);
