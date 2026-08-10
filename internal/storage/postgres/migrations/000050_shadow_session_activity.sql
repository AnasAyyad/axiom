SET TIME ZONE 'UTC';

-- Shadow activity is append-only operational evidence. It explains a quiet
-- session without mutating immutable decisions or pretending that a missing
-- signal is an error. Each observation carries the complete required input
-- health set for semantic sessions.
CREATE TABLE shadow_session_activity_observations (
  session_id text NOT NULL REFERENCES shadow_sessions(id),
  revision bigint NOT NULL CHECK (revision > 0),
  activity_state text NOT NULL CHECK (activity_state IN (
    'preparing','warming_up','waiting','evaluating','running','paused','blocked','stopped'
  )),
  reason_code text NOT NULL CHECK (reason_code ~ '^[a-z0-9_]{1,96}$'),
  summary text NOT NULL CHECK (length(summary) BETWEEN 1 AND 500),
  next_evaluation_at timestamptz,
  trigger_condition text NOT NULL CHECK (length(trigger_condition) BETWEEN 1 AND 500),
  observed_at timestamptz NOT NULL,
  PRIMARY KEY (session_id, revision)
);

CREATE TABLE shadow_session_input_health_observations (
  session_id text NOT NULL,
  activity_revision bigint NOT NULL,
  exchange_id text NOT NULL REFERENCES exchanges(id),
  instrument_id text NOT NULL REFERENCES instruments(id),
  state text NOT NULL CHECK (state IN (
    'CONNECTING','SYNCING','HEALTHY','STALE','PAUSED','DISCONNECTED','UNAVAILABLE'
  )),
  reason text NOT NULL CHECK (length(reason) BETWEEN 1 AND 500),
  fresh boolean NOT NULL,
  CHECK (fresh = (state = 'HEALTHY')),
  book_version bigint NOT NULL CHECK (book_version >= 0),
  age_nanoseconds bigint NOT NULL CHECK (age_nanoseconds >= 0),
  observed_at timestamptz NOT NULL,
  PRIMARY KEY (session_id, activity_revision, exchange_id, instrument_id),
  FOREIGN KEY (session_id, activity_revision)
    REFERENCES shadow_session_activity_observations(session_id, revision)
);

CREATE FUNCTION enforce_shadow_session_input_health_reference() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE
  scoped boolean;
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM public.exchanges exchange
    WHERE exchange.id = NEW.exchange_id AND exchange.environment = 'production_public'
  ) OR NOT EXISTS (
    SELECT 1 FROM public.instrument_metadata_versions metadata
    WHERE metadata.exchange_id = NEW.exchange_id AND metadata.instrument_id = NEW.instrument_id
  ) THEN
    RAISE EXCEPTION 'shadow_input_health_reference_invalid';
  END IF;

  SELECT session.market_scope_required INTO scoped
    FROM public.shadow_sessions session WHERE session.id = NEW.session_id;
  IF scoped AND NOT EXISTS (
    SELECT 1 FROM public.shadow_session_market_scopes scope
    WHERE scope.session_id = NEW.session_id
      AND scope.exchange_id = NEW.exchange_id
      AND scope.instrument_id = NEW.instrument_id
  ) THEN
    RAISE EXCEPTION 'shadow_input_health_outside_market_scope';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER shadow_session_input_health_reference
  BEFORE INSERT OR UPDATE OF session_id, exchange_id, instrument_id
  ON shadow_session_input_health_observations
  FOR EACH ROW EXECUTE FUNCTION enforce_shadow_session_input_health_reference();

CREATE FUNCTION enforce_shadow_session_activity_input_set() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE
  required boolean;
  expected_count integer;
  actual_count integer;
BEGIN
  SELECT session.market_scope_required INTO required
    FROM public.shadow_sessions session WHERE session.id = NEW.session_id;
  SELECT count(*) INTO actual_count
    FROM public.shadow_session_input_health_observations input
    WHERE input.session_id = NEW.session_id AND input.activity_revision = NEW.revision;

  IF required THEN
    SELECT count(*) INTO expected_count
      FROM public.shadow_session_market_scopes scope WHERE scope.session_id = NEW.session_id;
    IF expected_count = 0 OR actual_count <> expected_count THEN
      RAISE EXCEPTION 'shadow_activity_input_health_incomplete';
    END IF;
  ELSIF actual_count = 0 THEN
    RAISE EXCEPTION 'shadow_activity_input_health_missing';
  END IF;
  RETURN NEW;
END;
$$;

CREATE CONSTRAINT TRIGGER shadow_session_activity_input_set_guard
  AFTER INSERT ON shadow_session_activity_observations
  DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION enforce_shadow_session_activity_input_set();

CREATE TRIGGER shadow_session_activity_observations_immutable
  BEFORE UPDATE OR DELETE ON shadow_session_activity_observations
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();

CREATE TRIGGER shadow_session_input_health_observations_immutable
  BEFORE UPDATE OR DELETE ON shadow_session_input_health_observations
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();

CREATE INDEX shadow_session_activity_latest_idx
  ON shadow_session_activity_observations(session_id, revision DESC);
