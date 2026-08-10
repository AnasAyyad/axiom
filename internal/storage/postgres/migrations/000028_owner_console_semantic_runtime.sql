SET TIME ZONE 'UTC';

-- Historical users and role assignments are immutable evidence. The current
-- product authority is one explicit owner account, never a role lookup.
CREATE TABLE owner_accounts (
  singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
  user_id text NOT NULL UNIQUE REFERENCES users(id),
  established_at timestamptz NOT NULL,
  CHECK (singleton)
);

DO $$
DECLARE
  active_count bigint;
  active_user text;
BEGIN
  SELECT count(*), min(id) INTO active_count, active_user
  FROM users
  WHERE status = 'active';

  IF active_count > 1 THEN
    RAISE EXCEPTION 'owner_console_multiple_active_users';
  END IF;

  IF active_count = 1 THEN
    INSERT INTO owner_accounts(singleton, user_id, established_at)
    SELECT true, active_user, clock_timestamp()
    ON CONFLICT (singleton) DO NOTHING;
  END IF;
END;
$$;

-- These read-only semantic views let new runtime code avoid phase-derived
-- table names while preserving historical rows and their identities in place.
CREATE VIEW configuration_records AS
SELECT id, version, configuration_hash, actor, recorded_at
FROM configuration_versions;

CREATE VIEW activity_records AS
SELECT activity_revision AS revision, id, view_kind, source_type, source_id,
  source_revision, reason_code, outcome, strategy_id, instrument_id,
  exchange_id, side, mode, correlation_id, causation_id, occurred_at, details,
  projected_at
FROM v1d_activity_projection;

CREATE FUNCTION enforce_single_active_owner() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  active_count bigint;
BEGIN
  IF TG_OP = 'DELETE' OR (TG_OP = 'UPDATE' AND OLD.status = 'active' AND NEW.status <> 'active') THEN
    RETURN coalesce(NEW, OLD);
  END IF;

  IF NEW.status = 'active' THEN
    SELECT count(*) INTO active_count FROM users WHERE status = 'active';
    IF active_count > 1 THEN
      RAISE EXCEPTION 'owner_console_multiple_active_users';
    END IF;
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER users_single_active_owner
  AFTER INSERT OR UPDATE OF status ON users
  FOR EACH ROW EXECUTE FUNCTION enforce_single_active_owner();
