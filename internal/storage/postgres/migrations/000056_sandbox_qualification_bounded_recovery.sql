BEGIN;

-- Sandbox qualification bounded recovery

-- Runtime reconciliation failures retain only the existing typed exchange
-- class and a short sanitized cause code. Legacy rows may remain null; new
-- writes are validated by the store boundary.
ALTER TABLE sandbox_runtime_engine_runtime_events
  ADD COLUMN failure_kind text CHECK (
    failure_kind IS NULL OR failure_kind IN (
      'capability_unsupported','rate_limit','transient_outage',
      'timestamp_rejected','filter_rejected','insufficient_funds',
      'maintenance','validation_rejected','ambiguous_state',
      'operation_canceled'
    )
  ),
  ADD COLUMN cause_code text CHECK (
    cause_code IS NULL OR cause_code ~ '^[a-z0-9_]{1,64}$'
  );

ALTER TABLE sandbox_qualification_samples
  ADD COLUMN account_observations jsonb NOT NULL DEFAULT '[]'::jsonb
    CHECK (jsonb_typeof(account_observations) = 'array');

CREATE TABLE sandbox_qualification_recovery_events (
  id text PRIMARY KEY CHECK (char_length(id) BETWEEN 1 AND 128),
  run_id text NOT NULL REFERENCES sandbox_qualification_runs(id),
  account_id text NOT NULL REFERENCES sandbox_runtime_exchange_accounts(id),
  exchange text NOT NULL CHECK (exchange IN ('binance','bybit')),
  environment text NOT NULL CHECK (
    (exchange='binance' AND environment='spot_testnet') OR
    (exchange='bybit' AND environment='demo')
  ),
  account_epoch bigint NOT NULL CHECK (account_epoch > 0),
  event text NOT NULL CHECK (event IN (
    'detected','first_clean_check','recovered','expired','repeated',
    'unrecoverable'
  )),
  state text NOT NULL CHECK (state IN (
    'active','recovered','expired','repeated','unrecoverable'
  )),
  failure_kind text NOT NULL CHECK (failure_kind IN (
    'capability_unsupported','rate_limit','transient_outage',
    'timestamp_rejected','filter_rejected','insufficient_funds',
    'maintenance','validation_rejected','ambiguous_state',
    'operation_canceled'
  )),
  cause_code text NOT NULL CHECK (cause_code ~ '^[a-z0-9_]{1,64}$'),
  deadline_at timestamptz NOT NULL,
  clean_check_count smallint NOT NULL CHECK (clean_check_count BETWEEN 0 AND 2),
  recovery_timestamp timestamptz,
  evidence_hash sha256_hex NOT NULL,
  occurred_at timestamptz NOT NULL,
  FOREIGN KEY (account_id,account_epoch)
    REFERENCES sandbox_runtime_account_epochs(account_id,epoch)
);

CREATE INDEX sandbox_qualification_recovery_events_run_account_time
  ON sandbox_qualification_recovery_events(
    run_id,account_id,occurred_at DESC,id DESC
  );

CREATE TRIGGER sandbox_qualification_recovery_events_immutable
  BEFORE UPDATE OR DELETE ON sandbox_qualification_recovery_events
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();

COMMIT;
