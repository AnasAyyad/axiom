SET TIME ZONE 'UTC';

CREATE TABLE b8_replay_fault_schedule_states (
  replay_id text PRIMARY KEY REFERENCES jobs(id),
  revision bigint NOT NULL CHECK (revision >= 0),
  updated_at timestamptz NOT NULL
);

CREATE TABLE b8_replay_fault_schedules (
  id text PRIMARY KEY CHECK (id ~ '^b8-fault-[0-9a-f]{24}$'),
  replay_id text NOT NULL REFERENCES b8_replay_fault_schedule_states(replay_id),
  command_id text NOT NULL UNIQUE REFERENCES command_requests(id),
  schedule_revision bigint NOT NULL CHECK (schedule_revision > 0),
  fault_kind text NOT NULL CHECK (fault_kind IN (
    'disconnect','sequence_gap','latency','rejection','partial_fill',
    'cancel_fill_race','unknown_state','storage_failure','restart_at_event'
  )),
  event_ordinal bigint NOT NULL CHECK (event_ordinal > 0),
  delay_nanos bigint NOT NULL CHECK (delay_nanos >= 0),
  repeatable boolean NOT NULL,
  reason text NOT NULL CHECK (length(reason) BETWEEN 8 AND 500),
  actor_user_id text NOT NULL REFERENCES users(id),
  session_id text NOT NULL REFERENCES sessions(id),
  payload_hash sha256_hex NOT NULL,
  simulation_only boolean NOT NULL CHECK (simulation_only),
  created_at timestamptz NOT NULL,
  UNIQUE (replay_id, schedule_revision),
  UNIQUE (replay_id, event_ordinal),
  CHECK (
    (fault_kind = 'latency' AND delay_nanos > 0) OR
    (fault_kind <> 'latency' AND delay_nanos = 0)
  )
);

CREATE TABLE b8_report_exports (
  id text PRIMARY KEY CHECK (id ~ '^b8-export-[0-9a-f]{24}$'),
  report_id text NOT NULL REFERENCES b7_champion_challenger_reports(id),
  command_id text NOT NULL UNIQUE REFERENCES command_requests(id),
  format text NOT NULL CHECK (format IN ('json','csv')),
  content_type text NOT NULL CHECK (content_type IN ('application/json','text/csv')),
  content text NOT NULL CHECK (octet_length(content) BETWEEN 1 AND 1048576),
  payload_hash sha256_hex NOT NULL,
  actor_user_id text NOT NULL REFERENCES users(id),
  session_id text NOT NULL REFERENCES sessions(id),
  simulation_only boolean NOT NULL CHECK (simulation_only),
  created_at timestamptz NOT NULL
);

CREATE FUNCTION protect_b8_fault_schedule_state() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' OR NEW.replay_id <> OLD.replay_id OR
     NEW.revision <> OLD.revision + 1 OR NEW.updated_at < OLD.updated_at THEN
    RAISE EXCEPTION 'b8_fault_schedule_state_invalid';
  END IF;
  RETURN NEW;
END;
$$;

CREATE FUNCTION enforce_b8_fault_schedule_reference() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  replay_kind text;
  replay_state text;
  replay_revision bigint;
  command_kind text;
  command_actor text;
  command_session text;
  command_hash sha256_hex;
BEGIN
  SELECT job_type, state INTO replay_kind, replay_state
  FROM jobs WHERE id = NEW.replay_id;
  SELECT request.command_kind, request.actor_user_id, request.session_id,
    request.payload_hash
    INTO command_kind, command_actor, command_session, command_hash
  FROM command_requests request WHERE request.id = NEW.command_id;
  SELECT revision INTO replay_revision
  FROM b8_replay_fault_schedule_states WHERE replay_id = NEW.replay_id;
  IF replay_kind IS DISTINCT FROM 'replay' OR replay_state IS DISTINCT FROM 'QUEUED' OR
     command_kind IS DISTINCT FROM 'replay.fault.schedule' OR
     command_actor IS DISTINCT FROM NEW.actor_user_id OR
     command_session IS DISTINCT FROM NEW.session_id OR
     command_hash IS DISTINCT FROM NEW.payload_hash OR
     replay_revision IS DISTINCT FROM NEW.schedule_revision - 1 THEN
    RAISE EXCEPTION 'b8_fault_schedule_reference_invalid';
  END IF;
  RETURN NEW;
END;
$$;

CREATE FUNCTION enforce_b8_report_export_reference() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  command_kind text;
  command_actor text;
  command_session text;
  command_hash sha256_hex;
BEGIN
  SELECT request.command_kind, request.actor_user_id, request.session_id,
    request.payload_hash
    INTO command_kind, command_actor, command_session, command_hash
  FROM command_requests request WHERE request.id = NEW.command_id;
  IF command_kind IS DISTINCT FROM 'report.export' OR
     command_actor IS DISTINCT FROM NEW.actor_user_id OR
     command_session IS DISTINCT FROM NEW.session_id OR
     command_hash IS DISTINCT FROM NEW.payload_hash THEN
    RAISE EXCEPTION 'b8_report_export_reference_invalid';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER b8_fault_schedule_states_protected
  BEFORE UPDATE OR DELETE ON b8_replay_fault_schedule_states
  FOR EACH ROW EXECUTE FUNCTION protect_b8_fault_schedule_state();
CREATE TRIGGER b8_fault_schedules_reference_guard
  BEFORE INSERT ON b8_replay_fault_schedules
  FOR EACH ROW EXECUTE FUNCTION enforce_b8_fault_schedule_reference();
CREATE TRIGGER b8_fault_schedules_immutable
  BEFORE UPDATE OR DELETE ON b8_replay_fault_schedules
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER b8_report_exports_reference_guard
  BEFORE INSERT ON b8_report_exports
  FOR EACH ROW EXECUTE FUNCTION enforce_b8_report_export_reference();
CREATE TRIGGER b8_report_exports_immutable
  BEFORE UPDATE OR DELETE ON b8_report_exports
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();

CREATE INDEX b8_fault_schedules_replay_idx
  ON b8_replay_fault_schedules(replay_id, schedule_revision);
CREATE INDEX b8_report_exports_report_idx
  ON b8_report_exports(report_id, created_at, id);
CREATE INDEX triangular_candidates_console_idx
  ON triangular_candidates(recorded_at DESC, decision_id DESC);
CREATE INDEX cross_exchange_candidates_console_idx
  ON cross_exchange_candidates(recorded_at DESC, decision_id DESC);
CREATE INDEX rebalancing_recommendations_console_idx
  ON rebalancing_recommendations(recorded_at DESC, id DESC);
CREATE INDEX b7_champion_challenger_console_idx
  ON b7_champion_challenger_reports(created_at DESC, id DESC);
