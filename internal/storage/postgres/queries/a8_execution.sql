-- name: InsertModelNamespace :one
INSERT INTO model_namespaces (
  id, namespace_hash, market_context, liquidity_domain, fee_model_id,
  latency_model_id, fill_model_id, price_model_hash, canonical_payload, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
RETURNING *;

-- name: InsertRunManifest :one
INSERT INTO run_manifests (
  run_id, manifest_hash, code_commit, go_version, architecture, operating_system,
  build_flags_hash, go_sum_hash, pnpm_lock_hash, dataset_manifest_hash,
  dataset_revision, source_commit, schema_version, parser_version,
  normalization_version, segment_hashes_hash, configuration_hash,
  scheduler_version, serialization_version, model_namespace_id,
  starting_balance_hash, confidence_tier, canonical_payload, created_at
) VALUES (
  $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24
)
RETURNING *;

-- name: GetRunManifest :one
SELECT * FROM run_manifests WHERE run_id = $1;

-- name: InsertCanonicalOutput :one
INSERT INTO run_canonical_outputs (run_id, output_kind, ordinal, output_hash, canonical_payload)
VALUES ($1,$2,$3,$4,$5)
RETURNING *;

-- name: ListCanonicalOutputs :many
SELECT * FROM run_canonical_outputs WHERE run_id = $1
ORDER BY output_kind, ordinal;

-- name: LockCanonicalOrder :one
SELECT * FROM orders WHERE id = $1 FOR UPDATE;

-- name: ReduceCanonicalOrder :one
UPDATE orders SET state=$2, exchange_status=$3, cumulative_quantity=$4,
  cumulative_fee=$5, cumulative_rebate=$6, last_event_ordinal=$7,
  revision=revision+1, updated_at=$8
WHERE id=$1 AND revision=$9 AND last_event_ordinal < $7
RETURNING *;

-- name: InsertCanonicalOrderEvent :one
INSERT INTO order_events (
  id, order_id, exchange_event_identity, prior_state, new_state, revision,
  causation_id, occurred_at, ingest_ordinal, event_hash, exchange_status,
  cumulative_quantity, canonical_payload
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
ON CONFLICT (order_id, exchange_event_identity) DO NOTHING
RETURNING *;

-- name: InsertCanonicalFill :one
INSERT INTO fills (
  id, order_id, exchange_id, exchange_fill_id, quantity, price, fee_quantity,
  fee_asset, occurred_at, rebate_quantity, ingest_ordinal, fill_hash
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
ON CONFLICT (exchange_id, order_id, exchange_fill_id) DO NOTHING
RETURNING *;

-- name: InsertFillJournalPosting :one
INSERT INTO fill_journal_postings (fill_id, transaction_id, posting_kind)
VALUES ($1,$2,$3)
RETURNING *;

-- name: InsertOrderReductionIncident :one
INSERT INTO order_reduction_incidents (
  id, order_id, event_id, reason_code, prior_revision, canonical_payload, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7)
RETURNING *;

-- name: InsertA8Checkpoint :one
INSERT INTO run_checkpoints (
  id, run_id, revision, input_ordinal, state_hash, payload, created_at,
  cursor_logical_time, orders_hash, plans_hash, liquidity_hash, journal_hash,
  projection_hash, model_namespace_id, deterministic_state_hash
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
RETURNING *;
