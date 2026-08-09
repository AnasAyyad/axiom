SET TIME ZONE 'UTC';

-- Account-level readiness remains in v1c_engine_observations.  Automatic
-- strategy and canary admission needs a separate current record per spot
-- instrument so a healthy BTC book cannot accidentally authorize ETH.
CREATE TABLE v1c_engine_market_observations (
  account_id text NOT NULL REFERENCES v1c_exchange_accounts(id),
  account_epoch bigint NOT NULL CHECK (account_epoch > 0),
  exchange text NOT NULL CHECK (exchange IN ('binance','bybit')),
  instrument text NOT NULL CHECK (instrument IN ('BTCUSDT','ETHUSDT')),
  startup_cycle bigint NOT NULL CHECK (startup_cycle > 0),
  eligibility jsonb NOT NULL,
  observed_at timestamptz NOT NULL,
  PRIMARY KEY (account_id,instrument),
  FOREIGN KEY (account_id,account_epoch)
    REFERENCES v1c_account_epochs(account_id,epoch),
  CHECK (
    eligibility ? 'eligible'
    AND (eligibility->>'eligible')::boolean
    AND eligibility ? 'exchange'
    AND eligibility->>'exchange'=exchange
    AND eligibility ? 'instrument'
    AND eligibility->>'instrument'=instrument
  )
);

CREATE INDEX v1c_engine_market_observations_admission_idx
  ON v1c_engine_market_observations(account_id,account_epoch,instrument,observed_at);
