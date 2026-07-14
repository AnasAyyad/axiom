-- name: UpsertAlert :one
INSERT INTO alerts (
  id, incident_id, alert_type, state, created_at, severity, reason_code,
  deduplication_key, correlation_id, last_seen_at, occurrences, revision
) VALUES ($1, $2, $3, 'open', $4, $5, $6, $7, $8, $4, 1, 1)
ON CONFLICT (deduplication_key) DO UPDATE SET
  state = 'open',
  severity = EXCLUDED.severity,
  reason_code = EXCLUDED.reason_code,
  correlation_id = EXCLUDED.correlation_id,
  last_seen_at = EXCLUDED.last_seen_at,
  occurrences = alerts.occurrences + 1,
  revision = alerts.revision + 1,
  acknowledged_at = NULL,
  resolved_at = NULL
RETURNING *;

-- name: GetAlert :one
SELECT * FROM alerts WHERE id = $1;

-- name: AcknowledgeAlert :one
UPDATE alerts SET state = 'acknowledged', acknowledged_at = $2, revision = revision + 1
WHERE id = $1 AND state = 'open'
RETURNING *;

-- name: InsertAlertAcknowledgement :one
INSERT INTO alert_acknowledgements (alert_id, revision, actor, reason, acknowledged_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: UpsertAlertDelivery :one
INSERT INTO alert_deliveries (
  id, alert_id, sink_name, state, attempts, last_reason_code,
  next_attempt_at, created_at, delivered_at, revision
) VALUES ($1, $2, $3, 'pending', 0, NULL, $4, $4, NULL, 1)
ON CONFLICT (alert_id, sink_name) DO UPDATE SET
  state = 'pending', next_attempt_at = EXCLUDED.next_attempt_at,
  last_reason_code = NULL, revision = alert_deliveries.revision + 1
WHERE alert_deliveries.state <> 'delivered'
RETURNING *;

-- name: MarkAlertDelivery :one
UPDATE alert_deliveries SET
  state = $2, attempts = attempts + 1, last_reason_code = $3,
  next_attempt_at = $4, delivered_at = $5, revision = revision + 1
WHERE id = $1 AND state <> 'delivered' AND $2 IN ('failed','delivered')
RETURNING *;

-- name: ListDueAlertDeliveries :many
SELECT delivery.*, alert.severity, alert.reason_code, alert.alert_type,
  alert.correlation_id, alert.created_at AS alert_created_at,
  alert.last_seen_at AS alert_last_seen_at, alert.occurrences AS alert_occurrences
FROM alert_deliveries delivery
JOIN alerts alert ON alert.id = delivery.alert_id
WHERE delivery.state IN ('pending','failed') AND delivery.next_attempt_at <= $1
ORDER BY delivery.next_attempt_at, delivery.id
LIMIT $2;
