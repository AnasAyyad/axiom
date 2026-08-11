package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"axiom/internal/evaluation"

	"github.com/jackc/pgx/v5"
)

func queueEvaluationJob(ctx context.Context, tx pgx.Tx, campaignID, mode, memberID, owner,
	configurationID, datasetID, generationID, jobID string, request ownerConsoleOfflineRequest,
	now time.Time) error {
	payload, err := json.Marshal(request)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO jobs(id,job_type,idempotency_key,state,payload_hash,
	  created_at,updated_at,owner_user_id,request_payload,max_attempts)
VALUES($1,$2,$3,'QUEUED',$4,$5,$5,$6,$7,3)`, jobID, mode,
		"evaluation:"+campaignID+":"+memberID, ownerConsoleSHA256(payload), now, owner, payload); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE evaluation_campaign_members SET state='QUEUED',
  configuration_id=$3,dataset_id=$4,research_generation_id=$5,linked_job_id=$6,updated_at=$7
WHERE campaign_id=$1 AND id=$2 AND state='PENDING' AND linked_job_id IS NULL`, campaignID,
		memberID, configurationID, datasetID, generationID, jobID, now)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("evaluation_member_job_link_conflict")
	}
	return nil
}

func evaluationRunWindow(selection evaluationDatasetSelection,
	planned evaluation.PlannedRun) (int64, int64, error) {
	if selection.first <= 0 || selection.split < selection.first || selection.split >= selection.last {
		return 0, 0, fmt.Errorf("evaluation_run_window_invalid")
	}
	if planned.RepeatOrdinal == 2 {
		return selection.split + 1, selection.last, nil
	}
	if planned.RepeatOrdinal < 0 || planned.RepeatOrdinal > 1 {
		return 0, 0, fmt.Errorf("evaluation_run_window_invalid")
	}
	return selection.first, selection.split, nil
}

func evaluationTimeSplits(start, end time.Time) (time.Time, time.Time) {
	span := end.Sub(start)
	trainEnd := start.Add(span * 60 / 100).UTC()
	validationEnd := start.Add(span * 80 / 100).UTC()
	return trainEnd, validationEnd
}

func syncEvaluationMembers(ctx context.Context, tx pgx.Tx, campaignID, mode string, now time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE evaluation_campaign_members member SET
  state=CASE job.state WHEN 'QUEUED' THEN 'QUEUED' WHEN 'RUNNING' THEN 'RUNNING'
    WHEN 'PAUSE_REQUESTED' THEN 'RUNNING' WHEN 'PAUSED' THEN 'RUNNING'
    WHEN 'SUCCEEDED' THEN 'SUCCEEDED' WHEN 'FAILED' THEN 'FAILED'
    WHEN 'CANCELED' THEN 'CANCELED' WHEN 'CANCEL_REQUESTED' THEN 'CANCELED' ELSE member.state END,
  linked_run_id=job.run_id,
  result_hash=CASE WHEN result.result_hash IS NULL THEN member.result_hash ELSE decode(result.result_hash,'hex') END,
  metrics_payload=CASE WHEN result.canonical_payload IS NULL THEN member.metrics_payload ELSE
    convert_to((convert_from(result.canonical_payload,'UTF8')::jsonb->'metrics')::text,'UTF8') END,
  reason_code=CASE WHEN job.state='FAILED' THEN job.failure_code ELSE member.reason_code END,
  updated_at=$3
FROM jobs job LEFT JOIN run_results result ON result.run_id=job.run_id
WHERE member.campaign_id=$1 AND member.mode=$2 AND member.linked_job_id=job.id
  AND (member.state IS DISTINCT FROM CASE job.state WHEN 'QUEUED' THEN 'QUEUED'
    WHEN 'RUNNING' THEN 'RUNNING' WHEN 'PAUSE_REQUESTED' THEN 'RUNNING' WHEN 'PAUSED' THEN 'RUNNING'
    WHEN 'SUCCEEDED' THEN 'SUCCEEDED' WHEN 'FAILED' THEN 'FAILED'
    WHEN 'CANCELED' THEN 'CANCELED' WHEN 'CANCEL_REQUESTED' THEN 'CANCELED' ELSE member.state END
    OR (member.result_hash IS NULL AND result.result_hash IS NOT NULL))`, campaignID, mode, now)
	return err
}

func readEvaluationMatrixSummary(ctx context.Context, tx pgx.Tx, campaignID,
	mode string) (evaluationMatrixSummary, error) {
	var value evaluationMatrixSummary
	err := tx.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE state='QUEUED'),
  count(*) FILTER (WHERE state='RUNNING'),count(*) FILTER (WHERE state='SUCCEEDED'),
  count(*) FILTER (WHERE state='FAILED'),count(*) FILTER (WHERE state='CANCELED')
FROM evaluation_campaign_members WHERE campaign_id=$1 AND mode=$2`, campaignID, mode).
		Scan(&value.total, &value.queued, &value.running, &value.succeeded, &value.failed, &value.canceled)
	return value, err
}

func (driver *EvaluationCampaignDriver) selectEvaluationCandidates(ctx context.Context,
	campaign evaluation.Campaign) (evaluation.StageProgress, error) {
	tx, err := driver.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return evaluation.StageProgress{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	locks, finalCount, waitProgress, waiting, err := driver.prepareEvaluationFinalMatrix(ctx, tx, campaign)
	if err != nil {
		return evaluation.StageProgress{}, err
	}
	if waiting {
		return waitProgress, tx.Commit(ctx)
	}
	if err = recordEvaluationFinalConsumptions(ctx, tx, campaign.ID, driver.clock.Now().UTC); err != nil {
		return evaluation.StageProgress{}, err
	}
	stressResults, stressPassed, err := ensureEvaluationStressResults(ctx, tx, campaign.ID, driver.clock.Now().UTC)
	if err != nil {
		return evaluation.StageProgress{}, err
	}
	sharedDatasetFailure, err := allEvaluationDatasetsIncorrect(ctx, tx, campaign.ID)
	if err != nil {
		return evaluation.StageProgress{}, err
	}
	verdictCounts, err := driver.evaluateLockedCandidates(ctx, tx, campaign.ID, locks, stressPassed)
	if err != nil {
		return evaluation.StageProgress{}, err
	}
	if err = insertSelectedShadowMembers(ctx, tx, campaign.ID, driver.clock.Now().UTC); err != nil {
		return evaluation.StageProgress{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return evaluation.StageProgress{}, err
	}
	return evaluationSelectionProgress(campaign.ID, locks, finalCount, verdictCounts, stressResults,
		sharedDatasetFailure, stressPassed), nil
}

func (driver *EvaluationCampaignDriver) prepareEvaluationFinalMatrix(ctx context.Context, tx pgx.Tx,
	campaign evaluation.Campaign) ([]evaluationCandidateLock, int, evaluation.StageProgress, bool, error) {
	now := driver.clock.Now().UTC
	if err := syncEvaluationMembers(ctx, tx, campaign.ID, "backtest", now); err != nil {
		return nil, 0, evaluation.StageProgress{}, false, err
	}
	if err := syncEvaluationMembers(ctx, tx, campaign.ID, "replay", now); err != nil {
		return nil, 0, evaluation.StageProgress{}, false, err
	}
	unfinished, err := evaluationBaseMembersUnfinished(ctx, tx, campaign.ID)
	if err != nil {
		return nil, 0, evaluation.StageProgress{}, false, err
	}
	if unfinished > 0 {
		progress := evaluation.StageProgress{State: evaluation.ProgressWaiting,
			Summary: "Candidate selection is waiting for all durable matrix members."}
		return nil, 0, progress, true, nil
	}
	locks, err := ensureEvaluationCandidateLocks(ctx, tx, campaign.ID, now)
	if err != nil {
		return nil, 0, evaluation.StageProgress{}, false, err
	}
	owner, err := evaluationCampaignOwner(ctx, tx, campaign.ID)
	if err != nil {
		return nil, 0, evaluation.StageProgress{}, false, err
	}
	finalCount, err := ensureEvaluationFinalJobs(ctx, tx, campaign, locks, owner, now)
	if err != nil {
		return nil, 0, evaluation.StageProgress{}, false, err
	}
	if err = syncEvaluationMembers(ctx, tx, campaign.ID, "replay", now); err != nil {
		return nil, 0, evaluation.StageProgress{}, false, err
	}
	unfinished, err = finalEvaluationMembersUnfinished(ctx, tx, campaign.ID)
	if err != nil || validateEvaluationFinalJobCount(finalCount, unfinished) != nil {
		return nil, 0, evaluation.StageProgress{}, false, fmt.Errorf("evaluation_final_matrix_summary_invalid")
	}
	if unfinished == 0 {
		return locks, finalCount, evaluation.StageProgress{}, false, nil
	}
	checkpoint, _ := json.Marshal(map[string]any{"locked_strategies": len(locks),
		"final_test_total": finalCount, "final_test_unfinished": unfinished})
	progress := evaluation.StageProgress{State: evaluation.ProgressWaiting,
		Summary:    "Configurations are locked; only their final 20 percent is now being evaluated.",
		Checkpoint: checkpoint, LinkedResourceType: "final_test_matrix", LinkedResourceID: campaign.ID + ":final-test"}
	return locks, finalCount, progress, true, nil
}

func (driver *EvaluationCampaignDriver) evaluateLockedCandidates(ctx context.Context, tx pgx.Tx,
	campaignID string, locks []evaluationCandidateLock, stressPassed bool) (map[evaluation.Verdict]int, error) {
	counts := map[evaluation.Verdict]int{}
	for _, lock := range locks {
		if lock.state == "BLOCKED" {
			counts[evaluation.VerdictBlocked]++
			if _, err := tx.Exec(ctx, `UPDATE evaluation_campaign_members SET verdict='BLOCKED',
reason_code=$3,updated_at=$4 WHERE campaign_id=$1 AND strategy_id=$2`, campaignID,
				string(lock.strategy), lock.reason, driver.clock.Now().UTC); err != nil {
				return nil, err
			}
			continue
		}
		candidate, ok := evaluationCandidateByKey(lock.configurationKey)
		if !ok || candidate.Strategy != lock.strategy {
			return nil, fmt.Errorf("evaluation_candidate_lock_configuration_invalid")
		}
		metrics, err := evaluationCandidateEvidence(ctx, tx, campaignID, candidate, stressPassed)
		if err != nil {
			return nil, err
		}
		verdict, reason := evaluation.EvaluateCandidate(candidate.Strategy, metrics,
			evaluation.BalancedSelectionPolicy())
		counts[verdict]++
		if err = updateEvaluationCandidateVerdicts(ctx, tx, campaignID, candidate, verdict, reason,
			driver.clock.Now().UTC); err != nil {
			return nil, err
		}
	}
	return counts, nil
}

func updateEvaluationCandidateVerdicts(ctx context.Context, tx pgx.Tx, campaignID string,
	candidate evaluation.CandidateConfiguration, verdict evaluation.Verdict, reason evaluation.ReasonCode,
	now time.Time) error {
	if _, err := tx.Exec(ctx, `UPDATE evaluation_campaign_members SET verdict=$4,reason_code=$5,
updated_at=$6 WHERE campaign_id=$1 AND strategy_id=$2 AND configuration_key=$3`, campaignID,
		string(candidate.Strategy), candidate.ConfigurationKey, string(verdict), string(reason), now); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE evaluation_campaign_members SET verdict='IMPROVE',
reason_code='NOT_SELECTED_ON_VALIDATION',updated_at=$4
WHERE campaign_id=$1 AND strategy_id=$2 AND configuration_key<>$3`, campaignID,
		string(candidate.Strategy), candidate.ConfigurationKey, now)
	return err
}

func evaluationSelectionProgress(_ string, locks []evaluationCandidateLock, finalCount int,
	verdictCounts map[evaluation.Verdict]int, stressResults []evaluation.FocusedStressResult,
	sharedDatasetFailure, stressPassed bool) evaluation.StageProgress {
	checkpoint, _ := json.Marshal(map[string]any{"candidate_lock_count": len(locks),
		"final_test_count": finalCount, "verdicts": verdictCounts, "focused_stress": stressResults})
	if sharedDatasetFailure {
		return evaluation.StageProgress{State: evaluation.ProgressBlock, Reason: evaluation.ReasonDataCorrupt,
			Summary: "Every candidate failed the shared immutable dataset correctness gate.", Checkpoint: checkpoint}
	}
	if !stressPassed {
		return evaluation.StageProgress{State: evaluation.ProgressBlock, Reason: evaluation.ReasonSafetyFailed,
			Summary:    "A shared credential-free fault scenario failed; shadow remains blocked with preserved evidence.",
			Checkpoint: checkpoint}
	}
	return evaluation.StageProgress{State: evaluation.ProgressComplete,
		Summary:    "Locked candidate verdicts were derived from untouched final-window ledgers, deterministic repeats, and the focused fault suite.",
		Checkpoint: checkpoint}
}
