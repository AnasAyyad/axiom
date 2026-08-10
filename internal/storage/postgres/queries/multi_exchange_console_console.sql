-- name: InsertMultiExchangeConsoleReplayFaultScheduleState :one
INSERT INTO multi_exchange_console_replay_fault_schedule_states(replay_id, revision, updated_at)
VALUES ($1, 0, $2)
RETURNING *;

-- name: GetMultiExchangeConsoleReplayFaultScheduleState :one
SELECT * FROM multi_exchange_console_replay_fault_schedule_states WHERE replay_id = $1;

-- name: InsertMultiExchangeConsoleReplayFaultSchedule :one
INSERT INTO multi_exchange_console_replay_fault_schedules(
  id, replay_id, command_id, schedule_revision, fault_kind, event_ordinal,
  delay_nanos, repeatable, reason, actor_user_id, session_id, payload_hash,
  simulation_only, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,true,$13)
RETURNING *;

-- name: AdvanceMultiExchangeConsoleReplayFaultScheduleState :one
UPDATE multi_exchange_console_replay_fault_schedule_states
SET revision = revision + 1, updated_at = $2
WHERE replay_id = $1 AND revision = $3
RETURNING *;

-- name: ListMultiExchangeConsoleReplayFaultSchedules :many
SELECT * FROM multi_exchange_console_replay_fault_schedules
WHERE replay_id = $1 ORDER BY schedule_revision;

-- name: InsertMultiExchangeConsoleReportExport :one
INSERT INTO multi_exchange_console_report_exports(
  id, report_id, command_id, format, content_type, content, payload_hash,
  actor_user_id, session_id, simulation_only, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,true,$10)
RETURNING *;
