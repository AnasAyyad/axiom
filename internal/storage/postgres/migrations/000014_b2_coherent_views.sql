SET TIME ZONE 'UTC';

CREATE TABLE cross_market_view_headers (
  id sha256_hex PRIMARY KEY,
  version_vector_hash sha256_hex NOT NULL UNIQUE,
  policy_version text NOT NULL CHECK (policy_version = 'axiom.coherent-view-policy.v1'),
  maximum_book_age_nanos bigint NOT NULL CHECK (maximum_book_age_nanos = 250000000),
  maximum_inter_book_skew_nanos bigint NOT NULL CHECK (maximum_inter_book_skew_nanos = 250000000),
  maximum_clock_uncertainty_nanos bigint NOT NULL CHECK (maximum_clock_uncertainty_nanos = 100000000),
  trigger_monotonic_nanos bigint NOT NULL CHECK (trigger_monotonic_nanos > 0),
  trigger_ingest_ordinal bigint NOT NULL CHECK (trigger_ingest_ordinal > 0),
  trigger_utc timestamptz NOT NULL,
  trigger_utc_unix_nanos bigint NOT NULL,
  member_count integer NOT NULL CHECK (member_count >= 2),
  created_at timestamptz NOT NULL,
  CHECK (id = version_vector_hash)
);

CREATE TABLE cross_market_view_members (
  cross_market_view_id sha256_hex NOT NULL REFERENCES cross_market_view_headers(id),
  member_ordinal integer NOT NULL CHECK (member_ordinal >= 0),
  exchange_id text NOT NULL REFERENCES exchanges(id),
  instrument_id text NOT NULL REFERENCES instruments(id),
  book_version bigint NOT NULL CHECK (book_version > 0),
  connection_generation bigint NOT NULL CHECK (connection_generation > 0),
  receive_monotonic_nanos bigint NOT NULL CHECK (receive_monotonic_nanos > 0),
  receive_utc timestamptz NOT NULL,
  receive_utc_unix_nanos bigint NOT NULL,
  ingest_ordinal bigint NOT NULL CHECK (ingest_ordinal > 0),
  clock_offset_nanos bigint NOT NULL,
  clock_uncertainty_nanos bigint NOT NULL CHECK (clock_uncertainty_nanos >= 0),
  clock_interval_start timestamptz NOT NULL,
  clock_interval_end timestamptz NOT NULL,
  state_hash sha256_hex NOT NULL,
  collector_instance text NOT NULL CHECK (collector_instance ~ '^[A-Za-z0-9_.:-]{1,128}$'),
  collector_region text NOT NULL CHECK (collector_region ~ '^[A-Za-z0-9_.:-]{1,128}$'),
  PRIMARY KEY (cross_market_view_id, member_ordinal),
  UNIQUE (cross_market_view_id, exchange_id, instrument_id),
  CHECK (clock_interval_end >= clock_interval_start)
);

CREATE FUNCTION enforce_cross_market_view_complete() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  target_id text;
  header cross_market_view_headers%ROWTYPE;
  actual_members integer;
  first_ordinal integer;
  last_ordinal integer;
  exchange_count integer;
  earliest_receive bigint;
  latest_receive bigint;
  latest_interval_start bigint;
  earliest_interval_end bigint;
BEGIN
  IF TG_TABLE_NAME = 'cross_market_view_headers' THEN
    target_id := NEW.id;
  ELSE
    target_id := NEW.cross_market_view_id;
  END IF;
  SELECT * INTO header FROM cross_market_view_headers WHERE id = target_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'cross_market_view_header_missing';
  END IF;
  SELECT count(*), min(member_ordinal), max(member_ordinal), count(DISTINCT exchange_id),
         min(receive_monotonic_nanos), max(receive_monotonic_nanos),
         max(receive_utc_unix_nanos + clock_offset_nanos - clock_uncertainty_nanos),
         min(receive_utc_unix_nanos + clock_offset_nanos + clock_uncertainty_nanos)
    INTO actual_members, first_ordinal, last_ordinal, exchange_count,
         earliest_receive, latest_receive, latest_interval_start, earliest_interval_end
    FROM cross_market_view_members WHERE cross_market_view_id = target_id;
  IF actual_members <> header.member_count OR first_ordinal <> 0 OR
     last_ordinal <> header.member_count - 1 OR exchange_count < 2 THEN
    RAISE EXCEPTION 'cross_market_view_incomplete';
  END IF;
  IF latest_receive > header.trigger_monotonic_nanos OR
     header.trigger_monotonic_nanos - earliest_receive > header.maximum_book_age_nanos OR
     latest_receive - earliest_receive > header.maximum_inter_book_skew_nanos OR
     latest_interval_start > earliest_interval_end OR EXISTS (
       SELECT 1 FROM cross_market_view_members
       WHERE cross_market_view_id = target_id AND (
         clock_uncertainty_nanos > header.maximum_clock_uncertainty_nanos OR
         ingest_ordinal > header.trigger_ingest_ordinal
       )
     ) THEN
    RAISE EXCEPTION 'cross_market_view_ineligible';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER cross_market_view_headers_immutable
  BEFORE UPDATE OR DELETE ON cross_market_view_headers
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER cross_market_view_members_immutable
  BEFORE UPDATE OR DELETE ON cross_market_view_members
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE CONSTRAINT TRIGGER cross_market_view_headers_complete
  AFTER INSERT ON cross_market_view_headers DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION enforce_cross_market_view_complete();
CREATE CONSTRAINT TRIGGER cross_market_view_members_complete
  AFTER INSERT ON cross_market_view_members DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION enforce_cross_market_view_complete();

ALTER TABLE decisions
  ADD COLUMN decision_market_scope text NOT NULL DEFAULT 'single_market'
    CHECK (decision_market_scope IN ('single_market','cross_market')),
  ADD COLUMN cross_market_view_id sha256_hex REFERENCES cross_market_view_headers(id),
  ADD CONSTRAINT decisions_cross_market_view_required CHECK (
    (decision_market_scope = 'single_market' AND cross_market_view_id IS NULL) OR
    (decision_market_scope = 'cross_market' AND cross_market_view_id IS NOT NULL)
  );

ALTER TABLE dataset_manifests
  ADD COLUMN manifest_schema_version text,
  ADD COLUMN quality_tier text CHECK (quality_tier IS NULL OR quality_tier = 'A');

CREATE TABLE dataset_exchange_coverage (
  dataset_id text NOT NULL REFERENCES dataset_manifests(id),
  exchange_id text NOT NULL REFERENCES exchanges(id),
  collector_instance text NOT NULL CHECK (collector_instance ~ '^[A-Za-z0-9_.:-]{1,128}$'),
  collector_region text NOT NULL CHECK (collector_region ~ '^[A-Za-z0-9_.:-]{1,128}$'),
  coverage_start timestamptz NOT NULL,
  coverage_end timestamptz NOT NULL,
  first_ordinal bigint NOT NULL CHECK (first_ordinal > 0),
  last_ordinal bigint NOT NULL CHECK (last_ordinal >= first_ordinal),
  generation_history jsonb NOT NULL CHECK (jsonb_typeof(generation_history) = 'array'),
  schema_versions text[] NOT NULL CHECK (cardinality(schema_versions) > 0),
  parser_versions text[] NOT NULL CHECK (cardinality(parser_versions) > 0),
  normalization_versions text[] NOT NULL CHECK (cardinality(normalization_versions) > 0),
  compatibility_requirements jsonb NOT NULL CHECK (jsonb_typeof(compatibility_requirements) = 'object'),
  raw_record_count bigint NOT NULL CHECK (raw_record_count > 0),
  canonical_record_count bigint NOT NULL CHECK (canonical_record_count > 0),
  raw_canonical_linkage_complete boolean NOT NULL,
  hidden_gap_count bigint NOT NULL CHECK (hidden_gap_count >= 0),
  complete boolean NOT NULL,
  PRIMARY KEY (dataset_id, exchange_id),
  CHECK (coverage_end >= coverage_start),
  CHECK (raw_record_count = canonical_record_count OR NOT raw_canonical_linkage_complete)
);

CREATE TABLE dataset_tier_a_members (
  dataset_id text NOT NULL REFERENCES dataset_manifests(id),
  member_ordinal integer NOT NULL CHECK (member_ordinal >= 0),
  exchange_id text NOT NULL REFERENCES exchanges(id),
  member_dataset_id text NOT NULL REFERENCES dataset_manifests(id),
  member_manifest_hash sha256_hex NOT NULL,
  member_revision bigint NOT NULL CHECK (member_revision > 0),
  replay_hash sha256_hex NOT NULL,
  record_count bigint NOT NULL CHECK (record_count > 0),
  PRIMARY KEY (dataset_id, member_ordinal),
  UNIQUE (dataset_id, exchange_id),
  UNIQUE (dataset_id, member_dataset_id),
  CHECK (dataset_id <> member_dataset_id)
);

CREATE TRIGGER dataset_exchange_coverage_immutable
  BEFORE UPDATE OR DELETE ON dataset_exchange_coverage
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER dataset_tier_a_members_immutable
  BEFORE UPDATE OR DELETE ON dataset_tier_a_members
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();

CREATE FUNCTION require_building_dataset_evidence() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  manifest_state text;
BEGIN
  SELECT state INTO manifest_state FROM dataset_manifests WHERE id = NEW.dataset_id;
  IF manifest_state IS DISTINCT FROM 'building' THEN
    RAISE EXCEPTION 'dataset_evidence_immutable';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER dataset_segments_insert_while_building
  BEFORE INSERT ON dataset_segments
  FOR EACH ROW EXECUTE FUNCTION require_building_dataset_evidence();
CREATE TRIGGER dataset_gaps_insert_while_building
  BEFORE INSERT ON dataset_gaps
  FOR EACH ROW EXECUTE FUNCTION require_building_dataset_evidence();
CREATE TRIGGER dataset_exchange_coverage_insert_while_building
  BEFORE INSERT ON dataset_exchange_coverage
  FOR EACH ROW EXECUTE FUNCTION require_building_dataset_evidence();
CREATE TRIGGER dataset_tier_a_members_insert_while_building
  BEFORE INSERT ON dataset_tier_a_members
  FOR EACH ROW EXECUTE FUNCTION require_building_dataset_evidence();

CREATE OR REPLACE FUNCTION protect_dataset_manifest() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.state <> 'building' OR
       (NEW.quality_tier = 'A' AND NEW.manifest_schema_version IS NULL) THEN
      RAISE EXCEPTION 'invalid_dataset_manifest_initial_state';
    END IF;
    RETURN NEW;
  END IF;
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'immutable_dataset_manifest';
  END IF;
  IF (to_jsonb(NEW) - 'state') IS DISTINCT FROM (to_jsonb(OLD) - 'state') THEN
    RAISE EXCEPTION 'immutable_dataset_manifest';
  END IF;
  IF NEW.state <> OLD.state AND NOT (
    (OLD.state = 'building' AND NEW.state IN ('ready','rejected','deleted')) OR
    (OLD.state = 'ready' AND NEW.state IN ('qualified','rejected','deleted')) OR
    (OLD.state = 'qualified' AND NEW.state = 'deleted') OR
    (OLD.state = 'rejected' AND NEW.state = 'deleted')
  ) THEN
    RAISE EXCEPTION 'invalid_dataset_manifest_transition';
  END IF;
  RETURN NEW;
END;
$$;

CREATE FUNCTION enforce_tier_a_dataset_manifest() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  coverage_count integer;
  member_count integer;
BEGIN
  IF NEW.quality_tier = 'A' AND NEW.state IN ('ready','qualified') THEN
    SELECT count(*) INTO coverage_count FROM dataset_exchange_coverage
      WHERE dataset_id = NEW.id;
    SELECT count(*) INTO member_count FROM dataset_tier_a_members
      WHERE dataset_id = NEW.id;
    IF NEW.manifest_schema_version <> 'axiom.multi-exchange-dataset.v1' OR coverage_count < 2 OR
       member_count <> coverage_count OR
       NOT EXISTS (SELECT 1 FROM dataset_segments WHERE dataset_id = NEW.id) OR
       EXISTS (SELECT 1 FROM dataset_gaps WHERE dataset_id = NEW.id) THEN
      RAISE EXCEPTION 'tier_a_dataset_incomplete';
    END IF;
    IF EXISTS (
      SELECT 1 FROM dataset_exchange_coverage coverage
      WHERE coverage.dataset_id = NEW.id AND (
        NOT coverage.complete OR NOT coverage.raw_canonical_linkage_complete OR coverage.hidden_gap_count <> 0 OR
        coverage.raw_record_count <> coverage.canonical_record_count OR coverage.generation_history = '[]'::jsonb OR
        NOT EXISTS (
          SELECT 1 FROM dataset_segments membership
          JOIN market_data_segments segment ON segment.id = membership.segment_id
          WHERE membership.dataset_id = NEW.id AND segment.exchange_id = coverage.exchange_id
        )
      )
    ) THEN
      RAISE EXCEPTION 'tier_a_dataset_incomplete';
    END IF;
    IF EXISTS (
      SELECT 1 FROM dataset_segments parent_membership
      JOIN market_data_segments parent_segment ON parent_segment.id = parent_membership.segment_id
      WHERE parent_membership.dataset_id = NEW.id AND NOT EXISTS (
        SELECT 1 FROM dataset_tier_a_members member
        JOIN dataset_segments child_membership ON child_membership.dataset_id = member.member_dataset_id
        WHERE member.dataset_id = NEW.id AND member.exchange_id = parent_segment.exchange_id AND
              child_membership.segment_id = parent_membership.segment_id
      )
    ) THEN
      RAISE EXCEPTION 'tier_a_dataset_incomplete';
    END IF;
    IF EXISTS (
      SELECT 1 FROM dataset_tier_a_members member
      WHERE member.dataset_id = NEW.id AND (
        NOT EXISTS (
          SELECT 1 FROM dataset_manifests child
          WHERE child.id = member.member_dataset_id AND child.dataset_hash = member.member_manifest_hash AND
                child.state IN ('ready','qualified')
        ) OR NOT EXISTS (
          SELECT 1 FROM dataset_exchange_coverage coverage
          WHERE coverage.dataset_id = NEW.id AND coverage.exchange_id = member.exchange_id AND
                coverage.raw_record_count = member.record_count AND
                coverage.canonical_record_count = member.record_count
        ) OR NOT EXISTS (
          SELECT 1 FROM dataset_segments child_segment
          WHERE child_segment.dataset_id = member.member_dataset_id
        ) OR EXISTS (
          SELECT 1 FROM dataset_segments child_segment
          WHERE child_segment.dataset_id = member.member_dataset_id AND NOT EXISTS (
            SELECT 1 FROM dataset_segments parent_segment
            WHERE parent_segment.dataset_id = NEW.id AND parent_segment.segment_id = child_segment.segment_id
          )
        )
      )
    ) THEN
      RAISE EXCEPTION 'tier_a_dataset_incomplete';
    END IF;
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER dataset_manifests_tier_a_complete
  BEFORE INSERT OR UPDATE ON dataset_manifests
  FOR EACH ROW EXECUTE FUNCTION enforce_tier_a_dataset_manifest();
