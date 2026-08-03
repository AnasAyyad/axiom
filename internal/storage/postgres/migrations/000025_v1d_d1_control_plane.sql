SET TIME ZONE 'UTC';

-- D1 keeps authorization permission based. Viewer remains a compatibility
-- role and intentionally receives no mutation permission.
INSERT INTO authorization_permissions(id, description) VALUES
  ('activity.read', 'Read redacted decisions, orders, and system activity'),
  ('research.control', 'Create and control approved research runs'),
  ('operations.control', 'Control runtime strategy and risk state'),
  ('incident.write', 'Own and update operational incidents'),
  ('alert.write', 'Acknowledge and test operational alerts'),
  ('artifacts.read', 'Download authorized redacted artifacts'),
  ('artifacts.manage', 'Delete or hold authorized redacted artifacts'),
  ('qualification.monitor', 'Read and abort approved qualification runs'),
  ('qualification.start', 'Start a formal approved qualification'),
  ('configuration.admin', 'Create and activate versioned configuration'),
  ('roles.admin', 'Administer user roles')
ON CONFLICT (id) DO NOTHING;

INSERT INTO authorization_roles(id, name) VALUES
  ('researcher','researcher'),
  ('operator','operator'),
  ('auditor','auditor')
ON CONFLICT (id) DO NOTHING;

INSERT INTO role_permissions(role_id, permission_id, granted_at)
SELECT 'owner', id, CURRENT_TIMESTAMP FROM authorization_permissions
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions(role_id, permission_id, granted_at) VALUES
  ('researcher','operations.read',CURRENT_TIMESTAMP),
  ('researcher','activity.read',CURRENT_TIMESTAMP),
  ('researcher','research.control',CURRENT_TIMESTAMP),
  ('researcher','artifacts.read',CURRENT_TIMESTAMP),
  ('operator','operations.read',CURRENT_TIMESTAMP),
  ('operator','activity.read',CURRENT_TIMESTAMP),
  ('operator','operations.control',CURRENT_TIMESTAMP),
  ('operator','incident.write',CURRENT_TIMESTAMP),
  ('operator','alert.write',CURRENT_TIMESTAMP),
  ('operator','artifacts.read',CURRENT_TIMESTAMP),
  ('operator','qualification.monitor',CURRENT_TIMESTAMP),
  ('operator','sandbox.read',CURRENT_TIMESTAMP),
  ('operator','sandbox.arm',CURRENT_TIMESTAMP),
  ('operator','sandbox.cancel',CURRENT_TIMESTAMP),
  ('auditor','operations.read',CURRENT_TIMESTAMP),
  ('auditor','activity.read',CURRENT_TIMESTAMP),
  ('auditor','incident.raw',CURRENT_TIMESTAMP),
  ('auditor','audit.raw',CURRENT_TIMESTAMP),
  ('auditor','artifacts.read',CURRENT_TIMESTAMP)
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions(role_id, permission_id, granted_at)
VALUES ('viewer','activity.read',CURRENT_TIMESTAMP)
ON CONFLICT DO NOTHING;

ALTER TABLE v1c_sandbox_authorizations
  DROP CONSTRAINT v1c_sandbox_authorizations_purpose_check,
  ADD COLUMN target_revision bigint CHECK (target_revision IS NULL OR target_revision > 0),
  ADD CONSTRAINT v1c_sandbox_authorizations_purpose_check CHECK (purpose IN (
    'sandbox_arm','risk_unlock','credential_rotation','revoke_all_sessions',
    'strategy_configuration','risk_control','qualification_start',
    'configuration_activation','role_change','artifact_hold'
  ));

ALTER TABLE v1c_high_risk_audit_events
  DROP CONSTRAINT v1c_high_risk_audit_events_purpose_check,
  ADD COLUMN target_revision bigint CHECK (target_revision IS NULL OR target_revision > 0),
  ADD CONSTRAINT v1c_high_risk_audit_events_purpose_check CHECK (purpose IN (
    'sandbox_arm','risk_unlock','credential_rotation','revoke_all_sessions',
    'strategy_configuration','risk_control','qualification_start',
    'configuration_activation','role_change','artifact_hold'
  ));

ALTER TABLE v1c_sandbox_authorizations
  ADD CONSTRAINT v1d_authorization_target_revision_required CHECK (
    (purpose IN ('strategy_configuration','risk_control','qualification_start',
      'configuration_activation','role_change','artifact_hold')) =
    (target_revision IS NOT NULL)
  );
ALTER TABLE v1c_high_risk_audit_events
  ADD CONSTRAINT v1d_audit_target_revision_required CHECK (
    outcome NOT IN ('authorization_issued','authorization_consumed') OR
    purpose NOT IN ('strategy_configuration','risk_control','qualification_start',
      'configuration_activation','role_change','artifact_hold') OR
    target_revision IS NOT NULL
  );

CREATE TABLE v1d_reason_catalogue (
  code text NOT NULL,
  version bigint NOT NULL CHECK (version > 0),
  summary text NOT NULL CHECK (length(summary) BETWEEN 3 AND 160),
  explanation text NOT NULL CHECK (length(explanation) BETWEEN 3 AND 1000),
  suggested_action text NOT NULL CHECK (length(suggested_action) BETWEEN 3 AND 500),
  severity text NOT NULL CHECK (severity IN ('info','warning','error','critical')),
  active boolean NOT NULL,
  recorded_at timestamptz NOT NULL,
  PRIMARY KEY (code, version)
);
CREATE UNIQUE INDEX v1d_reason_catalogue_active_idx
  ON v1d_reason_catalogue(code) WHERE active;

INSERT INTO v1d_reason_catalogue(
  code,version,summary,explanation,suggested_action,severity,active,recorded_at
) VALUES
  ('activity.unknown',1,'Activity recorded','The system recorded an event without a published explanation. No private payload is shown.','Use the correlation ID to review related authorized evidence.','warning',true,CURRENT_TIMESTAMP),
  ('decision.recorded',1,'Decision recorded','A strategy decision was recorded by the authoritative decision pipeline.','Review its linked risk evaluation and order outcome.','info',true,CURRENT_TIMESTAMP),
  ('risk.recorded',1,'Risk evaluation recorded','The central risk engine evaluated a decision using a versioned policy.','Review the decision and policy revision before changing controls.','info',true,CURRENT_TIMESTAMP),
  ('order.recorded',1,'Order state recorded','A simulated, Testnet, or Demo order state changed.','Review the decision-to-fill chain and reconciliation state.','info',true,CURRENT_TIMESTAMP),
  ('fill.recorded',1,'Fill recorded','A simulated, Testnet, or Demo fill was recorded.','Review accounting and reconciliation links.','info',true,CURRENT_TIMESTAMP),
  ('job.recorded',1,'Job state recorded','An approved durable job changed lifecycle state.','Open the job for progress, failure, and reproducibility details.','info',true,CURRENT_TIMESTAMP),
  ('incident.recorded',1,'Incident state recorded','An operational incident changed lifecycle state.','Review ownership, related activity, and remediation evidence.','warning',true,CURRENT_TIMESTAMP),
  ('alert.recorded',1,'Alert state recorded','An operational alert changed lifecycle state.','Acknowledge or escalate according to the incident runbook.','warning',true,CURRENT_TIMESTAMP),
  ('reconciliation.recorded',1,'Reconciliation state recorded','A reconciliation case changed state.','Resolve mismatches before enabling or resuming affected activity.','warning',true,CURRENT_TIMESTAMP),
  ('audit.recorded',1,'Audit event recorded','A security or operational action was appended to the audit chain.','Review the authorized redacted detail when required.','info',true,CURRENT_TIMESTAMP);

CREATE TABLE v1d_activity_projection (
  activity_revision bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  id text NOT NULL UNIQUE,
  view_kind text NOT NULL CHECK (view_kind IN ('decisions_orders','system_events')),
  source_type text NOT NULL,
  source_id text NOT NULL,
  source_revision text NOT NULL,
  reason_code text NOT NULL,
  outcome text NOT NULL,
  strategy_id text,
  instrument_id text,
  exchange_id text,
  side text CHECK (side IS NULL OR side IN ('buy','sell')),
  mode text CHECK (mode IS NULL OR mode IN ('backtest','replay','paper','shadow','testnet','demo')),
  correlation_id text NOT NULL,
  causation_id text,
  occurred_at timestamptz NOT NULL,
  details jsonb NOT NULL,
  projected_at timestamptz NOT NULL,
  UNIQUE (source_type, source_id, source_revision),
  CHECK (jsonb_typeof(details) = 'object')
);
CREATE INDEX v1d_activity_time_idx
  ON v1d_activity_projection(view_kind, occurred_at DESC, activity_revision DESC);
CREATE INDEX v1d_activity_filter_idx
  ON v1d_activity_projection(strategy_id, instrument_id, exchange_id, outcome, reason_code);
CREATE INDEX v1d_activity_correlation_idx
  ON v1d_activity_projection(correlation_id, occurred_at DESC);

CREATE FUNCTION project_v1d_activity(source_name text, source_row jsonb) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE
  source_identity text;
  source_version text;
  event_time timestamptz;
  event_outcome text;
  event_reason text;
  event_view text;
BEGIN
  source_identity := coalesce(source_row->>'id', source_row->>'revision');
  IF source_identity IS NULL OR source_identity = '' THEN
    RAISE EXCEPTION 'v1d_activity_source_identity_missing';
  END IF;
  source_version := coalesce(
    source_row->>'revision', source_row->>'entity_revision',
    source_row->>'progress_revision', md5(source_row::text)
  );
  event_time := coalesce(
    (source_row->>'updated_at')::timestamptz,
    (source_row->>'occurred_at')::timestamptz,
    (source_row->>'decided_at')::timestamptz,
    (source_row->>'evaluated_at')::timestamptz,
    (source_row->>'recorded_at')::timestamptz,
    (source_row->>'created_at')::timestamptz,
    (source_row->>'opened_at')::timestamptz
  );
  event_outcome := coalesce(
    source_row->>'outcome', source_row->>'state', source_row->>'event_type',
    source_row->>'alert_type', 'recorded'
  );
  event_view := CASE WHEN source_name IN
    ('decisions','risk_evaluations','orders','fills')
    THEN 'decisions_orders' ELSE 'system_events' END;
  event_reason := coalesce(source_row->>'reason_code', CASE source_name
    WHEN 'decisions' THEN 'decision.recorded'
    WHEN 'risk_evaluations' THEN 'risk.recorded'
    WHEN 'orders' THEN 'order.recorded'
    WHEN 'fills' THEN 'fill.recorded'
    WHEN 'jobs' THEN 'job.recorded'
    WHEN 'incidents' THEN 'incident.recorded'
    WHEN 'alerts' THEN 'alert.recorded'
    WHEN 'reconciliation_cases' THEN 'reconciliation.recorded'
    WHEN 'audit_events' THEN 'audit.recorded'
    ELSE 'activity.unknown' END);
  INSERT INTO v1d_activity_projection(
    id,view_kind,source_type,source_id,source_revision,reason_code,outcome,
    strategy_id,instrument_id,exchange_id,side,mode,correlation_id,causation_id,
    occurred_at,details,projected_at
  ) VALUES (
    'activity-' || md5(source_name || chr(31) || source_identity || chr(31) || source_version),
    event_view,source_name,source_identity,source_version,event_reason,event_outcome,
    coalesce(source_row->>'strategy_id',source_row->>'strategy_version_id'),
    source_row->>'instrument_id',
    coalesce(source_row->>'exchange_id',source_row->>'exchange'),
    nullif(source_row->>'side',''),nullif(source_row->>'mode',''),
    coalesce(source_row->>'correlation_id',source_row->>'causation_id',source_identity),
    source_row->>'causation_id',event_time,
    jsonb_strip_nulls(jsonb_build_object(
      'decision_id',source_row->>'decision_id',
      'order_id',source_row->>'order_id',
      'incident_id',source_row->>'incident_id',
      'run_id',source_row->>'run_id',
      'account_id',source_row->>'account_id',
      'job_type',source_row->>'job_type',
      'state',source_row->>'state',
      'outcome',source_row->>'outcome'
    )),event_time
  ) ON CONFLICT (source_type,source_id,source_revision) DO NOTHING;
END;
$$;

CREATE FUNCTION project_v1d_activity_trigger() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
BEGIN
  PERFORM project_v1d_activity(TG_TABLE_NAME, to_jsonb(NEW));
  RETURN NEW;
END;
$$;
REVOKE ALL ON FUNCTION project_v1d_activity(text,jsonb) FROM PUBLIC;
REVOKE ALL ON FUNCTION project_v1d_activity_trigger() FROM PUBLIC;

CREATE TRIGGER v1d_decisions_activity AFTER INSERT OR UPDATE ON decisions
  FOR EACH ROW EXECUTE FUNCTION project_v1d_activity_trigger();
CREATE TRIGGER v1d_risk_activity AFTER INSERT OR UPDATE ON risk_evaluations
  FOR EACH ROW EXECUTE FUNCTION project_v1d_activity_trigger();
CREATE TRIGGER v1d_orders_activity AFTER INSERT OR UPDATE ON orders
  FOR EACH ROW EXECUTE FUNCTION project_v1d_activity_trigger();
CREATE TRIGGER v1d_fills_activity AFTER INSERT OR UPDATE ON fills
  FOR EACH ROW EXECUTE FUNCTION project_v1d_activity_trigger();
CREATE TRIGGER v1d_jobs_activity AFTER INSERT OR UPDATE ON jobs
  FOR EACH ROW EXECUTE FUNCTION project_v1d_activity_trigger();
CREATE TRIGGER v1d_incidents_activity AFTER INSERT OR UPDATE ON incidents
  FOR EACH ROW EXECUTE FUNCTION project_v1d_activity_trigger();
CREATE TRIGGER v1d_alerts_activity AFTER INSERT OR UPDATE ON alerts
  FOR EACH ROW EXECUTE FUNCTION project_v1d_activity_trigger();
CREATE TRIGGER v1d_reconciliation_activity AFTER INSERT OR UPDATE ON reconciliation_cases
  FOR EACH ROW EXECUTE FUNCTION project_v1d_activity_trigger();
CREATE TRIGGER v1d_audit_activity AFTER INSERT OR UPDATE ON audit_events
  FOR EACH ROW EXECUTE FUNCTION project_v1d_activity_trigger();

DO $$
DECLARE
  source_name text;
  source_row jsonb;
BEGIN
  FOREACH source_name IN ARRAY ARRAY[
    'decisions','risk_evaluations','orders','fills','jobs','incidents','alerts',
    'reconciliation_cases','audit_events'
  ] LOOP
    FOR source_row IN EXECUTE format('SELECT to_jsonb(source) FROM %I source', source_name)
    LOOP
      PERFORM project_v1d_activity(source_name, source_row);
    END LOOP;
  END LOOP;
END;
$$;

CREATE TRIGGER v1d_activity_immutable BEFORE UPDATE OR DELETE ON v1d_activity_projection
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE FUNCTION protect_v1d_reason_catalogue() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP='DELETE' OR OLD.active=false OR NEW.active=true OR
     (to_jsonb(NEW)-'active') IS DISTINCT FROM (to_jsonb(OLD)-'active') THEN
    RAISE EXCEPTION 'v1d_reason_catalogue_immutable';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER v1d_reason_protected BEFORE UPDATE OR DELETE ON v1d_reason_catalogue
  FOR EACH ROW EXECUTE FUNCTION protect_v1d_reason_catalogue();

CREATE VIEW v1d_activity_explanations AS
SELECT activity.*,
  coalesce(reason.summary, fallback.summary) AS reason_summary,
  coalesce(reason.explanation, fallback.explanation) AS reason_explanation,
  coalesce(reason.suggested_action, fallback.suggested_action) AS suggested_action,
  coalesce(reason.severity, fallback.severity) AS severity,
  coalesce(reason.version, fallback.version) AS reason_version,
  (reason.code IS NULL) AS unknown_reason
FROM v1d_activity_projection activity
LEFT JOIN v1d_reason_catalogue reason
  ON reason.code=activity.reason_code AND reason.active
CROSS JOIN v1d_reason_catalogue fallback
WHERE fallback.code='activity.unknown' AND fallback.active;

CREATE TABLE v1d_strategy_controls (
  strategy_id text PRIMARY KEY REFERENCES strategy_definitions(id),
  configured_state text NOT NULL CHECK (configured_state IN ('enabled','disabled')),
  runtime_state text NOT NULL CHECK (runtime_state IN ('running','paused','blocked')),
  blocking_prerequisites jsonb NOT NULL,
  configuration_id text REFERENCES configuration_versions(id),
  revision bigint NOT NULL CHECK (revision > 0),
  updated_by text NOT NULL,
  updated_at timestamptz NOT NULL,
  CHECK (jsonb_typeof(blocking_prerequisites) = 'array'),
  CHECK (configured_state='disabled' OR jsonb_array_length(blocking_prerequisites)=0),
  CHECK (runtime_state<>'running' OR (configured_state='enabled' AND jsonb_array_length(blocking_prerequisites)=0))
);
INSERT INTO v1d_strategy_controls(
  strategy_id,configured_state,runtime_state,blocking_prerequisites,
  configuration_id,revision,updated_by,updated_at
)
SELECT id,'disabled','blocked','["configuration_not_approved","runtime_preflight_required"]'::jsonb,
  NULL,1,'v1d_d1_migration',CURRENT_TIMESTAMP
FROM strategy_definitions
ON CONFLICT DO NOTHING;

CREATE FUNCTION seed_v1d_strategy_control() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
BEGIN
  INSERT INTO v1d_strategy_controls(
    strategy_id,configured_state,runtime_state,blocking_prerequisites,
    configuration_id,revision,updated_by,updated_at
  ) VALUES (
    NEW.id,'disabled','blocked',
    '["configuration_not_approved","runtime_preflight_required"]'::jsonb,
    NULL,1,'v1d_strategy_seed',CURRENT_TIMESTAMP
  ) ON CONFLICT DO NOTHING;
  RETURN NEW;
END;
$$;
REVOKE ALL ON FUNCTION seed_v1d_strategy_control() FROM PUBLIC;
CREATE TRIGGER v1d_strategy_control_seed AFTER INSERT ON strategy_definitions
  FOR EACH ROW EXECUTE FUNCTION seed_v1d_strategy_control();

CREATE FUNCTION protect_v1d_strategy_control() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP='DELETE' OR
     (TG_OP='INSERT' AND (NEW.revision<>1 OR NEW.configured_state<>'disabled' OR NEW.runtime_state<>'blocked')) OR
     (TG_OP='UPDATE' AND (
       NEW.strategy_id<>OLD.strategy_id OR NEW.revision<>OLD.revision+1 OR
       NEW.updated_at<OLD.updated_at
     )) THEN
    RAISE EXCEPTION 'v1d_strategy_control_invalid';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER v1d_strategy_control_protected
  BEFORE INSERT OR UPDATE OR DELETE ON v1d_strategy_controls
  FOR EACH ROW EXECUTE FUNCTION protect_v1d_strategy_control();

CREATE TABLE v1d_risk_controls (
  scope_type text NOT NULL CHECK (scope_type IN ('global','strategy','instrument','exchange','new_entries')),
  scope_id text NOT NULL,
  state text NOT NULL CHECK (state IN ('normal','paused','locked')),
  revision bigint NOT NULL CHECK (revision > 0),
  reason_code text NOT NULL,
  updated_by text NOT NULL,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (scope_type,scope_id)
);
INSERT INTO v1d_risk_controls(
  scope_type,scope_id,state,revision,reason_code,updated_by,updated_at
) VALUES ('global','global','locked',1,'activity.unknown','v1d_d1_migration',CURRENT_TIMESTAMP)
ON CONFLICT DO NOTHING;

CREATE FUNCTION protect_v1d_risk_control() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP='DELETE' OR
     (TG_OP='INSERT' AND (NEW.revision<>1 OR NEW.state<>'locked')) OR
     (TG_OP='UPDATE' AND (
       NEW.scope_type<>OLD.scope_type OR NEW.scope_id<>OLD.scope_id OR
       NEW.revision<>OLD.revision+1 OR NEW.updated_at<OLD.updated_at
     )) THEN
    RAISE EXCEPTION 'v1d_risk_control_invalid';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER v1d_risk_control_protected
  BEFORE INSERT OR UPDATE OR DELETE ON v1d_risk_controls
  FOR EACH ROW EXECUTE FUNCTION protect_v1d_risk_control();

CREATE TABLE v1d_export_artifacts (
  id text PRIMARY KEY,
  command_id text NOT NULL UNIQUE REFERENCES command_requests(id),
  job_id text NOT NULL UNIQUE REFERENCES jobs(id),
  owner_user_id text NOT NULL REFERENCES users(id),
  resource_type text NOT NULL,
  resource_id text NOT NULL,
  format text NOT NULL CHECK (format IN ('txt','csv','json','jsonl')),
  content_type text NOT NULL CHECK (content_type IN (
    'text/plain','text/csv','application/json','application/x-ndjson'
  )),
  content text,
  content_hash sha256_hex NOT NULL,
  size_bytes bigint NOT NULL CHECK (size_bytes BETWEEN 1 AND 10485760),
  redaction_version text NOT NULL,
  created_at timestamptz NOT NULL,
  expires_at timestamptz NOT NULL,
  deleted_at timestamptz,
  deletion_reason text,
  CHECK (expires_at = created_at + interval '7 days'),
  CHECK ((deleted_at IS NULL AND content IS NOT NULL AND deletion_reason IS NULL) OR
         (deleted_at IS NOT NULL AND content IS NULL AND deletion_reason IS NOT NULL))
);
CREATE INDEX v1d_export_retention_idx
  ON v1d_export_artifacts(expires_at,id) WHERE deleted_at IS NULL;

CREATE FUNCTION protect_v1d_export_artifact() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP='DELETE' OR OLD.deleted_at IS NOT NULL OR NEW.deleted_at IS NULL OR
     NEW.content IS NOT NULL OR NEW.deletion_reason IS NULL OR
     (to_jsonb(NEW)-ARRAY['content','deleted_at','deletion_reason']) IS DISTINCT FROM
     (to_jsonb(OLD)-ARRAY['content','deleted_at','deletion_reason']) THEN
    RAISE EXCEPTION 'v1d_export_artifact_immutable';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER v1d_export_artifact_protected
  BEFORE UPDATE OR DELETE ON v1d_export_artifacts
  FOR EACH ROW EXECUTE FUNCTION protect_v1d_export_artifact();

CREATE TABLE v1d_artifact_holds (
  id text PRIMARY KEY,
  artifact_id text NOT NULL REFERENCES v1d_export_artifacts(id),
  hold_type text NOT NULL CHECK (hold_type IN ('incident','reproducibility')),
  reference_id text NOT NULL,
  reason text NOT NULL CHECK (length(reason) BETWEEN 8 AND 500),
  actor_user_id text NOT NULL REFERENCES users(id),
  created_at timestamptz NOT NULL,
  released_at timestamptz,
  released_by text REFERENCES users(id),
  UNIQUE (artifact_id,hold_type,reference_id),
  CHECK ((released_at IS NULL) = (released_by IS NULL))
);
CREATE INDEX v1d_artifact_active_hold_idx
  ON v1d_artifact_holds(artifact_id) WHERE released_at IS NULL;

CREATE FUNCTION protect_v1d_artifact_hold() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP='DELETE' OR OLD.released_at IS NOT NULL OR NEW.released_at IS NULL OR
     NEW.released_by IS NULL OR
     (to_jsonb(NEW)-ARRAY['released_at','released_by']) IS DISTINCT FROM
     (to_jsonb(OLD)-ARRAY['released_at','released_by']) THEN
    RAISE EXCEPTION 'v1d_artifact_hold_immutable';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER v1d_artifact_hold_protected
  BEFORE UPDATE OR DELETE ON v1d_artifact_holds
  FOR EACH ROW EXECUTE FUNCTION protect_v1d_artifact_hold();

CREATE TABLE v1d_artifact_access_events (
  id text PRIMARY KEY,
  artifact_id text NOT NULL REFERENCES v1d_export_artifacts(id),
  actor_user_id text NOT NULL REFERENCES users(id),
  action text NOT NULL CHECK (action IN ('created','downloaded','deleted','held','hold_released')),
  reason text,
  correlation_id text NOT NULL,
  occurred_at timestamptz NOT NULL
);
CREATE TRIGGER v1d_artifact_access_immutable
  BEFORE UPDATE OR DELETE ON v1d_artifact_access_events
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();

CREATE TABLE v1d_qualification_catalogue (
  id text PRIMARY KEY,
  name text NOT NULL,
  kind text NOT NULL CHECK (kind IN ('backtest','replay','shadow','sandbox','formal','drill')),
  duration_seconds bigint CHECK (duration_seconds IS NULL OR duration_seconds > 0),
  owner_start_required boolean NOT NULL,
  abort_permission text NOT NULL REFERENCES authorization_permissions(id),
  active boolean NOT NULL,
  definition_revision bigint NOT NULL CHECK (definition_revision > 0),
  recorded_at timestamptz NOT NULL
);
INSERT INTO v1d_qualification_catalogue(
  id,name,kind,duration_seconds,owner_start_required,abort_permission,active,definition_revision,recorded_at
) VALUES
  ('b2-market-data','B2 coherent market-data qualification','formal',NULL,true,'qualification.monitor',true,1,CURRENT_TIMESTAMP),
  ('c6-sandbox-72h','C6 sandbox order and reconciliation qualification','sandbox',259200,true,'qualification.monitor',true,1,CURRENT_TIMESTAMP),
  ('d5-readiness-7d','D5 full-platform readiness qualification','formal',604800,true,'qualification.monitor',true,1,CURRENT_TIMESTAMP),
  ('restore-drill','Database and market-data restore drill','drill',NULL,true,'qualification.monitor',true,1,CURRENT_TIMESTAMP),
  ('incident-replay-drill','Incident replay drill','drill',NULL,false,'qualification.monitor',true,1,CURRENT_TIMESTAMP);

CREATE TABLE v1d_qualification_runs (
  id text PRIMARY KEY,
  qualification_id text NOT NULL REFERENCES v1d_qualification_catalogue(id),
  command_id text NOT NULL UNIQUE REFERENCES command_requests(id),
  state text NOT NULL CHECK (state IN (
    'PREFLIGHT','QUEUED','RUNNING','ABORT_REQUESTED','CANCELED','SUCCEEDED','FAILED'
  )),
  revision bigint NOT NULL CHECK (revision > 0),
  source_sha text NOT NULL CHECK (source_sha ~ '^[0-9a-f]{40}$'),
  configuration_hash sha256_hex NOT NULL,
  image_digest text,
  server_identity text,
  evidence_reference text,
  started_by text NOT NULL REFERENCES users(id),
  started_at timestamptz,
  completed_at timestamptz,
  failure_code text,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL
);
CREATE INDEX v1d_qualification_runs_state_idx
  ON v1d_qualification_runs(state,created_at DESC,id);

CREATE FUNCTION protect_v1d_qualification_run() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE valid_transition boolean;
BEGIN
  IF TG_OP='DELETE' THEN
    RAISE EXCEPTION 'v1d_qualification_run_immutable';
  END IF;
  IF TG_OP='INSERT' THEN
    IF NEW.state<>'PREFLIGHT' OR NEW.revision<>1 OR NEW.started_at IS NOT NULL OR
       NEW.completed_at IS NOT NULL OR NEW.failure_code IS NOT NULL OR
       NEW.evidence_reference IS NOT NULL THEN
      RAISE EXCEPTION 'v1d_qualification_initial_state_invalid';
    END IF;
    RETURN NEW;
  END IF;
  valid_transition :=
    (OLD.state='PREFLIGHT' AND NEW.state IN ('QUEUED','CANCELED','FAILED')) OR
    (OLD.state='QUEUED' AND NEW.state IN ('RUNNING','CANCELED','FAILED')) OR
    (OLD.state='RUNNING' AND NEW.state IN ('ABORT_REQUESTED','SUCCEEDED','FAILED')) OR
    (OLD.state='ABORT_REQUESTED' AND NEW.state IN ('CANCELED','FAILED'));
  IF NOT valid_transition OR NEW.revision<>OLD.revision+1 OR
     NEW.updated_at<OLD.updated_at OR
     (to_jsonb(NEW)-ARRAY['state','revision','started_at','completed_at',
       'failure_code','evidence_reference','updated_at']) IS DISTINCT FROM
     (to_jsonb(OLD)-ARRAY['state','revision','started_at','completed_at',
       'failure_code','evidence_reference','updated_at']) OR
     (NEW.state='RUNNING' AND NEW.started_at IS NULL) OR
     (NEW.state IN ('CANCELED','SUCCEEDED','FAILED') AND NEW.completed_at IS NULL) OR
     (NEW.state='FAILED' AND NEW.failure_code IS NULL) OR
     (NEW.state='SUCCEEDED' AND NEW.evidence_reference IS NULL) THEN
    RAISE EXCEPTION 'v1d_qualification_transition_invalid';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER v1d_qualification_run_protected
  BEFORE INSERT OR UPDATE OR DELETE ON v1d_qualification_runs
  FOR EACH ROW EXECUTE FUNCTION protect_v1d_qualification_run();

CREATE TABLE v1d_role_change_events (
  id text PRIMARY KEY,
  command_id text NOT NULL UNIQUE REFERENCES command_requests(id),
  target_user_id text NOT NULL REFERENCES users(id),
  prior_roles text[] NOT NULL,
  new_roles text[] NOT NULL,
  role_revision bigint NOT NULL CHECK (role_revision > 1),
  actor_user_id text NOT NULL REFERENCES users(id),
  reason text NOT NULL CHECK (length(reason) BETWEEN 8 AND 500),
  occurred_at timestamptz NOT NULL
);
CREATE TRIGGER v1d_role_change_immutable
  BEFORE UPDATE OR DELETE ON v1d_role_change_events
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
