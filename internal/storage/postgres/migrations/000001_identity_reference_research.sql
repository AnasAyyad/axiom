SET TIME ZONE 'UTC';

CREATE DOMAIN financial_amount AS numeric(38,18)
  CHECK (VALUE >= 0);
CREATE DOMAIN signed_financial_amount AS numeric(38,18);
CREATE DOMAIN sha256_hex AS text
  CHECK (VALUE ~ '^[0-9a-f]{64}$');

CREATE TABLE users (
  id text PRIMARY KEY,
  email text NOT NULL UNIQUE,
  password_hash text NOT NULL,
  status text NOT NULL CHECK (status IN ('active','disabled','locked')),
  created_at timestamptz NOT NULL,
  disabled_at timestamptz
);

CREATE TABLE authorization_roles (
  id text PRIMARY KEY,
  name text NOT NULL UNIQUE
);

CREATE TABLE user_roles (
  user_id text NOT NULL REFERENCES users(id),
  role_id text NOT NULL REFERENCES authorization_roles(id),
  granted_at timestamptz NOT NULL,
  PRIMARY KEY (user_id, role_id)
);

CREATE TABLE sessions (
  id text PRIMARY KEY,
  user_id text NOT NULL REFERENCES users(id),
  token_hash sha256_hex NOT NULL UNIQUE,
  created_at timestamptz NOT NULL,
  expires_at timestamptz NOT NULL,
  revoked_at timestamptz,
  CHECK (expires_at > created_at)
);

CREATE TABLE configuration_versions (
  id text PRIMARY KEY,
  version bigint NOT NULL UNIQUE CHECK (version > 0),
  configuration_hash sha256_hex NOT NULL UNIQUE,
  canonical_payload bytea NOT NULL,
  actor text NOT NULL,
  recorded_at timestamptz NOT NULL
);

CREATE TABLE configuration_activations (
  revision bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  configuration_id text NOT NULL REFERENCES configuration_versions(id),
  actor text NOT NULL,
  reason text NOT NULL,
  activated_at timestamptz NOT NULL
);

CREATE TABLE exchanges (
  id text PRIMARY KEY,
  name text NOT NULL UNIQUE,
  environment text NOT NULL CHECK (environment IN ('production_public','emulator'))
);

CREATE TABLE exchange_capabilities (
  exchange_id text NOT NULL REFERENCES exchanges(id),
  version bigint NOT NULL CHECK (version > 0),
  capability text NOT NULL,
  supported boolean NOT NULL,
  recorded_at timestamptz NOT NULL,
  PRIMARY KEY (exchange_id, version, capability)
);

CREATE TABLE assets (
  symbol text PRIMARY KEY CHECK (symbol ~ '^[A-Z0-9]{2,12}$')
);

CREATE TABLE asset_screening_versions (
  id text PRIMARY KEY,
  asset_symbol text NOT NULL REFERENCES assets(symbol),
  version bigint NOT NULL CHECK (version > 0),
  prior_status text CHECK (prior_status IS NULL OR prior_status IN ('approved','scan_only','blocked','pending_review')),
  status text NOT NULL CHECK (status IN ('approved','scan_only','blocked','pending_review')),
  actor text NOT NULL,
  reason text NOT NULL,
  causation_id text NOT NULL,
  configuration_id text NOT NULL REFERENCES configuration_versions(id),
  effective_at timestamptz NOT NULL,
  recorded_at timestamptz NOT NULL,
  UNIQUE (asset_symbol, version),
  CHECK (effective_at <= recorded_at)
);

CREATE TABLE instruments (
  id text PRIMARY KEY,
  base_asset text NOT NULL REFERENCES assets(symbol),
  quote_asset text NOT NULL REFERENCES assets(symbol),
  product text NOT NULL CHECK (product = 'spot'),
  UNIQUE (base_asset, quote_asset, product),
  CHECK (base_asset <> quote_asset)
);

CREATE TABLE instrument_metadata_versions (
  id text PRIMARY KEY,
  exchange_id text NOT NULL REFERENCES exchanges(id),
  instrument_id text NOT NULL REFERENCES instruments(id),
  version bigint NOT NULL CHECK (version > 0),
  price_tick financial_amount NOT NULL CHECK (price_tick > 0),
  quantity_step financial_amount NOT NULL CHECK (quantity_step > 0),
  minimum_quantity financial_amount NOT NULL,
  minimum_notional financial_amount NOT NULL,
  effective_at timestamptz NOT NULL,
  recorded_at timestamptz NOT NULL,
  UNIQUE (exchange_id, instrument_id, version),
  CHECK (effective_at <= recorded_at)
);

CREATE TABLE strategy_definitions (
  id text PRIMARY KEY,
  name text NOT NULL UNIQUE,
  family text NOT NULL
);

CREATE TABLE strategy_versions (
  id text PRIMARY KEY,
  strategy_id text NOT NULL REFERENCES strategy_definitions(id),
  version bigint NOT NULL CHECK (version > 0),
  implementation_hash sha256_hex NOT NULL,
  promotion_status text NOT NULL CHECK (promotion_status IN ('research','candidate','locked_test','promoted','retired')),
  used_at timestamptz,
  created_at timestamptz NOT NULL,
  UNIQUE (strategy_id, version)
);

CREATE TABLE strategy_parameters (
  strategy_version_id text NOT NULL REFERENCES strategy_versions(id),
  parameter_name text NOT NULL,
  decimal_value text NOT NULL,
  unit text NOT NULL,
  PRIMARY KEY (strategy_version_id, parameter_name)
);

CREATE TABLE experiment_registrations (
  id text PRIMARY KEY,
  strategy_version_id text NOT NULL REFERENCES strategy_versions(id),
  configuration_id text NOT NULL REFERENCES configuration_versions(id),
  dataset_id text NOT NULL,
  hypothesis text NOT NULL,
  status text NOT NULL CHECK (status IN ('registered','running','completed','failed','locked')),
  registered_at timestamptz NOT NULL
);

CREATE TABLE model_versions (
  id text PRIMARY KEY,
  model_type text NOT NULL CHECK (model_type IN ('fee','latency','spread','slippage','fill','cost_basis')),
  version bigint NOT NULL CHECK (version > 0),
  model_hash sha256_hex NOT NULL,
  canonical_payload bytea NOT NULL,
  used_at timestamptz,
  created_at timestamptz NOT NULL,
  UNIQUE (model_type, version)
);

CREATE TABLE runs (
  id text PRIMARY KEY,
  mode text NOT NULL CHECK (mode IN ('backtest','replay','paper','shadow')),
  configuration_id text NOT NULL REFERENCES configuration_versions(id),
  strategy_version_id text REFERENCES strategy_versions(id),
  dataset_id text,
  root_seed_hash sha256_hex NOT NULL,
  reproducibility_hash sha256_hex NOT NULL,
  state text NOT NULL CHECK (state IN ('created','running','paused','completed','failed','cancelled')),
  created_at timestamptz NOT NULL,
  started_at timestamptz,
  completed_at timestamptz
);

CREATE TABLE run_checkpoints (
  id text PRIMARY KEY,
  run_id text NOT NULL REFERENCES runs(id),
  revision bigint NOT NULL CHECK (revision > 0),
  input_ordinal bigint NOT NULL CHECK (input_ordinal >= 0),
  state_hash sha256_hex NOT NULL,
  payload bytea NOT NULL,
  created_at timestamptz NOT NULL,
  UNIQUE (run_id, revision)
);

CREATE TABLE run_results (
  run_id text PRIMARY KEY REFERENCES runs(id),
  result_hash sha256_hex NOT NULL,
  canonical_payload bytea NOT NULL,
  completed_at timestamptz NOT NULL
);

CREATE INDEX sessions_active_user_idx ON sessions(user_id, expires_at) WHERE revoked_at IS NULL;
CREATE INDEX metadata_effective_idx ON instrument_metadata_versions(exchange_id, instrument_id, effective_at DESC);
CREATE INDEX runs_state_created_idx ON runs(state, created_at);
