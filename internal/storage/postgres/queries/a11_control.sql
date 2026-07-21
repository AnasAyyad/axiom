-- name: CountUsersForBootstrap :one
SELECT count(*) FROM users;

-- name: BootstrapOwnerUser :one
INSERT INTO users (
  id, email, normalized_email, password_hash, status, created_at, password_changed_at
) VALUES ($1,$2,$3,$4,'active',$5,$5)
RETURNING *;

-- name: GetBootstrapAuthorizationRole :one
SELECT * FROM authorization_roles WHERE id=$1 AND name=$2;

-- name: GrantUserRole :one
INSERT INTO user_roles (user_id, role_id, granted_at)
VALUES ($1,$2,$3)
ON CONFLICT (user_id, role_id) DO NOTHING
RETURNING *;

-- name: GrantRolePermission :one
INSERT INTO role_permissions (role_id, permission_id, granted_at)
VALUES ($1,$2,$3)
ON CONFLICT (role_id, permission_id) DO NOTHING
RETURNING *;

-- name: GetUserForAuthentication :one
SELECT u.*, coalesce(array_agg(DISTINCT r.name) FILTER (WHERE r.name IS NOT NULL), '{}')::text[] AS roles,
  coalesce(array_agg(DISTINCT p.id) FILTER (WHERE p.id IS NOT NULL), '{}')::text[] AS permissions
FROM users u
LEFT JOIN user_roles ur ON ur.user_id = u.id
LEFT JOIN authorization_roles r ON r.id = ur.role_id
LEFT JOIN role_permissions rp ON rp.role_id = r.id
LEFT JOIN authorization_permissions p ON p.id = rp.permission_id
WHERE u.normalized_email = $1
GROUP BY u.id;

-- name: UpdateUserPasswordHash :one
UPDATE users SET password_hash=$2, password_changed_at=$3
WHERE id=$1 AND password_hash=$4
RETURNING *;

-- name: CountRecentAuthenticationFailures :one
SELECT count(*) FROM authentication_failures
WHERE normalized_email_hash=$1 AND source_scope_hash=$2 AND occurred_at >= $3;

-- name: RecordAuthenticationFailure :one
INSERT INTO authentication_failures (
  id, normalized_email_hash, source_scope_hash, occurred_at, correlation_id
) VALUES ($1,$2,$3,$4,$5)
RETURNING *;

-- name: InsertA11Session :one
INSERT INTO sessions (
  id,user_id,token_hash,csrf_token_hash,created_at,expires_at,last_seen_at,
  idle_expires_at,reauthenticated_at,revision
) VALUES ($1,$2,$3,$4,$5,$6,$5,$7,$5,1)
RETURNING *;

-- name: RevokeOldestExcessSessions :many
UPDATE sessions AS target SET revoked_at=sqlc.arg(now), revoked_reason='session_limit', revision=target.revision+1
WHERE target.id IN (
  SELECT candidate.id FROM sessions AS candidate
  WHERE candidate.user_id=sqlc.arg(user_id) AND candidate.revoked_at IS NULL
    AND candidate.expires_at>sqlc.arg(now) AND candidate.idle_expires_at>sqlc.arg(now)
  ORDER BY candidate.created_at DESC, candidate.id DESC
  OFFSET 5
)
RETURNING *;

-- name: GetSessionByTokenHash :one
SELECT s.*, u.email, u.normalized_email, u.status AS user_status, u.role_revision,
  coalesce(array_agg(DISTINCT r.name) FILTER (WHERE r.name IS NOT NULL), '{}')::text[] AS roles,
  coalesce(array_agg(DISTINCT p.id) FILTER (WHERE p.id IS NOT NULL), '{}')::text[] AS permissions
FROM sessions s
JOIN users u ON u.id=s.user_id
LEFT JOIN user_roles ur ON ur.user_id=u.id
LEFT JOIN authorization_roles r ON r.id=ur.role_id
LEFT JOIN role_permissions rp ON rp.role_id=r.id
LEFT JOIN authorization_permissions p ON p.id=rp.permission_id
WHERE s.token_hash=$1
GROUP BY s.id,u.id;

-- name: TouchSession :one
UPDATE sessions SET last_seen_at=greatest(last_seen_at,$2),
  idle_expires_at=greatest(idle_expires_at,least(expires_at,$3)), revision=revision+1
WHERE id=$1 AND revoked_at IS NULL AND expires_at>$2 AND idle_expires_at>$2
RETURNING *;

-- name: RevokeSession :one
UPDATE sessions SET revoked_at=$2, revoked_reason=$3, revision=revision+1
WHERE id=$1 AND revoked_at IS NULL
RETURNING *;

-- name: InsertA11AuditEvent :one
INSERT INTO audit_events (
  id,event_type,actor,causation_id,correlation_id,configuration_id,event_hash,recorded_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
RETURNING *;

-- name: InsertDurableCommand :one
INSERT INTO command_requests (
  id,deduplication_key,payload_hash,configuration_id,state,created_at,actor_user_id,
  session_id,command_kind,target_type,target_id,reason,idempotency_key,
  expected_revision,correlation_id,causation_id,audit_event_id,updated_at
) VALUES ($1,$2,$3,$4,'pending',$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$5)
ON CONFLICT (deduplication_key) DO NOTHING
RETURNING *;

-- name: GetDurableCommandByDedupe :one
SELECT * FROM command_requests WHERE deduplication_key=$1;

-- name: CompleteDurableCommand :one
UPDATE command_requests SET state=$2,result_payload=$3,applied_at=$4,updated_at=$4,
  entity_revision=entity_revision+1
WHERE id=$1 AND state='pending' AND $2 IN ('applied','rejected','failed')
RETURNING *;

-- name: InsertA11Job :one
INSERT INTO jobs (
  id,job_type,idempotency_key,state,payload_hash,created_at,updated_at,owner_user_id,
  request_payload,max_attempts
) VALUES ($1,$2,$3,'QUEUED',$4,$5,$5,$6,$7,$8)
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING *;

-- name: GetA11JobByIdempotency :one
SELECT * FROM jobs WHERE idempotency_key=$1;

-- name: CountOwnerJobsByState :one
SELECT count(*) FROM jobs WHERE owner_user_id=$1 AND state=ANY($2::text[]);

-- name: GetA11Job :one
SELECT * FROM jobs WHERE id=$1;

-- name: RequestA11JobState :one
UPDATE jobs SET state=$2,progress_revision=progress_revision+1,updated_at=$3
WHERE id=$1 AND state=$4
RETURNING *;

-- name: InsertA11Outbox :one
INSERT INTO outbox_events (
  id,topic,payload_hash,created_at,stream,schema_version,entity_type,entity_id,
  entity_revision,event_time,correlation_id,causation_id,payload
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$4,$10,$11,$12)
RETURNING *;

-- name: ListA11OutboxAfter :many
SELECT * FROM outbox_events WHERE revision>$1 ORDER BY revision LIMIT $2;

-- name: OldestA11OutboxRevision :one
SELECT coalesce(min(candidate.revision),0)::bigint FROM outbox_events AS candidate
WHERE candidate.created_at >= sqlc.arg(retained_after) OR candidate.revision >
  (SELECT greatest(coalesce(max(current.revision),0)-100000,0) FROM outbox_events AS current);

-- name: InsertShadowSession :one
INSERT INTO shadow_sessions (
  id,command_id,state,revision,public_exchange,simulation_only,entries_enabled,
  configuration_id,strategy_version_id,created_at,exchange_id
) VALUES ($1,$2,'QUEUED',1,'binance-production-public',true,false,$3,$4,$5,'binance')
RETURNING *;

-- name: GetShadowSession :one
SELECT * FROM shadow_sessions WHERE id=$1;

-- name: TransitionShadowSession :one
UPDATE shadow_sessions SET state=$2,revision=revision+1,entries_enabled=$3,
  started_at=coalesce(started_at,$4),stopped_at=$5,failure_code=$6
WHERE id=$1 AND revision=$7
RETURNING *;

-- name: OpenStreamConnection :one
INSERT INTO stream_connections (id,user_id,session_id,opened_at,heartbeat_at,last_revision)
VALUES ($1,$2,$3,$4,$4,$5)
RETURNING *;

-- name: CountActiveStreams :one
SELECT count(*) FROM stream_connections WHERE user_id=$1 AND closed_at IS NULL;

-- name: HeartbeatStreamConnection :one
UPDATE stream_connections SET heartbeat_at=$2,last_revision=$3
WHERE id=$1 AND closed_at IS NULL
RETURNING *;

-- name: CloseStreamConnection :one
UPDATE stream_connections SET closed_at=$2 WHERE id=$1 AND closed_at IS NULL RETURNING *;
