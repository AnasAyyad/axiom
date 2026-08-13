SET TIME ZONE 'UTC';

-- Strategy sessions created before this additive owner-console field retain a
-- NULL instrument rather than receiving a guessed historical value.
ALTER TABLE sandbox_strategy_sessions
  ADD COLUMN instrument text;

ALTER TABLE sandbox_strategy_sessions
  ADD CONSTRAINT sandbox_strategy_session_instrument_valid
  CHECK (instrument IS NULL OR instrument IN ('BTCUSDT','ETHUSDT'));
