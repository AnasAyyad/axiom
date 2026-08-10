package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"axiom/internal/domain"
	"axiom/internal/reporting"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OperationalEvidenceReportWorker queues due UTC schedules and generates safe bounded report
// snapshots. It owns no exchange client or private credentials.
type OperationalEvidenceReportWorker struct {
	pool  *pgxpool.Pool
	owner string
	clock domain.Clock
	build func(context.Context, pgx.Tx, string, string, time.Time) ([]byte, error)
}

// NewOperationalEvidenceReportWorker constructs the durable report worker boundary.
func NewOperationalEvidenceReportWorker(pool *pgxpool.Pool, owner string, clock domain.Clock) (*OperationalEvidenceReportWorker, error) {
	if pool == nil || owner == "" || clock == nil {
		return nil, fmt.Errorf("operational_evidence_report_worker_invalid")
	}
	return &OperationalEvidenceReportWorker{pool: pool, owner: owner, clock: clock, build: buildOperationalEvidenceReportContent}, nil
}

// RunOne queues at most one due schedule, then generates at most one report.
func (worker *OperationalEvidenceReportWorker) RunOne(ctx context.Context) (bool, error) {
	now := worker.clock.Now().UTC
	queued, err := worker.queueDue(ctx, now)
	if err != nil || queued {
		return queued, err
	}
	return worker.generateOne(ctx, now)
}

func (worker *OperationalEvidenceReportWorker) queueDue(ctx context.Context, now time.Time) (bool, error) {
	tx, err := worker.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	storageReady, err := operationalReadinessHeavyWorkAllowed(ctx, tx)
	if err != nil {
		return false, err
	}
	if !storageReady {
		return false, tx.Commit(ctx)
	}
	var id, reportType, frequency, owner string
	var minute int
	var hour, weekday *int
	var scheduledFor time.Time
	err = tx.QueryRow(ctx, `SELECT id,report_type,frequency,minute_utc,hour_utc,
weekday_utc,owner_user_id,next_run_at FROM owner_console_report_schedules
WHERE state='active' AND next_run_at<=$1 ORDER BY next_run_at,id
FOR UPDATE SKIP LOCKED LIMIT 1`, now).Scan(&id, &reportType, &frequency, &minute,
		&hour, &weekday, &owner, &scheduledFor)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, tx.Commit(ctx)
	}
	if err != nil {
		return false, err
	}
	next, err := (reporting.Schedule{Frequency: reporting.Frequency(frequency),
		Minute: minute, Hour: hour, Weekday: weekday}).Next(scheduledFor)
	if err != nil {
		return false, err
	}
	if err = queueScheduledOperationalEvidenceReport(ctx, tx, id, owner, reportType, scheduledFor, now); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `UPDATE owner_console_report_schedules SET last_run_at=$2,
next_run_at=$3,revision=revision+1,updated_at=$4 WHERE id=$1`, id, scheduledFor,
		next, now); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func queueScheduledOperationalEvidenceReport(
	ctx context.Context, tx pgx.Tx, scheduleID, owner, reportType string,
	scheduledFor, now time.Time,
) error {
	identity := ownerConsoleHash([]byte(scheduleID + "\x00" + scheduledFor.Format(time.RFC3339Nano)))
	jobID, reportID := "report-job-"+identity[:24], "report-"+identity[24:48]
	payload, _ := json.Marshal(map[string]any{"report_type": reportType,
		"schedule_id": scheduleID, "scheduled_for": scheduledFor.Format(time.RFC3339Nano)})
	provenance, err := operationalEvidenceReportSnapshot(ctx, tx, reportType)
	if err != nil {
		return err
	}
	models, _ := json.Marshal(provenance.models)
	if _, err = tx.Exec(ctx, `INSERT INTO jobs(
id,job_type,idempotency_key,state,payload_hash,created_at,updated_at,owner_user_id,
request_payload,max_attempts
) VALUES($1,$2,$3,'QUEUED',$4,$5,$5,$6,$7,3) ON CONFLICT DO NOTHING`, jobID,
		"report:"+reportType, "scheduled:"+identity, ownerConsoleHash(payload), now, owner, payload); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO owner_console_reports(
id,job_id,schedule_id,scheduled_for,report_type,state,mode,confidence_tier,
valuation_basis,model_provenance,maturity,source_identity,source_revision,
created_at,updated_at,revision
) VALUES($1,$2,$3,$4,$5,'QUEUED',$6,$7,$8,$9,$10,$11,$12,$13,$13,1)
ON CONFLICT DO NOTHING`, reportID, jobID, scheduleID, scheduledFor, reportType,
		provenance.mode, provenance.confidence, provenance.valuation, models,
		provenance.maturity, provenance.sourceIdentity, provenance.sourceRevision, now)
	return err
}

func (worker *OperationalEvidenceReportWorker) generateOne(ctx context.Context, now time.Time) (bool, error) {
	tx, err := worker.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	jobID, reportID, reportType, err := worker.claimOperationalEvidenceReport(ctx, tx, now)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, tx.Commit(ctx)
	}
	if err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, "SAVEPOINT operational_evidence_report_content"); err != nil {
		return false, err
	}
	content, err := worker.build(ctx, tx, reportID, reportType, now)
	if err != nil {
		if failErr := failOperationalEvidenceReportGeneration(ctx, tx, jobID, reportID, now); failErr != nil {
			return false, failErr
		}
		return true, tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, "RELEASE SAVEPOINT operational_evidence_report_content"); err != nil {
		return false, err
	}
	if err = completeOperationalEvidenceReportGeneration(ctx, tx, jobID, reportID, content, now); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (worker *OperationalEvidenceReportWorker) claimOperationalEvidenceReport(
	ctx context.Context, tx pgx.Tx, now time.Time,
) (string, string, string, error) {
	var jobID, reportID, reportType string
	err := tx.QueryRow(ctx, `SELECT job.id,report.id,report.report_type FROM jobs job
JOIN owner_console_reports report ON report.job_id=job.id
WHERE job.state='QUEUED' AND report.state='QUEUED'
ORDER BY job.created_at,job.id FOR UPDATE OF job,report SKIP LOCKED LIMIT 1`).Scan(
		&jobID, &reportID, &reportType)
	if err != nil {
		return "", "", "", err
	}
	if _, err = tx.Exec(ctx, `UPDATE jobs SET state='RUNNING',claim_owner=$2,
claim_epoch=coalesce(claim_epoch,0)+1,claim_expires_at=$3,started_at=$4,
updated_at=$4,progress_revision=progress_revision+1 WHERE id=$1`, jobID,
		worker.owner, now.Add(5*time.Minute), now); err != nil {
		return "", "", "", err
	}
	_, err = tx.Exec(ctx, `UPDATE owner_console_reports SET state='RUNNING',updated_at=$2,
revision=revision+1 WHERE id=$1`, reportID, now)
	return jobID, reportID, reportType, err
}

func completeOperationalEvidenceReportGeneration(
	ctx context.Context, tx pgx.Tx, jobID, reportID string, content []byte, now time.Time,
) error {
	hash := ownerConsoleHash(content)
	if _, err := tx.Exec(ctx, `UPDATE owner_console_reports SET state='SUCCEEDED',content=$2,
content_hash=$3,generated_at=$4,updated_at=$4,revision=revision+1 WHERE id=$1`,
		reportID, content, hash, now); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE jobs SET state='SUCCEEDED',claim_owner=NULL,
claim_epoch=NULL,claim_expires_at=NULL,result_payload=jsonb_build_object(
'report_id',$2::text,'content_hash',$3::text),completed_at=$4,updated_at=$4,
progress_revision=progress_revision+1 WHERE id=$1`, jobID, reportID, hash, now)
	return err
}

func failOperationalEvidenceReportGeneration(
	ctx context.Context, tx pgx.Tx, jobID, reportID string, now time.Time,
) error {
	if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT operational_evidence_report_content"); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "RELEASE SAVEPOINT operational_evidence_report_content"); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE owner_console_reports SET state='FAILED',failure_code=$2,
updated_at=$3,revision=revision+1 WHERE id=$1`, reportID,
		"report_generation_failed", now); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE jobs SET state='FAILED',claim_owner=NULL,
claim_expires_at=NULL,failure_code=$2,completed_at=$3,updated_at=$3,
progress_revision=progress_revision+1 WHERE id=$1`, jobID,
		"report_generation_failed", now)
	return err
}
