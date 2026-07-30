BEGIN;

DO $$
DECLARE
  route_constraint text;
BEGIN
  SELECT constraint_name
  INTO route_constraint
  FROM information_schema.check_constraints
  WHERE constraint_schema=current_schema()
    AND constraint_name IN (
      SELECT conname
      FROM pg_constraint
      WHERE conrelid='v1c_authenticated_request_evidence'::regclass
        AND contype='c'
        AND pg_get_constraintdef(oid) LIKE '%testnet.binance.vision%'
        AND pg_get_constraintdef(oid) LIKE '%api-demo.bybit.com%'
    );

  IF route_constraint IS NULL THEN
    RAISE EXCEPTION 'v1c_authenticated_route_constraint_missing';
  END IF;

  EXECUTE format(
    'ALTER TABLE v1c_authenticated_request_evidence DROP CONSTRAINT %I',
    route_constraint
  );
END;
$$;

ALTER TABLE v1c_authenticated_request_evidence
ADD CONSTRAINT v1c_authenticated_request_route_closed CHECK (
  (
    exchange='binance' AND host='testnet.binance.vision' AND
    (method,path) IN (
      ('GET','/api/v3/account'),
      ('GET','/api/v3/openOrders'),
      ('GET','/api/v3/allOrders'),
      ('GET','/api/v3/myTrades'),
      ('POST','/api/v3/order/test'),
      ('POST','/api/v3/order'),
      ('GET','/api/v3/order'),
      ('DELETE','/api/v3/order')
    )
  ) OR (
    exchange='binance' AND host='ws-api.testnet.binance.vision' AND
    method='WS' AND
    path='/ws-api/v3/userDataStream.subscribe.signature'
  ) OR (
    exchange='bybit' AND host='api-demo.bybit.com' AND
    (method,path) IN (
      ('GET','/v5/user/query-api'),
      ('GET','/v5/account/wallet-balance'),
      ('POST','/v5/order/create'),
      ('POST','/v5/order/cancel'),
      ('GET','/v5/order/realtime'),
      ('GET','/v5/order/history'),
      ('GET','/v5/execution/list')
    )
  ) OR (
    exchange='bybit' AND host='stream-demo.bybit.com' AND
    method='WS' AND
    path='/v5/private/auth'
  )
);

CREATE TABLE v1c_engine_startup_evidence (
  id text PRIMARY KEY,
  account_id text NOT NULL REFERENCES v1c_exchange_accounts(id),
  exchange text NOT NULL CHECK (exchange IN ('binance','bybit')),
  startup_cycle bigint NOT NULL CHECK (startup_cycle > 0),
  stage text NOT NULL CHECK (stage IN (
    'acquire_lease',
    'enter_locked',
    'validate_build_configuration',
    'validate_credential_account_generation',
    'recover_outbox_inbox',
    'load_balances_orders_fills',
    'resolve_unknown_orders',
    'reconcile_journal_reservations',
    'synchronize_filters_book_clock',
    'start_private_stream',
    'enter_ready_paused'
  )),
  reached_healthy boolean NOT NULL,
  evidence_hash sha256_hex NOT NULL,
  observed_at timestamptz NOT NULL,
  UNIQUE (account_id,startup_cycle,stage)
);

CREATE TRIGGER v1c_engine_startup_evidence_immutable
  BEFORE UPDATE OR DELETE ON v1c_engine_startup_evidence
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();

CREATE TABLE v1c_engine_commands (
  id text PRIMARY KEY CHECK (char_length(id) BETWEEN 1 AND 128),
  account_id text NOT NULL REFERENCES v1c_exchange_accounts(id),
  account_epoch bigint NOT NULL CHECK (account_epoch > 0),
  kind text NOT NULL CHECK (kind IN ('QUERY','CANCEL','RECONCILE')),
  client_order_id text CHECK (
    client_order_id IS NULL OR char_length(client_order_id) BETWEEN 1 AND 64
  ),
  state text NOT NULL DEFAULT 'PENDING'
    CHECK (state IN ('PENDING','CLAIMED','SUCCEEDED','FAILED')),
  claim_owner text CHECK (
    claim_owner IS NULL OR char_length(claim_owner) BETWEEN 1 AND 128
  ),
  fencing_token bigint,
  requested_at timestamptz NOT NULL,
  claimed_at timestamptz,
  claim_expires_at timestamptz,
  completed_at timestamptz,
  evidence_hash sha256_hex,
  CHECK (
    (kind='RECONCILE' AND client_order_id IS NULL) OR
    (kind IN ('QUERY','CANCEL') AND client_order_id IS NOT NULL)
  ),
  CHECK (
    (state='PENDING' AND claim_owner IS NULL AND fencing_token IS NULL
      AND claimed_at IS NULL AND claim_expires_at IS NULL
      AND completed_at IS NULL AND evidence_hash IS NULL) OR
    (state='CLAIMED' AND claim_owner IS NOT NULL AND fencing_token > 0
      AND claimed_at IS NOT NULL AND claim_expires_at > claimed_at
      AND completed_at IS NULL AND evidence_hash IS NULL) OR
    (state IN ('SUCCEEDED','FAILED') AND claim_owner IS NOT NULL
      AND fencing_token > 0 AND claimed_at IS NOT NULL
      AND claim_expires_at IS NOT NULL AND completed_at IS NOT NULL
      AND evidence_hash IS NOT NULL)
  )
);

CREATE INDEX v1c_engine_commands_claim_idx
  ON v1c_engine_commands(account_id,account_epoch,state,requested_at,id);

CREATE TABLE v1c_engine_observations (
  account_id text PRIMARY KEY REFERENCES v1c_exchange_accounts(id),
  account_epoch bigint NOT NULL CHECK (account_epoch > 0),
  exchange text NOT NULL CHECK (exchange IN ('binance','bybit')),
  startup_cycle bigint NOT NULL CHECK (startup_cycle > 0),
  eligibility jsonb NOT NULL,
  private_stream_healthy boolean NOT NULL CHECK (private_stream_healthy),
  reconciliation_clean boolean NOT NULL CHECK (reconciliation_clean),
  evidence_healthy boolean NOT NULL CHECK (evidence_healthy),
  observed_at timestamptz NOT NULL,
  FOREIGN KEY (account_id,account_epoch)
    REFERENCES v1c_account_epochs(account_id,epoch),
  CHECK (
    eligibility ? 'eligible'
    AND (eligibility->>'eligible')::boolean
    AND eligibility ? 'exchange'
    AND eligibility->>'exchange'=exchange
    AND eligibility ? 'instrument'
    AND eligibility->>'instrument' IN ('BTCUSDT','ETHUSDT','ETHBTC')
  )
);

CREATE TABLE v1c_canary_evidence (
  id text PRIMARY KEY CHECK (char_length(id) BETWEEN 1 AND 128),
  canary_id text NOT NULL,
  exchange text NOT NULL CHECK (exchange IN ('binance','bybit')),
  account_id text NOT NULL,
  account_epoch bigint NOT NULL CHECK (account_epoch > 0),
  sandbox_session_id text NOT NULL REFERENCES v1c_sandbox_sessions(id),
  plan_id text NOT NULL REFERENCES v1c_submission_plans(id),
  stage text NOT NULL CHECK (stage IN (
    'PLAN_APPROVED',
    'QUERY_SUCCEEDED',
    'CANCEL_OR_FILL_CONFIRMED',
    'RECONCILED',
    'RESTART_VERIFIED'
  )),
  startup_cycle bigint NOT NULL CHECK (startup_cycle > 0),
  evidence_hash sha256_hex NOT NULL,
  observed_at timestamptz NOT NULL,
  FOREIGN KEY (account_id,account_epoch)
    REFERENCES v1c_account_epochs(account_id,epoch),
  UNIQUE (exchange,canary_id,stage)
);

CREATE TRIGGER v1c_canary_evidence_immutable
  BEFORE UPDATE OR DELETE ON v1c_canary_evidence
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();

COMMIT;
