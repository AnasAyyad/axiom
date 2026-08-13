SET TIME ZONE 'UTC';

-- Both authenticated sandbox adapters already load and validate ETHBTC as the
-- third approved Spot market. Preserve that credential-free readiness fact so
-- an automatic triangular decision can prove all three books independently.
ALTER TABLE v1c_engine_market_observations
  DROP CONSTRAINT v1c_engine_market_observations_instrument_check;

ALTER TABLE v1c_engine_market_observations
  ADD CONSTRAINT v1c_engine_market_observations_instrument_check
  CHECK (instrument IN ('BTCUSDT','ETHUSDT','ETHBTC'));
