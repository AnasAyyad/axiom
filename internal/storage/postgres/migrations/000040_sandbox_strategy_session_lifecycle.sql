SET TIME ZONE 'UTC';

-- Migration 000029 required started_at to be non-null if and only if a
-- strategy session was currently running. That made the supported
-- running-to-blocked and running-to-stopped transitions impossible without
-- erasing immutable lifecycle history. Replace the three partial checks with
-- one explicit lifecycle contract and retain the original start timestamp.
ALTER TABLE sandbox_strategy_sessions
  DROP CONSTRAINT sandbox_strategy_sessions_check,
  DROP CONSTRAINT sandbox_strategy_sessions_check1,
  DROP CONSTRAINT sandbox_strategy_sessions_check2;

ALTER TABLE sandbox_strategy_sessions
  ADD CONSTRAINT sandbox_strategy_sessions_lifecycle_valid CHECK (
    (state = 'prepared'
      AND started_at IS NULL
      AND stopped_at IS NULL
      AND blocking_reason IS NULL)
    OR
    (state = 'running'
      AND started_at IS NOT NULL
      AND stopped_at IS NULL
      AND blocking_reason IS NULL)
    OR
    (state = 'blocked'
      AND started_at IS NOT NULL
      AND stopped_at IS NULL
      AND blocking_reason IS NOT NULL)
    OR
    (state = 'stopped'
      AND stopped_at IS NOT NULL)
  ),
  ADD CONSTRAINT sandbox_strategy_sessions_lifecycle_chronology_valid CHECK (
    (started_at IS NULL OR started_at >= created_at)
    AND
    (stopped_at IS NULL OR stopped_at >= COALESCE(started_at, created_at))
  );
