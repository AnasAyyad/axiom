SET TIME ZONE 'UTC';

-- Semantic run creation persists canonical strategy-version IDs. Preserve
-- historical IDs while validating newly created canonical sessions exactly.
CREATE OR REPLACE FUNCTION validate_shadow_session_market_scope_set(p_session_id text) RETURNS void
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

  IF strategy IN (
    'trend-v1a-1', 'mean-reversion-v1b-1',
    'trend-following-1-0-0', 'mean-reversion-1-0-0'
  ) THEN
    SELECT count(*) INTO purpose_count
      FROM public.shadow_session_market_scopes scope
      WHERE scope.session_id = p_session_id AND scope.purpose = 'primary';
    IF member_count <> 1 OR exchange_count <> 1 OR instrument_count <> 1 OR purpose_count <> 1 THEN
      RAISE EXCEPTION 'shadow_single_market_scope_invalid';
    END IF;
  ELSIF strategy IN ('triangular-v1b-1', 'triangular-arbitrage-1-0-0') THEN
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
  ELSIF strategy IN ('cross-exchange-v1b-1', 'cross-exchange-arbitrage-1-0-0') THEN
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

REVOKE ALL ON FUNCTION validate_shadow_session_market_scope_set(text) FROM PUBLIC;
