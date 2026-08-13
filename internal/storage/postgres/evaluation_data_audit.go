package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"axiom/internal/domain"
	"axiom/internal/evaluation"
	"axiom/internal/recorder"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const evaluationDataAuditLease = 5 * time.Minute

type evaluationDataAuditSummary struct {
	ID, State                              string
	Total, Completed, Eligible, Ineligible int
}

type evaluationDataAuditTask struct {
	AuditID    string
	ClaimEpoch int64
	Expected   evaluation.RecordedDatasetExpectation
}

// EvaluationDataAuditCoordinator preserves every source file and appends one
// explicit eligibility finding per pre-campaign dataset.
type EvaluationDataAuditCoordinator struct {
	pool  *pgxpool.Pool
	owner string
	root  string
	clock domain.Clock
}

// NewEvaluationDataAuditCoordinator constructs the immutable dataset-audit worker.
func NewEvaluationDataAuditCoordinator(pool *pgxpool.Pool, owner, root string,
	clock domain.Clock) (*EvaluationDataAuditCoordinator, error) {
	if pool == nil || owner == "" || !filepath.IsAbs(filepath.Clean(root)) || clock == nil {
		return nil, fmt.Errorf("evaluation_data_audit_dependencies_missing")
	}
	return &EvaluationDataAuditCoordinator{pool: pool, owner: owner, root: filepath.Clean(root), clock: clock}, nil
}

// Advance progresses the audit bound to the supplied evaluation campaign.
func (coordinator *EvaluationDataAuditCoordinator) Advance(ctx context.Context,
	campaign evaluation.Campaign) (evaluation.StageProgress, error) {
	auditID, err := coordinator.ensureCampaignAudit(ctx, campaign)
	if err != nil {
		return evaluation.StageProgress{}, err
	}
	return coordinator.advanceAudit(ctx, auditID)
}

// RunOne advances the oldest owner-created standalone audit through the same
// immutable finding and lease-fencing path used by campaign audits.
func (coordinator *EvaluationDataAuditCoordinator) RunOne(ctx context.Context) (bool, error) {
	var auditID string
	err := coordinator.pool.QueryRow(ctx, `SELECT id FROM evaluation_data_audits
WHERE campaign_id IS NULL AND state IN ('PENDING','RUNNING')
  AND (claim_owner IS NULL OR claim_expires_at<=$1)
ORDER BY created_at,id LIMIT 1`, coordinator.clock.Now().UTC).Scan(&auditID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, err = coordinator.advanceAudit(ctx, auditID)
	return true, err
}

func (coordinator *EvaluationDataAuditCoordinator) advanceAudit(ctx context.Context,
	auditID string) (evaluation.StageProgress, error) {
	summary, err := coordinator.summary(ctx, auditID)
	if err != nil {
		return evaluation.StageProgress{}, err
	}
	if progress, terminal := terminalDataAuditProgress(summary, auditID); terminal {
		return progress, nil
	}
	task, found, err := coordinator.claim(ctx, auditID)
	if err != nil {
		return evaluation.StageProgress{}, err
	}
	if !found {
		summary, err = coordinator.summary(ctx, auditID)
		if err != nil {
			return evaluation.StageProgress{}, err
		}
		return currentDataAuditProgress(summary, auditID, "Existing-data audit checkpoint is current."), nil
	}
	root, ambiguous := coordinator.resolveRoot(task.Expected)
	finding := evaluation.AuditRecordedDataset(root, task.Expected)
	if ambiguous {
		finding.Eligibility, finding.ReasonCode = "ineligible", "MANIFEST_PATH_AMBIGUOUS"
	}
	if err = coordinator.commitFinding(ctx, task, finding); err != nil {
		return evaluation.StageProgress{}, err
	}
	summary, err = coordinator.summary(ctx, auditID)
	if err != nil {
		return evaluation.StageProgress{}, err
	}
	message := "Existing-data audit advanced one immutable finding."
	if summary.State == "COMPLETED" {
		message = "Existing recordings were audited without modification."
	}
	return currentDataAuditProgress(summary, auditID, message), nil
}

func terminalDataAuditProgress(summary evaluationDataAuditSummary,
	auditID string) (evaluation.StageProgress, bool) {
	if summary.State == "BLOCKED" {
		return evaluation.StageProgress{State: evaluation.ProgressBlock, Reason: evaluation.ReasonPersistenceFailed,
			Summary: "Existing-data audit is blocked."}, true
	}
	if summary.State == "COMPLETED" {
		return currentDataAuditProgress(summary, auditID,
			"Existing recordings were audited without modification."), true
	}
	return evaluation.StageProgress{}, false
}

func currentDataAuditProgress(summary evaluationDataAuditSummary, auditID, message string) evaluation.StageProgress {
	state := evaluation.ProgressWaiting
	if summary.State == "COMPLETED" {
		state = evaluation.ProgressComplete
	}
	return evaluation.StageProgress{State: state, Summary: message, Checkpoint: dataAuditCheckpoint(summary),
		LinkedResourceType: "data_audit", LinkedResourceID: auditID}
}

func (coordinator *EvaluationDataAuditCoordinator) ensureCampaignAudit(ctx context.Context,
	campaign evaluation.Campaign) (string, error) {
	digest := sha256.Sum256([]byte("evaluation-data-audit:" + campaign.ID))
	id := "evaluation-audit-" + hex.EncodeToString(digest[:8])
	now := coordinator.clock.Now().UTC
	_, err := coordinator.pool.Exec(ctx, `INSERT INTO evaluation_data_audits(
	  id,campaign_id,state,baseline_at,created_at,updated_at) VALUES($1,$2,'PENDING',$3,$4,$4)
	  ON CONFLICT (campaign_id) DO NOTHING`, id, campaign.ID, campaign.CreatedAt.UTC(), now)
	if err != nil {
		return "", err
	}
	var actual string
	if err = coordinator.pool.QueryRow(ctx, `SELECT id FROM evaluation_data_audits WHERE campaign_id=$1`,
		campaign.ID).Scan(&actual); err != nil || actual != id {
		return "", fmt.Errorf("evaluation_data_audit_identity_conflict")
	}
	return id, nil
}

func (coordinator *EvaluationDataAuditCoordinator) summary(ctx context.Context,
	auditID string) (evaluationDataAuditSummary, error) {
	var value evaluationDataAuditSummary
	err := coordinator.pool.QueryRow(ctx, `SELECT audit.id,audit.state,
	  (SELECT count(*) FROM dataset_manifests dataset WHERE dataset.created_at<audit.baseline_at),
	  (SELECT count(*) FROM evaluation_data_audit_findings finding WHERE finding.audit_id=audit.id),
	  (SELECT count(*) FROM evaluation_data_audit_findings finding WHERE finding.audit_id=audit.id AND finding.eligibility='eligible'),
	  (SELECT count(*) FROM evaluation_data_audit_findings finding WHERE finding.audit_id=audit.id AND finding.eligibility='ineligible')
	  FROM evaluation_data_audits audit WHERE audit.id=$1`, auditID).Scan(&value.ID, &value.State,
		&value.Total, &value.Completed, &value.Eligible, &value.Ineligible)
	return value, err
}

func (coordinator *EvaluationDataAuditCoordinator) claim(ctx context.Context,
	auditID string) (evaluationDataAuditTask, bool, error) {
	now := coordinator.clock.Now().UTC
	tx, err := coordinator.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return evaluationDataAuditTask{}, false, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	state, baseline, epoch, found, err := lockDataAudit(ctx, tx, auditID, now)
	if err != nil {
		return evaluationDataAuditTask{}, false, err
	}
	if !found {
		return evaluationDataAuditTask{}, false, tx.Commit(ctx)
	}
	if state == "COMPLETED" || state == "BLOCKED" {
		return evaluationDataAuditTask{}, false, tx.Commit(ctx)
	}
	task, found, err := nextDataAuditTask(ctx, tx, auditID, baseline)
	if err != nil {
		return evaluationDataAuditTask{}, false, err
	}
	if !found {
		if _, err = tx.Exec(ctx, `UPDATE evaluation_data_audits SET state='COMPLETED',completed_at=$2,
		  updated_at=$2,claim_owner=NULL,claim_expires_at=NULL WHERE id=$1`, auditID, now); err != nil {
			return evaluationDataAuditTask{}, false, err
		}
		return evaluationDataAuditTask{}, false, tx.Commit(ctx)
	}
	task.AuditID, task.ClaimEpoch = auditID, epoch+1
	tag, err := tx.Exec(ctx, `UPDATE evaluation_data_audits SET state='RUNNING',claim_owner=$2,
	  claim_epoch=$3,claim_expires_at=$4,updated_at=$5 WHERE id=$1`, auditID, coordinator.owner,
		task.ClaimEpoch, now.Add(evaluationDataAuditLease), now)
	if err != nil || tag.RowsAffected() != 1 {
		return evaluationDataAuditTask{}, false, fmt.Errorf("evaluation_data_audit_claim_failed")
	}
	return task, true, tx.Commit(ctx)
}

func lockDataAudit(ctx context.Context, tx pgx.Tx, auditID string,
	now time.Time) (string, time.Time, int64, bool, error) {
	var state string
	var baseline time.Time
	var epoch int64
	err := tx.QueryRow(ctx, `SELECT state,baseline_at,claim_epoch FROM evaluation_data_audits
WHERE id=$1 AND (claim_owner IS NULL OR claim_expires_at<=$2) FOR UPDATE`, auditID, now).
		Scan(&state, &baseline, &epoch)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", time.Time{}, 0, false, nil
	}
	return state, baseline, epoch, err == nil, err
}

func nextDataAuditTask(ctx context.Context, tx pgx.Tx, auditID string,
	baseline time.Time) (evaluationDataAuditTask, bool, error) {
	var task evaluationDataAuditTask
	var recorderID, manifestHash, manifestPath, exchangeID *string
	var revision *int64
	err := tx.QueryRow(ctx, `SELECT dataset.id,dataset.recorder_dataset_id,dataset.dataset_hash,
dataset.manifest_revision,dataset.manifest_path,
(SELECT CASE WHEN min(segment.exchange_id)=max(segment.exchange_id) THEN min(segment.exchange_id) END
 FROM dataset_segments member JOIN market_data_segments segment ON segment.id=member.segment_id
 WHERE member.dataset_id=dataset.id)
FROM dataset_manifests dataset WHERE dataset.created_at<$2 AND NOT EXISTS (
 SELECT 1 FROM evaluation_data_audit_findings finding WHERE finding.audit_id=$1 AND finding.dataset_id=dataset.id)
ORDER BY dataset.created_at,dataset.id LIMIT 1 FOR UPDATE OF dataset SKIP LOCKED`, auditID, baseline).
		Scan(&task.Expected.DatasetID, &recorderID, &manifestHash, &revision, &manifestPath, &exchangeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return evaluationDataAuditTask{}, false, nil
	}
	if err != nil {
		return evaluationDataAuditTask{}, false, err
	}
	applyDataAuditExpectation(&task.Expected, recorderID, manifestHash, revision, manifestPath, exchangeID)
	return task, true, nil
}

func applyDataAuditExpectation(expected *evaluation.RecordedDatasetExpectation,
	recorderID, manifestHash *string, revision *int64, manifestPath, exchangeID *string) {
	if recorderID != nil {
		expected.RecorderDatasetID = *recorderID
	}
	if manifestHash != nil {
		expected.ManifestHash = *manifestHash
	}
	if revision != nil && *revision > 0 {
		expected.ManifestRevision = uint64(*revision)
	}
	if manifestPath != nil {
		expected.ManifestPath = *manifestPath
	}
	if exchangeID != nil {
		expected.ExpectedExchange = *exchangeID
	}
}

func (coordinator *EvaluationDataAuditCoordinator) commitFinding(ctx context.Context,
	task evaluationDataAuditTask, finding evaluation.DataAuditFinding) error {
	if task.AuditID == "" || task.ClaimEpoch <= 0 || finding.DatasetID != task.Expected.DatasetID ||
		(finding.Eligibility != "eligible" && finding.Eligibility != "ineligible") || finding.ReasonCode == "" ||
		finding.SegmentCount > math.MaxInt64 || finding.ByteCount > math.MaxInt64 ||
		finding.GapCount > math.MaxInt64 || finding.DuplicateCount > math.MaxInt64 {
		return fmt.Errorf("evaluation_data_audit_finding_invalid")
	}
	now := coordinator.clock.Now().UTC
	tx, err := coordinator.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var epoch int64
	if err = tx.QueryRow(ctx, `SELECT claim_epoch FROM evaluation_data_audits WHERE id=$1
	  AND claim_owner=$2 AND claim_expires_at>$3 FOR UPDATE`, task.AuditID, coordinator.owner, now).
		Scan(&epoch); err != nil || epoch != task.ClaimEpoch {
		return fmt.Errorf("evaluation_data_audit_claim_conflict")
	}
	if err = insertDataAuditFinding(ctx, tx, task.AuditID, finding, now); err != nil {
		return err
	}
	if err = releaseDataAuditClaim(ctx, tx, task.AuditID, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func insertDataAuditFinding(ctx context.Context, tx pgx.Tx, auditID string,
	finding evaluation.DataAuditFinding, now time.Time) error {
	var ordinal int64
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM evaluation_data_audit_findings WHERE audit_id=$1`,
		auditID).Scan(&ordinal); err != nil {
		return err
	}
	manifestHash := any(nil)
	zeroHash := [sha256.Size]byte{}
	if finding.ManifestHash != zeroHash {
		manifestHash = finding.ManifestHash[:]
	}
	_, err := tx.Exec(ctx, `INSERT INTO evaluation_data_audit_findings(audit_id,ordinal,dataset_id,
	  exchange_id,instrument_id,eligibility,reason_code,manifest_hash,segment_count,byte_count,
	  gap_count,duplicate_count,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		auditID, ordinal, finding.DatasetID, nullableAuditText(finding.ExchangeID),
		nullableAuditText(finding.InstrumentID), finding.Eligibility, finding.ReasonCode, manifestHash,
		int64(finding.SegmentCount), int64(finding.ByteCount), int64(finding.GapCount),
		int64(finding.DuplicateCount), now)
	if err != nil {
		return err
	}
	return nil
}

func releaseDataAuditClaim(ctx context.Context, tx pgx.Tx, auditID string, now time.Time) error {
	var remaining bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM dataset_manifests dataset
	  JOIN evaluation_data_audits audit ON audit.id=$1 WHERE dataset.created_at<audit.baseline_at
	  AND NOT EXISTS (SELECT 1 FROM evaluation_data_audit_findings finding
	    WHERE finding.audit_id=audit.id AND finding.dataset_id=dataset.id))`, auditID).Scan(&remaining)
	if err != nil {
		return err
	}
	state := "RUNNING"
	var completedAt any
	if !remaining {
		state, completedAt = "COMPLETED", now
	}
	_, err = tx.Exec(ctx, `UPDATE evaluation_data_audits SET state=$2,completed_at=$3,updated_at=$4,
	  claim_owner=NULL,claim_expires_at=NULL WHERE id=$1`, auditID, state, completedAt, now)
	return err
}

func (coordinator *EvaluationDataAuditCoordinator) resolveRoot(
	expected evaluation.RecordedDatasetExpectation) (string, bool) {
	candidates := []string{coordinator.root}
	if expected.ExpectedExchange != "" {
		candidates = append(candidates, filepath.Join(coordinator.root, expected.ExpectedExchange))
	} else {
		candidates = append(candidates, filepath.Join(coordinator.root, "binance"),
			filepath.Join(coordinator.root, "bybit"))
	}
	matches := make([]string, 0, 1)
	for _, candidate := range candidates {
		path := filepath.Join(candidate, expected.ManifestPath)
		if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
			continue
		}
		manifest, err := recorder.ReadManifest(path)
		if err == nil && manifest.Hash == expected.ManifestHash && manifest.DatasetID == expected.RecorderDatasetID {
			matches = append(matches, candidate)
		}
	}
	if len(matches) == 1 {
		return matches[0], false
	}
	return coordinator.root, len(matches) > 1
}

func dataAuditCheckpoint(summary evaluationDataAuditSummary) []byte {
	return []byte(fmt.Sprintf(`{"total":%d,"completed":%d,"eligible":%d,"ineligible":%d}`,
		summary.Total, summary.Completed, summary.Eligible, summary.Ineligible))
}

func nullableAuditText(value string) any {
	if value == "" {
		return nil
	}
	return value
}
