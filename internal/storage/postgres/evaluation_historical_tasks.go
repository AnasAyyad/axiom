package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"time"

	"axiom/internal/domain"
	"axiom/internal/evaluation"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	evaluationHistoricalLease = 2 * time.Minute
)

// EvaluationHistoricalTaskStore owns fenced import checkpoints and bounded
// source retry state. Immutable segment evidence is committed separately.
type EvaluationHistoricalTaskStore struct {
	pool  *pgxpool.Pool
	owner string
	clock domain.Clock
}

// NewEvaluationHistoricalTaskStore constructs the fenced historical-import queue.
func NewEvaluationHistoricalTaskStore(pool *pgxpool.Pool, owner string,
	clock domain.Clock) (*EvaluationHistoricalTaskStore, error) {
	if pool == nil || owner == "" || clock == nil {
		return nil, fmt.Errorf("evaluation_historical_task_store_dependencies_missing")
	}
	return &EvaluationHistoricalTaskStore{pool: pool, owner: owner, clock: clock}, nil
}

// HistoricalImportSummary reports aggregate progress for a campaign's import tasks.
func (store *EvaluationHistoricalTaskStore) HistoricalImportSummary(ctx context.Context,
	campaignID string) (evaluation.HistoricalImportSummary, error) {
	var summary evaluation.HistoricalImportSummary
	var rows, bytes int64
	var retryAt *time.Time
	var blockedReason *string
	err := store.pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE state='COMPLETED'),
	  count(*) FILTER (WHERE state='BLOCKED'),COALESCE(sum(row_count),0),COALESCE(sum(byte_count),0),
	  min(retry_at) FILTER (WHERE state='RUNNING'),min(reason_code) FILTER (WHERE state='BLOCKED')
	  FROM evaluation_historical_imports WHERE campaign_id=$1`, campaignID).Scan(&summary.Total,
		&summary.Completed, &summary.Blocked, &rows, &bytes, &retryAt, &blockedReason)
	if err != nil {
		return evaluation.HistoricalImportSummary{}, err
	}
	if rows < 0 || bytes < 0 {
		return evaluation.HistoricalImportSummary{}, fmt.Errorf("evaluation_historical_summary_invalid")
	}
	summary.RowCount, summary.ByteCount = uint64(rows), bytes
	if retryAt != nil {
		summary.RetryAt = retryAt.UTC()
	}
	if blockedReason != nil {
		summary.BlockedReason = evaluation.ReasonCode(*blockedReason)
	}
	return summary, nil
}

// ClaimHistoricalImport leases the next eligible task to this worker.
func (store *EvaluationHistoricalTaskStore) ClaimHistoricalImport(ctx context.Context,
	campaignID string) (evaluation.HistoricalImportTask, bool, error) {
	now := store.clock.Now().UTC
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return evaluation.HistoricalImportTask{}, false, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	row := tx.QueryRow(ctx, `SELECT id,campaign_id,exchange_id,instrument,interval,window_start,
	  window_end,checkpoint_time,session_id,recorder_dataset_id,created_at,claim_epoch
	  FROM evaluation_historical_imports WHERE campaign_id=$1 AND state IN ('PENDING','RUNNING')
	  AND (retry_at IS NULL OR retry_at<=$2) AND (claim_owner IS NULL OR claim_expires_at<=$2)
	  ORDER BY exchange_id,instrument,CASE interval WHEN '15m' THEN 1 WHEN '1h' THEN 2 ELSE 3 END
	  FOR UPDATE SKIP LOCKED LIMIT 1`, campaignID, now)
	task, err := scanHistoricalImportTask(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return evaluation.HistoricalImportTask{}, false, tx.Commit(ctx)
	}
	if err != nil {
		return evaluation.HistoricalImportTask{}, false, err
	}
	task.ClaimEpoch++
	tag, err := tx.Exec(ctx, `UPDATE evaluation_historical_imports SET state='RUNNING',reason_code=NULL,
	  retry_at=NULL,claim_owner=$2,claim_epoch=$3,claim_expires_at=$4,updated_at=$5 WHERE id=$1`,
		task.Spec.ID, store.owner, task.ClaimEpoch, now.Add(evaluationHistoricalLease), now)
	if err != nil || tag.RowsAffected() != 1 {
		return evaluation.HistoricalImportTask{}, false, fmt.Errorf("evaluation_historical_claim_failed")
	}
	return task, true, tx.Commit(ctx)
}

// CommitHistoricalImport advances an import checkpoint and optionally finalizes its dataset.
func (store *EvaluationHistoricalTaskStore) CommitHistoricalImport(ctx context.Context,
	task evaluation.HistoricalImportTask, progress evaluation.HistoricalImportProgress,
	completion *evaluation.HistoricalImportCompletion) error {
	if task.ClaimEpoch <= 0 || progress.Artifact.ID == "" || progress.RowCount == 0 ||
		progress.Artifact.ByteCount <= 0 || !progress.PageStart.Equal(task.Spec.Checkpoint) ||
		!progress.NextCheckpoint.After(progress.PageStart) || progress.Complete != (completion != nil) {
		return fmt.Errorf("evaluation_historical_checkpoint_invalid")
	}
	now := store.clock.Now().UTC
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	rowCount, byteCount, existingRaw, existingNormalized, err := store.lockHistoricalCheckpoint(
		ctx, tx, task, progress, now)
	if err != nil {
		return err
	}
	rawHash := historicalRollingHash(existingRaw, progress.Artifact.RawPayloadHash)
	normalizedHash := historicalRollingHash(existingNormalized, progress.Artifact.NormalizedPageHash)
	state, manifestRevision, manifestHash, manifestPath, registeredDatasetID, err := historicalCompletionValues(
		task, progress, completion)
	if err != nil {
		return err
	}
	metadata, _ := json.Marshal(map[string]any{"official_public_source": task.Spec.Exchange,
		"interval": task.Spec.Interval, "last_page_start": progress.PageStart,
		"last_page_artifact": progress.Artifact.ID, "raw_canonical_linked": true})
	tag, err := tx.Exec(ctx, `UPDATE evaluation_historical_imports SET state=$2,checkpoint_time=$3,
	  row_count=$4,byte_count=$5,raw_hash=$6,normalized_hash=$7,raw_segment_id=$8,
	  manifest_revision=CASE WHEN $9=0 THEN manifest_revision ELSE $9 END,
	  manifest_hash=COALESCE($10,manifest_hash),manifest_path=COALESCE($11,manifest_path),
	  normalized_dataset_id=COALESCE($12,normalized_dataset_id),last_ordinal=last_ordinal+1,
	  source_metadata=$13,reason_code=NULL,claim_owner=NULL,claim_expires_at=NULL,updated_at=$14
	  WHERE id=$1 AND claim_owner=$15 AND claim_epoch=$16`, task.Spec.ID, state,
		progress.NextCheckpoint, rowCount+int64(progress.RowCount), byteCount+progress.Artifact.ByteCount,
		rawHash[:], normalizedHash[:], progress.Artifact.ID+"-wire", manifestRevision, manifestHash,
		manifestPath, registeredDatasetID, metadata, now, store.owner, task.ClaimEpoch)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("evaluation_historical_checkpoint_conflict")
	}
	return tx.Commit(ctx)
}

func (store *EvaluationHistoricalTaskStore) lockHistoricalCheckpoint(ctx context.Context, tx pgx.Tx,
	task evaluation.HistoricalImportTask, progress evaluation.HistoricalImportProgress,
	now time.Time) (int64, int64, []byte, []byte, error) {
	var checkpoint time.Time
	var existingRaw, existingNormalized []byte
	var rowCount, byteCount, epoch int64
	err := tx.QueryRow(ctx, `SELECT checkpoint_time,raw_hash,normalized_hash,row_count,byte_count,claim_epoch
FROM evaluation_historical_imports WHERE id=$1 AND claim_owner=$2 AND claim_expires_at>$3 FOR UPDATE`,
		task.Spec.ID, store.owner, now).Scan(&checkpoint, &existingRaw, &existingNormalized,
		&rowCount, &byteCount, &epoch)
	if err != nil || epoch != task.ClaimEpoch || !checkpoint.Equal(task.Spec.Checkpoint) ||
		rowCount < 0 || byteCount < 0 || progress.RowCount > math.MaxInt64 ||
		progress.Artifact.ByteCount > math.MaxInt64-byteCount || int64(progress.RowCount) > math.MaxInt64-rowCount {
		return 0, 0, nil, nil, fmt.Errorf("evaluation_historical_claim_conflict")
	}
	return rowCount, byteCount, existingRaw, existingNormalized, nil
}

func historicalCompletionValues(task evaluation.HistoricalImportTask, progress evaluation.HistoricalImportProgress,
	completion *evaluation.HistoricalImportCompletion) (string, int64, []byte, any, any, error) {
	if completion == nil {
		return "RUNNING", 0, nil, nil, nil, nil
	}
	revision := int64(completion.Manifest.Revision)
	hash, err := hex.DecodeString(completion.Manifest.Hash)
	manifestFile := fmt.Sprintf("%s-%06d.dataset.json", completion.Manifest.SessionID, completion.Manifest.Revision)
	if err != nil || len(hash) != sha256.Size || revision <= 0 || filepath.Base(manifestFile) != manifestFile ||
		completion.Manifest.SessionID != task.SessionID || completion.Manifest.DatasetID != task.DatasetID ||
		completion.RegisteredDatasetID == "" || !progress.NextCheckpoint.Equal(task.Spec.WindowEnd) {
		return "", 0, nil, nil, nil, fmt.Errorf("evaluation_historical_completion_invalid")
	}
	return "COMPLETED", revision, hash, manifestFile, completion.RegisteredDatasetID, nil
}

// FailHistoricalImport records retryable or terminal import failure evidence.
// Recoverable failures have no attempt-count terminal cutoff.
func (store *EvaluationHistoricalTaskStore) FailHistoricalImport(ctx context.Context,
	task evaluation.HistoricalImportTask, failure evaluation.HistoricalImportError) (bool, error) {
	if task.ClaimEpoch <= 0 || failure.Reason == "" || failure.Code == "" {
		return true, fmt.Errorf("evaluation_historical_failure_invalid")
	}
	now := store.clock.Now().UTC
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return true, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var retries int
	var epoch int64
	err = tx.QueryRow(ctx, `SELECT retry_count,claim_epoch FROM evaluation_historical_imports
	  WHERE id=$1 AND claim_owner=$2 AND claim_expires_at>$3 FOR UPDATE`, task.Spec.ID, store.owner, now).
		Scan(&retries, &epoch)
	if err != nil || epoch != task.ClaimEpoch {
		return true, fmt.Errorf("evaluation_historical_claim_conflict")
	}
	blocked := !failure.Recoverable
	state := "RUNNING"
	var retryAt any
	if blocked {
		state = "BLOCKED"
	} else {
		retryAt = now.Add(historicalRetryDelay(retries + 1))
	}
	tag, err := tx.Exec(ctx, `UPDATE evaluation_historical_imports SET state=$2,reason_code=$3,
	  retry_count=retry_count+1,retry_at=$4,claim_owner=NULL,claim_expires_at=NULL,updated_at=$5
	  WHERE id=$1 AND claim_owner=$6 AND claim_epoch=$7`, task.Spec.ID, state, string(failure.Reason),
		retryAt, now, store.owner, task.ClaimEpoch)
	if err != nil || tag.RowsAffected() != 1 {
		return true, fmt.Errorf("evaluation_historical_failure_conflict")
	}
	return blocked, tx.Commit(ctx)
}

type historicalTaskScanner interface{ Scan(...any) error }

func scanHistoricalImportTask(row historicalTaskScanner) (evaluation.HistoricalImportTask, error) {
	var task evaluation.HistoricalImportTask
	var instrument string
	err := row.Scan(&task.Spec.ID, &task.Spec.CampaignID, &task.Spec.Exchange, &instrument,
		&task.Spec.Interval, &task.Spec.WindowStart, &task.Spec.WindowEnd, &task.Spec.Checkpoint,
		&task.SessionID, &task.DatasetID, &task.CreatedAt, &task.ClaimEpoch)
	if err != nil {
		return evaluation.HistoricalImportTask{}, err
	}
	base := "BTC"
	if instrument == "ETH/USDT" {
		base = "ETH"
	} else if instrument != "BTC/USDT" {
		return evaluation.HistoricalImportTask{}, fmt.Errorf("evaluation_historical_instrument_invalid")
	}
	task.Spec.Instrument, err = domain.NewSpotInstrument(domain.AssetSymbol(base), "USDT")
	task.Spec.WindowStart, task.Spec.WindowEnd = task.Spec.WindowStart.UTC(), task.Spec.WindowEnd.UTC()
	task.Spec.Checkpoint, task.CreatedAt = task.Spec.Checkpoint.UTC(), task.CreatedAt.UTC()
	if err != nil || task.SessionID != evaluation.HistoricalImportSessionID(task.Spec.ID) ||
		task.DatasetID != evaluation.HistoricalImportDatasetID(task.Spec.ID) {
		return evaluation.HistoricalImportTask{}, fmt.Errorf("evaluation_historical_task_invalid")
	}
	return task, nil
}

func historicalRollingHash(previous []byte, next [sha256.Size]byte) [sha256.Size]byte {
	digest := sha256.New()
	_, _ = digest.Write(previous)
	_, _ = digest.Write(next[:])
	var result [sha256.Size]byte
	copy(result[:], digest.Sum(nil))
	return result
}

func historicalRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := 15 * time.Second * time.Duration(1<<min(attempt-1, 6))
	if delay > 10*time.Minute {
		return 10 * time.Minute
	}
	return delay
}

var _ evaluation.HistoricalImportTaskStore = (*EvaluationHistoricalTaskStore)(nil)
