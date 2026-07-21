SET TIME ZONE 'UTC';

ALTER TABLE model_versions DROP CONSTRAINT model_versions_model_type_check;
ALTER TABLE model_versions ADD CONSTRAINT model_versions_model_type_check CHECK (model_type IN (
  'fee','latency','spread','slippage','fill','cost_basis','impact',
  'adverse_selection','maker_queue'
));

CREATE TABLE model_namespaces (
  id text PRIMARY KEY,
  namespace_hash sha256_hex NOT NULL UNIQUE,
  market_context text NOT NULL,
  liquidity_domain text NOT NULL,
  fee_model_id text NOT NULL REFERENCES model_versions(id),
  latency_model_id text NOT NULL REFERENCES model_versions(id),
  fill_model_id text NOT NULL REFERENCES model_versions(id),
  price_model_hash sha256_hex NOT NULL,
  canonical_payload bytea NOT NULL,
  created_at timestamptz NOT NULL
);

CREATE TABLE run_manifests (
  run_id text PRIMARY KEY REFERENCES runs(id),
  manifest_hash sha256_hex NOT NULL UNIQUE,
  code_commit text NOT NULL CHECK (code_commit ~ '^([0-9a-f]{40}|[0-9a-f]{64})$'),
  go_version text NOT NULL,
  architecture text NOT NULL,
  operating_system text NOT NULL,
  build_flags_hash sha256_hex NOT NULL,
  go_sum_hash sha256_hex NOT NULL,
  pnpm_lock_hash sha256_hex NOT NULL,
  dataset_manifest_hash sha256_hex NOT NULL,
  dataset_revision bigint NOT NULL CHECK (dataset_revision > 0),
  source_commit text NOT NULL CHECK (source_commit ~ '^([0-9a-f]{40}|[0-9a-f]{64})$'),
  schema_version text NOT NULL,
  parser_version text NOT NULL,
  normalization_version text NOT NULL,
  segment_hashes_hash sha256_hex NOT NULL,
  configuration_hash sha256_hex NOT NULL,
  scheduler_version text NOT NULL,
  serialization_version text NOT NULL,
  model_namespace_id text NOT NULL REFERENCES model_namespaces(id),
  starting_balance_hash sha256_hex NOT NULL,
  confidence_tier text NOT NULL CHECK (confidence_tier IN ('A','B','C','D')),
  canonical_payload bytea NOT NULL,
  created_at timestamptz NOT NULL
);

CREATE TABLE run_canonical_outputs (
  run_id text NOT NULL REFERENCES runs(id),
  output_kind text NOT NULL CHECK (output_kind IN ('event','decision','order','balance','projection','metric','result')),
  ordinal bigint NOT NULL CHECK (ordinal >= 0),
  output_hash sha256_hex NOT NULL,
  canonical_payload bytea NOT NULL,
  PRIMARY KEY (run_id, output_kind, ordinal)
);

ALTER TABLE execution_plans DROP CONSTRAINT execution_plans_state_check;
ALTER TABLE execution_plans ADD CONSTRAINT execution_plans_state_check CHECK (state IN (
  'planned','active','completed','failed','recovery_required','recovered','quarantined'
));
ALTER TABLE execution_plans ADD COLUMN dispatch_policy text NOT NULL DEFAULT 'sequential'
  CHECK (dispatch_policy IN ('sequential','concurrent'));
ALTER TABLE execution_plans ADD COLUMN remaining_exposure bytea;
ALTER TABLE execution_plans ADD COLUMN final_disposition text;

ALTER TABLE execution_plan_legs ADD COLUMN order_id text;
ALTER TABLE execution_plan_legs ADD COLUMN client_order_id text;

ALTER TABLE orders DROP CONSTRAINT orders_state_check;
ALTER TABLE orders ADD CONSTRAINT orders_state_check CHECK (state IN (
  'created','validating','reserved','approved','submitting','acknowledged',
  'partially_filled','filled','cancel_pending','canceled','rejected','expired',
  'unknown','recovery_required','recovered'
));
ALTER TABLE orders ADD COLUMN exchange_status text NOT NULL DEFAULT '';
ALTER TABLE orders ADD COLUMN cumulative_quantity financial_amount NOT NULL DEFAULT 0;
ALTER TABLE orders ADD COLUMN cumulative_fee financial_amount NOT NULL DEFAULT 0;
ALTER TABLE orders ADD COLUMN cumulative_rebate financial_amount NOT NULL DEFAULT 0;
ALTER TABLE orders ADD COLUMN last_event_ordinal bigint NOT NULL DEFAULT 0 CHECK (last_event_ordinal >= 0);

CREATE OR REPLACE FUNCTION enforce_order_transition() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.state <> 'created' OR NEW.revision <> 1 OR NEW.updated_at < NEW.created_at OR
       NEW.cumulative_quantity <> 0 OR NEW.cumulative_fee <> 0 OR NEW.cumulative_rebate <> 0 OR
       NEW.last_event_ordinal <> 0 THEN
      RAISE EXCEPTION 'invalid_order_initial_state';
    END IF;
    RETURN NEW;
  END IF;
  IF (to_jsonb(NEW) - 'state' - 'revision' - 'updated_at' - 'exchange_status' -
      'cumulative_quantity' - 'cumulative_fee' - 'cumulative_rebate' - 'last_event_ordinal') IS DISTINCT FROM
     (to_jsonb(OLD) - 'state' - 'revision' - 'updated_at' - 'exchange_status' -
      'cumulative_quantity' - 'cumulative_fee' - 'cumulative_rebate' - 'last_event_ordinal') THEN
    RAISE EXCEPTION 'immutable_order_identity';
  END IF;
  IF NEW.revision <> OLD.revision + 1 OR NEW.updated_at < OLD.updated_at OR
     NEW.last_event_ordinal <= OLD.last_event_ordinal OR
     NEW.cumulative_quantity < OLD.cumulative_quantity OR NEW.cumulative_quantity > NEW.quantity OR
     NEW.cumulative_fee < OLD.cumulative_fee OR NEW.cumulative_rebate < OLD.cumulative_rebate THEN
    RAISE EXCEPTION 'invalid_order_revision';
  END IF;
  IF NEW.state = OLD.state THEN
    RETURN NEW;
  END IF;
  IF NOT (
    (OLD.state = 'created' AND NEW.state IN ('validating','rejected')) OR
    (OLD.state = 'validating' AND NEW.state IN ('reserved','rejected')) OR
    (OLD.state = 'reserved' AND NEW.state IN ('approved','rejected','expired')) OR
    (OLD.state = 'approved' AND NEW.state IN ('submitting','cancel_pending','expired')) OR
    (OLD.state = 'submitting' AND NEW.state IN ('acknowledged','partially_filled','filled','rejected','expired','unknown')) OR
    (OLD.state = 'acknowledged' AND NEW.state IN ('partially_filled','filled','cancel_pending','canceled','expired','unknown','recovery_required')) OR
    (OLD.state = 'partially_filled' AND NEW.state IN ('filled','cancel_pending','canceled','expired','unknown','recovery_required')) OR
    (OLD.state = 'cancel_pending' AND NEW.state IN ('partially_filled','filled','canceled','unknown','recovery_required')) OR
    (OLD.state IN ('canceled','expired') AND NEW.state IN ('partially_filled','filled','recovery_required')) OR
    (OLD.state = 'rejected' AND NEW.state = 'recovery_required') OR
    (OLD.state = 'unknown' AND NEW.state IN ('acknowledged','partially_filled','filled','canceled','rejected','expired','recovery_required')) OR
    (OLD.state = 'recovery_required' AND NEW.state = 'recovered')
  ) THEN
    RAISE EXCEPTION 'invalid_order_transition';
  END IF;
  RETURN NEW;
END;
$$;

ALTER TABLE order_events ADD COLUMN ingest_ordinal bigint CHECK (ingest_ordinal > 0);
ALTER TABLE order_events ADD COLUMN event_hash sha256_hex;
ALTER TABLE order_events ADD COLUMN exchange_status text;
ALTER TABLE order_events ADD COLUMN cumulative_quantity financial_amount;
ALTER TABLE order_events ADD COLUMN canonical_payload bytea;
CREATE UNIQUE INDEX order_events_order_ordinal_idx ON order_events(order_id, ingest_ordinal)
  WHERE ingest_ordinal IS NOT NULL;

ALTER TABLE fills ADD COLUMN rebate_quantity financial_amount NOT NULL DEFAULT 0;
ALTER TABLE fills ADD COLUMN ingest_ordinal bigint CHECK (ingest_ordinal > 0);
ALTER TABLE fills ADD COLUMN fill_hash sha256_hex;
CREATE UNIQUE INDEX fills_order_ordinal_idx ON fills(order_id, ingest_ordinal)
  WHERE ingest_ordinal IS NOT NULL;

CREATE TABLE order_reduction_incidents (
  id text PRIMARY KEY,
  order_id text NOT NULL REFERENCES orders(id),
  event_id text,
  reason_code text NOT NULL,
  prior_revision bigint NOT NULL CHECK (prior_revision > 0),
  canonical_payload bytea NOT NULL,
  created_at timestamptz NOT NULL
);

CREATE TABLE fill_journal_postings (
  fill_id text NOT NULL REFERENCES fills(id),
  transaction_id text NOT NULL REFERENCES journal_transactions(id),
  posting_kind text NOT NULL CHECK (posting_kind IN ('fill','fee','rebate','dust','recovery_loss')),
  PRIMARY KEY (fill_id, posting_kind),
  UNIQUE (transaction_id)
);

ALTER TABLE run_checkpoints ADD COLUMN cursor_logical_time bigint CHECK (cursor_logical_time > 0);
ALTER TABLE run_checkpoints ADD COLUMN orders_hash sha256_hex;
ALTER TABLE run_checkpoints ADD COLUMN plans_hash sha256_hex;
ALTER TABLE run_checkpoints ADD COLUMN liquidity_hash sha256_hex;
ALTER TABLE run_checkpoints ADD COLUMN journal_hash sha256_hex;
ALTER TABLE run_checkpoints ADD COLUMN projection_hash sha256_hex;
ALTER TABLE run_checkpoints ADD COLUMN model_namespace_id text REFERENCES model_namespaces(id);
ALTER TABLE run_checkpoints ADD COLUMN deterministic_state_hash sha256_hex;

CREATE INDEX run_output_hash_idx ON run_canonical_outputs(run_id, output_hash);
CREATE INDEX order_reduction_incidents_order_idx ON order_reduction_incidents(order_id, created_at);
