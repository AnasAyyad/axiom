SET TIME ZONE 'UTC';

CREATE TABLE portfolio_ownership (
  account_id text PRIMARY KEY REFERENCES virtual_accounts(id),
  portfolio_id text NOT NULL REFERENCES portfolios(id),
  exchange_id text NOT NULL REFERENCES exchanges(id),
  strategy_version_id text NOT NULL REFERENCES strategy_versions(id),
  strategy_key text NOT NULL,
  initialization_transaction_id text NOT NULL UNIQUE REFERENCES journal_transactions(id),
  numeraire_asset text NOT NULL REFERENCES assets(symbol),
  ownership_hash sha256_hex NOT NULL UNIQUE,
  created_at timestamptz NOT NULL,
  UNIQUE (portfolio_id, exchange_id, strategy_version_id),
  CHECK (strategy_key = 'trend'),
  CHECK (numeraire_asset = 'USDT')
);

ALTER TABLE positions ADD COLUMN cost financial_amount NOT NULL DEFAULT 0;
ALTER TABLE positions ADD COLUMN unrealized_pnl signed_financial_amount NOT NULL DEFAULT 0;

ALTER TABLE account_snapshots ADD COLUMN ownership_hash sha256_hex;
ALTER TABLE account_snapshots ADD COLUMN balances_hash sha256_hex;
ALTER TABLE account_snapshots ADD COLUMN positions_hash sha256_hex;
ALTER TABLE account_snapshots ADD COLUMN reservations_hash sha256_hex;
ALTER TABLE account_snapshots ADD COLUMN risk_state_hash sha256_hex;

CREATE TABLE allocation_candidates (
  id text PRIMARY KEY,
  account_id text NOT NULL REFERENCES virtual_accounts(id),
  instrument_id text NOT NULL REFERENCES instruments(id),
  side text NOT NULL CHECK (side IN ('buy','sell')),
  quantity financial_amount NOT NULL CHECK (quantity > 0),
  notional financial_amount NOT NULL CHECK (notional > 0),
  aggregate_score signed_financial_amount NOT NULL,
  base_eligibility_version bigint NOT NULL CHECK (base_eligibility_version > 0),
  quote_eligibility_version bigint NOT NULL CHECK (quote_eligibility_version > 0),
  state text NOT NULL CHECK (state IN ('ranked','reserved','rejected','settled','released','expired','quarantined')),
  reason_code text NOT NULL,
  revision bigint NOT NULL CHECK (revision > 0),
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL
);

CREATE TABLE allocation_score_components (
  candidate_id text NOT NULL REFERENCES allocation_candidates(id),
  component_name text NOT NULL,
  component_value signed_financial_amount NOT NULL,
  ordinal integer NOT NULL CHECK (ordinal >= 0),
  PRIMARY KEY (candidate_id, component_name),
  UNIQUE (candidate_id, ordinal)
);

CREATE TABLE liquidity_domains (
  id text PRIMARY KEY,
  namespace_id text NOT NULL REFERENCES model_namespaces(id),
  available_quantity financial_amount NOT NULL CHECK (available_quantity >= 0),
  revision bigint NOT NULL CHECK (revision > 0),
  updated_at timestamptz NOT NULL
);

CREATE TABLE liquidity_reservations (
  id text PRIMARY KEY,
  candidate_id text NOT NULL UNIQUE REFERENCES allocation_candidates(id),
  domain_id text NOT NULL REFERENCES liquidity_domains(id),
  quantity financial_amount NOT NULL CHECK (quantity > 0),
  remaining_quantity financial_amount NOT NULL CHECK (remaining_quantity >= 0 AND remaining_quantity <= quantity),
  state text NOT NULL CHECK (state IN ('active','consumed','released','expired','quarantined')),
  fencing_token bigint NOT NULL CHECK (fencing_token > 0),
  revision bigint NOT NULL CHECK (revision > 0),
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  CHECK ((state IN ('active','quarantined') AND remaining_quantity > 0) OR
         (state IN ('consumed','released','expired') AND remaining_quantity = 0))
);

CREATE TABLE allocation_reservations (
  candidate_id text NOT NULL REFERENCES allocation_candidates(id),
  reservation_id text NOT NULL UNIQUE REFERENCES reservations(id),
  liquidity_reservation_id text NOT NULL UNIQUE REFERENCES liquidity_reservations(id),
  PRIMARY KEY (candidate_id, reservation_id, liquidity_reservation_id)
);

CREATE FUNCTION enforce_liquidity_reservation_transition() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.state <> 'active' OR NEW.revision <> 1 OR NEW.remaining_quantity <> NEW.quantity OR
       NEW.updated_at < NEW.created_at THEN
      RAISE EXCEPTION 'invalid_liquidity_reservation_initial_state';
    END IF;
    RETURN NEW;
  END IF;
  IF (to_jsonb(NEW) - 'state' - 'remaining_quantity' - 'revision' - 'updated_at') IS DISTINCT FROM
     (to_jsonb(OLD) - 'state' - 'remaining_quantity' - 'revision' - 'updated_at') OR
     OLD.state <> 'active' OR NEW.revision <> OLD.revision + 1 OR NEW.updated_at < OLD.updated_at THEN
    RAISE EXCEPTION 'invalid_liquidity_reservation_transition';
  END IF;
  IF NEW.state = 'active' THEN
    IF NEW.remaining_quantity <= 0 OR NEW.remaining_quantity >= OLD.remaining_quantity THEN
      RAISE EXCEPTION 'invalid_liquidity_partial_fill';
    END IF;
  ELSIF NEW.state = 'quarantined' THEN
    IF NEW.remaining_quantity <> OLD.remaining_quantity THEN
      RAISE EXCEPTION 'invalid_liquidity_quarantine';
    END IF;
  ELSIF NEW.state NOT IN ('consumed','released','expired') OR NEW.remaining_quantity <> 0 THEN
    RAISE EXCEPTION 'invalid_liquidity_reservation_transition';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER liquidity_reservations_follow_state_machine
  BEFORE INSERT OR UPDATE ON liquidity_reservations
  FOR EACH ROW EXECUTE FUNCTION enforce_liquidity_reservation_transition();

CREATE TABLE risk_policies (
  id text NOT NULL,
  version bigint NOT NULL CHECK (version > 0),
  scope_kind text NOT NULL CHECK (scope_kind IN ('global','exchange_account','exchange','strategy','portfolio','asset','instrument')),
  scope_id text NOT NULL,
  state text NOT NULL CHECK (state IN ('NORMAL','CAUTIOUS','PAUSED','LOCKED')),
  policy_hash sha256_hex NOT NULL,
  canonical_payload bytea NOT NULL,
  effective_at timestamptz NOT NULL,
  recorded_at timestamptz NOT NULL,
  PRIMARY KEY (id, version),
  UNIQUE (scope_kind, scope_id, version),
  CHECK (recorded_at >= effective_at)
);

CREATE TABLE risk_policy_limits (
  policy_id text NOT NULL,
  policy_version bigint NOT NULL,
  account_drawdown financial_amount NOT NULL,
  utc_day_loss financial_amount NOT NULL,
  rolling_24_hour_loss financial_amount NOT NULL,
  strategy_loss financial_amount NOT NULL,
  asset_exposure financial_amount NOT NULL,
  combined_exposure financial_amount NOT NULL,
  exchange_exposure financial_amount NOT NULL,
  minimum_reserve financial_amount NOT NULL,
  maximum_reserved_capital financial_amount NOT NULL,
  maximum_spread financial_amount NOT NULL,
  maximum_slippage financial_amount NOT NULL,
  maximum_open_orders integer NOT NULL CHECK (maximum_open_orders >= 0),
  maximum_book_age_microseconds bigint NOT NULL CHECK (maximum_book_age_microseconds >= 0),
  maximum_queue_lag_microseconds bigint NOT NULL CHECK (maximum_queue_lag_microseconds >= 0),
  maximum_clock_drift_microseconds bigint NOT NULL CHECK (maximum_clock_drift_microseconds >= 0),
  minimum_quality_score integer NOT NULL CHECK (minimum_quality_score BETWEEN 0 AND 100),
  PRIMARY KEY (policy_id, policy_version),
  FOREIGN KEY (policy_id, policy_version) REFERENCES risk_policies(id, version)
);

CREATE TABLE risk_state_events (
  id text PRIMARY KEY,
  prior_state text NOT NULL CHECK (prior_state IN ('NORMAL','CAUTIOUS','PAUSED','LOCKED')),
  next_state text NOT NULL CHECK (next_state IN ('NORMAL','CAUTIOUS','PAUSED','LOCKED')),
  reason_code text NOT NULL,
  actor text NOT NULL,
  evidence_hash sha256_hex NOT NULL,
  occurred_at timestamptz NOT NULL,
  CHECK (prior_state <> next_state)
);

ALTER TABLE risk_evaluations ADD COLUMN action text NOT NULL DEFAULT 'reject'
  CHECK (action IN ('approve','reject','pause_strategy','pause_instrument','pause_exchange','lock_engine','quarantine'));
ALTER TABLE risk_evaluations ADD COLUMN effective_state text NOT NULL DEFAULT 'PAUSED'
  CHECK (effective_state IN ('NORMAL','CAUTIOUS','PAUSED','LOCKED'));
ALTER TABLE risk_evaluations ADD COLUMN observation_hash sha256_hex;
ALTER TABLE risk_evaluations ADD COLUMN canonical_payload bytea;

CREATE TABLE risk_evaluation_policies (
  evaluation_id text NOT NULL REFERENCES risk_evaluations(id),
  policy_id text NOT NULL,
  policy_version bigint NOT NULL,
  precedence integer NOT NULL CHECK (precedence >= 0),
  PRIMARY KEY (evaluation_id, policy_id, policy_version),
  UNIQUE (evaluation_id, precedence),
  FOREIGN KEY (policy_id, policy_version) REFERENCES risk_policies(id, version)
);

CREATE TABLE circuit_breaker_events (
  id text PRIMARY KEY,
  breaker_kind text NOT NULL CHECK (breaker_kind IN (
    'gap_or_stale_data','reconciliation_mismatch','unknown_order','loss_or_drawdown',
    'excessive_slippage','persistence_failure','disk_failure','clock_drift',
    'api_failure','queue_lag','lease_loss'
  )),
  scope_kind text NOT NULL CHECK (scope_kind IN ('global','exchange_account','exchange','strategy','portfolio','asset','instrument')),
  scope_id text NOT NULL,
  action text NOT NULL CHECK (action IN ('pause_strategy','pause_instrument','pause_exchange','lock_engine','quarantine')),
  resulting_state text NOT NULL CHECK (resulting_state IN ('CAUTIOUS','PAUSED','LOCKED')),
  evidence_hash sha256_hex NOT NULL,
  occurred_at timestamptz NOT NULL
);

ALTER TABLE reconciliation_cases ADD COLUMN scope text;
ALTER TABLE reconciliation_cases ADD COLUMN expected_state_hash sha256_hex;
ALTER TABLE reconciliation_cases ADD COLUMN actual_state_hash sha256_hex;
ALTER TABLE reconciliation_cases ADD COLUMN case_hash sha256_hex;

CREATE TABLE reconciliation_differences (
  case_id text NOT NULL REFERENCES reconciliation_cases(id),
  ordinal integer NOT NULL CHECK (ordinal >= 0),
  category text NOT NULL CHECK (category IN (
    'orders','fills','reservations','balances','positions','ownership','journal','projections'
  )),
  classification text NOT NULL CHECK (classification IN ('missing_fact','duplicate_fact','inconsistent_fact','unknown_fact')),
  expected_hash sha256_hex,
  actual_hash sha256_hex,
  asset_symbol text REFERENCES assets(symbol),
  quantity financial_amount,
  critical boolean NOT NULL,
  canonical_payload bytea NOT NULL,
  PRIMARY KEY (case_id, ordinal),
  CHECK ((asset_symbol IS NULL) = (quantity IS NULL))
);

CREATE TABLE quarantined_scopes (
  scope text PRIMARY KEY,
  reason_code text NOT NULL,
  case_id text NOT NULL REFERENCES reconciliation_cases(id),
  revision bigint NOT NULL CHECK (revision > 0),
  quarantined_at timestamptz NOT NULL,
  released_at timestamptz,
  CHECK (released_at IS NULL OR released_at >= quarantined_at)
);

CREATE TABLE startup_recovery_attempts (
  id text PRIMARY KEY,
  run_id text NOT NULL REFERENCES runs(id),
  state text NOT NULL CHECK (state IN ('locked','ready_paused','failed')),
  build_hash sha256_hex NOT NULL,
  configuration_hash sha256_hex NOT NULL,
  started_at timestamptz NOT NULL,
  completed_at timestamptz,
  CHECK ((state = 'locked' AND completed_at IS NULL) OR
         (state IN ('ready_paused','failed') AND completed_at IS NOT NULL AND completed_at >= started_at))
);

CREATE TABLE startup_recovery_evidence (
  attempt_id text NOT NULL REFERENCES startup_recovery_attempts(id),
  ordinal integer NOT NULL CHECK (ordinal BETWEEN 0 AND 13),
  stage text NOT NULL CHECK (stage IN (
    'database_prerequisites','fenced_ownership','build_safety_manifest','configuration_graph',
    'schema_and_durability','checkpoint_and_cursor','protected_state','committed_event_replay',
    'journal_and_projections','simulator_reconciliation','recorder_segments','public_market_state',
    'operational_invariants','administrative_readiness'
  )),
  evidence_hash sha256_hex NOT NULL,
  recorded_at timestamptz NOT NULL,
  PRIMARY KEY (attempt_id, ordinal),
  UNIQUE (attempt_id, stage)
);

CREATE TRIGGER portfolio_ownership_immutable BEFORE UPDATE OR DELETE ON portfolio_ownership
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER allocation_score_components_immutable BEFORE UPDATE OR DELETE ON allocation_score_components
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER risk_policies_immutable BEFORE UPDATE OR DELETE ON risk_policies
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER risk_policy_limits_immutable BEFORE UPDATE OR DELETE ON risk_policy_limits
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER risk_state_events_immutable BEFORE UPDATE OR DELETE ON risk_state_events
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER risk_evaluation_policies_immutable BEFORE UPDATE OR DELETE ON risk_evaluation_policies
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER circuit_breaker_events_immutable BEFORE UPDATE OR DELETE ON circuit_breaker_events
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER reconciliation_differences_immutable BEFORE UPDATE OR DELETE ON reconciliation_differences
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER startup_recovery_evidence_immutable BEFORE UPDATE OR DELETE ON startup_recovery_evidence
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();

CREATE INDEX allocation_candidates_account_state_idx ON allocation_candidates(account_id, state, created_at);
CREATE INDEX liquidity_reservations_domain_state_idx ON liquidity_reservations(domain_id, state, created_at);
CREATE INDEX risk_policies_scope_idx ON risk_policies(scope_kind, scope_id, version DESC);
CREATE INDEX risk_evaluations_state_idx ON risk_evaluations(effective_state, evaluated_at);
CREATE INDEX circuit_breaker_scope_idx ON circuit_breaker_events(scope_kind, scope_id, occurred_at);
CREATE INDEX reconciliation_case_scope_idx ON reconciliation_cases(scope, opened_at);
