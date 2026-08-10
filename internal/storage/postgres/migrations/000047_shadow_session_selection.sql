SET TIME ZONE 'UTC';

-- Historical shadow sessions evaluated the full configuration universe and
-- therefore have no honest single-instrument value to backfill. New unified
-- owner runs persist the reviewed instrument explicitly; NULL remains only as
-- the compatibility representation for pre-migration sessions and the legacy
-- shadow command.
ALTER TABLE shadow_sessions
  ADD COLUMN instrument_id text REFERENCES instruments(id);

CREATE FUNCTION enforce_shadow_session_instrument_reference() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
BEGIN
  IF NEW.instrument_id IS NOT NULL AND NOT EXISTS (
    SELECT 1
    FROM public.instrument_metadata_versions metadata
    JOIN public.exchanges exchange ON exchange.id = metadata.exchange_id
    WHERE metadata.instrument_id = NEW.instrument_id
      AND exchange.id = NEW.exchange_id
      AND exchange.environment = 'production_public'
  ) THEN
    RAISE EXCEPTION 'shadow_session_instrument_exchange_mismatch';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER shadow_session_instrument_reference
  BEFORE INSERT OR UPDATE OF exchange_id, instrument_id ON shadow_sessions
  FOR EACH ROW EXECUTE FUNCTION enforce_shadow_session_instrument_reference();
