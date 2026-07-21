SET TIME ZONE 'UTC';

INSERT INTO exchanges (id, name, environment)
VALUES ('bybit', 'Bybit', 'production_public')
ON CONFLICT (id) DO NOTHING;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM exchanges
    WHERE id = 'bybit' AND name = 'Bybit' AND environment = 'production_public'
  ) THEN
    RAISE EXCEPTION 'bybit_public_reference_conflict';
  END IF;
END;
$$;

CREATE TABLE public_clock_samples (
  id text PRIMARY KEY,
  exchange_id text NOT NULL REFERENCES exchanges(id),
  instrument_id text NOT NULL REFERENCES instruments(id),
  recorder_session text NOT NULL,
  connection_id text NOT NULL,
  connection_generation bigint NOT NULL CHECK (connection_generation > 0),
  observed_at timestamptz NOT NULL,
  offset_nanoseconds bigint NOT NULL,
  uncertainty_nanoseconds bigint NOT NULL CHECK (uncertainty_nanoseconds >= 0),
  eligible boolean NOT NULL,
  raw_payload_hash sha256_hex NOT NULL,
  UNIQUE (recorder_session, connection_generation, observed_at)
);

CREATE TABLE public_connection_events (
  id text PRIMARY KEY,
  exchange_id text NOT NULL REFERENCES exchanges(id),
  instrument_id text NOT NULL REFERENCES instruments(id),
  recorder_session text NOT NULL,
  connection_id text NOT NULL,
  connection_generation bigint NOT NULL CHECK (connection_generation > 0),
  state text NOT NULL CHECK (state IN ('CONNECTING','SYNCING','SUBSCRIBED','HEALTHY','PAUSED','DISCONNECTED')),
  reason text NOT NULL,
  observed_at timestamptz NOT NULL,
  ingest_ordinal bigint NOT NULL CHECK (ingest_ordinal > 0),
  UNIQUE (recorder_session, ingest_ordinal)
);

CREATE TRIGGER public_clock_samples_immutable BEFORE UPDATE OR DELETE ON public_clock_samples
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER public_connection_events_immutable BEFORE UPDATE OR DELETE ON public_connection_events
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
