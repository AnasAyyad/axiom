SET TIME ZONE 'UTC';

-- New semantic run creation records the exact production-public market set
-- reviewed by the owner. Historical sessions keep market_scope_required=false
-- because a missing selection cannot be reconstructed honestly after the fact.
ALTER TABLE shadow_sessions
  ADD COLUMN market_scope_required boolean NOT NULL DEFAULT false;

CREATE TABLE shadow_session_market_scopes (
  session_id text NOT NULL REFERENCES shadow_sessions(id),
  ordinal smallint NOT NULL CHECK (ordinal BETWEEN 1 AND 16),
  exchange_id text NOT NULL REFERENCES exchanges(id),
  instrument_id text NOT NULL REFERENCES instruments(id),
  purpose text NOT NULL CHECK (purpose IN ('primary','triangle_market','paired_market')),
  PRIMARY KEY (session_id, ordinal),
  UNIQUE (session_id, exchange_id, instrument_id)
);

CREATE FUNCTION enforce_shadow_market_scope_reference() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM public.instrument_metadata_versions metadata
    JOIN public.exchanges exchange ON exchange.id = metadata.exchange_id
    WHERE metadata.exchange_id = NEW.exchange_id
      AND metadata.instrument_id = NEW.instrument_id
      AND exchange.environment = 'production_public'
  ) THEN
    RAISE EXCEPTION 'shadow_market_scope_reference_invalid';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER shadow_market_scope_reference
  BEFORE INSERT OR UPDATE OF exchange_id, instrument_id ON shadow_session_market_scopes
  FOR EACH ROW EXECUTE FUNCTION enforce_shadow_market_scope_reference();

CREATE TRIGGER shadow_session_market_scopes_immutable
  BEFORE UPDATE OR DELETE ON shadow_session_market_scopes
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();

CREATE FUNCTION validate_shadow_session_market_scope_set(p_session_id text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE
  required boolean;
  strategy text;
  member_count integer;
  exchange_count integer;
  instrument_count integer;
  purpose_count integer;
  baseline_count integer;
BEGIN
  SELECT session.market_scope_required, session.strategy_version_id
    INTO required, strategy
    FROM public.shadow_sessions session
    WHERE session.id = p_session_id;

  IF NOT FOUND OR NOT required THEN
    RETURN;
  END IF;

  SELECT count(*), count(DISTINCT scope.exchange_id), count(DISTINCT scope.instrument_id)
    INTO member_count, exchange_count, instrument_count
    FROM public.shadow_session_market_scopes scope
    WHERE scope.session_id = p_session_id;

  IF strategy IN ('trend-v1a-1','mean-reversion-v1b-1') THEN
    SELECT count(*) INTO purpose_count
      FROM public.shadow_session_market_scopes scope
      WHERE scope.session_id = p_session_id AND scope.purpose = 'primary';
    IF member_count <> 1 OR exchange_count <> 1 OR instrument_count <> 1 OR purpose_count <> 1 THEN
      RAISE EXCEPTION 'shadow_single_market_scope_invalid';
    END IF;
  ELSIF strategy = 'triangular-v1b-1' THEN
    SELECT count(*) INTO purpose_count
      FROM public.shadow_session_market_scopes scope
      WHERE scope.session_id = p_session_id AND scope.purpose = 'triangle_market';
    SELECT count(*) INTO baseline_count
      FROM public.shadow_session_market_scopes scope
      JOIN public.instruments instrument ON instrument.id = scope.instrument_id
      WHERE scope.session_id = p_session_id AND (
        (instrument.base_asset = 'BTC' AND instrument.quote_asset = 'USDT') OR
        (instrument.base_asset = 'ETH' AND instrument.quote_asset = 'USDT') OR
        (instrument.base_asset = 'ETH' AND instrument.quote_asset = 'BTC') OR
        (instrument.base_asset = 'BTC' AND instrument.quote_asset = 'ETH')
      );
    IF member_count <> 3 OR exchange_count <> 1 OR instrument_count <> 3 OR
       purpose_count <> 3 OR baseline_count <> 3 THEN
      RAISE EXCEPTION 'shadow_triangle_market_scope_invalid';
    END IF;
  ELSIF strategy = 'cross-exchange-v1b-1' THEN
    SELECT count(*) INTO purpose_count
      FROM public.shadow_session_market_scopes scope
      WHERE scope.session_id = p_session_id AND scope.purpose = 'paired_market';
    SELECT count(*) INTO baseline_count
      FROM public.shadow_session_market_scopes scope
      WHERE scope.session_id = p_session_id AND scope.exchange_id IN ('binance','bybit');
    IF member_count <> 2 OR exchange_count <> 2 OR instrument_count <> 1 OR
       purpose_count <> 2 OR baseline_count <> 2 THEN
      RAISE EXCEPTION 'shadow_paired_market_scope_invalid';
    END IF;
  ELSE
    RAISE EXCEPTION 'shadow_market_scope_strategy_invalid';
  END IF;
END;
$$;

CREATE FUNCTION enforce_shadow_session_market_scope_set() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
BEGIN
  PERFORM public.validate_shadow_session_market_scope_set(
    CASE WHEN TG_OP = 'DELETE' THEN OLD.session_id ELSE NEW.session_id END
  );
  IF TG_OP = 'DELETE' THEN
    RETURN OLD;
  END IF;
  RETURN NEW;
END;
$$;

CREATE FUNCTION enforce_shadow_session_market_scope_parent() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
BEGIN
  PERFORM public.validate_shadow_session_market_scope_set(NEW.id);
  RETURN NEW;
END;
$$;

CREATE CONSTRAINT TRIGGER shadow_session_market_scope_set_guard
  AFTER INSERT OR UPDATE OR DELETE ON shadow_session_market_scopes
  DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION enforce_shadow_session_market_scope_set();

CREATE CONSTRAINT TRIGGER shadow_session_market_scope_parent_guard
  AFTER INSERT OR UPDATE OF market_scope_required, strategy_version_id ON shadow_sessions
  DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION enforce_shadow_session_market_scope_parent();

CREATE INDEX shadow_session_market_scopes_lookup_idx
  ON shadow_session_market_scopes(exchange_id, instrument_id, session_id);
