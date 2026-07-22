SET TIME ZONE 'UTC';

ALTER TABLE portfolio_ownership
  DROP CONSTRAINT portfolio_ownership_strategy_key_check;

CREATE FUNCTION enforce_portfolio_ownership_strategy_reference() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE
  referenced_strategy text;
BEGIN
  SELECT definition.family INTO referenced_strategy
  FROM public.strategy_versions version
  JOIN public.strategy_definitions definition ON definition.id = version.strategy_id
  WHERE version.id = NEW.strategy_version_id;
  IF referenced_strategy IS NULL OR referenced_strategy <> NEW.strategy_key THEN
    RAISE EXCEPTION 'portfolio_ownership_strategy_mismatch';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER portfolio_ownership_strategy_reference
  BEFORE INSERT OR UPDATE OF strategy_version_id, strategy_key ON portfolio_ownership
  FOR EACH ROW EXECUTE FUNCTION enforce_portfolio_ownership_strategy_reference();

ALTER TABLE shadow_sessions
  DROP CONSTRAINT shadow_sessions_public_exchange_check,
  ADD COLUMN exchange_id text REFERENCES exchanges(id);

UPDATE shadow_sessions
SET exchange_id = split_part(public_exchange, '-production-public', 1);

ALTER TABLE shadow_sessions
  ALTER COLUMN exchange_id SET NOT NULL,
  ADD CONSTRAINT shadow_sessions_public_exchange_alias CHECK (
    public_exchange = exchange_id || '-production-public'
  );

CREATE FUNCTION enforce_shadow_public_exchange_reference() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM public.exchanges
    WHERE id = NEW.exchange_id AND environment = 'production_public'
  ) THEN
    RAISE EXCEPTION 'shadow_exchange_not_production_public';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER shadow_public_exchange_reference
  BEFORE INSERT OR UPDATE OF exchange_id, public_exchange ON shadow_sessions
  FOR EACH ROW EXECUTE FUNCTION enforce_shadow_public_exchange_reference();
