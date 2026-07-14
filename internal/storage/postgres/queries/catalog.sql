-- name: InsertConfigurationVersion :one
INSERT INTO configuration_versions (
  id, version, configuration_hash, canonical_payload, actor, recorded_at
) VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: InsertAuditEvent :one
INSERT INTO audit_events (
  id, event_type, actor, causation_id, correlation_id, configuration_id, event_hash, recorded_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: InsertMarketDataSegment :one
INSERT INTO market_data_segments (
  id, recorder_session, exchange_id, instrument_id, event_type, schema_version,
  parser_version, normalization_version, compression, path, checksum,
  ordered_content_hash, record_count, first_ordinal, last_ordinal,
  first_source_sequence, last_source_sequence, started_at, ended_at, state,
  finalized_at
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, 'zstd', $9, $10,
  $11, $12, $13, $14, $15, $16, $17, $18, $19, $20
)
RETURNING *;

-- name: InsertDatasetManifest :one
INSERT INTO dataset_manifests (
  id, dataset_hash, schema_compatibility, coverage_start, coverage_end, state, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: InsertDatasetSegment :one
INSERT INTO dataset_segments (dataset_id, segment_id, ordinal)
VALUES ($1, $2, $3)
RETURNING *;

-- name: TransitionDatasetManifest :one
UPDATE dataset_manifests
SET state = $3
WHERE id = $1 AND state = $2
RETURNING *;

-- name: TransitionMarketDataSegment :one
UPDATE market_data_segments
SET state = $3
WHERE id = $1 AND state = $2
RETURNING *;

-- name: ListDatasetSegments :many
SELECT segment.*
FROM dataset_segments member
JOIN market_data_segments segment ON segment.id = member.segment_id
WHERE member.dataset_id = $1
ORDER BY member.ordinal;

-- name: InsertDatasetGap :one
INSERT INTO dataset_gaps (
  id, dataset_id, first_ordinal, last_ordinal, reason_code, detected_at
) VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: InsertDataQualityEvent :one
INSERT INTO data_quality_events (
  id, dataset_id, segment_id, event_type, severity, reason_code,
  first_ordinal, last_ordinal, occurred_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: ListDatasetGaps :many
SELECT * FROM dataset_gaps
WHERE dataset_id = $1
ORDER BY first_ordinal, last_ordinal, id;

-- name: InsertRun :one
INSERT INTO runs (
  id, mode, configuration_id, strategy_version_id, dataset_id, root_seed_hash,
  reproducibility_hash, state, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, 'created', $8)
RETURNING *;

-- name: InsertRunCheckpoint :one
INSERT INTO run_checkpoints (
  id, run_id, revision, input_ordinal, state_hash, payload, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: TransitionRun :one
UPDATE runs
SET state = $3,
    started_at = CASE WHEN $3 = 'running' THEN coalesce(started_at, $4) ELSE started_at END,
    completed_at = CASE WHEN $3 IN ('completed','failed','cancelled') THEN $5 ELSE NULL END
WHERE id = $1 AND state = $2
RETURNING *;

-- name: GetRun :one
SELECT * FROM runs WHERE id = $1;

-- name: LatestRunCheckpoint :one
SELECT * FROM run_checkpoints
WHERE run_id = $1
ORDER BY revision DESC
LIMIT 1;

-- name: InsertRunResult :one
INSERT INTO run_results (run_id, result_hash, canonical_payload, completed_at)
VALUES ($1, $2, $3, $4)
RETURNING *;
