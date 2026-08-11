package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"time"

	"axiom/internal/evaluation"

	"github.com/jackc/pgx/v5"
)

func ensureEvaluationFinalJobs(ctx context.Context, tx pgx.Tx, campaign evaluation.Campaign,
	locks []evaluationCandidateLock, owner string, now time.Time) (int, error) {
	expected := 0
	for _, lock := range locks {
		if lock.state == "SELECTED" {
			expected += 2
		}
	}
	var existing int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM evaluation_campaign_members
WHERE campaign_id=$1 AND repeat_ordinal=2 AND linked_job_id IS NOT NULL`, campaign.ID).Scan(&existing); err != nil {
		return 0, err
	}
	if existing == expected {
		return expected, nil
	}
	if existing != 0 {
		return 0, fmt.Errorf("evaluation_final_matrix_partial_initialization")
	}
	for _, lock := range locks {
		if lock.state != "SELECTED" {
			continue
		}
		candidate, ok := evaluationCandidateByKey(lock.configurationKey)
		if !ok || candidate.Strategy != lock.strategy {
			return 0, fmt.Errorf("evaluation_candidate_lock_configuration_invalid")
		}
		selection, err := loadStoredEvaluationDatasetSelection(ctx, tx, campaign.ID, lock.strategy)
		if err != nil {
			return 0, err
		}
		for _, planned := range evaluation.BalancedFinalRuns(candidate) {
			memberID := evaluationMemberID(planned.ID)
			_, err = tx.Exec(ctx, `INSERT INTO evaluation_campaign_members(campaign_id,id,strategy_id,
  configuration_key,mode,capital_micros,repeat_ordinal,cost_stress_bps,state,
  configuration_id,dataset_id,created_at,updated_at)
VALUES($1,$2,$3,$4,'replay',$5,2,$6,'PENDING',$7,$8,$9,$9)`, campaign.ID, memberID,
				string(lock.strategy), lock.configurationKey, planned.CapitalMicros, planned.CostStressBPS,
				lock.configurationID, lock.datasetID, now)
			if err != nil {
				return 0, err
			}
			if err = insertEvaluationJob(ctx, tx, campaign, planned, memberID, owner,
				lock.configurationID, selection, now); err != nil {
				return 0, err
			}
		}
	}
	return expected, nil
}

func loadStoredEvaluationDatasetSelection(ctx context.Context, tx pgx.Tx, campaignID string,
	strategy evaluation.Strategy) (evaluationDatasetSelection, error) {
	value := evaluationDatasetSelection{strategy: strategy}
	err := tx.QueryRow(ctx, `SELECT selected.dataset_id,selected.manifest_hash,selected.first_ordinal,
  selected.last_ordinal,selected.split_ordinal,min(manifest.coverage_start),max(manifest.coverage_end),
  min(member.evidence_role)
FROM evaluation_campaign_datasets selected
JOIN evaluation_campaign_dataset_members member ON member.campaign_id=selected.campaign_id
  AND member.strategy_id=selected.strategy_id
JOIN dataset_manifests manifest ON manifest.id=member.dataset_id
WHERE selected.campaign_id=$1 AND selected.strategy_id=$2
GROUP BY selected.dataset_id,selected.manifest_hash,selected.first_ordinal,
  selected.last_ordinal,selected.split_ordinal`, campaignID, string(strategy)).Scan(&value.primaryID,
		&value.manifestHash, &value.first, &value.last, &value.split, &value.coverageStart,
		&value.coverageEnd, &value.role)
	if err != nil || len(value.manifestHash) != sha256.Size || value.split < value.first || value.split >= value.last {
		return evaluationDatasetSelection{}, fmt.Errorf("evaluation_stored_dataset_selection_invalid")
	}
	rows, err := tx.Query(ctx, `SELECT dataset_id,evidence_role FROM evaluation_campaign_dataset_members
WHERE campaign_id=$1 AND strategy_id=$2 ORDER BY member_ordinal`, campaignID, string(strategy))
	if err != nil {
		return evaluationDatasetSelection{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, role string
		if err = rows.Scan(&id, &role); err != nil || role != value.role {
			return evaluationDatasetSelection{}, fmt.Errorf("evaluation_stored_dataset_member_invalid")
		}
		value.members = append(value.members, id)
	}
	if rows.Err() != nil || len(value.members) == 0 || value.members[0] != value.primaryID {
		return evaluationDatasetSelection{}, fmt.Errorf("evaluation_stored_dataset_members_invalid")
	}
	return value, nil
}

func recordEvaluationFinalConsumptions(ctx context.Context, tx pgx.Tx, campaignID string,
	now time.Time) error {
	rows, err := tx.Query(ctx, `SELECT id,research_generation_id,linked_run_id
FROM evaluation_campaign_members WHERE campaign_id=$1 AND repeat_ordinal=2
  AND linked_run_id IS NOT NULL ORDER BY id`, campaignID)
	if err != nil {
		return err
	}
	type consumption struct{ member, generation, run string }
	values := []consumption{}
	for rows.Next() {
		var value consumption
		if err = rows.Scan(&value.member, &value.generation, &value.run); err != nil {
			rows.Close()
			return err
		}
		values = append(values, value)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, value := range values {
		digest := sha256.Sum256([]byte("evaluation-final-test-v1:" + campaignID + ":" +
			value.member + ":" + value.generation + ":" + value.run))
		_, err = tx.Exec(ctx, `INSERT INTO experiment_final_test_consumptions(
  research_generation_id,consumed_by_run_id,consumption_hash,consumed_at)
VALUES($1,$2,$3,$4) ON CONFLICT (research_generation_id) DO NOTHING`, value.generation,
			value.run, hex.EncodeToString(digest[:]), now)
		if err != nil {
			return err
		}
		var storedRun, storedHash string
		if err = tx.QueryRow(ctx, `SELECT consumed_by_run_id,consumption_hash
FROM experiment_final_test_consumptions WHERE research_generation_id=$1`, value.generation).
			Scan(&storedRun, &storedHash); err != nil || storedRun != value.run ||
			storedHash != hex.EncodeToString(digest[:]) {
			return fmt.Errorf("evaluation_final_test_consumption_conflict")
		}
	}
	return nil
}

func evaluationCandidateByKey(key string) (evaluation.CandidateConfiguration, bool) {
	for _, candidate := range evaluation.BalancedFullDefinition() {
		if candidate.ConfigurationKey == key {
			return candidate, true
		}
	}
	return evaluation.CandidateConfiguration{}, false
}

func evaluationCandidateCapital(candidate evaluation.CandidateConfiguration) int64 {
	if candidate.OrderCapable {
		return evaluation.CombinedStrategyMicros
	}
	return evaluation.CombinedCapitalMicros
}

func finalEvaluationMembersUnfinished(ctx context.Context, tx pgx.Tx,
	campaignID string) (int, error) {
	var value int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM evaluation_campaign_members
WHERE campaign_id=$1 AND repeat_ordinal=2 AND state NOT IN ('SUCCEEDED','FAILED','CANCELED')`,
		campaignID).Scan(&value)
	return value, err
}

func evaluationBaseMembersUnfinished(ctx context.Context, tx pgx.Tx,
	campaignID string) (int, error) {
	var value int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM evaluation_campaign_members
WHERE campaign_id=$1 AND repeat_ordinal<2 AND state NOT IN ('SUCCEEDED','FAILED','CANCELED')`,
		campaignID).Scan(&value)
	return value, err
}

func allEvaluationDatasetsIncorrect(ctx context.Context, tx pgx.Tx, campaignID string) (bool, error) {
	allIncorrect := true
	for _, strategy := range []evaluation.Strategy{evaluation.StrategyTrend, evaluation.StrategyMean,
		evaluation.StrategyTriangular, evaluation.StrategyCross, evaluation.StrategyInventory} {
		correct, err := evaluationDatasetCorrect(ctx, tx, campaignID, strategy)
		if err != nil {
			return false, err
		}
		allIncorrect = allIncorrect && !correct
	}
	return allIncorrect, nil
}

func validateEvaluationFinalJobCount(expected, unfinished int) error {
	if expected < 0 || unfinished < 0 || unfinished > expected || expected > math.MaxInt16 {
		return errors.New("evaluation_final_matrix_summary_invalid")
	}
	return nil
}
