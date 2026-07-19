package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"axiom/internal/api/generated"
	"axiom/internal/backtest"
	"axiom/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const a11JobLease = 30 * time.Second

var a11FailureCode = regexp.MustCompile(`^[a-z][a-z0-9_]{0,95}$`)

// A11JobMaterializer turns a durable request into verified offline inputs.
// Exchange clients are deliberately absent from this boundary.
type A11JobMaterializer func(context.Context, string, string, json.RawMessage) (backtest.JobClaim, error)

// A11JobStore implements durable, exclusively leased offline job execution.
type A11JobStore struct {
	pool        *pgxpool.Pool
	owner       string
	clock       domain.Clock
	materialize A11JobMaterializer
}

// NewA11JobStore constructs the PostgreSQL backtest.Worker boundary.
func NewA11JobStore(pool *pgxpool.Pool, owner string, clock domain.Clock, materialize A11JobMaterializer) (*A11JobStore, error) {
	if pool == nil || owner == "" || clock == nil || materialize == nil {
		return nil, fmt.Errorf("a11_job_store_dependencies_missing")
	}
	return &A11JobStore{pool: pool, owner: owner, clock: clock, materialize: materialize}, nil
}

// Claim exclusively leases the oldest queued or expired offline job.
func (store *A11JobStore) Claim(ctx context.Context) (backtest.JobClaim, bool, error) {
	now := store.clock.Now().UTC
	jobID, kind, payload, found, err := store.claimRow(ctx, now)
	if err != nil || !found {
		return backtest.JobClaim{}, found, err
	}
	claim, err := store.materialize(ctx, jobID, kind, payload)
	if err != nil {
		_ = store.Fail(ctx, jobID, "offline_claim_materialization_failed")
		return backtest.JobClaim{}, false, fmt.Errorf("a11_job_materialization_failed")
	}
	claim.ID = jobID
	var resumeOrdinal int64
	if err = store.pool.QueryRow(ctx, `SELECT resume_ordinal,single_step FROM jobs WHERE id=$1 AND state='RUNNING'
      AND claim_owner=$2`, jobID, store.owner).Scan(&resumeOrdinal, &claim.SingleStep); err != nil || resumeOrdinal < 0 {
		_ = store.Fail(ctx, jobID, "offline_control_materialization_failed")
		return backtest.JobClaim{}, false, fmt.Errorf("a11_job_control_materialization_failed")
	}
	claim.ResumeOrdinal = uint64(resumeOrdinal)
	if err = store.attachRun(ctx, kind, payload, claim); err != nil {
		_ = store.Fail(ctx, jobID, "offline_run_manifest_persist_failed")
		return backtest.JobClaim{}, false, fmt.Errorf("a11_job_run_manifest_failed: %w", err)
	}
	return claim, true, nil
}

func (store *A11JobStore) claimRow(ctx context.Context, now time.Time) (string, string, json.RawMessage, bool, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return "", "", nil, false, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, `UPDATE jobs SET state='FAILED',claim_owner=NULL,claim_expires_at=NULL,
      failure_code='offline_job_attempts_exhausted',completed_at=$1,updated_at=$1,progress_revision=progress_revision+1
      WHERE state='RUNNING' AND claim_expires_at<=$1 AND retry_count>=max_attempts`, now); err != nil {
		return "", "", nil, false, err
	}
	var id, kind string
	var payload []byte
	err = tx.QueryRow(ctx, `SELECT candidate.id,candidate.job_type,candidate.request_payload FROM jobs candidate
      WHERE (candidate.state='QUEUED' AND (SELECT count(*) FROM jobs active
        WHERE active.owner_user_id=candidate.owner_user_id AND active.state IN ('RUNNING','PAUSE_REQUESTED'))<2)
      OR (candidate.state='RUNNING' AND candidate.claim_expires_at<=$1 AND candidate.retry_count<candidate.max_attempts)
      ORDER BY CASE WHEN candidate.state='RUNNING' THEN 0 ELSE 1 END,candidate.created_at,candidate.id
      FOR UPDATE OF candidate SKIP LOCKED LIMIT 1`, now).
		Scan(&id, &kind, &payload)
	if err == pgx.ErrNoRows {
		return "", "", nil, false, tx.Commit(ctx)
	}
	if err != nil {
		return "", "", nil, false, err
	}
	leaseEnd := now.Add(a11JobLease)
	_, err = tx.Exec(ctx, `UPDATE jobs SET state='RUNNING',claim_owner=$2,claim_epoch=coalesce(claim_epoch,0)+1,
      claim_expires_at=$3,retry_count=retry_count+CASE WHEN state='RUNNING' OR run_id IS NULL THEN 1 ELSE 0 END,
      started_at=coalesce(started_at,$1),updated_at=$1,
      progress_revision=progress_revision+1 WHERE id=$4`, now, store.owner, leaseEnd, id)
	if err != nil {
		return "", "", nil, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", "", nil, false, err
	}
	return id, kind, append(json.RawMessage(nil), payload...), true, nil
}

// Renew extends only this worker's still-valid lease.
func (store *A11JobStore) Renew(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("a11_job_renewal_invalid")
	}
	now := store.clock.Now().UTC
	tag, err := store.pool.Exec(ctx, `UPDATE jobs SET claim_expires_at=$1,updated_at=$2,
      progress_revision=progress_revision+1 WHERE id=$3 AND state IN ('RUNNING','PAUSE_REQUESTED') AND claim_owner=$4
      AND claim_expires_at>$2`, now.Add(a11JobLease), now, id, store.owner)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("a11_job_renewal_rejected")
	}
	return nil
}

// Control reads the current fenced replay posture without mutating progress.
func (store *A11JobStore) Control(ctx context.Context, id string) (backtest.JobControl, error) {
	var state string
	var resume int64
	var step bool
	err := store.pool.QueryRow(ctx, `SELECT state,resume_ordinal,single_step FROM jobs WHERE id=$1
      AND state IN ('RUNNING','PAUSE_REQUESTED') AND claim_owner=$2 AND claim_expires_at>CURRENT_TIMESTAMP`,
		id, store.owner).Scan(&state, &resume, &step)
	if err != nil || resume < 0 {
		return backtest.JobControl{}, fmt.Errorf("a11_job_control_unavailable")
	}
	return backtest.JobControl{PauseRequested: state == "PAUSE_REQUESTED", SingleStep: step,
		ResumeOrdinal: uint64(resume)}, nil
}

// Pause durably acknowledges a requested or single-step replay boundary.
func (store *A11JobStore) Pause(ctx context.Context, id string, ordinal uint64, checkpoint []byte) error {
	if id == "" || ordinal == 0 || ordinal > uint64(^uint64(0)>>1) || !json.Valid(checkpoint) {
		return fmt.Errorf("a11_job_pause_invalid")
	}
	now := store.clock.Now().UTC
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var state, runID string
	var step bool
	if err = tx.QueryRow(ctx, `SELECT state,run_id,single_step FROM jobs WHERE id=$1 AND
      state IN ('RUNNING','PAUSE_REQUESTED') AND claim_owner=$2 AND claim_expires_at>$3 FOR UPDATE`,
		id, store.owner, now).Scan(&state, &runID, &step); err != nil || (state == "RUNNING" && !step) {
		return fmt.Errorf("a11_job_pause_not_requested")
	}
	if state == "RUNNING" {
		if _, err = tx.Exec(ctx, `UPDATE jobs SET state='PAUSE_REQUESTED',progress_revision=progress_revision+1,
        updated_at=$2 WHERE id=$1`, id, now); err != nil {
			return err
		}
	}
	tag, err := tx.Exec(ctx, `UPDATE jobs SET state='PAUSED',claim_owner=NULL,claim_expires_at=NULL,
      resume_ordinal=$2,checkpoint_payload=$3,progress_revision=progress_revision+1,updated_at=$4
      WHERE id=$1 AND state='PAUSE_REQUESTED' AND claim_owner=$5`, id, int64(ordinal), string(checkpoint),
		now, store.owner)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("a11_job_pause_conflict")
	}
	runTag, err := tx.Exec(ctx, `UPDATE runs SET state='paused' WHERE id=$1 AND state='running'`, runID)
	if err != nil || runTag.RowsAffected() != 1 {
		return fmt.Errorf("a11_run_pause_conflict")
	}
	var checkpointRevision int64
	if err = tx.QueryRow(ctx, `SELECT coalesce(max(revision),0)+1 FROM run_checkpoints WHERE run_id=$1`,
		runID).Scan(&checkpointRevision); err != nil {
		return err
	}
	checkpointHash := a11SHA256(checkpoint)
	checkpointID := fmt.Sprintf("replay-checkpoint-%s-%d", id, checkpointRevision)
	if _, err = tx.Exec(ctx, `INSERT INTO run_checkpoints(id,run_id,revision,input_ordinal,state_hash,payload,
      created_at,deterministic_state_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$5)`, checkpointID, runID,
		checkpointRevision, int64(ordinal), checkpointHash, checkpoint, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Complete atomically publishes the safe API result projection and releases the lease.
func (store *A11JobStore) Complete(ctx context.Context, id string, result backtest.CanonicalResult, canonical []byte) error {
	if id == "" || result.ResultHash == "" || !json.Valid(canonical) {
		return fmt.Errorf("a11_job_result_invalid")
	}
	projection, err := json.Marshal(a11JobResult(result))
	if err != nil {
		return fmt.Errorf("a11_job_result_invalid")
	}
	now := store.clock.Now().UTC
	if err = store.completeRun(ctx, id, result, canonical, projection, now); err != nil {
		return fmt.Errorf("a11_job_completion_rejected")
	}
	return nil
}

// Fail records one bounded terminal reason and releases the worker lease.
func (store *A11JobStore) Fail(ctx context.Context, id, reason string) error {
	if id == "" || !a11FailureCode.MatchString(reason) {
		return fmt.Errorf("a11_job_failure_invalid")
	}
	now := store.clock.Now().UTC
	if err := store.failRun(ctx, id, reason, now); err != nil {
		return fmt.Errorf("a11_job_failure_rejected")
	}
	return nil
}

func a11JobResult(result backtest.CanonicalResult) generated.JobResult {
	metrics := map[string]generated.Decimal{
		"total_net_return": result.Metrics.TotalNetReturn, "maximum_drawdown": result.Metrics.MaximumDrawdown,
		"current_drawdown": result.Metrics.CurrentDrawdown, "sharpe_ratio": result.Metrics.SharpeRatio,
		"sortino_ratio": result.Metrics.SortinoRatio, "profit_factor": result.Metrics.ProfitFactor,
		"expectancy": result.Metrics.Expectancy, "win_rate": result.Metrics.WinRate,
		"turnover": result.Metrics.Turnover, "exposure": result.Metrics.Exposure,
		"trades": generated.Decimal(strconv.FormatUint(result.Metrics.Trades, 10)),
	}
	return generated.JobResult{ResultHash: result.ResultHash, Metrics: &metrics,
		PlatformCorrectness: "canonical_pipeline_completed", StrategyEvidence: "confidence_tier_" + string(result.Confidence),
		Viability: generated.JobResultViability("undetermined"), Reproducibility: result.ManifestHash}
}

var _ backtest.ControlledJobStore = (*A11JobStore)(nil)
