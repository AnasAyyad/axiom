SET TIME ZONE 'UTC';

ALTER TABLE dataset_manifests
  ADD COLUMN recorder_dataset_id text,
  ADD COLUMN manifest_revision bigint CHECK (manifest_revision > 0),
  ADD COLUMN manifest_path text CHECK (manifest_path IS NULL OR manifest_path !~ '(^|/)\.\.(/|$)'),
  ADD COLUMN source_commit text CHECK (source_commit IS NULL OR source_commit ~ '^([0-9a-f]{40}|[0-9a-f]{64})$'),
  ADD COLUMN dataset_kind text NOT NULL DEFAULT 'public_market'
    CHECK (dataset_kind IN ('public_market','decision_inputs')),
  ADD CONSTRAINT dataset_recorder_identity_complete CHECK (
    (recorder_dataset_id IS NULL AND manifest_revision IS NULL AND manifest_path IS NULL AND source_commit IS NULL) OR
    (recorder_dataset_id IS NOT NULL AND manifest_revision IS NOT NULL AND manifest_path IS NOT NULL AND source_commit IS NOT NULL)
  );
CREATE UNIQUE INDEX dataset_recorder_revision_idx
  ON dataset_manifests(recorder_dataset_id, manifest_revision)
  WHERE recorder_dataset_id IS NOT NULL;

CREATE FUNCTION protect_a11_dataset_recorder_identity() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'UPDATE' AND (
    NEW.recorder_dataset_id IS DISTINCT FROM OLD.recorder_dataset_id OR
    NEW.manifest_revision IS DISTINCT FROM OLD.manifest_revision OR
    NEW.manifest_path IS DISTINCT FROM OLD.manifest_path OR
    NEW.source_commit IS DISTINCT FROM OLD.source_commit OR
    (NEW.dataset_kind IS DISTINCT FROM OLD.dataset_kind AND OLD.recorder_dataset_id IS NOT NULL)
  ) THEN
    RAISE EXCEPTION 'immutable_dataset_recorder_identity';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER dataset_recorder_identity_protected BEFORE UPDATE ON dataset_manifests
  FOR EACH ROW EXECUTE FUNCTION protect_a11_dataset_recorder_identity();

ALTER TABLE dataset_gaps
  ALTER COLUMN first_ordinal DROP NOT NULL,
  ALTER COLUMN last_ordinal DROP NOT NULL,
  DROP CONSTRAINT dataset_gaps_first_ordinal_check,
  DROP CONSTRAINT dataset_gaps_check,
  ADD COLUMN first_source_sequence text,
  ADD COLUMN last_source_sequence text,
  ADD CONSTRAINT dataset_gap_identity_complete CHECK (
    (first_ordinal IS NOT NULL AND last_ordinal IS NOT NULL AND first_ordinal > 0 AND last_ordinal >= first_ordinal AND
     first_source_sequence IS NULL AND last_source_sequence IS NULL) OR
    (first_ordinal IS NULL AND last_ordinal IS NULL AND first_source_sequence IS NOT NULL AND last_source_sequence IS NOT NULL)
  );

ALTER TABLE users
  ADD COLUMN normalized_email text,
  ADD COLUMN role_revision bigint NOT NULL DEFAULT 1 CHECK (role_revision > 0),
  ADD COLUMN password_changed_at timestamptz;
UPDATE users
SET normalized_email = lower(btrim(email)), password_changed_at = created_at
WHERE normalized_email IS NULL;
ALTER TABLE users
  ALTER COLUMN normalized_email SET NOT NULL,
  ALTER COLUMN password_changed_at SET NOT NULL;
CREATE UNIQUE INDEX users_normalized_email_idx ON users(normalized_email);

CREATE TABLE authorization_permissions (
  id text PRIMARY KEY,
  description text NOT NULL
);

CREATE TABLE role_permissions (
  role_id text NOT NULL REFERENCES authorization_roles(id),
  permission_id text NOT NULL REFERENCES authorization_permissions(id),
  granted_at timestamptz NOT NULL,
  PRIMARY KEY (role_id, permission_id)
);

INSERT INTO authorization_permissions(id, description) VALUES
  ('operations.read', 'Read redacted operational state'),
  ('commands.write', 'Create authorized administrative commands'),
  ('incident.raw', 'Read redacted raw incident evidence'),
  ('audit.raw', 'Read detailed redacted audit evidence');

INSERT INTO authorization_roles(id, name) VALUES ('owner','owner'),('viewer','viewer')
ON CONFLICT (id) DO NOTHING;
INSERT INTO role_permissions(role_id, permission_id, granted_at)
SELECT 'owner', id, CURRENT_TIMESTAMP FROM authorization_permissions
ON CONFLICT DO NOTHING;
INSERT INTO role_permissions(role_id, permission_id, granted_at)
VALUES ('viewer','operations.read',CURRENT_TIMESTAMP)
ON CONFLICT DO NOTHING;

ALTER TABLE sessions
  ADD COLUMN csrf_token_hash sha256_hex,
  ADD COLUMN last_seen_at timestamptz,
  ADD COLUMN idle_expires_at timestamptz,
  ADD COLUMN reauthenticated_at timestamptz,
  ADD COLUMN revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  ADD COLUMN revoked_reason text;
UPDATE sessions
SET csrf_token_hash = repeat('0', 64),
    last_seen_at = created_at,
    idle_expires_at = least(expires_at, created_at + interval '30 minutes'),
    reauthenticated_at = created_at,
    revoked_at = coalesce(revoked_at, CURRENT_TIMESTAMP),
    revoked_reason = coalesce(revoked_reason, 'a11_security_migration');
ALTER TABLE sessions
  ALTER COLUMN csrf_token_hash SET NOT NULL,
  ALTER COLUMN last_seen_at SET NOT NULL,
  ALTER COLUMN idle_expires_at SET NOT NULL,
  ALTER COLUMN reauthenticated_at SET NOT NULL;
ALTER TABLE sessions ADD CONSTRAINT sessions_a11_lifetime_check CHECK (
  expires_at <= created_at + interval '12 hours' AND
  idle_expires_at <= expires_at AND idle_expires_at > created_at AND
  last_seen_at >= created_at AND reauthenticated_at >= created_at
);
CREATE INDEX sessions_idle_idx ON sessions(idle_expires_at) WHERE revoked_at IS NULL;

CREATE TABLE authentication_failures (
  id text PRIMARY KEY,
  normalized_email_hash sha256_hex NOT NULL,
  source_scope_hash sha256_hex NOT NULL,
  occurred_at timestamptz NOT NULL,
  correlation_id text NOT NULL
);
CREATE INDEX authentication_failures_window_idx
  ON authentication_failures(normalized_email_hash, source_scope_hash, occurred_at DESC);

ALTER TABLE jobs DROP CONSTRAINT jobs_state_check;
DROP TRIGGER jobs_follow_state_machine ON jobs;
UPDATE jobs SET state = CASE state
  WHEN 'queued' THEN 'QUEUED'
  WHEN 'claimed' THEN 'RUNNING'
  WHEN 'running' THEN 'RUNNING'
  WHEN 'completed' THEN 'SUCCEEDED'
  WHEN 'failed' THEN 'FAILED'
  WHEN 'cancelled' THEN 'CANCELED'
  ELSE state END;
ALTER TABLE jobs
  ADD CONSTRAINT jobs_state_check CHECK (state IN (
    'QUEUED','RUNNING','PAUSE_REQUESTED','PAUSED',
    'CANCEL_REQUESTED','CANCELED','SUCCEEDED','FAILED'
  )),
  ADD COLUMN owner_user_id text REFERENCES users(id),
  ADD COLUMN run_id text REFERENCES runs(id),
  ADD COLUMN request_payload jsonb,
  ADD COLUMN result_payload jsonb,
  ADD COLUMN failure_code text,
  ADD COLUMN retry_count integer NOT NULL DEFAULT 0 CHECK (retry_count >= 0),
  ADD COLUMN max_attempts integer NOT NULL DEFAULT 3 CHECK (max_attempts BETWEEN 1 AND 10),
  ADD COLUMN progress_revision bigint NOT NULL DEFAULT 1 CHECK (progress_revision > 0),
  ADD COLUMN resume_ordinal bigint NOT NULL DEFAULT 0 CHECK (resume_ordinal >= 0),
  ADD COLUMN single_step boolean NOT NULL DEFAULT false,
  ADD COLUMN checkpoint_payload jsonb,
  ADD COLUMN started_at timestamptz,
  ADD COLUMN completed_at timestamptz;
CREATE INDEX jobs_owner_state_idx ON jobs(owner_user_id, state, created_at, id);

CREATE OR REPLACE FUNCTION enforce_job_transition() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.state <> 'QUEUED' OR NEW.claim_owner IS NOT NULL OR NEW.claim_epoch IS NOT NULL OR
       NEW.claim_expires_at IS NOT NULL OR NEW.updated_at < NEW.created_at OR
       NEW.started_at IS NOT NULL OR NEW.completed_at IS NOT NULL OR NEW.progress_revision <> 1 OR
       NEW.resume_ordinal <> 0 OR NEW.single_step OR NEW.checkpoint_payload IS NOT NULL THEN
      RAISE EXCEPTION 'invalid_job_initial_state';
    END IF;
    RETURN NEW;
  END IF;
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'immutable_job_identity';
  END IF;
  IF NEW.run_id IS DISTINCT FROM OLD.run_id AND NOT (
    OLD.run_id IS NULL AND NEW.run_id = NEW.id AND NEW.state = 'RUNNING'
  ) THEN
    RAISE EXCEPTION 'immutable_job_run_identity';
  END IF;
  IF (to_jsonb(NEW) - 'state' - 'claim_owner' - 'claim_epoch' - 'claim_expires_at' -
      'updated_at' - 'result_payload' - 'failure_code' - 'retry_count' -
      'progress_revision' - 'started_at' - 'completed_at' - 'run_id' -
      'resume_ordinal' - 'single_step' - 'checkpoint_payload') IS DISTINCT FROM
     (to_jsonb(OLD) - 'state' - 'claim_owner' - 'claim_epoch' - 'claim_expires_at' -
      'updated_at' - 'result_payload' - 'failure_code' - 'retry_count' -
      'progress_revision' - 'started_at' - 'completed_at' - 'run_id' -
      'resume_ordinal' - 'single_step' - 'checkpoint_payload') OR
     NEW.updated_at < OLD.updated_at OR NEW.progress_revision <> OLD.progress_revision + 1 THEN
    RAISE EXCEPTION 'immutable_job_identity';
  END IF;
  IF NEW.state = OLD.state THEN
    IF NEW.state NOT IN ('RUNNING','PAUSE_REQUESTED') OR NEW.claim_expires_at <= NEW.updated_at OR NOT (
      (NEW.claim_owner = OLD.claim_owner AND NEW.claim_epoch = OLD.claim_epoch AND
       NEW.claim_expires_at > OLD.claim_expires_at) OR
      (NEW.state = 'RUNNING' AND OLD.claim_expires_at <= NEW.updated_at AND NEW.claim_owner IS NOT NULL AND
       NEW.claim_epoch > OLD.claim_epoch)
    ) THEN
      RAISE EXCEPTION 'invalid_job_renewal';
    END IF;
  ELSIF NEW.state = 'RUNNING' THEN
    IF NEW.claim_owner IS NULL OR NEW.claim_epoch IS NULL OR NEW.claim_expires_at <= NEW.updated_at OR NOT (
      OLD.state = 'QUEUED' OR
      (OLD.state = 'RUNNING' AND OLD.claim_expires_at <= NEW.updated_at AND NEW.claim_epoch > OLD.claim_epoch)
    ) THEN
      RAISE EXCEPTION 'invalid_job_claim';
    END IF;
  ELSIF NEW.state = 'PAUSE_REQUESTED' THEN
    IF OLD.state <> 'RUNNING' THEN RAISE EXCEPTION 'invalid_job_pause'; END IF;
  ELSIF NEW.state = 'PAUSED' THEN
    IF OLD.state <> 'PAUSE_REQUESTED' OR NEW.resume_ordinal <= OLD.resume_ordinal OR
       NEW.checkpoint_payload IS NULL THEN RAISE EXCEPTION 'invalid_job_pause'; END IF;
  ELSIF NEW.state = 'QUEUED' THEN
    IF OLD.state <> 'PAUSED' OR NEW.resume_ordinal <> OLD.resume_ordinal OR
       NEW.checkpoint_payload IS DISTINCT FROM OLD.checkpoint_payload THEN RAISE EXCEPTION 'invalid_job_resume'; END IF;
  ELSIF NEW.state = 'CANCEL_REQUESTED' THEN
    IF OLD.state NOT IN ('RUNNING','PAUSE_REQUESTED','PAUSED') THEN RAISE EXCEPTION 'invalid_job_cancel'; END IF;
  ELSIF NEW.state = 'CANCELED' THEN
    IF OLD.state NOT IN ('QUEUED','CANCEL_REQUESTED') THEN RAISE EXCEPTION 'invalid_job_cancel'; END IF;
  ELSIF NEW.state IN ('SUCCEEDED','FAILED') THEN
    IF (NEW.state = 'SUCCEEDED' AND OLD.state <> 'RUNNING') OR
       (NEW.state = 'FAILED' AND OLD.state NOT IN ('RUNNING','PAUSE_REQUESTED')) OR
       NEW.completed_at IS NULL OR NEW.claim_expires_at IS NOT NULL THEN
      RAISE EXCEPTION 'invalid_job_completion';
    END IF;
  ELSE
    RAISE EXCEPTION 'invalid_job_transition';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER jobs_follow_state_machine BEFORE INSERT OR UPDATE OR DELETE ON jobs
  FOR EACH ROW EXECUTE FUNCTION enforce_job_transition();

ALTER TABLE command_requests
  ALTER COLUMN configuration_id DROP NOT NULL,
  ADD COLUMN actor_user_id text REFERENCES users(id),
  ADD COLUMN session_id text REFERENCES sessions(id),
  ADD COLUMN command_kind text,
  ADD COLUMN target_type text,
  ADD COLUMN target_id text,
  ADD COLUMN reason text,
  ADD COLUMN idempotency_key text,
  ADD COLUMN expected_revision bigint CHECK (expected_revision > 0),
  ADD COLUMN correlation_id text,
  ADD COLUMN causation_id text,
  ADD COLUMN result_payload jsonb,
  ADD COLUMN audit_event_id text REFERENCES audit_events(id),
  ADD COLUMN updated_at timestamptz,
  ADD COLUMN entity_revision bigint NOT NULL DEFAULT 1 CHECK (entity_revision > 0);
CREATE UNIQUE INDEX command_actor_idempotency_idx
  ON command_requests(actor_user_id, idempotency_key)
  WHERE actor_user_id IS NOT NULL AND idempotency_key IS NOT NULL;

CREATE OR REPLACE FUNCTION protect_command_request() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.state <> 'pending' OR NEW.applied_at IS NOT NULL OR
       coalesce(NEW.updated_at, NEW.created_at) < NEW.created_at THEN
      RAISE EXCEPTION 'invalid_command_initial_state';
    END IF;
    NEW.updated_at := coalesce(NEW.updated_at, NEW.created_at);
    RETURN NEW;
  END IF;
  IF TG_OP = 'DELETE' THEN RAISE EXCEPTION 'invalid_command_transition'; END IF;
  IF (to_jsonb(NEW) - 'state' - 'applied_at' - 'updated_at' - 'result_payload' - 'entity_revision')
       IS DISTINCT FROM
     (to_jsonb(OLD) - 'state' - 'applied_at' - 'updated_at' - 'result_payload' - 'entity_revision') OR
     OLD.state <> 'pending' OR NEW.state NOT IN ('applied','rejected','failed') OR
     NEW.applied_at IS NULL OR NEW.applied_at < NEW.created_at OR
     NEW.updated_at < OLD.updated_at OR NEW.entity_revision <> OLD.entity_revision + 1 THEN
    RAISE EXCEPTION 'invalid_command_transition';
  END IF;
  RETURN NEW;
END;
$$;
ALTER TABLE outbox_events
  ADD COLUMN stream text NOT NULL DEFAULT 'system',
  ADD COLUMN schema_version text NOT NULL DEFAULT 'axiom.stream.v1',
  ADD COLUMN entity_type text NOT NULL DEFAULT 'legacy',
  ADD COLUMN entity_id text NOT NULL DEFAULT 'legacy',
  ADD COLUMN entity_revision bigint NOT NULL DEFAULT 1 CHECK (entity_revision > 0),
  ADD COLUMN event_time timestamptz,
  ADD COLUMN correlation_id text NOT NULL DEFAULT 'legacy',
  ADD COLUMN causation_id text NOT NULL DEFAULT 'legacy',
  ADD COLUMN payload jsonb NOT NULL DEFAULT '{}'::jsonb;
UPDATE outbox_events SET event_time = created_at WHERE event_time IS NULL;
ALTER TABLE outbox_events ALTER COLUMN event_time SET NOT NULL;
CREATE INDEX outbox_stream_resume_idx ON outbox_events(stream, revision);
CREATE INDEX outbox_retention_idx ON outbox_events(created_at, revision);

CREATE TABLE api_entity_revisions (
  entity_type text NOT NULL,
  entity_id text NOT NULL,
  revision bigint NOT NULL CHECK (revision > 0),
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (entity_type, entity_id)
);
INSERT INTO api_entity_revisions(entity_type,entity_id,revision,updated_at)
VALUES ('risk','global',1,CURRENT_TIMESTAMP);

ALTER TABLE risk_state_events ADD COLUMN entity_revision bigint;
WITH ordered AS (
  SELECT id,row_number() OVER (ORDER BY occurred_at,id) AS revision FROM risk_state_events
)
UPDATE risk_state_events event SET entity_revision=ordered.revision FROM ordered WHERE ordered.id=event.id;
ALTER TABLE risk_state_events ALTER COLUMN entity_revision SET NOT NULL;
CREATE UNIQUE INDEX risk_state_event_revision_idx ON risk_state_events(entity_revision);
UPDATE api_entity_revisions SET revision=greatest(
  revision,coalesce((SELECT max(entity_revision) FROM risk_state_events),0)
) WHERE entity_type='risk' AND entity_id='global';

CREATE TABLE shadow_sessions (
  id text PRIMARY KEY,
  command_id text NOT NULL UNIQUE REFERENCES command_requests(id),
  run_id text UNIQUE REFERENCES runs(id),
  portfolio_id text REFERENCES portfolios(id),
  state text NOT NULL CHECK (state IN ('QUEUED','RUNNING','PAUSED','CANCEL_REQUESTED','CANCELED','FAILED')),
  revision bigint NOT NULL CHECK (revision > 0),
  public_exchange text NOT NULL CHECK (public_exchange = 'binance-production-public'),
  simulation_only boolean NOT NULL CHECK (simulation_only),
  entries_enabled boolean NOT NULL,
  configuration_id text REFERENCES configuration_versions(id),
  strategy_version_id text REFERENCES strategy_versions(id),
  decision_dataset_id text REFERENCES dataset_manifests(id),
  created_at timestamptz NOT NULL,
  started_at timestamptz,
  stopped_at timestamptz,
  failure_code text,
  claim_owner text,
  claim_epoch bigint CHECK (claim_epoch IS NULL OR claim_epoch > 0),
  claim_expires_at timestamptz,
  CONSTRAINT shadow_claim_complete CHECK (
    (claim_owner IS NULL AND claim_epoch IS NULL AND claim_expires_at IS NULL) OR
    (claim_owner IS NOT NULL AND claim_epoch IS NOT NULL AND claim_expires_at IS NOT NULL)
  )
);
CREATE UNIQUE INDEX one_active_v1a_shadow_idx ON shadow_sessions((simulation_only))
  WHERE state IN ('QUEUED','RUNNING','PAUSED','CANCEL_REQUESTED');

CREATE TABLE stream_connections (
  id text PRIMARY KEY,
  user_id text NOT NULL REFERENCES users(id),
  session_id text NOT NULL REFERENCES sessions(id),
  opened_at timestamptz NOT NULL,
  heartbeat_at timestamptz NOT NULL,
  closed_at timestamptz,
  last_revision bigint NOT NULL CHECK (last_revision >= 0)
);
CREATE INDEX stream_connections_active_user_idx ON stream_connections(user_id, opened_at)
  WHERE closed_at IS NULL;

CREATE FUNCTION protect_session() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN RAISE EXCEPTION 'immutable_session_identity'; END IF;
  IF TG_OP = 'INSERT' THEN
    IF NEW.revision <> 1 OR NEW.revoked_at IS NOT NULL THEN RAISE EXCEPTION 'invalid_session_initial_state'; END IF;
    RETURN NEW;
  END IF;
  IF (to_jsonb(NEW) - 'last_seen_at' - 'idle_expires_at' - 'revoked_at' - 'revoked_reason' - 'revision')
       IS DISTINCT FROM
     (to_jsonb(OLD) - 'last_seen_at' - 'idle_expires_at' - 'revoked_at' - 'revoked_reason' - 'revision') OR
     NEW.revision <> OLD.revision + 1 OR NEW.last_seen_at < OLD.last_seen_at OR
     (OLD.revoked_at IS NOT NULL AND NEW.revoked_at IS DISTINCT FROM OLD.revoked_at) THEN
    RAISE EXCEPTION 'invalid_session_transition';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER sessions_protected BEFORE INSERT OR UPDATE OR DELETE ON sessions
  FOR EACH ROW EXECUTE FUNCTION protect_session();
CREATE TRIGGER authentication_failures_immutable BEFORE UPDATE OR DELETE ON authentication_failures
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER permissions_immutable BEFORE UPDATE OR DELETE ON authorization_permissions
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE FUNCTION protect_shadow_session() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN RAISE EXCEPTION 'immutable_shadow_session'; END IF;
  IF (to_jsonb(NEW) - 'run_id' - 'decision_dataset_id' - 'state' - 'revision' - 'entries_enabled' - 'started_at' - 'stopped_at' -
      'failure_code' - 'claim_owner' - 'claim_epoch' - 'claim_expires_at') IS DISTINCT FROM
     (to_jsonb(OLD) - 'run_id' - 'decision_dataset_id' - 'state' - 'revision' - 'entries_enabled' - 'started_at' - 'stopped_at' -
      'failure_code' - 'claim_owner' - 'claim_epoch' - 'claim_expires_at') THEN
    RAISE EXCEPTION 'immutable_shadow_session_identity';
  END IF;
  IF OLD.run_id IS NOT NULL AND NEW.run_id IS DISTINCT FROM OLD.run_id THEN
    RAISE EXCEPTION 'immutable_shadow_run';
  END IF;
  IF NEW.decision_dataset_id IS DISTINCT FROM OLD.decision_dataset_id AND
     (OLD.state NOT IN ('PAUSED','RUNNING','CANCEL_REQUESTED') OR
      NEW.state NOT IN ('PAUSED','RUNNING','CANCEL_REQUESTED')) THEN
    RAISE EXCEPTION 'immutable_shadow_dataset';
  END IF;
  IF NEW.state <> OLD.state THEN
    IF NEW.revision <> OLD.revision + 1 OR NOT (
      (OLD.state='QUEUED' AND NEW.state IN ('PAUSED','RUNNING','CANCELED','FAILED')) OR
      (OLD.state='PAUSED' AND NEW.state IN ('RUNNING','CANCEL_REQUESTED','FAILED')) OR
      (OLD.state='RUNNING' AND NEW.state IN ('PAUSED','CANCEL_REQUESTED','FAILED')) OR
      (OLD.state='CANCEL_REQUESTED' AND NEW.state IN ('CANCELED','FAILED'))
    ) THEN RAISE EXCEPTION 'invalid_shadow_transition'; END IF;
  ELSIF NEW.revision <> OLD.revision THEN
    RAISE EXCEPTION 'invalid_shadow_revision';
  END IF;
  IF NEW.entries_enabled AND NEW.state <> 'RUNNING' THEN
    RAISE EXCEPTION 'invalid_shadow_entries';
  END IF;
  IF NEW.claim_epoch IS NOT NULL AND OLD.claim_epoch IS NOT NULL AND NEW.claim_epoch < OLD.claim_epoch THEN
    RAISE EXCEPTION 'invalid_shadow_claim';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER shadow_sessions_protected BEFORE UPDATE OR DELETE ON shadow_sessions
  FOR EACH ROW EXECUTE FUNCTION protect_shadow_session();

CREATE OR REPLACE FUNCTION rotate_sessions_after_privilege_change() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE affected_user text;
BEGIN
  IF TG_TABLE_NAME = 'user_roles' THEN
    affected_user := coalesce(NEW.user_id, OLD.user_id);
    UPDATE users SET role_revision=role_revision+1 WHERE id=affected_user;
    UPDATE sessions SET revoked_at=CURRENT_TIMESTAMP, revoked_reason='privilege_change', revision=revision+1
      WHERE user_id=affected_user AND revoked_at IS NULL;
  ELSE
    UPDATE users SET role_revision=role_revision+1
      WHERE id IN (SELECT user_id FROM user_roles WHERE role_id=coalesce(NEW.role_id,OLD.role_id));
    UPDATE sessions SET revoked_at=CURRENT_TIMESTAMP, revoked_reason='privilege_change', revision=revision+1
      WHERE user_id IN (SELECT user_id FROM user_roles WHERE role_id=coalesce(NEW.role_id,OLD.role_id))
        AND revoked_at IS NULL;
  END IF;
  IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER user_role_rotates_sessions AFTER INSERT OR UPDATE OR DELETE ON user_roles
  FOR EACH ROW EXECUTE FUNCTION rotate_sessions_after_privilege_change();
CREATE TRIGGER role_permission_rotates_sessions AFTER INSERT OR UPDATE OR DELETE ON role_permissions
  FOR EACH ROW EXECUTE FUNCTION rotate_sessions_after_privilege_change();
