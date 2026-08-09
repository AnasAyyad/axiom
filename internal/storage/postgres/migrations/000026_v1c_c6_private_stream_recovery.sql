BEGIN;

-- V1C C6 private-stream recovery

-- A private-stream receive failure is a distinct read-only incident source.
-- The runtime row still contains only a stable kind, sanitized cause code,
-- duration, account/epoch binding, and evidence hash.
ALTER TABLE v1c_engine_runtime_events
  DROP CONSTRAINT v1c_engine_runtime_events_kind_check,
  ADD CONSTRAINT v1c_engine_runtime_events_kind_check CHECK (kind IN (
    'PRIVATE_STREAM','PRIVATE_RECONNECT','UNKNOWN_RECOVERY','RECONCILIATION'
  ));

-- Existing reconciliation recovery evidence remains valid and is explicitly
-- classified. New writes must provide one closed incident source.
ALTER TABLE v1c_c6_recovery_events
  ADD COLUMN incident_source text NOT NULL DEFAULT 'reconciliation'
    CHECK (incident_source IN ('reconciliation','private_stream'));

ALTER TABLE v1c_c6_recovery_events
  ALTER COLUMN incident_source DROP DEFAULT;

-- Recovery terminal reasons are part of the same closed C6 failure vocabulary
-- and must be persistable before the run can be sealed FAILED.
ALTER TABLE v1c_c6_qualification_failures
  DROP CONSTRAINT v1c_c6_qualification_failures_reason_check,
  ADD CONSTRAINT v1c_c6_qualification_failures_reason_check CHECK (reason IN (
    'duplicate_create','lost_fill','double_posted_fill','unresolved_unknown',
    'reconciliation_mismatch','suspense','stale_data','lease_loss',
    'persistence_failure','unsafe_restart','production_target',
    'cap_violation','memory_leak','critical_alert_slo','operator_abort',
    'evidence_failure','recovery_expired','recovery_repeated',
    'recovery_unrecoverable'
  ));

COMMIT;
