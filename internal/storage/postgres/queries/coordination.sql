-- name: InsertCommand :one
INSERT INTO command_requests (
  id, deduplication_key, payload_hash, configuration_id, state, created_at
) VALUES ($1, $2, $3, $4, 'pending', $5)
ON CONFLICT (deduplication_key) DO NOTHING
RETURNING *;

-- name: ConsumeInbox :one
INSERT INTO inbox_events (consumer, message_id, payload_hash, consumed_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (consumer, message_id) DO NOTHING
RETURNING *;

-- name: InsertOutbox :one
INSERT INTO outbox_events (id, topic, payload_hash, created_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListOutboxAfter :many
SELECT * FROM outbox_events
WHERE revision > $1
ORDER BY revision
LIMIT $2;

-- name: MarkOutboxPublished :one
UPDATE outbox_events
SET published_at = $2
WHERE id = $1 AND published_at IS NULL
RETURNING *;

-- name: AdvanceConsumerCursor :one
INSERT INTO consumer_cursors (consumer, outbox_revision, updated_at)
VALUES ($1, $2, $3)
ON CONFLICT (consumer) DO UPDATE
SET outbox_revision = EXCLUDED.outbox_revision,
    updated_at = EXCLUDED.updated_at
WHERE consumer_cursors.outbox_revision < EXCLUDED.outbox_revision
RETURNING *;

-- name: MarkCommandState :one
UPDATE command_requests
SET state = $2,
    applied_at = $3
WHERE id = $1 AND state = 'pending' AND $2 IN ('applied','rejected','failed')
RETURNING *;

-- name: ClaimNextJob :one
WITH candidate AS (
  SELECT id FROM jobs
  WHERE (state = 'QUEUED' OR
      (state = 'RUNNING' AND claim_expires_at <= $1))
    AND (claim_epoch IS NULL OR claim_epoch < $3)
    AND (owner_user_id IS NULL OR state = 'RUNNING' OR
      (SELECT count(*) FROM jobs active
       WHERE active.owner_user_id = jobs.owner_user_id
         AND active.state IN ('RUNNING','PAUSE_REQUESTED','CANCEL_REQUESTED')) < 2)
    AND (state = 'RUNNING' OR
      (SELECT count(*) FROM jobs active
       WHERE active.state IN ('RUNNING','PAUSE_REQUESTED','CANCEL_REQUESTED')) < 8)
  ORDER BY created_at, id
  FOR UPDATE SKIP LOCKED
  LIMIT 1
)
UPDATE jobs
SET state = 'RUNNING', claim_owner = $2, claim_epoch = $3,
    claim_expires_at = $4, started_at = coalesce(started_at, $1),
    progress_revision = progress_revision + 1, updated_at = $1
FROM candidate
WHERE jobs.id = candidate.id
RETURNING jobs.*;

-- name: RenewJobClaim :one
UPDATE jobs
SET claim_expires_at = $4, progress_revision = progress_revision + 1, updated_at = $5
WHERE id = $1 AND claim_owner = $2 AND claim_epoch = $3
  AND state = 'RUNNING' AND claim_expires_at > $5 AND $4 > $5
RETURNING *;

-- name: StartJob :one
SELECT * FROM jobs
WHERE id = $1 AND claim_owner = $2 AND claim_epoch = $3
  AND state = 'RUNNING' AND claim_expires_at > $4;

-- name: CompleteJob :one
UPDATE jobs
SET state = $4, claim_expires_at = NULL, completed_at = $5,
    progress_revision = progress_revision + 1, updated_at = $5
WHERE id = $1 AND claim_owner = $2 AND claim_epoch = $3
  AND state = 'RUNNING' AND $4 IN ('SUCCEEDED','FAILED')
RETURNING *;

-- name: EnsureLeaseEpoch :exec
INSERT INTO execution_lease_epochs (resource, last_fencing_token)
VALUES ($1, 0)
ON CONFLICT (resource) DO NOTHING;

-- name: LockLeaseEpoch :one
SELECT * FROM execution_lease_epochs
WHERE resource = $1
FOR UPDATE;

-- name: IncrementLeaseEpoch :one
UPDATE execution_lease_epochs
SET last_fencing_token = last_fencing_token + 1
WHERE resource = $1 AND last_fencing_token < 9223372036854775807
RETURNING *;

-- name: AcquireLease :one
INSERT INTO execution_leases (resource, owner, fencing_token, acquired_at, expires_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (resource) DO UPDATE
SET owner = EXCLUDED.owner,
    fencing_token = EXCLUDED.fencing_token,
    acquired_at = EXCLUDED.acquired_at,
    expires_at = EXCLUDED.expires_at
WHERE execution_leases.expires_at <= EXCLUDED.acquired_at
RETURNING *;

-- name: RenewLease :one
UPDATE execution_leases
SET expires_at = $4
WHERE resource = $1 AND owner = $2 AND fencing_token = $3 AND expires_at > $5
RETURNING *;

-- name: ReleaseLease :execrows
DELETE FROM execution_leases
WHERE resource = $1 AND owner = $2 AND fencing_token = $3;
