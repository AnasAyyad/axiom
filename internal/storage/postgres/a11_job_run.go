package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"axiom/internal/backtest"

	"github.com/jackc/pgx/v5"
)

func (store *A11JobStore) attachRun(ctx context.Context, kind string, payload json.RawMessage, claim backtest.JobClaim) error {
	request, err := decodeA11OfflineRequest(kind, payload)
	if err != nil || claim.Manifest.RunID.Value() != claim.ID || claim.Manifest.Mode != kind ||
		claim.Manifest.Seed != request.RootSeedHash {
		return fmt.Errorf("a11_job_run_identity_invalid")
	}
	manifestHash, err := claim.Manifest.CanonicalHash()
	if err != nil {
		return err
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var linked string
	var leaseEnd time.Time
	if err = tx.QueryRow(ctx, `SELECT coalesce(run_id,''),claim_expires_at FROM jobs WHERE id=$1 AND state='RUNNING'
	      AND claim_owner=$2 FOR UPDATE`, claim.ID, store.owner).Scan(&linked, &leaseEnd); err != nil {
		return fmt.Errorf("a11_job_lease_lost")
	}
	if linked != "" {
		if err = verifyA11LinkedRun(ctx, tx, linked, manifestHash); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE runs SET state='running' WHERE id=$1 AND state='paused'`, linked); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	now := store.clock.Now().UTC
	if err = insertA11Run(ctx, tx, request, claim.Manifest, manifestHash, now); err != nil {
		return err
	}
	renewedLeaseEnd := renewedA11JobLease(now, leaseEnd)
	tag, err := tx.Exec(ctx, `UPDATE jobs SET run_id=$1,claim_expires_at=$2,updated_at=$3,
      progress_revision=progress_revision+1 WHERE id=$1 AND state='RUNNING' AND claim_owner=$4`,
		claim.ID, renewedLeaseEnd, now, store.owner)
	if err != nil {
		return fmt.Errorf("a11_job_run_link_failed: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("a11_job_run_link_conflict")
	}
	return tx.Commit(ctx)
}

func renewedA11JobLease(now, leaseEnd time.Time) time.Time {
	renewed := now.Add(a11JobLease)
	if renewed.After(leaseEnd) {
		return renewed
	}
	// PostgreSQL timestamps are microsecond-precision, so the minimum
	// monotonic renewal must survive its wire/storage round trip.
	return leaseEnd.Add(time.Microsecond)
}

func insertA11Run(ctx context.Context, tx pgx.Tx, request a11OfflineRequest, manifest backtest.RunManifest,
	manifestHash string, now time.Time) error {
	strategyID := a11StrategyVersionID(request.StrategyVersion)
	_, err := tx.Exec(ctx, `INSERT INTO runs(id,mode,configuration_id,strategy_version_id,dataset_id,root_seed_hash,
      reproducibility_hash,state,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,'created',$8)`, manifest.RunID.Value(),
		manifest.Mode, request.ConfigurationID, strategyID, request.DatasetID, request.RootSeedHash, manifestHash, now)
	if err != nil {
		return err
	}
	canonical, _ := json.Marshal(manifest)
	flags, _ := json.Marshal(manifest.Build.BuildFlags)
	segments, _ := json.Marshal(manifest.Dataset.SegmentHashes)
	_, err = tx.Exec(ctx, `INSERT INTO run_manifests(run_id,manifest_hash,code_commit,go_version,architecture,
      operating_system,build_flags_hash,go_sum_hash,pnpm_lock_hash,dataset_manifest_hash,dataset_revision,
      source_commit,schema_version,parser_version,normalization_version,segment_hashes_hash,configuration_hash,
      scheduler_version,serialization_version,model_namespace_id,starting_balance_hash,confidence_tier,
      canonical_payload,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,
      $19,$20,$21,$22,$23,$24)`, manifest.RunID.Value(), manifestHash, manifest.CodeCommit, manifest.Build.GoVersion,
		manifest.Build.Architecture, manifest.Build.OperatingSystem, a11SHA256(flags), manifest.Build.GoSumHash,
		manifest.Build.PNPMLockHash, manifest.Dataset.ManifestHash, manifest.Dataset.Revision,
		manifest.Dataset.SourceCommit, manifest.Dataset.SchemaVersion, manifest.Dataset.ParserVersion,
		manifest.Dataset.NormalizationVersion, a11SHA256(segments), manifest.ConfigurationHash,
		manifest.SchedulerVersion, manifest.SerializationVersion, manifest.Models.ID, manifest.StartingBalanceHash,
		manifest.Dataset.Confidence, canonical, now)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE runs SET state='running',started_at=$2 WHERE id=$1`, manifest.RunID.Value(), now)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("a11_run_start_conflict")
	}
	return nil
}

func verifyA11LinkedRun(ctx context.Context, tx pgx.Tx, runID, manifestHash string) error {
	var stored string
	if err := tx.QueryRow(ctx, `SELECT manifest_hash FROM run_manifests WHERE run_id=$1`, runID).Scan(&stored); err != nil || stored != manifestHash {
		return fmt.Errorf("a11_job_run_manifest_conflict")
	}
	return nil
}

func (store *A11JobStore) completeRun(ctx context.Context, id string, result backtest.CanonicalResult,
	canonical, projection []byte, now time.Time) error {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var runID string
	if err = tx.QueryRow(ctx, `SELECT run_id FROM jobs WHERE id=$1 AND state='RUNNING' AND claim_owner=$2
	  FOR UPDATE`, id, store.owner).Scan(&runID); err != nil || runID == "" {
		return fmt.Errorf("a11_job_run_missing")
	}
	if _, err = tx.Exec(ctx, `INSERT INTO run_results(run_id,result_hash,canonical_payload,completed_at)
	  VALUES($1,$2,$3,$4)`, runID, result.ResultHash, canonical, now); err != nil {
		return err
	}
	runTag, updateErr := tx.Exec(ctx, `UPDATE runs SET state='completed',completed_at=$2 WHERE id=$1 AND state='running'`, runID, now)
	if updateErr != nil || runTag.RowsAffected() != 1 {
		return fmt.Errorf("a11_run_completion_conflict")
	}
	tag, err := tx.Exec(ctx, `UPDATE jobs SET state='SUCCEEDED',claim_owner=NULL,claim_expires_at=NULL,
	  result_payload=$1,failure_code=NULL,completed_at=$2,updated_at=$2,progress_revision=progress_revision+1
	  WHERE id=$3 AND state='RUNNING' AND claim_owner=$4`, projection, now, id, store.owner)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("a11_job_completion_conflict")
	}
	return tx.Commit(ctx)
}

func (store *A11JobStore) failRun(ctx context.Context, id, reason string, now time.Time) error {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var runID string
	if err = tx.QueryRow(ctx, `SELECT coalesce(run_id,'') FROM jobs WHERE id=$1 AND state IN ('RUNNING','PAUSE_REQUESTED')
	  AND claim_owner=$2 FOR UPDATE`, id, store.owner).Scan(&runID); err != nil {
		return err
	}
	if runID != "" {
		runTag, updateErr := tx.Exec(ctx, `UPDATE runs SET state='failed',completed_at=$2 WHERE id=$1 AND state='running'`, runID, now)
		if updateErr != nil || runTag.RowsAffected() != 1 {
			return fmt.Errorf("a11_run_failure_conflict")
		}
	}
	tag, err := tx.Exec(ctx, `UPDATE jobs SET state='FAILED',claim_owner=NULL,claim_expires_at=NULL,
	  failure_code=$1,completed_at=$2,updated_at=$2,progress_revision=progress_revision+1
	  WHERE id=$3 AND state IN ('RUNNING','PAUSE_REQUESTED') AND claim_owner=$4`, reason, now, id, store.owner)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("a11_job_failure_conflict")
	}
	return tx.Commit(ctx)
}
