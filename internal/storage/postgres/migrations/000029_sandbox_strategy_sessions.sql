SET TIME ZONE 'UTC';

-- Automatic strategy sessions are additive to the existing armable sandbox
-- session. They carry no credential, order, price, quantity, or transfer data.
CREATE TABLE sandbox_strategy_sessions (
  id text PRIMARY KEY,
  sandbox_session_id text NOT NULL UNIQUE REFERENCES v1c_sandbox_sessions(id),
  strategy_id text NOT NULL CHECK (strategy_id IN (
    'trend','mean-reversion','triangular','cross-exchange-arbitrage'
  )),
  state text NOT NULL CHECK (state IN ('prepared','running','blocked','stopped')),
  created_by text NOT NULL REFERENCES users(id),
  created_at timestamptz NOT NULL,
  started_at timestamptz,
  stopped_at timestamptz,
  blocking_reason text,
  revision bigint NOT NULL CHECK (revision > 0),
  CHECK ((state = 'running') = (started_at IS NOT NULL)),
  CHECK (stopped_at IS NULL OR state = 'stopped'),
  CHECK (blocking_reason IS NULL OR state = 'blocked')
);

CREATE TABLE sandbox_strategy_session_accounts (
  strategy_session_id text NOT NULL REFERENCES sandbox_strategy_sessions(id),
  account_id text NOT NULL,
  account_epoch bigint NOT NULL,
  exchange text NOT NULL CHECK (exchange IN ('binance','bybit')),
  PRIMARY KEY (strategy_session_id,account_id),
  FOREIGN KEY (account_id,account_epoch)
    REFERENCES v1c_account_epochs(account_id,epoch)
);

CREATE FUNCTION enforce_sandbox_strategy_session_account() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  parent_session text;
  parent_exchange text;
BEGIN
  SELECT session_id INTO parent_session
  FROM sandbox_strategy_sessions
  WHERE id = NEW.strategy_session_id;
  IF parent_session IS NULL OR NOT EXISTS (
    SELECT 1 FROM v1c_sandbox_session_accounts membership
    WHERE membership.session_id = parent_session
      AND membership.account_id = NEW.account_id
      AND membership.account_epoch = NEW.account_epoch
  ) THEN
    RAISE EXCEPTION 'sandbox_strategy_session_account_membership_invalid';
  END IF;
  SELECT exchange INTO parent_exchange FROM v1c_exchange_accounts
  WHERE id = NEW.account_id AND current_epoch = NEW.account_epoch;
  IF parent_exchange IS DISTINCT FROM NEW.exchange THEN
    RAISE EXCEPTION 'sandbox_strategy_session_account_exchange_invalid';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER sandbox_strategy_session_account_guard
  BEFORE INSERT OR UPDATE ON sandbox_strategy_session_accounts
  FOR EACH ROW EXECUTE FUNCTION enforce_sandbox_strategy_session_account();

CREATE FUNCTION enforce_sandbox_strategy_session_topology() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  strategy text;
  members bigint;
  binance_members bigint;
  bybit_members bigint;
BEGIN
  SELECT strategy_id INTO strategy FROM sandbox_strategy_sessions
  WHERE id = NEW.strategy_session_id;
  SELECT count(*), count(*) FILTER (WHERE exchange = 'binance'),
         count(*) FILTER (WHERE exchange = 'bybit')
  INTO members, binance_members, bybit_members
  FROM sandbox_strategy_session_accounts
  WHERE strategy_session_id = NEW.strategy_session_id;
  IF (strategy = 'cross-exchange-arbitrage' AND
      (members <> 2 OR binance_members <> 1 OR bybit_members <> 1)) OR
     (strategy <> 'cross-exchange-arbitrage' AND members <> 1) THEN
    RAISE EXCEPTION 'sandbox_strategy_session_topology_invalid';
  END IF;
  RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER sandbox_strategy_session_topology_guard
  AFTER INSERT OR UPDATE OR DELETE ON sandbox_strategy_session_accounts
  DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION enforce_sandbox_strategy_session_topology();

CREATE FUNCTION protect_sandbox_strategy_session_membership() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'sandbox_strategy_session_membership_immutable';
END;
$$;

CREATE TRIGGER sandbox_strategy_session_accounts_immutable
  BEFORE UPDATE OR DELETE ON sandbox_strategy_session_accounts
  FOR EACH ROW EXECUTE FUNCTION protect_sandbox_strategy_session_membership();
