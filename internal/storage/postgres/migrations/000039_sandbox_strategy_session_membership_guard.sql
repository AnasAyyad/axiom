SET TIME ZONE 'UTC';

-- Migration 000029 named the parent reference sandbox_session_id, but its
-- membership guard attempted to read a nonexistent session_id column. Keep
-- the applied migration immutable and repair the live guard in place so an
-- automatic strategy session can bind only its exact parent account epoch.
CREATE OR REPLACE FUNCTION enforce_sandbox_strategy_session_account() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  parent_session text;
  parent_exchange text;
BEGIN
  SELECT sandbox_session_id INTO parent_session
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
