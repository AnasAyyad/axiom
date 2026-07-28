SET TIME ZONE 'UTC';

CREATE TABLE v1c_exchange_accounts (
  id text PRIMARY KEY,
  exchange text NOT NULL CHECK (exchange IN ('binance','bybit')),
  environment text NOT NULL CHECK (
    (exchange='binance' AND environment='spot_testnet') OR
    (exchange='bybit' AND environment='demo')
  ),
  native_account_hash sha256_hex NOT NULL,
  state text NOT NULL CHECK (state IN ('LOCKED','READY_PAUSED','ARMED','DEGRADED','QUARANTINED')),
  current_epoch bigint NOT NULL CHECK (current_epoch > 0),
  credential_generation bigint NOT NULL CHECK (credential_generation > 0),
  revision bigint NOT NULL CHECK (revision > 0),
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  UNIQUE (exchange,environment,native_account_hash)
);

CREATE TABLE v1c_account_epochs (
  account_id text NOT NULL REFERENCES v1c_exchange_accounts(id),
  epoch bigint NOT NULL CHECK (epoch > 0),
  reason text NOT NULL CHECK (reason IN ('initial','exchange_reset')),
  opened_at timestamptz NOT NULL,
  closed_at timestamptz,
  PRIMARY KEY (account_id,epoch),
  CHECK (closed_at IS NULL OR closed_at >= opened_at)
);

CREATE TABLE v1c_credential_generations (
  account_id text NOT NULL REFERENCES v1c_exchange_accounts(id),
  generation bigint NOT NULL CHECK (generation > 0),
  key_fingerprint text NOT NULL CHECK (key_fingerprint ~ '^[0-9a-f]{32}$'),
  account_identity_hash sha256_hex NOT NULL,
  validated_at timestamptz NOT NULL,
  retired_at timestamptz,
  PRIMARY KEY (account_id,generation)
);

CREATE TABLE v1c_credential_rotations (
  id text PRIMARY KEY,
  account_id text NOT NULL REFERENCES v1c_exchange_accounts(id),
  authorization_id text NOT NULL UNIQUE REFERENCES v1c_sandbox_authorizations(id),
  actor_user_id text NOT NULL REFERENCES users(id),
  actor_session_id text NOT NULL REFERENCES sessions(id),
  source_hash sha256_hex NOT NULL,
  reason_hash sha256_hex NOT NULL,
  stage text NOT NULL CHECK (stage IN (
    'COMMAND_LOCKED','SECRETS_REPLACED_EXTERNALLY','RESTART_VALIDATED',
    'RECONCILED','READY_PAUSED'
  )),
  prior_generation bigint NOT NULL CHECK (prior_generation > 0),
  new_generation bigint CHECK (new_generation=prior_generation+1),
  prior_fingerprint text NOT NULL CHECK (prior_fingerprint ~ '^[0-9a-f]{32}$'),
  new_fingerprint text CHECK (new_fingerprint ~ '^[0-9a-f]{32}$'),
  nonterminal_quarantined boolean NOT NULL,
  reconciliation_id text,
  started_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  revision bigint NOT NULL CHECK (revision > 0)
);

CREATE FUNCTION enforce_v1c_credential_rotation_authorization() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  authorization_user text;
  authorization_session text;
  authorization_purpose text;
  authorization_source sha256_hex;
  authorization_reason sha256_hex;
  authorization_expires timestamptz;
  authorization_consumed timestamptz;
BEGIN
  SELECT user_id,session_id,purpose,source_hash,reason_hash,expires_at,consumed_at
  INTO authorization_user,authorization_session,authorization_purpose,
       authorization_source,authorization_reason,authorization_expires,
       authorization_consumed
  FROM v1c_sandbox_authorizations
  WHERE id=NEW.authorization_id
  FOR SHARE;

  IF authorization_purpose IS DISTINCT FROM 'credential_rotation' OR
     authorization_user IS DISTINCT FROM NEW.actor_user_id OR
     authorization_session IS DISTINCT FROM NEW.actor_session_id OR
     authorization_source IS DISTINCT FROM NEW.source_hash OR
     authorization_reason IS DISTINCT FROM NEW.reason_hash OR
     authorization_consumed IS NULL OR
     NEW.started_at < authorization_consumed OR
     NEW.started_at > authorization_expires OR
     NOT EXISTS (
       SELECT 1
       FROM sessions session
       JOIN users actor ON actor.id=session.user_id
       WHERE session.id=NEW.actor_session_id
         AND session.user_id=NEW.actor_user_id
         AND actor.status='active'
         AND session.revoked_at IS NULL
         AND session.expires_at>NEW.started_at
         AND session.idle_expires_at>NEW.started_at
       FOR SHARE OF session,actor
     ) THEN
    RAISE EXCEPTION 'v1c_credential_rotation_authorization_invalid';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER v1c_credential_rotation_authorized
  BEFORE INSERT ON v1c_credential_rotations
  FOR EACH ROW EXECUTE FUNCTION enforce_v1c_credential_rotation_authorization();

CREATE FUNCTION protect_v1c_credential_rotation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP='DELETE' OR
     NEW.id IS DISTINCT FROM OLD.id OR
     NEW.account_id IS DISTINCT FROM OLD.account_id OR
     NEW.authorization_id IS DISTINCT FROM OLD.authorization_id OR
     NEW.actor_user_id IS DISTINCT FROM OLD.actor_user_id OR
     NEW.actor_session_id IS DISTINCT FROM OLD.actor_session_id OR
     NEW.source_hash IS DISTINCT FROM OLD.source_hash OR
     NEW.reason_hash IS DISTINCT FROM OLD.reason_hash OR
     NEW.prior_generation IS DISTINCT FROM OLD.prior_generation OR
     NEW.prior_fingerprint IS DISTINCT FROM OLD.prior_fingerprint OR
     NEW.nonterminal_quarantined IS DISTINCT FROM OLD.nonterminal_quarantined OR
     NEW.started_at IS DISTINCT FROM OLD.started_at OR
     NEW.updated_at<OLD.updated_at OR
     NEW.revision<>OLD.revision+1 OR
     NOT (
       (
         OLD.stage='COMMAND_LOCKED' AND
         NEW.stage='SECRETS_REPLACED_EXTERNALLY' AND
         NEW.new_generation IS NULL AND
         NEW.new_fingerprint IS NULL AND
         NEW.reconciliation_id IS NULL
       ) OR (
         OLD.stage='SECRETS_REPLACED_EXTERNALLY' AND
         NEW.stage='RESTART_VALIDATED' AND
         NEW.new_generation=OLD.prior_generation+1 AND
         NEW.new_fingerprint IS NOT NULL AND
         NEW.new_fingerprint<>OLD.prior_fingerprint AND
         NEW.reconciliation_id IS NULL
       ) OR (
         OLD.stage='RESTART_VALIDATED' AND
         NEW.stage='RECONCILED' AND
         NEW.new_generation=OLD.new_generation AND
         NEW.new_fingerprint=OLD.new_fingerprint AND
         NEW.reconciliation_id IS NOT NULL
       ) OR (
         OLD.stage='RECONCILED' AND
         NEW.stage='READY_PAUSED' AND
         NEW.new_generation=OLD.new_generation AND
         NEW.new_fingerprint=OLD.new_fingerprint AND
         NEW.reconciliation_id=OLD.reconciliation_id
       )
     ) THEN
    RAISE EXCEPTION 'v1c_credential_rotation_mutation_rejected';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER v1c_credential_rotation_protected
  BEFORE UPDATE OR DELETE ON v1c_credential_rotations
  FOR EACH ROW EXECUTE FUNCTION protect_v1c_credential_rotation();

CREATE TABLE v1c_sandbox_sessions (
  id text PRIMARY KEY,
  state text NOT NULL CHECK (state IN ('LOCKED','READY_PAUSED','ARMED','PAUSED','STOPPED','DEGRADED')),
  configuration_id text NOT NULL,
  strategy_set_hash sha256_hex NOT NULL,
  revision bigint NOT NULL CHECK (revision > 0),
  created_by text NOT NULL REFERENCES users(id),
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL
);

CREATE TABLE v1c_sandbox_session_accounts (
  session_id text NOT NULL REFERENCES v1c_sandbox_sessions(id),
  account_id text NOT NULL REFERENCES v1c_exchange_accounts(id),
  account_epoch bigint NOT NULL,
  PRIMARY KEY (session_id,account_id),
  FOREIGN KEY (account_id,account_epoch) REFERENCES v1c_account_epochs(account_id,epoch)
);

CREATE TABLE v1c_sandbox_arms (
  id text PRIMARY KEY,
  sandbox_session_id text NOT NULL REFERENCES v1c_sandbox_sessions(id),
  authorization_id text NOT NULL UNIQUE REFERENCES v1c_sandbox_authorizations(id),
  actor_user_id text NOT NULL REFERENCES users(id),
  actor_session_id text NOT NULL REFERENCES sessions(id),
  source_hash sha256_hex NOT NULL,
  reason_hash sha256_hex NOT NULL,
  created_at timestamptz NOT NULL,
  expires_at timestamptz NOT NULL,
  revoked_at timestamptz,
  revision bigint NOT NULL CHECK (revision > 0),
  CHECK (expires_at = created_at + interval '15 minutes'),
  CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);

CREATE FUNCTION enforce_v1c_sandbox_arm_authorization() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  authorization_user text;
  authorization_session text;
  authorization_purpose text;
  authorization_source sha256_hex;
  authorization_reason sha256_hex;
  authorization_expires timestamptz;
  authorization_consumed timestamptz;
BEGIN
  SELECT user_id,session_id,purpose,source_hash,reason_hash,expires_at,consumed_at
  INTO authorization_user,authorization_session,authorization_purpose,
       authorization_source,authorization_reason,authorization_expires,
       authorization_consumed
  FROM v1c_sandbox_authorizations
  WHERE id=NEW.authorization_id
  FOR SHARE;

  IF authorization_purpose IS DISTINCT FROM 'sandbox_arm' OR
     authorization_user IS DISTINCT FROM NEW.actor_user_id OR
     authorization_session IS DISTINCT FROM NEW.actor_session_id OR
     authorization_source IS DISTINCT FROM NEW.source_hash OR
     authorization_reason IS DISTINCT FROM NEW.reason_hash OR
     authorization_consumed IS NULL OR
     NEW.created_at < authorization_consumed OR
     NEW.created_at > authorization_expires OR
     NOT EXISTS (
       SELECT 1
       FROM sessions session
       JOIN users actor ON actor.id=session.user_id
       WHERE session.id=NEW.actor_session_id
         AND session.user_id=NEW.actor_user_id
         AND actor.status='active'
         AND session.revoked_at IS NULL
         AND session.expires_at>NEW.created_at
         AND session.idle_expires_at>NEW.created_at
       FOR SHARE OF session,actor
     ) THEN
    RAISE EXCEPTION 'v1c_sandbox_arm_authorization_invalid';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER v1c_sandbox_arm_authorized
  BEFORE INSERT ON v1c_sandbox_arms
  FOR EACH ROW EXECUTE FUNCTION enforce_v1c_sandbox_arm_authorization();

CREATE FUNCTION v1c_authenticated_evidence_fields_valid(
  exchange_name text,
  candidate text[]
) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
  SELECT cardinality(candidate) BETWEEN 1 AND 32
    AND array_position(candidate,NULL) IS NULL
    AND candidate=ARRAY(
      SELECT DISTINCT field_name
      FROM unnest(candidate) AS field(field_name)
      ORDER BY field_name
    )
    AND CASE exchange_name
      WHEN 'binance' THEN candidate <@ ARRAY[
        'endTime','fromId','limit','newClientOrderId','newOrderRespType',
        'orderId','origClientOrderId','price','quantity','recvWindow',
        'side','startTime','symbol','timeInForce','timestamp','type'
      ]::text[]
      WHEN 'bybit' THEN candidate <@ ARRAY[
        'accountType','category','cursor','endTime','isLeverage','limit',
        'orderFilter','orderId','orderLinkId','orderType','price','qty',
        'recvWindow','side','startTime','symbol','timeInForce','timestamp'
      ]::text[]
      ELSE false
    END
$$;

CREATE FUNCTION v1c_authenticated_evidence_enumerations_valid(
  exchange_name text,
  field_names text[],
  enumerated_fields jsonb
) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE PARALLEL SAFE AS $$
DECLARE
  field_name text;
  enum_name text;
  enum_value text;
BEGIN
  IF jsonb_typeof(enumerated_fields) IS DISTINCT FROM 'object' THEN
    RETURN false;
  END IF;
  IF (SELECT count(*) FROM jsonb_object_keys(enumerated_fields))>8 THEN
    RETURN false;
  END IF;

  FOR enum_name,enum_value IN
    SELECT key,value FROM jsonb_each_text(enumerated_fields)
  LOOP
    IF NOT enum_name=ANY(field_names) THEN
      RETURN false;
    END IF;
    IF exchange_name='binance' AND NOT (
      (enum_name='newOrderRespType' AND enum_value='ACK') OR
      (enum_name='side' AND enum_value IN ('BUY','SELL')) OR
      (enum_name='timeInForce' AND enum_value IN ('GTC','IOC')) OR
      (enum_name='type' AND enum_value IN ('LIMIT','LIMIT_MAKER'))
    ) THEN
      RETURN false;
    ELSIF exchange_name='bybit' AND NOT (
      (enum_name='accountType' AND enum_value='UNIFIED') OR
      (enum_name='category' AND enum_value='spot') OR
      (enum_name='isLeverage' AND enum_value='0') OR
      (enum_name='orderFilter' AND enum_value='Order') OR
      (enum_name='orderType' AND enum_value='Limit') OR
      (enum_name='side' AND enum_value IN ('Buy','Sell')) OR
      (enum_name='timeInForce' AND enum_value IN ('GTC','IOC','PostOnly'))
    ) THEN
      RETURN false;
    ELSIF exchange_name NOT IN ('binance','bybit') THEN
      RETURN false;
    END IF;
  END LOOP;

  FOREACH field_name IN ARRAY field_names
  LOOP
    IF (
      exchange_name='binance' AND field_name IN (
        'newOrderRespType','side','timeInForce','type'
      )
    ) OR (
      exchange_name='bybit' AND field_name IN (
        'accountType','category','isLeverage','orderFilter',
        'orderType','side','timeInForce'
      )
    ) THEN
      IF NOT enumerated_fields ? field_name THEN
        RETURN false;
      END IF;
    END IF;
  END LOOP;
  RETURN true;
END;
$$;

CREATE TABLE v1c_authenticated_request_evidence (
  exchange text NOT NULL CHECK (exchange IN ('binance','bybit')),
  host text NOT NULL,
  method text NOT NULL,
  path text NOT NULL,
  field_names text[] NOT NULL,
  enumerated_fields jsonb NOT NULL,
  request_hash sha256_hex NOT NULL,
  configuration_id text NOT NULL CHECK (
    length(configuration_id) BETWEEN 1 AND 128
  ),
  recorded_at timestamptz NOT NULL,
  PRIMARY KEY (exchange,request_hash),
  CHECK (
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
    )
  ),
  CHECK (v1c_authenticated_evidence_fields_valid(exchange,field_names)),
  CHECK (
    v1c_authenticated_evidence_enumerations_valid(
      exchange,field_names,enumerated_fields
    )
  )
);
CREATE TRIGGER v1c_authenticated_request_evidence_immutable
  BEFORE UPDATE OR DELETE ON v1c_authenticated_request_evidence
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();

CREATE TABLE v1c_account_snapshots (
  id text PRIMARY KEY,
  account_id text NOT NULL,
  account_epoch bigint NOT NULL,
  balances_payload jsonb NOT NULL,
  orders_hash sha256_hex NOT NULL,
  fills_hash sha256_hex NOT NULL,
  snapshot_hash sha256_hex NOT NULL,
  observed_at timestamptz NOT NULL,
  FOREIGN KEY (account_id,account_epoch) REFERENCES v1c_account_epochs(account_id,epoch),
  UNIQUE (account_id,account_epoch,snapshot_hash)
);

CREATE TABLE v1c_daily_cap_counters (
  utc_day date PRIMARY KEY,
  reserved_notional financial_amount NOT NULL CHECK (
    reserved_notional >= 0 AND reserved_notional <= 50
  ),
  revision bigint NOT NULL CHECK (revision > 0),
  updated_at timestamptz NOT NULL
);

CREATE TABLE v1c_submission_plans (
  id text PRIMARY KEY,
  sandbox_session_id text NOT NULL REFERENCES v1c_sandbox_sessions(id),
  arm_id text NOT NULL REFERENCES v1c_sandbox_arms(id),
  approval_hash sha256_hex NOT NULL,
  intent_kind text NOT NULL CHECK (intent_kind IN ('CANARY','STRATEGY')),
  intent_hash sha256_hex NOT NULL,
  allocator_hash sha256_hex NOT NULL,
  risk_hash sha256_hex NOT NULL,
  planner_hash sha256_hex NOT NULL,
  asset_approval_hash sha256_hex NOT NULL,
  policy_hash sha256_hex NOT NULL,
  configuration_id text NOT NULL,
  leg_count integer NOT NULL CHECK (leg_count BETWEEN 1 AND 2),
  dispatch_policy text NOT NULL CHECK (dispatch_policy IN ('sequential','concurrent')),
  state text NOT NULL CHECK (state IN (
    'APPROVED','ACTIVE','COMPLETED','FAILED','RECOVERY_REQUIRED','QUARANTINED'
  )),
  final_disposition text,
  approved_at timestamptz NOT NULL,
  saga_revision bigint NOT NULL CHECK (saga_revision > 0),
  revision bigint NOT NULL CHECK (revision > 0)
);

CREATE TABLE v1c_plan_eligibility (
  plan_id text NOT NULL REFERENCES v1c_submission_plans(id),
  exchange text NOT NULL CHECK (exchange IN ('binance','bybit')),
  snapshot jsonb NOT NULL,
  observed_at timestamptz NOT NULL,
  PRIMARY KEY (plan_id,exchange),
  CHECK (snapshot ? 'eligible' AND (snapshot->>'eligible')::boolean),
  CHECK (snapshot ? 'exchange' AND snapshot->>'exchange'=exchange)
);

CREATE TABLE v1c_plan_entry_safety (
  plan_id text NOT NULL REFERENCES v1c_submission_plans(id),
  account_id text NOT NULL,
  account_epoch bigint NOT NULL,
  exchange text NOT NULL CHECK (exchange IN ('binance','bybit')),
  state text NOT NULL CHECK (state='ARMED'),
  arm_active boolean NOT NULL CHECK (arm_active),
  global_integration_enabled boolean NOT NULL CHECK (global_integration_enabled),
  global_submission_enabled boolean NOT NULL CHECK (global_submission_enabled),
  exchange_integration_enabled boolean NOT NULL CHECK (exchange_integration_enabled),
  exchange_submission_enabled boolean NOT NULL CHECK (exchange_submission_enabled),
  public_eligible boolean NOT NULL CHECK (public_eligible),
  private_stream_healthy boolean NOT NULL CHECK (private_stream_healthy),
  account_state_fresh boolean NOT NULL CHECK (account_state_fresh),
  reconciliation_clean boolean NOT NULL CHECK (reconciliation_clean),
  lease_held boolean NOT NULL CHECK (lease_held),
  evidence_healthy boolean NOT NULL CHECK (evidence_healthy),
  open_capacity_available boolean NOT NULL CHECK (open_capacity_available),
  daily_capacity_available boolean NOT NULL CHECK (daily_capacity_available),
  observed_at timestamptz NOT NULL,
  PRIMARY KEY (plan_id,account_id),
  FOREIGN KEY (account_id,account_epoch)
    REFERENCES v1c_account_epochs(account_id,epoch)
);

CREATE TABLE v1c_sandbox_reservations (
  id text PRIMARY KEY,
  plan_id text NOT NULL REFERENCES v1c_submission_plans(id),
  account_id text NOT NULL,
  account_epoch bigint NOT NULL,
  order_id text NOT NULL UNIQUE,
  asset_symbol text NOT NULL REFERENCES assets(symbol),
  quantity financial_amount NOT NULL CHECK (quantity > 0),
  state text NOT NULL CHECK (state IN ('ACTIVE','CONSUMED','RELEASED','QUARANTINED')),
  released_at timestamptz,
  release_reason text,
  created_at timestamptz NOT NULL,
  FOREIGN KEY (account_id,account_epoch) REFERENCES v1c_account_epochs(account_id,epoch),
  CHECK (
    (state IN ('ACTIVE','QUARANTINED') AND released_at IS NULL AND release_reason IS NULL) OR
    (state IN ('CONSUMED','RELEASED') AND released_at IS NOT NULL AND release_reason IS NOT NULL)
  )
);

CREATE TABLE v1c_submission_outbox (
  id text PRIMARY KEY,
  plan_id text NOT NULL REFERENCES v1c_submission_plans(id),
  account_id text NOT NULL,
  account_epoch bigint NOT NULL,
  order_id text NOT NULL UNIQUE,
  client_order_id text NOT NULL,
  strategy_id text NOT NULL CHECK (strategy_id IN (
    'strategy:trend','strategy:mean-reversion','strategy:triangular',
    'strategy:cross-exchange-arbitrage','strategy:sandbox-canary'
  )),
  instrument text NOT NULL CHECK (instrument IN ('BTCUSDT','ETHUSDT','ETHBTC')),
  side text NOT NULL CHECK (side IN ('buy','sell')),
  quantity financial_amount NOT NULL CHECK (quantity > 0),
  limit_price financial_amount NOT NULL CHECK (limit_price > 0),
  reserved_notional financial_amount NOT NULL CHECK (
    reserved_notional > 0 AND reserved_notional <= 10
  ),
  order_style text NOT NULL CHECK (order_style IN ('LIMIT_GTC','LIMIT_IOC','POST_ONLY')),
  intent_action text NOT NULL CHECK (intent_action IN ('ENTRY','EXIT','CANCEL','RECOVERY')),
  request_hash sha256_hex NOT NULL,
  policy_hash sha256_hex NOT NULL,
  state text NOT NULL CHECK (state IN ('PENDING','CLAIMED','ACKNOWLEDGED','UNKNOWN','TERMINAL')),
  order_state text NOT NULL CHECK (order_state IN (
    'APPROVED','SUBMITTING','ACKNOWLEDGED','PARTIALLY_FILLED','FILLED',
    'CANCEL_PENDING','CANCELED','REJECTED','EXPIRED','UNKNOWN','RECOVERY_REQUIRED'
  )),
  claim_owner text,
  fencing_token bigint CHECK (fencing_token > 0),
  claim_expires_at timestamptz,
  attempt integer NOT NULL DEFAULT 0 CHECK (attempt >= 0),
  approved_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  FOREIGN KEY (account_id,account_epoch) REFERENCES v1c_account_epochs(account_id,epoch),
  UNIQUE (account_id,account_epoch,client_order_id),
  CHECK (
    (state='CLAIMED' AND claim_owner IS NOT NULL AND fencing_token IS NOT NULL AND claim_expires_at IS NOT NULL) OR
    (state<>'CLAIMED')
  )
);
CREATE UNIQUE INDEX v1c_one_nonterminal_per_account
  ON v1c_submission_outbox(account_id)
  WHERE state IN ('PENDING','CLAIMED','ACKNOWLEDGED','UNKNOWN');
CREATE INDEX v1c_submission_claim_idx
  ON v1c_submission_outbox(account_id,state,updated_at,id)
  WHERE state IN ('PENDING','CLAIMED');

CREATE TABLE v1c_private_inbox (
  id text PRIMARY KEY,
  account_id text NOT NULL,
  account_epoch bigint NOT NULL,
  event_identity text NOT NULL,
  event_kind text NOT NULL CHECK (event_kind IN ('order','fill','balance')),
  order_id text,
  client_order_id text,
  native_order_hash sha256_hex NOT NULL,
  native_fill_hash sha256_hex,
  balance_hash sha256_hex,
  canonical_event jsonb,
  occurred_at timestamptz NOT NULL,
  received_at timestamptz NOT NULL,
  reduced_at timestamptz,
  FOREIGN KEY (account_id,account_epoch) REFERENCES v1c_account_epochs(account_id,epoch),
  UNIQUE (account_id,account_epoch,event_identity),
  CHECK (received_at >= occurred_at)
);

CREATE TABLE v1c_exchange_fills (
  account_id text NOT NULL,
  account_epoch bigint NOT NULL,
  native_fill_id_hash sha256_hex NOT NULL,
  order_id text NOT NULL,
  canonical_fill jsonb NOT NULL,
  occurred_at timestamptz NOT NULL,
  FOREIGN KEY (account_id,account_epoch) REFERENCES v1c_account_epochs(account_id,epoch),
  PRIMARY KEY (account_id,account_epoch,native_fill_id_hash)
);
CREATE UNIQUE INDEX v1c_account_native_fill_unique
  ON v1c_exchange_fills(account_id,native_fill_id_hash);

CREATE TABLE v1c_exchange_metadata (
  exchange text NOT NULL CHECK (exchange IN ('binance','bybit')),
  instrument text NOT NULL CHECK (instrument IN ('BTCUSDT','ETHUSDT','ETHBTC')),
  metadata_hash sha256_hex NOT NULL,
  canonical_filters jsonb NOT NULL,
  observed_at timestamptz NOT NULL,
  PRIMARY KEY (exchange,instrument,metadata_hash)
);

CREATE TABLE v1c_reconciliations (
  id text PRIMARY KEY,
  account_id text NOT NULL,
  account_epoch bigint NOT NULL,
  state text NOT NULL CHECK (state IN ('clean','quarantined')),
  evidence_hash sha256_hex NOT NULL,
  reconciled_at timestamptz NOT NULL,
  FOREIGN KEY (account_id,account_epoch) REFERENCES v1c_account_epochs(account_id,epoch),
  UNIQUE (account_id,account_epoch,evidence_hash)
);
ALTER TABLE v1c_credential_rotations
  ADD CONSTRAINT v1c_rotation_reconciliation_fk
  FOREIGN KEY (reconciliation_id) REFERENCES v1c_reconciliations(id);

CREATE TABLE v1c_risk_unlocks (
  id text PRIMARY KEY,
  account_id text NOT NULL REFERENCES v1c_exchange_accounts(id),
  account_epoch bigint NOT NULL,
  authorization_id text NOT NULL UNIQUE REFERENCES v1c_sandbox_authorizations(id),
  actor_user_id text NOT NULL REFERENCES users(id),
  actor_session_id text NOT NULL REFERENCES sessions(id),
  source_hash sha256_hex NOT NULL,
  reason_hash sha256_hex NOT NULL,
  reconciliation_id text NOT NULL REFERENCES v1c_reconciliations(id),
  prior_state text NOT NULL CHECK (prior_state IN ('LOCKED','QUARANTINED')),
  resulting_state text NOT NULL CHECK (resulting_state='READY_PAUSED'),
  unlocked_at timestamptz NOT NULL,
  revision bigint NOT NULL CHECK (revision > 0),
  FOREIGN KEY (account_id,account_epoch) REFERENCES v1c_account_epochs(account_id,epoch)
);

CREATE FUNCTION enforce_v1c_risk_unlock_authorization() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  authorization_user text;
  authorization_session text;
  authorization_purpose text;
  authorization_source sha256_hex;
  authorization_reason sha256_hex;
  authorization_expires timestamptz;
  authorization_consumed timestamptz;
BEGIN
  SELECT user_id,session_id,purpose,source_hash,reason_hash,expires_at,consumed_at
  INTO authorization_user,authorization_session,authorization_purpose,
       authorization_source,authorization_reason,authorization_expires,
       authorization_consumed
  FROM v1c_sandbox_authorizations
  WHERE id=NEW.authorization_id
  FOR SHARE;

  IF authorization_purpose IS DISTINCT FROM 'risk_unlock' OR
     authorization_user IS DISTINCT FROM NEW.actor_user_id OR
     authorization_session IS DISTINCT FROM NEW.actor_session_id OR
     authorization_source IS DISTINCT FROM NEW.source_hash OR
     authorization_reason IS DISTINCT FROM NEW.reason_hash OR
     authorization_consumed IS NULL OR
     NEW.unlocked_at < authorization_consumed OR
     NEW.unlocked_at > authorization_expires OR
     NOT EXISTS (
       SELECT 1
       FROM sessions session
       JOIN users actor ON actor.id=session.user_id
       WHERE session.id=NEW.actor_session_id
         AND session.user_id=NEW.actor_user_id
         AND actor.status='active'
         AND session.revoked_at IS NULL
         AND session.expires_at>NEW.unlocked_at
         AND session.idle_expires_at>NEW.unlocked_at
       FOR SHARE OF session,actor
     ) THEN
    RAISE EXCEPTION 'v1c_risk_unlock_authorization_invalid';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER v1c_risk_unlock_authorized
  BEFORE INSERT ON v1c_risk_unlocks
  FOR EACH ROW EXECUTE FUNCTION enforce_v1c_risk_unlock_authorization();

CREATE TABLE v1c_reconciliation_differences (
  id text PRIMARY KEY,
  reconciliation_id text NOT NULL REFERENCES v1c_reconciliations(id),
  account_id text NOT NULL,
  account_epoch bigint NOT NULL,
  category text NOT NULL,
  classification text NOT NULL,
  expected_hash sha256_hex NOT NULL,
  actual_hash sha256_hex NOT NULL,
  asset_symbol text REFERENCES assets(symbol),
  quantity signed_financial_amount,
  critical boolean NOT NULL,
  state text NOT NULL CHECK (state IN ('OPEN','RESOLVED','QUARANTINED','ADJUSTED')),
  recorded_at timestamptz NOT NULL,
  FOREIGN KEY (account_id,account_epoch) REFERENCES v1c_account_epochs(account_id,epoch)
);

CREATE TABLE v1c_reset_incidents (
  id text PRIMARY KEY,
  account_id text NOT NULL REFERENCES v1c_exchange_accounts(id),
  prior_epoch bigint NOT NULL,
  new_epoch bigint NOT NULL,
  evidence_hash sha256_hex NOT NULL,
  state text NOT NULL CHECK (state IN ('OPEN','RECONCILING','RESOLVED','QUARANTINED')),
  detected_at timestamptz NOT NULL,
  resolved_at timestamptz,
  UNIQUE (account_id,new_epoch),
  FOREIGN KEY (account_id,prior_epoch) REFERENCES v1c_account_epochs(account_id,epoch),
  FOREIGN KEY (account_id,new_epoch) REFERENCES v1c_account_epochs(account_id,epoch),
  CHECK (new_epoch=prior_epoch+1)
);

CREATE TABLE v1c_external_adjustments (
  id text PRIMARY KEY,
  reset_incident_id text NOT NULL REFERENCES v1c_reset_incidents(id),
  account_id text NOT NULL REFERENCES v1c_exchange_accounts(id),
  asset_symbol text NOT NULL REFERENCES assets(symbol),
  quantity signed_financial_amount NOT NULL CHECK (quantity<>0),
  adjustment_hash sha256_hex NOT NULL,
  pnl_effect boolean NOT NULL CHECK (NOT pnl_effect),
  recorded_at timestamptz NOT NULL
);

CREATE TABLE v1c_account_leases (
  account_id text PRIMARY KEY REFERENCES v1c_exchange_accounts(id),
  environment text NOT NULL CHECK (environment IN ('spot_testnet','demo')),
  owner text NOT NULL,
  fencing_token bigint NOT NULL CHECK (fencing_token > 0),
  acquired_at timestamptz NOT NULL,
  expires_at timestamptz NOT NULL,
  CHECK (expires_at > acquired_at),
  UNIQUE (account_id,environment,fencing_token)
);

CREATE FUNCTION protect_v1c_daily_cap() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP='DELETE' OR NEW.utc_day<>OLD.utc_day OR
     NEW.reserved_notional<OLD.reserved_notional OR NEW.revision<>OLD.revision+1 OR
     NEW.updated_at<OLD.updated_at THEN
    RAISE EXCEPTION 'v1c_daily_cap_non_refundable';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER v1c_daily_cap_non_refundable
  BEFORE UPDATE OR DELETE ON v1c_daily_cap_counters
  FOR EACH ROW EXECUTE FUNCTION protect_v1c_daily_cap();

CREATE FUNCTION enforce_v1c_open_order_capacity() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  global_open bigint;
  account_open bigint;
BEGIN
  IF NEW.state NOT IN ('PENDING','CLAIMED','ACKNOWLEDGED','UNKNOWN') THEN
    RETURN NEW;
  END IF;
  PERFORM pg_advisory_xact_lock(hashtext('axiom:v1c:open-order-capacity'));
  SELECT count(*) INTO global_open
  FROM v1c_submission_outbox
  WHERE state IN ('PENDING','CLAIMED','ACKNOWLEDGED','UNKNOWN')
    AND id<>NEW.id;
  SELECT count(*) INTO account_open
  FROM v1c_submission_outbox
  WHERE account_id=NEW.account_id
    AND state IN ('PENDING','CLAIMED','ACKNOWLEDGED','UNKNOWN')
    AND id<>NEW.id;
  IF global_open>=2 OR account_open>=1 THEN
    RAISE EXCEPTION 'v1c_open_order_capacity_exceeded';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER v1c_open_order_capacity
  BEFORE INSERT OR UPDATE ON v1c_submission_outbox
  FOR EACH ROW EXECUTE FUNCTION enforce_v1c_open_order_capacity();

CREATE FUNCTION protect_v1c_submission_outbox() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP='DELETE' OR
     NEW.id IS DISTINCT FROM OLD.id OR
     NEW.plan_id IS DISTINCT FROM OLD.plan_id OR
     NEW.account_id IS DISTINCT FROM OLD.account_id OR
     NEW.account_epoch IS DISTINCT FROM OLD.account_epoch OR
     NEW.order_id IS DISTINCT FROM OLD.order_id OR
     NEW.client_order_id IS DISTINCT FROM OLD.client_order_id OR
     NEW.strategy_id IS DISTINCT FROM OLD.strategy_id OR
     NEW.instrument IS DISTINCT FROM OLD.instrument OR
     NEW.side IS DISTINCT FROM OLD.side OR
     NEW.quantity IS DISTINCT FROM OLD.quantity OR
     NEW.limit_price IS DISTINCT FROM OLD.limit_price OR
     NEW.reserved_notional IS DISTINCT FROM OLD.reserved_notional OR
     NEW.order_style IS DISTINCT FROM OLD.order_style OR
     NEW.intent_action IS DISTINCT FROM OLD.intent_action OR
     NEW.request_hash IS DISTINCT FROM OLD.request_hash OR
     NEW.policy_hash IS DISTINCT FROM OLD.policy_hash OR
     NEW.approved_at IS DISTINCT FROM OLD.approved_at OR
     NEW.attempt<OLD.attempt OR NEW.updated_at<OLD.updated_at OR
     (OLD.fencing_token IS NOT NULL AND
      NEW.fencing_token IS NOT NULL AND NEW.fencing_token<OLD.fencing_token) OR
     OLD.state='TERMINAL' OR
     NOT (
       NEW.state=OLD.state OR
       (OLD.state='PENDING' AND NEW.state IN ('CLAIMED','UNKNOWN')) OR
       (OLD.state='CLAIMED' AND NEW.state IN ('ACKNOWLEDGED','UNKNOWN','TERMINAL')) OR
       (OLD.state='ACKNOWLEDGED' AND NEW.state IN ('UNKNOWN','TERMINAL')) OR
       (OLD.state='UNKNOWN' AND NEW.state IN ('ACKNOWLEDGED','TERMINAL'))
     ) OR
     NOT (
       NEW.order_state=OLD.order_state OR
       NEW.order_state='RECOVERY_REQUIRED' OR
       (OLD.order_state='APPROVED' AND NEW.order_state='SUBMITTING') OR
       (OLD.order_state='SUBMITTING' AND NEW.order_state IN (
         'ACKNOWLEDGED','PARTIALLY_FILLED','FILLED','REJECTED','EXPIRED','UNKNOWN'
       )) OR
       (OLD.order_state='ACKNOWLEDGED' AND NEW.order_state IN (
         'PARTIALLY_FILLED','FILLED','CANCEL_PENDING','CANCELED','EXPIRED','UNKNOWN'
       )) OR
       (OLD.order_state='PARTIALLY_FILLED' AND NEW.order_state IN (
         'FILLED','CANCEL_PENDING','CANCELED','EXPIRED','UNKNOWN'
       )) OR
       (OLD.order_state='CANCEL_PENDING' AND NEW.order_state IN (
         'PARTIALLY_FILLED','FILLED','CANCELED','UNKNOWN'
       )) OR
       (OLD.order_state IN ('UNKNOWN','RECOVERY_REQUIRED') AND
        NEW.order_state='CANCEL_PENDING') OR
       (OLD.order_state='UNKNOWN' AND NEW.order_state IN (
         'ACKNOWLEDGED','PARTIALLY_FILLED','FILLED','CANCELED','REJECTED','EXPIRED'
       )) OR
       (OLD.order_state IN ('CANCELED','EXPIRED') AND NEW.order_state IN (
         'PARTIALLY_FILLED','FILLED'
       ))
     ) THEN
    RAISE EXCEPTION 'v1c_submission_outbox_mutation_rejected';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER v1c_submission_outbox_protected
  BEFORE UPDATE OR DELETE ON v1c_submission_outbox
  FOR EACH ROW EXECUTE FUNCTION protect_v1c_submission_outbox();

CREATE FUNCTION protect_v1c_submission_plan() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP='DELETE' OR
     NEW.id IS DISTINCT FROM OLD.id OR
     NEW.sandbox_session_id IS DISTINCT FROM OLD.sandbox_session_id OR
     NEW.arm_id IS DISTINCT FROM OLD.arm_id OR
     NEW.approval_hash IS DISTINCT FROM OLD.approval_hash OR
     NEW.intent_kind IS DISTINCT FROM OLD.intent_kind OR
     NEW.intent_hash IS DISTINCT FROM OLD.intent_hash OR
     NEW.allocator_hash IS DISTINCT FROM OLD.allocator_hash OR
     NEW.risk_hash IS DISTINCT FROM OLD.risk_hash OR
     NEW.planner_hash IS DISTINCT FROM OLD.planner_hash OR
     NEW.asset_approval_hash IS DISTINCT FROM OLD.asset_approval_hash OR
     NEW.policy_hash IS DISTINCT FROM OLD.policy_hash OR
     NEW.configuration_id IS DISTINCT FROM OLD.configuration_id OR
     NEW.leg_count IS DISTINCT FROM OLD.leg_count OR
     NEW.dispatch_policy IS DISTINCT FROM OLD.dispatch_policy OR
     NEW.approved_at IS DISTINCT FROM OLD.approved_at OR
     NEW.revision<>OLD.revision+1 OR
     NEW.saga_revision<>OLD.saga_revision+1 OR
     OLD.state IN ('COMPLETED','FAILED','QUARANTINED') OR
     NOT (
       NEW.state=OLD.state OR
       (OLD.state='APPROVED' AND NEW.state IN (
         'ACTIVE','FAILED','RECOVERY_REQUIRED','QUARANTINED'
       )) OR
       (OLD.state='ACTIVE' AND NEW.state IN (
         'COMPLETED','FAILED','RECOVERY_REQUIRED','QUARANTINED'
       )) OR
       (OLD.state='RECOVERY_REQUIRED' AND NEW.state IN (
         'ACTIVE','COMPLETED','FAILED','QUARANTINED'
       ))
     ) THEN
    RAISE EXCEPTION 'v1c_submission_plan_mutation_rejected';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER v1c_submission_plan_protected
  BEFORE UPDATE OR DELETE ON v1c_submission_plans
  FOR EACH ROW EXECUTE FUNCTION protect_v1c_submission_plan();
CREATE TRIGGER v1c_plan_eligibility_immutable
  BEFORE UPDATE OR DELETE ON v1c_plan_eligibility
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER v1c_plan_entry_safety_immutable
  BEFORE UPDATE OR DELETE ON v1c_plan_entry_safety
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();

CREATE FUNCTION protect_v1c_reservation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP='DELETE' OR
     NEW.id IS DISTINCT FROM OLD.id OR
     NEW.plan_id IS DISTINCT FROM OLD.plan_id OR
     NEW.account_id IS DISTINCT FROM OLD.account_id OR
     NEW.account_epoch IS DISTINCT FROM OLD.account_epoch OR
     NEW.order_id IS DISTINCT FROM OLD.order_id OR
     NEW.asset_symbol IS DISTINCT FROM OLD.asset_symbol OR
     NEW.quantity IS DISTINCT FROM OLD.quantity OR
     NEW.created_at IS DISTINCT FROM OLD.created_at OR
     OLD.state<>'ACTIVE' OR
     NEW.state NOT IN ('CONSUMED','RELEASED','QUARANTINED') THEN
    RAISE EXCEPTION 'v1c_reservation_mutation_rejected';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER v1c_reservation_protected
  BEFORE UPDATE OR DELETE ON v1c_sandbox_reservations
  FOR EACH ROW EXECUTE FUNCTION protect_v1c_reservation();

CREATE FUNCTION protect_v1c_private_inbox() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP='DELETE' OR
     (to_jsonb(NEW)-'reduced_at') IS DISTINCT FROM
       (to_jsonb(OLD)-'reduced_at') OR
     OLD.reduced_at IS NOT NULL OR NEW.reduced_at IS NULL THEN
    RAISE EXCEPTION 'v1c_private_inbox_mutation_rejected';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER v1c_private_inbox_protected
  BEFORE UPDATE OR DELETE ON v1c_private_inbox
  FOR EACH ROW EXECUTE FUNCTION protect_v1c_private_inbox();

CREATE FUNCTION protect_v1c_sandbox_arm() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP='DELETE' OR
     (to_jsonb(NEW)-ARRAY['revoked_at','revision']) IS DISTINCT FROM
       (to_jsonb(OLD)-ARRAY['revoked_at','revision']) OR
     OLD.revoked_at IS NOT NULL OR NEW.revoked_at IS NULL OR
     NEW.revision<>OLD.revision+1 THEN
    RAISE EXCEPTION 'v1c_sandbox_arm_mutation_rejected';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER v1c_sandbox_arm_protected
  BEFORE UPDATE OR DELETE ON v1c_sandbox_arms
  FOR EACH ROW EXECUTE FUNCTION protect_v1c_sandbox_arm();

CREATE TRIGGER v1c_account_epochs_immutable
  BEFORE DELETE ON v1c_account_epochs
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER v1c_credential_generations_immutable
  BEFORE UPDATE OR DELETE ON v1c_credential_generations
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER v1c_account_snapshots_immutable
  BEFORE UPDATE OR DELETE ON v1c_account_snapshots
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER v1c_exchange_fills_immutable
  BEFORE UPDATE OR DELETE ON v1c_exchange_fills
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER v1c_external_adjustments_immutable
  BEFORE UPDATE OR DELETE ON v1c_external_adjustments
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER v1c_reconciliations_immutable
  BEFORE UPDATE OR DELETE ON v1c_reconciliations
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER v1c_reconciliation_differences_immutable
  BEFORE UPDATE OR DELETE ON v1c_reconciliation_differences
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER v1c_risk_unlocks_immutable
  BEFORE UPDATE OR DELETE ON v1c_risk_unlocks
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
