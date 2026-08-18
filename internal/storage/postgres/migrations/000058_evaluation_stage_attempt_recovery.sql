SET TIME ZONE 'UTC';

-- A campaign stage remains the current projection. These fields fence its
-- independently retryable execution attempt and keep paused campaigns out of
-- a hot claim loop while their dependency recovers.
ALTER TABLE evaluation_campaign_stages
  ADD COLUMN attempt_started_at timestamptz,
  ADD COLUMN next_retry_at timestamptz,
  ADD COLUMN recoverable_failure_count integer NOT NULL DEFAULT 0
    CHECK (recoverable_failure_count >= 0);

UPDATE evaluation_campaign_stages
SET attempt_started_at=COALESCE(started_at,updated_at)
WHERE attempt > 0;

ALTER TABLE evaluation_campaign_stages
  ADD CONSTRAINT evaluation_campaign_stage_attempt_started
  CHECK ((attempt=0)=(attempt_started_at IS NULL));

CREATE INDEX evaluation_campaign_stages_retry_due
  ON evaluation_campaign_stages(next_retry_at)
  WHERE next_retry_at IS NOT NULL AND state IN ('RUNNING','PAUSED_RECOVERABLE');

-- One row closes one stage execution attempt. It is append-only evidence: a
-- retry gets the next attempt number and never rewrites the prior checkpoint.
CREATE TABLE evaluation_campaign_stage_attempts (
  campaign_id text NOT NULL,
  stage text NOT NULL,
  attempt integer NOT NULL CHECK (attempt >= 1),
  outcome text NOT NULL CHECK (outcome IN ('PAUSED_RECOVERABLE','COMPLETED','BLOCKED')),
  reason_code text,
  summary text NOT NULL CHECK (length(summary) BETWEEN 1 AND 500),
  checkpoint_payload bytea,
  checkpoint_hash bytea CHECK (checkpoint_hash IS NULL OR octet_length(checkpoint_hash)=32),
  linked_resource_type text,
  linked_resource_id text,
  started_at timestamptz NOT NULL,
  finished_at timestamptz NOT NULL,
  retry_at timestamptz,
  PRIMARY KEY (campaign_id,stage,attempt),
  FOREIGN KEY (campaign_id,stage) REFERENCES evaluation_campaign_stages(campaign_id,stage),
  CHECK ((checkpoint_payload IS NULL)=(checkpoint_hash IS NULL)),
  CHECK (checkpoint_payload IS NULL OR octet_length(checkpoint_payload) <= 1048576),
  CHECK ((linked_resource_type IS NULL)=(linked_resource_id IS NULL)),
  CHECK (finished_at >= started_at),
  CHECK ((outcome='COMPLETED')=(reason_code IS NULL)),
  CHECK ((outcome='PAUSED_RECOVERABLE')=(retry_at IS NOT NULL)),
  CHECK (retry_at IS NULL OR retry_at >= finished_at)
);

CREATE TRIGGER evaluation_campaign_stage_attempts_immutable
  BEFORE UPDATE OR DELETE ON evaluation_campaign_stage_attempts
  FOR EACH ROW EXECUTE FUNCTION protect_evaluation_immutable_evidence();
