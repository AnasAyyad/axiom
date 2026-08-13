-- V1D D5: current storage pressure, immutable observations, and fail-closed bootstrap.

CREATE TABLE v1d_storage_pressure_state (
  scope_id text PRIMARY KEY CHECK (scope_id='market-data'),
  level text NOT NULL CHECK (level IN ('NORMAL','HIGH','CRITICAL')),
  available_bytes bigint NOT NULL CHECK (available_bytes>=0),
  total_bytes bigint NOT NULL CHECK (total_bytes>0 AND available_bytes<=total_bytes),
  high_free_bytes bigint NOT NULL CHECK (high_free_bytes>=10737418240),
  critical_free_bytes bigint NOT NULL CHECK (
    critical_free_bytes>=1073741824 AND critical_free_bytes<high_free_bytes
  ),
  revision bigint NOT NULL CHECK (revision>0),
  observed_at timestamptz NOT NULL,
  source_instance text NOT NULL CHECK (length(source_instance) BETWEEN 1 AND 128)
);

CREATE TABLE v1d_storage_pressure_events (
  id text PRIMARY KEY,
  scope_id text NOT NULL CHECK (scope_id='market-data'),
  prior_level text CHECK (prior_level IN ('NORMAL','HIGH','CRITICAL')),
  level text NOT NULL CHECK (level IN ('NORMAL','HIGH','CRITICAL')),
  available_bytes bigint NOT NULL CHECK (available_bytes>=0),
  total_bytes bigint NOT NULL CHECK (total_bytes>0 AND available_bytes<=total_bytes),
  high_free_bytes bigint NOT NULL CHECK (high_free_bytes>=10737418240),
  critical_free_bytes bigint NOT NULL CHECK (
    critical_free_bytes>=1073741824 AND critical_free_bytes<high_free_bytes
  ),
  revision bigint NOT NULL CHECK (revision>0),
  observed_at timestamptz NOT NULL,
  source_instance text NOT NULL CHECK (length(source_instance) BETWEEN 1 AND 128),
  evidence_hash sha256_hex NOT NULL,
  UNIQUE(scope_id,revision)
);
CREATE TRIGGER v1d_storage_pressure_event_immutable
  BEFORE UPDATE OR DELETE ON v1d_storage_pressure_events
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();

-- A new installation is intentionally critical until the recorder publishes
-- a measured filesystem observation. D5 formal preflight also rejects this source.
INSERT INTO v1d_storage_pressure_state(
  scope_id,level,available_bytes,total_bytes,high_free_bytes,
  critical_free_bytes,revision,observed_at,source_instance
) VALUES(
  'market-data','CRITICAL',0,1,10737418240,5368709120,1,
  CURRENT_TIMESTAMP,'migration-bootstrap'
);
INSERT INTO v1d_storage_pressure_events(
  id,scope_id,prior_level,level,available_bytes,total_bytes,high_free_bytes,
  critical_free_bytes,revision,observed_at,source_instance,evidence_hash
) SELECT
  'storage-pressure-bootstrap','market-data',NULL,'CRITICAL',0,1,
  10737418240,5368709120,1,observed_at,'migration-bootstrap',
  encode(sha256(convert_to(
    'market-data'||chr(31)||'CRITICAL'||chr(31)||'0'||chr(31)||'1'||chr(31)||'1',
    'UTF8')),'hex')
FROM v1d_storage_pressure_state WHERE scope_id='market-data';

INSERT INTO v1d_reason_catalogue(
  code,version,summary,explanation,suggested_action,severity,active,recorded_at
) VALUES
  ('storage.pressure.high',1,'Storage headroom is low','The market-data filesystem reached the high pressure watermark. New lab, report, and export jobs are blocked.','Free verified unheld data or add independently provisioned capacity before starting heavy work.','warning',true,CURRENT_TIMESTAMP),
  ('storage.pressure.critical',1,'Storage headroom is critical','The market-data filesystem reached the critical watermark. Recording and new shadow decisions are paused while journal and audit writes remain available.','Preserve evidence, add capacity, verify recorder finalization, then wait for a fresh normal observation.','critical',true,CURRENT_TIMESTAMP),
  ('storage.pressure.normal',1,'Storage headroom recovered','A fresh filesystem observation is above the high watermark.','Review the pressure timeline before resuming paused activity.','info',true,CURRENT_TIMESTAMP)
ON CONFLICT DO NOTHING;
