SET TIME ZONE 'UTC';

ALTER TABLE alerts ADD COLUMN severity text;
ALTER TABLE alerts ADD COLUMN reason_code text;
ALTER TABLE alerts ADD COLUMN deduplication_key sha256_hex;
ALTER TABLE alerts ADD COLUMN correlation_id text;
ALTER TABLE alerts ADD COLUMN last_seen_at timestamptz;
ALTER TABLE alerts ADD COLUMN occurrences bigint;
ALTER TABLE alerts ADD COLUMN revision bigint;

UPDATE alerts SET
  severity = 'warning',
  reason_code = alert_type,
  deduplication_key = md5('axiom-alert-a:' || id) || md5('axiom-alert-b:' || id),
  correlation_id = id,
  last_seen_at = created_at,
  occurrences = 1,
  revision = 1;

ALTER TABLE alerts ALTER COLUMN severity SET NOT NULL;
ALTER TABLE alerts ALTER COLUMN reason_code SET NOT NULL;
ALTER TABLE alerts ALTER COLUMN deduplication_key SET NOT NULL;
ALTER TABLE alerts ALTER COLUMN correlation_id SET NOT NULL;
ALTER TABLE alerts ALTER COLUMN last_seen_at SET NOT NULL;
ALTER TABLE alerts ALTER COLUMN occurrences SET NOT NULL;
ALTER TABLE alerts ALTER COLUMN revision SET NOT NULL;
ALTER TABLE alerts ADD CONSTRAINT alerts_severity_check CHECK (severity IN ('info','warning','critical'));
ALTER TABLE alerts ADD CONSTRAINT alerts_occurrences_check CHECK (occurrences > 0);
ALTER TABLE alerts ADD CONSTRAINT alerts_revision_check CHECK (revision > 0);
ALTER TABLE alerts ADD CONSTRAINT alerts_time_order_check CHECK (last_seen_at >= created_at);
ALTER TABLE alerts ADD CONSTRAINT alerts_deduplication_unique UNIQUE (deduplication_key);

CREATE TABLE alert_deliveries (
  id text PRIMARY KEY,
  alert_id text NOT NULL REFERENCES alerts(id),
  sink_name text NOT NULL,
  state text NOT NULL CHECK (state IN ('pending','delivered','failed')),
  attempts integer NOT NULL CHECK (attempts >= 0),
  last_reason_code text,
  next_attempt_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL,
  delivered_at timestamptz,
  revision bigint NOT NULL CHECK (revision > 0),
  UNIQUE (alert_id, sink_name),
  CHECK (next_attempt_at >= created_at),
  CHECK ((state = 'delivered') = (delivered_at IS NOT NULL))
);

CREATE INDEX alerts_open_severity_idx ON alerts(severity, last_seen_at DESC) WHERE state = 'open';
CREATE INDEX alert_deliveries_retry_idx ON alert_deliveries(next_attempt_at, id) WHERE state IN ('pending','failed');
