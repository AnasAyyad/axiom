SET TIME ZONE 'UTC';

ALTER TABLE incidents
  ADD COLUMN owner_user_id text REFERENCES users(id),
  ADD COLUMN revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  ADD COLUMN acknowledged_at timestamptz,
  ADD COLUMN updated_at timestamptz;
UPDATE incidents SET
  revision=CASE state WHEN 'open' THEN 1 WHEN 'acknowledged' THEN 2 ELSE 3 END,
  acknowledged_at=CASE WHEN state IN ('acknowledged','resolved') THEN opened_at ELSE NULL END,
  updated_at=coalesce(resolved_at,opened_at);
ALTER TABLE incidents ALTER COLUMN updated_at SET NOT NULL;
ALTER TABLE incidents ALTER COLUMN updated_at SET DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE incidents ADD CONSTRAINT incidents_lifecycle_time_order CHECK (
  updated_at>=opened_at AND
  (acknowledged_at IS NULL OR acknowledged_at>=opened_at) AND
  (resolved_at IS NULL OR resolved_at>=opened_at)
);

CREATE TABLE v1d_incident_events (
  id text PRIMARY KEY,
  incident_id text NOT NULL REFERENCES incidents(id),
  incident_revision bigint NOT NULL CHECK (incident_revision > 0),
  event_type text NOT NULL CHECK (event_type IN (
    'opened','acknowledged','owner_assigned','remediation_added','alert_linked',
    'activity_linked','replay_linked','evidence_held','resolved'
  )),
  actor text NOT NULL,
  reason text NOT NULL CHECK (length(reason) BETWEEN 3 AND 2000),
  reference_type text,
  reference_id text,
  detail text CHECK (detail IS NULL OR length(detail) BETWEEN 3 AND 2000),
  previous_hash sha256_hex,
  event_hash sha256_hex NOT NULL UNIQUE,
  correlation_id text NOT NULL,
  occurred_at timestamptz NOT NULL,
  UNIQUE (incident_id,incident_revision),
  CHECK ((reference_type IS NULL)=(reference_id IS NULL))
);
CREATE INDEX v1d_incident_events_time_idx
  ON v1d_incident_events(incident_id,occurred_at,id);
CREATE TRIGGER v1d_incident_events_immutable BEFORE UPDATE OR DELETE ON v1d_incident_events
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();

INSERT INTO v1d_incident_events(
  id,incident_id,incident_revision,event_type,actor,reason,event_hash,
  correlation_id,occurred_at
)
SELECT 'incident-event-'||id,id,1,'opened','migration',reason_code,
  encode(sha256(convert_to(id||chr(31)||reason_code||chr(31)||
    opened_at::text,'UTF8')),'hex'),id,opened_at
FROM incidents
ON CONFLICT DO NOTHING;

CREATE TABLE v1d_incident_replay_inputs (
  incident_id text PRIMARY KEY REFERENCES incidents(id),
  dataset_id text NOT NULL REFERENCES dataset_manifests(id),
  first_ordinal bigint NOT NULL CHECK (first_ordinal > 0),
  last_ordinal bigint NOT NULL CHECK (last_ordinal >= first_ordinal),
  source_identity text NOT NULL,
  linked_by text NOT NULL REFERENCES users(id),
  linked_at timestamptz NOT NULL
);
CREATE TABLE v1d_incident_alert_links (
  incident_id text NOT NULL REFERENCES incidents(id),
  alert_id text NOT NULL REFERENCES alerts(id),
  linked_by text NOT NULL REFERENCES users(id),
  linked_at timestamptz NOT NULL,
  PRIMARY KEY (incident_id,alert_id)
);
CREATE TABLE v1d_incident_activity_links (
  incident_id text NOT NULL REFERENCES incidents(id),
  activity_id text NOT NULL REFERENCES v1d_activity_projection(id),
  linked_by text NOT NULL REFERENCES users(id),
  linked_at timestamptz NOT NULL,
  PRIMARY KEY (incident_id,activity_id)
);
CREATE TABLE v1d_incident_resolution_evidence (
  incident_id text PRIMARY KEY REFERENCES incidents(id),
  evidence text NOT NULL CHECK (length(evidence) BETWEEN 3 AND 2000),
  evidence_hash sha256_hex NOT NULL,
  recorded_by text NOT NULL REFERENCES users(id),
  recorded_at timestamptz NOT NULL
);
CREATE TRIGGER v1d_incident_replay_immutable BEFORE UPDATE OR DELETE ON v1d_incident_replay_inputs
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER v1d_incident_alert_links_immutable BEFORE UPDATE OR DELETE ON v1d_incident_alert_links
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER v1d_incident_activity_links_immutable BEFORE UPDATE OR DELETE ON v1d_incident_activity_links
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER v1d_incident_resolution_immutable BEFORE UPDATE OR DELETE ON v1d_incident_resolution_evidence
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();

CREATE TABLE v1d_report_schedules (
  id text PRIMARY KEY,
  report_type text NOT NULL CHECK (report_type IN (
    'strategy_results','decisions_orders','portfolios','inventory_pnl','risk',
    'exchange_data_health','lab_runs','sandbox_qualifications','platform_readiness'
  )),
  frequency text NOT NULL CHECK (frequency IN ('hourly','daily','weekly')),
  minute_utc integer NOT NULL CHECK (minute_utc BETWEEN 0 AND 59),
  hour_utc integer CHECK (hour_utc BETWEEN 0 AND 23),
  weekday_utc integer CHECK (weekday_utc BETWEEN 0 AND 6),
  state text NOT NULL CHECK (state IN ('active','paused')),
  next_run_at timestamptz NOT NULL,
  last_run_at timestamptz,
  revision bigint NOT NULL CHECK (revision > 0),
  owner_user_id text NOT NULL REFERENCES users(id),
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  CHECK (
    (frequency='hourly' AND hour_utc IS NULL AND weekday_utc IS NULL) OR
    (frequency='daily' AND hour_utc IS NOT NULL AND weekday_utc IS NULL) OR
    (frequency='weekly' AND hour_utc IS NOT NULL AND weekday_utc IS NOT NULL)
  ),
  CHECK (updated_at>=created_at),
  CHECK (last_run_at IS NULL OR last_run_at>=created_at)
);
CREATE INDEX v1d_report_schedules_due_idx
  ON v1d_report_schedules(next_run_at,id) WHERE state='active';

CREATE TABLE v1d_reports (
  id text PRIMARY KEY,
  job_id text NOT NULL UNIQUE REFERENCES jobs(id),
  schedule_id text REFERENCES v1d_report_schedules(id),
  scheduled_for timestamptz,
  report_type text NOT NULL CHECK (report_type IN (
    'strategy_results','decisions_orders','portfolios','inventory_pnl','risk',
    'exchange_data_health','lab_runs','sandbox_qualifications','platform_readiness'
  )),
  state text NOT NULL CHECK (state IN ('QUEUED','RUNNING','SUCCEEDED','FAILED')),
  mode text NOT NULL CHECK (mode IN (
    'backtest','replay','paper','shadow','testnet','demo','mixed','operational'
  )),
  confidence_tier text NOT NULL,
  valuation_basis text NOT NULL,
  model_provenance jsonb NOT NULL CHECK (jsonb_typeof(model_provenance)='object'),
  maturity text NOT NULL,
  source_identity text NOT NULL,
  source_revision bigint NOT NULL CHECK (source_revision >= 0),
  content jsonb,
  content_hash sha256_hex,
  failure_code text,
  generated_at timestamptz,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  revision bigint NOT NULL CHECK (revision > 0),
  CHECK ((state='SUCCEEDED')=(content IS NOT NULL AND content_hash IS NOT NULL AND generated_at IS NOT NULL)),
  CHECK ((state='FAILED')=(failure_code IS NOT NULL)),
  CHECK (updated_at>=created_at)
);
CREATE INDEX v1d_reports_time_idx ON v1d_reports(created_at DESC,id);
CREATE UNIQUE INDEX v1d_reports_schedule_window_idx
  ON v1d_reports(schedule_id,scheduled_for) WHERE schedule_id IS NOT NULL;

CREATE FUNCTION protect_v1d_report() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP='DELETE' OR
    (TG_OP='INSERT' AND (NEW.state<>'QUEUED' OR NEW.revision<>1)) OR
    (TG_OP='UPDATE' AND (
      OLD.state NOT IN ('QUEUED','RUNNING') OR
      (OLD.state='QUEUED' AND NEW.state NOT IN ('RUNNING','FAILED')) OR
      (OLD.state='RUNNING' AND NEW.state NOT IN ('SUCCEEDED','FAILED')) OR
      NEW.revision<>OLD.revision+1 OR NEW.updated_at<OLD.updated_at OR
      (to_jsonb(NEW)-ARRAY['state','content','content_hash','failure_code',
        'generated_at','updated_at','revision']) IS DISTINCT FROM
      (to_jsonb(OLD)-ARRAY['state','content','content_hash','failure_code',
        'generated_at','updated_at','revision'])
    )) THEN RAISE EXCEPTION 'v1d_report_transition_invalid'; END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER v1d_report_protected BEFORE INSERT OR UPDATE OR DELETE ON v1d_reports
  FOR EACH ROW EXECUTE FUNCTION protect_v1d_report();

CREATE TABLE v1d_alert_routes (
  id text PRIMARY KEY,
  sink_name text NOT NULL UNIQUE,
  enabled boolean NOT NULL,
  minimum_severity text NOT NULL CHECK (minimum_severity IN ('info','warning','critical')),
  target_label text NOT NULL,
  revision bigint NOT NULL CHECK (revision > 0),
  updated_at timestamptz NOT NULL
);
INSERT INTO v1d_alert_routes(id,sink_name,enabled,minimum_severity,target_label,revision,updated_at) VALUES
  ('in-app','in_app',true,'info','Axiom in-app Alert Center',1,CURRENT_TIMESTAMP),
  ('webhook','webhook',false,'warning','Allowlisted HTTPS webhook when runtime configured',1,CURRENT_TIMESTAMP);

CREATE TABLE v1d_alert_delivery_attempts (
  id text PRIMARY KEY,
  delivery_id text NOT NULL REFERENCES alert_deliveries(id),
  alert_id text NOT NULL REFERENCES alerts(id),
  sink_name text NOT NULL,
  attempt integer NOT NULL CHECK (attempt > 0),
  state text NOT NULL CHECK (state IN ('delivered','failed')),
  reason_code text,
  started_at timestamptz NOT NULL,
  completed_at timestamptz NOT NULL,
  latency_ms bigint NOT NULL CHECK (latency_ms >= 0),
  UNIQUE (delivery_id,attempt),
  CHECK (completed_at>=started_at)
);
CREATE TRIGGER v1d_alert_attempt_immutable BEFORE UPDATE OR DELETE ON v1d_alert_delivery_attempts
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();

CREATE TABLE v1d_alert_escalations (
  id text PRIMARY KEY,
  alert_id text NOT NULL REFERENCES alerts(id),
  revision bigint NOT NULL CHECK (revision > 0),
  actor_user_id text NOT NULL REFERENCES users(id),
  reason text NOT NULL CHECK (length(reason) BETWEEN 8 AND 500),
  escalated_at timestamptz NOT NULL,
  UNIQUE (alert_id,revision)
);
CREATE TRIGGER v1d_alert_escalation_immutable BEFORE UPDATE OR DELETE ON v1d_alert_escalations
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();

CREATE TABLE v1d_alert_route_tests (
  id text PRIMARY KEY,
  command_id text NOT NULL UNIQUE REFERENCES command_requests(id),
  route_id text NOT NULL REFERENCES v1d_alert_routes(id),
  alert_id text REFERENCES alerts(id),
  state text NOT NULL CHECK (state IN ('pending','delivered','failed')),
  requested_by text NOT NULL REFERENCES users(id),
  reason text NOT NULL CHECK (length(reason) BETWEEN 8 AND 500),
  requested_at timestamptz NOT NULL,
  completed_at timestamptz
);

CREATE TABLE v1d_audit_chain (
  chain_sequence bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  audit_event_id text NOT NULL UNIQUE REFERENCES audit_events(id),
  previous_event_hash sha256_hex,
  event_hash sha256_hex NOT NULL,
  recorded_at timestamptz NOT NULL
);
CREATE TRIGGER v1d_audit_chain_immutable BEFORE UPDATE OR DELETE ON v1d_audit_chain
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();

CREATE FUNCTION append_v1d_audit_chain() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
DECLARE prior sha256_hex;
BEGIN
  PERFORM pg_advisory_xact_lock(hashtext('axiom:v1d:audit-chain'));
  SELECT event_hash INTO prior FROM v1d_audit_chain ORDER BY chain_sequence DESC LIMIT 1;
  INSERT INTO v1d_audit_chain(audit_event_id,previous_event_hash,event_hash,recorded_at)
  VALUES(NEW.id,prior,NEW.event_hash,NEW.recorded_at);
  RETURN NEW;
END;
$$;
REVOKE ALL ON FUNCTION append_v1d_audit_chain() FROM PUBLIC;

DO $$
DECLARE row_value record; prior sha256_hex;
BEGIN
  FOR row_value IN SELECT id,event_hash,recorded_at FROM audit_events ORDER BY recorded_at,id LOOP
    INSERT INTO v1d_audit_chain(audit_event_id,previous_event_hash,event_hash,recorded_at)
    VALUES(row_value.id,prior,row_value.event_hash,row_value.recorded_at);
    prior:=row_value.event_hash;
  END LOOP;
END;
$$;
CREATE TRIGGER v1d_audit_chain_append AFTER INSERT ON audit_events
  FOR EACH ROW EXECUTE FUNCTION append_v1d_audit_chain();

CREATE FUNCTION audit_v1d_report_lifecycle() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
BEGIN
  INSERT INTO audit_events(
    id,event_type,actor,causation_id,correlation_id,event_hash,recorded_at
  ) VALUES(
    'audit-report-'||NEW.id||'-'||NEW.revision,
    'report_'||lower(NEW.state),'system',NEW.job_id,NEW.id,
    encode(sha256(convert_to(NEW.id||chr(31)||NEW.job_id||chr(31)||NEW.state||
      chr(31)||NEW.revision::text||chr(31)||coalesce(NEW.content_hash::text,''),
      'UTF8')),'hex'),NEW.updated_at
  );
  RETURN NEW;
END;
$$;
REVOKE ALL ON FUNCTION audit_v1d_report_lifecycle() FROM PUBLIC;
CREATE TRIGGER v1d_report_lifecycle_audit
  AFTER INSERT OR UPDATE ON v1d_reports
  FOR EACH ROW EXECUTE FUNCTION audit_v1d_report_lifecycle();

CREATE FUNCTION audit_v1d_artifact_access() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
BEGIN
  INSERT INTO audit_events(
    id,event_type,actor,causation_id,correlation_id,event_hash,recorded_at
  ) VALUES(
    'audit-'||NEW.id,'evidence_access_'||NEW.action,NEW.actor_user_id,
    NEW.artifact_id,NEW.correlation_id,
    encode(sha256(convert_to(NEW.id||chr(31)||NEW.artifact_id||chr(31)||
      NEW.action||chr(31)||coalesce(NEW.reason,''),'UTF8')),'hex'),NEW.occurred_at
  ) ON CONFLICT DO NOTHING;
  RETURN NEW;
END;
$$;
REVOKE ALL ON FUNCTION audit_v1d_artifact_access() FROM PUBLIC;
INSERT INTO audit_events(
  id,event_type,actor,causation_id,correlation_id,event_hash,recorded_at
)
SELECT 'audit-'||id,'evidence_access_'||action,actor_user_id,artifact_id,correlation_id,
  encode(sha256(convert_to(id||chr(31)||artifact_id||chr(31)||action||chr(31)||
    coalesce(reason,''),'UTF8')),'hex'),occurred_at
FROM v1d_artifact_access_events ON CONFLICT DO NOTHING;
CREATE TRIGGER v1d_artifact_access_audit AFTER INSERT ON v1d_artifact_access_events
  FOR EACH ROW EXECUTE FUNCTION audit_v1d_artifact_access();

CREATE FUNCTION audit_v1d_high_risk_event() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
BEGIN
  INSERT INTO audit_events(
    id,event_type,actor,causation_id,correlation_id,event_hash,recorded_at
  ) VALUES(
    'audit-'||NEW.id,'high_risk_'||NEW.purpose||'_'||NEW.outcome,
    NEW.actor_user_id,NEW.id,NEW.session_id,NEW.event_hash,NEW.occurred_at
  ) ON CONFLICT DO NOTHING;
  RETURN NEW;
END;
$$;
REVOKE ALL ON FUNCTION audit_v1d_high_risk_event() FROM PUBLIC;
INSERT INTO audit_events(
  id,event_type,actor,causation_id,correlation_id,event_hash,recorded_at
)
SELECT 'audit-'||id,'high_risk_'||purpose||'_'||outcome,actor_user_id,id,
  session_id,event_hash,occurred_at FROM v1c_high_risk_audit_events
ON CONFLICT DO NOTHING;
CREATE TRIGGER v1d_high_risk_audit_projection AFTER INSERT ON v1c_high_risk_audit_events
  FOR EACH ROW EXECUTE FUNCTION audit_v1d_high_risk_event();

CREATE FUNCTION validate_v1d_artifact_hold_reference() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
BEGIN
  IF NEW.hold_type='incident' AND NOT EXISTS(
    SELECT 1 FROM incidents WHERE id=NEW.reference_id
  ) THEN RAISE EXCEPTION 'v1d_incident_hold_reference_missing'; END IF;
  IF NEW.hold_type='reproducibility' AND NOT EXISTS(
    SELECT 1 FROM jobs WHERE id=NEW.reference_id
  ) THEN RAISE EXCEPTION 'v1d_reproduction_hold_reference_missing'; END IF;
  RETURN NEW;
END;
$$;
REVOKE ALL ON FUNCTION validate_v1d_artifact_hold_reference() FROM PUBLIC;
CREATE TRIGGER v1d_artifact_hold_reference
  BEFORE INSERT ON v1d_artifact_holds
  FOR EACH ROW EXECUTE FUNCTION validate_v1d_artifact_hold_reference();

INSERT INTO v1d_reason_catalogue(
  code,version,summary,explanation,suggested_action,severity,active,recorded_at
) VALUES
  ('report.recorded',1,'Report state changed','A provenance-preserving report changed durable lifecycle state.','Review its source identity, model provenance, and content hash.','info',true,CURRENT_TIMESTAMP),
  ('incident.remediation',1,'Incident remediation updated','An authorized operator added incident remediation or resolution evidence.','Review the immutable timeline and replay inputs before resolution.','warning',true,CURRENT_TIMESTAMP),
  ('alert.delivery',1,'Alert delivery updated','A sanitized alert delivery or escalation changed state.','Review delivery attempts and retry state without exposing route secrets.','warning',true,CURRENT_TIMESTAMP),
  ('audit.verification',1,'Audit verification recorded','The tamper-evident audit chain received a verification verdict.','Investigate immediately if the verdict is broken.','critical',true,CURRENT_TIMESTAMP)
ON CONFLICT DO NOTHING;
