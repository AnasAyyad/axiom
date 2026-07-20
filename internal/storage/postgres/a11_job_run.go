package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/research"

	"github.com/jackc/pgx/v5"
)

func (store *A11JobStore) attachRun(ctx context.Context, kind string, payload json.RawMessage, claim backtest.JobClaim) error {
	request, err := decodeA11OfflineRequest(kind, payload)
	if err != nil || claim.Manifest.RunID.Value() != claim.ID || claim.Manifest.Mode != kind ||
		claim.Manifest.Seed != request.RootSeedHash || claim.Manifest.ResearchGenerationID != request.ResearchGenerationID {
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
	if err = tx.QueryRow(ctx, `SELECT coalesce(run_id,''),claim_expires_at FROM jobs WHERE id=$1 AND state IN ('RUNNING','PAUSE_REQUESTED')
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
	  progress_revision=progress_revision+1 WHERE id=$1 AND state IN ('RUNNING','PAUSE_REQUESTED') AND claim_owner=$4`,
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
	canonical []byte, now time.Time) error {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var runID, kind string
	var requestPayload []byte
	if err = tx.QueryRow(ctx, `SELECT run_id,job_type,request_payload FROM jobs WHERE id=$1 AND state='RUNNING' AND claim_owner=$2
	  FOR UPDATE`, id, store.owner).Scan(&runID, &kind, &requestPayload); err != nil || runID == "" {
		return fmt.Errorf("a11_job_run_missing")
	}
	request, err := decodeA11OfflineRequest(kind, requestPayload)
	if err != nil || request.ResearchGenerationID == "" || result.ManifestHash == "" {
		return fmt.Errorf("a11_job_report_identity_invalid")
	}
	reportID, reportHash, err := insertA11RunEvidenceReport(ctx, tx, id, runID, kind, request, result, now)
	if err != nil {
		return err
	}
	projection, err := json.Marshal(a11JobResult(result, reportID, reportHash))
	if err != nil {
		return fmt.Errorf("a11_job_result_invalid")
	}
	if err = insertA11CanonicalOutputs(ctx, tx, runID, result.Events); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO run_results(run_id,result_hash,canonical_payload,completed_at)
	  VALUES($1,$2,$3,$4)`, runID, result.ResultHash, canonical, now); err != nil {
		return err
	}
	if err = finishA11RunAndJob(ctx, tx, id, runID, store.owner, projection, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func insertA11RunEvidenceReport(ctx context.Context, tx pgx.Tx, id, runID, kind string, request a11OfflineRequest,
	result backtest.CanonicalResult, now time.Time) (string, string, error) {
	metricValues := a11JobMetrics(result)
	reportMetrics := make(map[string]string, len(metricValues))
	for key, value := range metricValues {
		reportMetrics[key] = string(value)
	}
	report, reportCanonical, err := research.BuildRunEvidenceReport(research.RunEvidenceInput{
		ResearchGenerationID: request.ResearchGenerationID, RunID: runID, Mode: kind,
		ResultHash: result.ResultHash, ReproducibilityHash: result.ManifestHash,
		Metrics: reportMetrics, CreatedAt: now,
	})
	if err != nil {
		return "", "", err
	}
	reportID := "report-" + id
	runReferences, _ := json.Marshal([]string{runID})
	if _, err = tx.Exec(ctx, `INSERT INTO research_reports(id,research_generation_id,manifest_hash,artifact_hash,
	  canonical_manifest,run_references,confidence_label,platform_correctness,strategy_evidence,
	  viability_disposition,disclaimer_policy,created_at) VALUES($1,$2,$3,$4,$5,$6,'insufficient',
	  'canonical_pipeline_completed','single_registered_run_only','undetermined',
	  'no_production_profitability_claim',$7)`, reportID, request.ResearchGenerationID, report.ManifestHash,
		result.ResultHash, reportCanonical, runReferences, now); err != nil {
		return "", "", err
	}
	return reportID, report.ManifestHash, nil
}

func finishA11RunAndJob(ctx context.Context, tx pgx.Tx, id, runID, owner string, projection []byte, now time.Time) error {
	runTag, updateErr := tx.Exec(ctx, `UPDATE runs SET state='completed',completed_at=$2 WHERE id=$1 AND state='running'`, runID, now)
	if updateErr != nil || runTag.RowsAffected() != 1 {
		return fmt.Errorf("a11_run_completion_conflict")
	}
	tag, err := tx.Exec(ctx, `UPDATE jobs SET state='SUCCEEDED',claim_owner=NULL,claim_expires_at=NULL,
	  result_payload=$1,failure_code=NULL,completed_at=$2,updated_at=$2,progress_revision=progress_revision+1
	  WHERE id=$3 AND state='RUNNING' AND claim_owner=$4`, projection, now, id, owner)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("a11_job_completion_conflict")
	}
	return nil
}

func insertA11CanonicalOutputs(ctx context.Context, tx pgx.Tx, runID string, events []backtest.EventResult) error {
	for _, event := range events {
		if event.Ordinal == 0 || event.Ordinal > uint64(^uint64(0)>>1) || !json.Valid(event.Decision) || !json.Valid(event.Orders) ||
			!json.Valid(event.Balances) || (len(event.ExecutionEvents) > 0 && !json.Valid(event.ExecutionEvents)) {
			return fmt.Errorf("a11_run_output_invalid")
		}
		executionEvents := event.ExecutionEvents
		if len(executionEvents) == 0 {
			executionEvents = json.RawMessage("[]")
		}
		canonical, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("a11_run_output_invalid")
		}
		outputs := []struct {
			kind    string
			payload []byte
		}{
			{kind: "event", payload: canonical},
			{kind: "decision", payload: event.Decision},
			{kind: "order", payload: event.Orders},
			{kind: "projection", payload: executionEvents},
			{kind: "balance", payload: event.Balances},
		}
		for _, output := range outputs {
			hash := a11SHA256(output.payload)
			tag, insertErr := tx.Exec(ctx, `INSERT INTO run_canonical_outputs
			  (run_id,output_kind,ordinal,output_hash,canonical_payload) VALUES($1,$2,$3,$4,$5)
			  ON CONFLICT (run_id,output_kind,ordinal) DO NOTHING`, runID, output.kind, int64(event.Ordinal), hash, output.payload)
			if insertErr != nil {
				return fmt.Errorf("a11_run_output_persist_failed")
			}
			if tag.RowsAffected() == 0 {
				var storedHash string
				var storedPayload []byte
				if err = tx.QueryRow(ctx, `SELECT output_hash::text,canonical_payload FROM run_canonical_outputs
				  WHERE run_id=$1 AND output_kind=$2 AND ordinal=$3`, runID, output.kind, int64(event.Ordinal)).
					Scan(&storedHash, &storedPayload); err != nil || storedHash != hash || !bytes.Equal(storedPayload, output.payload) {
					return fmt.Errorf("a11_run_output_conflict")
				}
			}
		}
	}
	return nil
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
