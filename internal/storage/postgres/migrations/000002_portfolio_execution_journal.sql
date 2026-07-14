SET TIME ZONE 'UTC';

CREATE TABLE portfolios (
  id text PRIMARY KEY,
  name text NOT NULL UNIQUE,
  reporting_asset text NOT NULL REFERENCES assets(symbol),
  created_at timestamptz NOT NULL
);

CREATE TABLE strategy_portfolios (
  portfolio_id text NOT NULL REFERENCES portfolios(id),
  strategy_version_id text NOT NULL REFERENCES strategy_versions(id),
  allocation financial_amount NOT NULL,
  assigned_at timestamptz NOT NULL,
  PRIMARY KEY (portfolio_id, strategy_version_id)
);

CREATE TABLE virtual_accounts (
  id text PRIMARY KEY,
  portfolio_id text NOT NULL REFERENCES portfolios(id),
  run_id text NOT NULL REFERENCES runs(id),
  name text NOT NULL,
  created_at timestamptz NOT NULL,
  UNIQUE (run_id, name)
);

CREATE TABLE virtual_balances (
  account_id text NOT NULL REFERENCES virtual_accounts(id),
  asset_symbol text NOT NULL REFERENCES assets(symbol),
  available financial_amount NOT NULL,
  reserved financial_amount NOT NULL,
  revision bigint NOT NULL CHECK (revision > 0),
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (account_id, asset_symbol)
);

CREATE TABLE positions (
  account_id text NOT NULL REFERENCES virtual_accounts(id),
  instrument_id text NOT NULL REFERENCES instruments(id),
  quantity financial_amount NOT NULL,
  weighted_average_cost financial_amount NOT NULL,
  realized_pnl signed_financial_amount NOT NULL,
  revision bigint NOT NULL CHECK (revision > 0),
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (account_id, instrument_id)
);

CREATE TABLE account_snapshots (
  id text PRIMARY KEY,
  account_id text NOT NULL REFERENCES virtual_accounts(id),
  revision bigint NOT NULL CHECK (revision > 0),
  snapshot_hash sha256_hex NOT NULL,
  canonical_payload bytea NOT NULL,
  recorded_at timestamptz NOT NULL,
  UNIQUE (account_id, revision)
);

CREATE TABLE reservations (
  id text PRIMARY KEY,
  account_id text NOT NULL REFERENCES virtual_accounts(id),
  asset_symbol text NOT NULL REFERENCES assets(symbol),
  quantity financial_amount NOT NULL CHECK (quantity > 0),
  state text NOT NULL CHECK (state IN ('active','consumed','released','expired','quarantined')),
  fencing_token bigint NOT NULL CHECK (fencing_token > 0),
  revision bigint NOT NULL CHECK (revision > 0),
  order_id text,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL
);

CREATE UNIQUE INDEX reservations_active_owner_idx
  ON reservations(account_id, asset_symbol, id) WHERE state = 'active';

CREATE TABLE opportunities (
  id text PRIMARY KEY,
  run_id text NOT NULL REFERENCES runs(id),
  strategy_version_id text NOT NULL REFERENCES strategy_versions(id),
  instrument_id text NOT NULL REFERENCES instruments(id),
  configuration_id text NOT NULL REFERENCES configuration_versions(id),
  detected_at timestamptz NOT NULL,
  ingest_ordinal bigint NOT NULL CHECK (ingest_ordinal > 0),
  payload_hash sha256_hex NOT NULL
);

CREATE TABLE decisions (
  id text PRIMARY KEY,
  opportunity_id text REFERENCES opportunities(id),
  run_id text NOT NULL REFERENCES runs(id),
  configuration_id text NOT NULL REFERENCES configuration_versions(id),
  strategy_version_id text NOT NULL REFERENCES strategy_versions(id),
  outcome text NOT NULL,
  reason_code text NOT NULL,
  causation_id text NOT NULL,
  decided_at timestamptz NOT NULL,
  ingest_ordinal bigint NOT NULL CHECK (ingest_ordinal > 0)
);

CREATE TABLE decision_inputs (
  decision_id text NOT NULL REFERENCES decisions(id),
  input_kind text NOT NULL,
  input_id text NOT NULL,
  version bigint NOT NULL CHECK (version > 0),
  input_hash sha256_hex NOT NULL,
  PRIMARY KEY (decision_id, input_kind, input_id)
);

CREATE TABLE risk_evaluations (
  id text PRIMARY KEY,
  decision_id text NOT NULL REFERENCES decisions(id),
  policy_version text NOT NULL,
  outcome text NOT NULL CHECK (outcome IN ('approved','rejected','paused','locked')),
  reason_code text NOT NULL,
  evaluated_at timestamptz NOT NULL
);

CREATE TABLE execution_plans (
  id text PRIMARY KEY,
  decision_id text NOT NULL REFERENCES decisions(id),
  reservation_id text REFERENCES reservations(id),
  state text NOT NULL CHECK (state IN ('planned','active','completed','failed','recovery_required','quarantined')),
  recovery_state text NOT NULL,
  revision bigint NOT NULL CHECK (revision > 0),
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL
);

CREATE TABLE execution_plan_legs (
  plan_id text NOT NULL REFERENCES execution_plans(id),
  leg_index integer NOT NULL CHECK (leg_index >= 0),
  instrument_id text NOT NULL REFERENCES instruments(id),
  side text NOT NULL CHECK (side IN ('buy','sell')),
  quantity financial_amount NOT NULL CHECK (quantity > 0),
  dependency_index integer,
  state text NOT NULL,
  PRIMARY KEY (plan_id, leg_index)
);

CREATE TABLE recovery_attempts (
  id text PRIMARY KEY,
  plan_id text NOT NULL REFERENCES execution_plans(id),
  attempt bigint NOT NULL CHECK (attempt > 0),
  action text NOT NULL,
  state text NOT NULL CHECK (state IN ('planned','running','completed','failed','quarantined')),
  loss_asset text REFERENCES assets(symbol),
  loss_quantity financial_amount,
  causation_id text NOT NULL,
  recorded_at timestamptz NOT NULL,
  UNIQUE (plan_id, attempt),
  CHECK ((loss_asset IS NULL) = (loss_quantity IS NULL))
);

CREATE TABLE orders (
  id text PRIMARY KEY,
  plan_id text REFERENCES execution_plans(id),
  account_id text NOT NULL REFERENCES virtual_accounts(id),
  client_order_id text NOT NULL,
  account_epoch bigint NOT NULL CHECK (account_epoch > 0),
  instrument_id text NOT NULL REFERENCES instruments(id),
  side text NOT NULL CHECK (side IN ('buy','sell')),
  quantity financial_amount NOT NULL CHECK (quantity > 0),
  state text NOT NULL CHECK (state IN ('created','scheduled','open','partially_filled','filled','cancel_pending','cancelled','rejected','expired','unknown','recovery_required')),
  revision bigint NOT NULL CHECK (revision > 0),
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  UNIQUE (account_id, account_epoch, client_order_id)
);

ALTER TABLE reservations
  ADD CONSTRAINT reservations_order_fk FOREIGN KEY (order_id) REFERENCES orders(id);

CREATE TABLE order_attempts (
  id text PRIMARY KEY,
  order_id text NOT NULL REFERENCES orders(id),
  attempt bigint NOT NULL CHECK (attempt > 0),
  request_hash sha256_hex NOT NULL,
  state text NOT NULL,
  created_at timestamptz NOT NULL,
  UNIQUE (order_id, attempt)
);

CREATE TABLE order_events (
  id text PRIMARY KEY,
  order_id text NOT NULL REFERENCES orders(id),
  exchange_event_identity text NOT NULL,
  prior_state text,
  new_state text NOT NULL,
  revision bigint NOT NULL CHECK (revision > 0),
  causation_id text NOT NULL,
  occurred_at timestamptz NOT NULL,
  UNIQUE (order_id, exchange_event_identity),
  UNIQUE (order_id, revision)
);

CREATE TABLE fills (
  id text PRIMARY KEY,
  order_id text NOT NULL REFERENCES orders(id),
  exchange_id text NOT NULL REFERENCES exchanges(id),
  exchange_fill_id text NOT NULL,
  quantity financial_amount NOT NULL CHECK (quantity > 0),
  price financial_amount NOT NULL CHECK (price > 0),
  fee_quantity financial_amount NOT NULL,
  fee_asset text NOT NULL REFERENCES assets(symbol),
  occurred_at timestamptz NOT NULL,
  UNIQUE (exchange_id, order_id, exchange_fill_id)
);

CREATE TABLE journal_transactions (
  id text PRIMARY KEY,
  transaction_type text NOT NULL,
  run_id text NOT NULL REFERENCES runs(id),
  portfolio_id text NOT NULL REFERENCES portfolios(id),
  order_id text REFERENCES orders(id),
  fill_id text REFERENCES fills(id),
  configuration_id text NOT NULL REFERENCES configuration_versions(id),
  causation_id text NOT NULL,
  correlation_id text NOT NULL,
  reversal_of text REFERENCES journal_transactions(id),
  recorded_at timestamptz NOT NULL,
  ingest_ordinal bigint NOT NULL CHECK (ingest_ordinal > 0),
  sealed boolean NOT NULL DEFAULT false,
  CHECK (reversal_of IS NULL OR reversal_of <> id)
);

CREATE TABLE ledger_entries (
  transaction_id text NOT NULL REFERENCES journal_transactions(id),
  line_number integer NOT NULL CHECK (line_number > 0),
  account_class text NOT NULL CHECK (account_class IN (
    'external_equity','available_asset','reserved_asset','strategy_inventory',
    'exchange_inventory','trade_cost_proceeds','fee_expense','spread_attribution',
    'slippage_attribution','latency_attribution','realized_pnl','unrealized_pnl',
    'inventory_valuation','rebalancing_expense','recovery_loss','rounding_dust',
    'reconciliation_suspense'
  )),
  account_owner text NOT NULL,
  asset_symbol text NOT NULL REFERENCES assets(symbol),
  direction text NOT NULL CHECK (direction IN ('debit','credit')),
  quantity financial_amount NOT NULL CHECK (quantity > 0),
  functional_value signed_financial_amount,
  lot_reference text,
  rounding_metadata text,
  PRIMARY KEY (transaction_id, line_number)
);

CREATE TABLE projection_revisions (
  account_id text NOT NULL REFERENCES virtual_accounts(id),
  projection_kind text NOT NULL,
  revision bigint NOT NULL CHECK (revision > 0),
  source_journal_id text NOT NULL REFERENCES journal_transactions(id),
  projection_hash sha256_hex NOT NULL,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (account_id, projection_kind)
);

CREATE TABLE reconciliation_cases (
  id text PRIMARY KEY,
  account_id text NOT NULL REFERENCES virtual_accounts(id),
  classification text NOT NULL,
  state text NOT NULL CHECK (state IN ('open','quarantined','resolved','adjusted')),
  incident_id text,
  opened_at timestamptz NOT NULL,
  resolved_at timestamptz
);

CREATE TABLE reconciliation_suspense (
  case_id text NOT NULL REFERENCES reconciliation_cases(id),
  asset_symbol text NOT NULL REFERENCES assets(symbol),
  quantity signed_financial_amount NOT NULL,
  reason text NOT NULL,
  PRIMARY KEY (case_id, asset_symbol)
);

CREATE INDEX reservations_active_idx ON reservations(account_id, asset_symbol, created_at) WHERE state = 'active';
CREATE INDEX order_state_idx ON orders(account_id, state, updated_at);
CREATE INDEX journal_run_ordinal_idx ON journal_transactions(run_id, ingest_ordinal);
CREATE UNIQUE INDEX journal_single_reversal_idx ON journal_transactions(reversal_of) WHERE reversal_of IS NOT NULL;
CREATE INDEX ledger_account_asset_idx ON ledger_entries(account_owner, account_class, asset_symbol);
