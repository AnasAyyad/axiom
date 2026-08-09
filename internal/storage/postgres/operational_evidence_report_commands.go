package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/authentication"
	"axiom/internal/reporting"

	"github.com/jackc/pgx/v5"
)

type operationalEvidenceReportProvenance struct {
	mode, confidence, valuation, maturity, sourceIdentity string
	sourceRevision                                        int64
	models                                                map[string]string
}

func queueOperationalEvidenceReport(
	ctx context.Context, tx pgx.Tx, principal authentication.Principal,
	command console.OwnerControlCommand, commandID string, now time.Time,
	scheduleID *string, scheduledFor *time.Time,
) (map[string]any, error) {
	reportType, ok := command.Payload["report_type"].(string)
	if !ok || !operationalEvidenceReportType(reportType) {
		return nil, console.ErrInvalidRequest
	}
	if err := operationalEvidenceReportQuota(ctx, tx, principal.UserID); err != nil {
		return nil, err
	}
	provenance, err := operationalEvidenceReportSnapshot(ctx, tx, reportType)
	if err != nil {
		return nil, err
	}
	jobID, _ := ownerConsoleIdentifier("report-job")
	payload, hash, err := ownerConsoleCommandPayload(command.Payload)
	if err != nil {
		return nil, err
	}
	jobKey := ownerConsoleDedupe(principal.UserID, command.IdempotencyKey)
	if _, err = tx.Exec(ctx, `INSERT INTO jobs(
  id,job_type,idempotency_key,state,payload_hash,created_at,updated_at,
  owner_user_id,request_payload,max_attempts
) VALUES ($1,$2,$3,'QUEUED',$4,$5,$5,$6,$7,3)`, jobID,
		"report:"+reportType, jobKey, hash, now, principal.UserID, string(payload)); err != nil {
		return nil, ownerConsoleConstraintError(err)
	}
	models, _ := json.Marshal(provenance.models)
	if _, err = tx.Exec(ctx, `INSERT INTO owner_console_reports(
  id,job_id,schedule_id,scheduled_for,report_type,state,mode,confidence_tier,
  valuation_basis,model_provenance,maturity,source_identity,source_revision,
  created_at,updated_at,revision
) VALUES($1,$2,$3,$4,$5,'QUEUED',$6,$7,$8,$9,$10,$11,$12,$13,$13,1)`,
		command.TargetID, jobID, scheduleID, scheduledFor, reportType, provenance.mode,
		provenance.confidence, provenance.valuation, models, provenance.maturity,
		provenance.sourceIdentity, provenance.sourceRevision, now); err != nil {
		return nil, ownerConsoleConstraintError(err)
	}
	return map[string]any{"report_id": command.TargetID, "job_id": jobID,
		"command_id": commandID, "state": "QUEUED"}, nil
}

func operationalEvidenceReportQuota(ctx context.Context, tx pgx.Tx, userID string) error {
	var ownerQueued, globalQueued int
	if err := tx.QueryRow(ctx, `SELECT
count(*) FILTER (WHERE owner_user_id=$1 AND state='QUEUED')::integer,
count(*) FILTER (WHERE state='QUEUED')::integer FROM jobs`, userID).Scan(
		&ownerQueued, &globalQueued); err != nil {
		return err
	}
	storageReady, err := operationalReadinessHeavyWorkAllowed(ctx, tx)
	if err != nil {
		return err
	}
	if ownerQueued >= 4 || globalQueued >= 32 || !storageReady {
		return console.ErrQuota
	}
	return nil
}

func operationalEvidenceReportSnapshot(ctx context.Context, tx pgx.Tx, reportType string) (operationalEvidenceReportProvenance, error) {
	var revision int64
	if err := tx.QueryRow(ctx, `SELECT coalesce(max(activity_revision),0)
FROM owner_console_activity_projection`).Scan(&revision); err != nil {
		return operationalEvidenceReportProvenance{}, err
	}
	mode, confidence, valuation, maturity, models := operationalEvidenceReportMetadata(reportType)
	identity := ownerConsoleHash([]byte(fmt.Sprintf("%s\x00%d\x00%s\x00%s", reportType, revision, mode, maturity)))
	return operationalEvidenceReportProvenance{mode: mode, confidence: confidence, valuation: valuation,
		maturity: maturity, sourceIdentity: identity, sourceRevision: revision, models: models}, nil
}

func operationalEvidenceReportMetadata(reportType string) (string, string, string, string, map[string]string) {
	models := map[string]string{"report_schema": "axiom.report.owner_console.operational_evidence", "aggregation": "deterministic-snapshot-v1"}
	switch reportType {
	case "strategy_results", "lab_runs":
		models["research_models"] = "versioned-run-manifest"
		return "mixed", "source-declared", "run-manifest valuation basis", "research", models
	case "portfolios", "inventory_pnl":
		models["accounting"] = "immutable-ledger-v1"
		return "mixed", "operational", "ledger-posted quote-currency book value", "operational", models
	case "sandbox_qualifications":
		models["qualification"] = "authenticated-verdict-v1"
		return "mixed", "qualification-declared", "not applicable", "qualification", models
	case "platform_readiness":
		models["readiness"] = "cumulative-gates-v1"
		return "operational", "evidence-gated", "not applicable", "readiness", models
	default:
		return "operational", "operational", "not applicable", "operational", models
	}
}

func operationalEvidenceReportType(value string) bool {
	switch value {
	case "strategy_results", "decisions_orders", "portfolios", "inventory_pnl", "risk",
		"exchange_data_health", "lab_runs", "sandbox_qualifications", "platform_readiness":
		return true
	default:
		return false
	}
}

func applyOperationalEvidenceReportSchedule(
	ctx context.Context, tx pgx.Tx, principal authentication.Principal,
	command console.OwnerControlCommand, now time.Time,
) (map[string]any, error) {
	if command.Action == "create" {
		return createOperationalEvidenceReportSchedule(ctx, tx, principal, command, now)
	}
	if command.Action == "transition" {
		return transitionOperationalEvidenceReportSchedule(ctx, tx, principal, command, now)
	}
	return nil, console.ErrInvalidRequest
}

func createOperationalEvidenceReportSchedule(
	ctx context.Context, tx pgx.Tx, principal authentication.Principal,
	command console.OwnerControlCommand, now time.Time,
) (map[string]any, error) {
	if command.ExpectedRevision != 1 {
		return nil, console.ErrConflict
	}
	schedule, reportType, err := operationalEvidenceScheduleFromPayload(command.Payload)
	if err != nil || !operationalEvidenceReportType(reportType) {
		return nil, console.ErrInvalidRequest
	}
	next, err := schedule.Next(now)
	if err != nil {
		return nil, console.ErrInvalidRequest
	}
	_, err = tx.Exec(ctx, `INSERT INTO owner_console_report_schedules(
id,report_type,frequency,minute_utc,hour_utc,weekday_utc,state,next_run_at,
revision,owner_user_id,created_at,updated_at
) VALUES($1,$2,$3,$4,$5,$6,'active',$7,1,$8,$9,$9)`, command.TargetID,
		reportType, schedule.Frequency, schedule.Minute, schedule.Hour, schedule.Weekday,
		next, principal.UserID, now)
	if err != nil {
		return nil, ownerConsoleConstraintError(err)
	}
	return map[string]any{"schedule_id": command.TargetID, "state": "active",
		"next_run_at": next, "revision": 1}, nil
}

func operationalEvidenceScheduleFromPayload(payload map[string]any) (reporting.Schedule, string, error) {
	reportType, typeOK := payload["report_type"].(string)
	frequency, frequencyOK := payload["frequency"].(string)
	minute, minuteOK := payload["minute_utc"].(int)
	if !minuteOK {
		if value, ok := payload["minute_utc"].(float64); ok {
			minute, minuteOK = int(value), value == float64(int(value))
		}
	}
	if !typeOK || !frequencyOK || !minuteOK {
		return reporting.Schedule{}, "", console.ErrInvalidRequest
	}
	return reporting.Schedule{Frequency: reporting.Frequency(frequency), Minute: minute,
		Hour: operationalEvidenceOptionalInt(payload["hour_utc"]), Weekday: operationalEvidenceOptionalInt(payload["weekday_utc"])}, reportType, nil
}

func operationalEvidenceOptionalInt(value any) *int {
	switch typed := value.(type) {
	case *int:
		return typed
	case int:
		return &typed
	case float64:
		converted := int(typed)
		if typed == float64(converted) {
			return &converted
		}
	}
	return nil
}

func transitionOperationalEvidenceReportSchedule(
	ctx context.Context, tx pgx.Tx, principal authentication.Principal,
	command console.OwnerControlCommand, now time.Time,
) (map[string]any, error) {
	var frequency, current string
	var minute int
	var hour, weekday *int
	var revision int64
	err := tx.QueryRow(ctx, `SELECT frequency,minute_utc,hour_utc,weekday_utc,revision,state
FROM owner_console_report_schedules WHERE id=$1 FOR UPDATE`, command.TargetID).Scan(
		&frequency, &minute, &hour, &weekday, &revision, &current)
	if err == pgx.ErrNoRows {
		return nil, console.ErrNotFound
	}
	if err != nil || revision != command.ExpectedRevision || current == command.State ||
		(command.State != "active" && command.State != "paused") {
		return nil, console.ErrConflict
	}
	next, err := (reporting.Schedule{Frequency: reporting.Frequency(frequency), Minute: minute,
		Hour: hour, Weekday: weekday}).Next(now)
	if err != nil {
		return nil, console.ErrPrecondition
	}
	_, err = tx.Exec(ctx, `UPDATE owner_console_report_schedules SET state=$2,next_run_at=$3,
revision=revision+1,updated_at=$4 WHERE id=$1`, command.TargetID, command.State, next, now)
	_ = principal
	if err != nil {
		return nil, err
	}
	return map[string]any{"schedule_id": command.TargetID, "state": command.State,
		"next_run_at": next, "revision": revision + 1}, nil
}
