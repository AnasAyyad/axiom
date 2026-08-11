package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/evaluation"

	"github.com/jackc/pgx/v5"
)

const evaluationValidationIncomplete = "VALIDATION_EVIDENCE_INCOMPLETE"

type evaluationCandidateLock struct {
	strategy         evaluation.Strategy
	state            string
	configurationKey string
	configurationID  string
	datasetID        string
	validationHash   []byte
	reason           string
}

type evaluationValidationCandidate struct {
	candidate       evaluation.CandidateConfiguration
	configurationID string
	datasetID       string
	scoreMicros     int64
	evidenceHash    [sha256.Size]byte
}

type evaluationRequestedResult struct {
	mode   string
	repeat int16
	stress int32
}

// ensureEvaluationCandidateLocks performs selection using only first-80-percent
// matrix results. The immutable lock rows are committed in the same transaction
// that precedes creation of any final-window job.
func ensureEvaluationCandidateLocks(ctx context.Context, tx pgx.Tx, campaignID string,
	now time.Time) ([]evaluationCandidateLock, error) {
	locks, err := loadEvaluationCandidateLocks(ctx, tx, campaignID)
	if err != nil {
		return nil, err
	}
	if len(locks) == 5 {
		return locks, nil
	}
	if len(locks) != 0 {
		return nil, fmt.Errorf("evaluation_candidate_lock_partial")
	}
	for _, strategy := range []evaluation.Strategy{evaluation.StrategyTrend, evaluation.StrategyMean,
		evaluation.StrategyTriangular, evaluation.StrategyCross, evaluation.StrategyInventory} {
		var best *evaluationValidationCandidate
		for _, candidate := range evaluation.BalancedFullDefinition() {
			if candidate.Strategy != strategy {
				continue
			}
			value, eligible, candidateErr := evaluationValidationEvidence(ctx, tx, campaignID, candidate)
			if candidateErr != nil {
				return nil, candidateErr
			}
			if !eligible || (best != nil && (value.scoreMicros < best.scoreMicros ||
				(value.scoreMicros == best.scoreMicros && value.candidate.ConfigurationKey >
					best.candidate.ConfigurationKey))) {
				continue
			}
			copy := value
			best = &copy
		}
		if best == nil {
			if _, err = tx.Exec(ctx, `INSERT INTO evaluation_campaign_candidate_locks(
  campaign_id,strategy_id,state,reason_code,locked_at) VALUES($1,$2,'BLOCKED',$3,$4)`,
				campaignID, string(strategy), evaluationValidationIncomplete, now); err != nil {
				return nil, err
			}
			continue
		}
		if _, err = tx.Exec(ctx, `INSERT INTO evaluation_campaign_candidate_locks(
  campaign_id,strategy_id,state,configuration_key,configuration_id,dataset_id,
  validation_result_hash,locked_at) VALUES($1,$2,'SELECTED',$3,$4,$5,$6,$7)`,
			campaignID, string(strategy), best.candidate.ConfigurationKey, best.configurationID,
			best.datasetID, best.evidenceHash[:], now); err != nil {
			return nil, err
		}
	}
	return loadEvaluationCandidateLocks(ctx, tx, campaignID)
}

func loadEvaluationCandidateLocks(ctx context.Context, tx pgx.Tx,
	campaignID string) ([]evaluationCandidateLock, error) {
	rows, err := tx.Query(ctx, `SELECT strategy_id,state,COALESCE(configuration_key,''),
  COALESCE(configuration_id,''),COALESCE(dataset_id,''),
  COALESCE(validation_result_hash,''::bytea),COALESCE(reason_code,'')
FROM evaluation_campaign_candidate_locks WHERE campaign_id=$1 ORDER BY strategy_id`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]evaluationCandidateLock, 0, 5)
	for rows.Next() {
		var value evaluationCandidateLock
		if err = rows.Scan(&value.strategy, &value.state, &value.configurationKey,
			&value.configurationID, &value.datasetID, &value.validationHash, &value.reason); err != nil {
			return nil, err
		}
		if (value.state == "SELECTED" && (value.configurationKey == "" || value.configurationID == "" ||
			value.datasetID == "" || len(value.validationHash) != sha256.Size || value.reason != "")) ||
			(value.state == "BLOCKED" && value.reason == "") {
			return nil, fmt.Errorf("evaluation_candidate_lock_invalid")
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func evaluationValidationEvidence(ctx context.Context, tx pgx.Tx, campaignID string,
	candidate evaluation.CandidateConfiguration) (evaluationValidationCandidate, bool, error) {
	results, complete, err := loadEvaluationValidationResults(ctx, tx, campaignID, candidate)
	if err != nil || !complete {
		return evaluationValidationCandidate{}, false, err
	}
	valid, err := evaluationCandidateChecks(ctx, tx, campaignID, candidate, results)
	if err != nil || !valid {
		return evaluationValidationCandidate{}, false, err
	}
	var configurationID, datasetID string
	err = tx.QueryRow(ctx, `SELECT configuration_id,dataset_id FROM evaluation_campaign_members
WHERE campaign_id=$1 AND strategy_id=$2 AND configuration_key=$3 AND mode='replay'
  AND repeat_ordinal=0 AND cost_stress_bps=10000 AND state='SUCCEEDED'`, campaignID,
		string(candidate.Strategy), candidate.ConfigurationKey).Scan(&configurationID, &datasetID)
	if err != nil {
		return evaluationValidationCandidate{}, false, err
	}
	evidenceHash, err := evaluationValidationHash(campaignID, candidate, results)
	if err != nil {
		return evaluationValidationCandidate{}, false, err
	}
	return evaluationValidationCandidate{candidate: candidate, configurationID: configurationID,
		datasetID: datasetID, scoreMicros: metricMicros(results[4].Metrics.TotalNetReturn,
			evaluationCandidateCapital(candidate)), evidenceHash: evidenceHash}, true, nil
}

func loadEvaluationValidationResults(ctx context.Context, tx pgx.Tx, campaignID string,
	candidate evaluation.CandidateConfiguration) ([]backtest.CanonicalResult, bool, error) {
	wanted := []evaluationRequestedResult{{"backtest", 0, 10_000}, {"backtest", 1, 10_000},
		{"backtest", 0, 15_000}, {"backtest", 0, 20_000}, {"replay", 0, 10_000},
		{"replay", 1, 10_000}, {"replay", 0, 15_000}, {"replay", 0, 20_000}}
	results := make([]backtest.CanonicalResult, 0, len(wanted))
	for _, request := range wanted {
		result, ok, err := loadEvaluationResult(ctx, tx, campaignID, candidate, request.mode,
			request.repeat, request.stress)
		if err != nil {
			return nil, false, err
		}
		if !ok {
			return nil, false, nil
		}
		results = append(results, result)
	}
	return results, true, nil
}

func evaluationCandidateChecks(ctx context.Context, tx pgx.Tx, campaignID string,
	candidate evaluation.CandidateConfiguration, results []backtest.CanonicalResult) (bool, error) {
	datasetCorrect, err := evaluationDatasetCorrect(ctx, tx, campaignID, candidate.Strategy)
	if err != nil {
		return false, err
	}
	capacityComplete, err := evaluationCapacityEvidenceComplete(ctx, tx, campaignID, candidate.Strategy)
	if err != nil {
		return false, err
	}
	accounting := evaluationMetricFlagEvery(results, "accounting_reconciled", true) &&
		evaluationMetricFlagEvery(results, "negative_inventory_count", false) &&
		evaluationMetricFlagEvery(results, "duplicate_fill_count", false) &&
		evaluationMetricFlagEvery(results, "unsupported_sale_count", false)
	deterministic := evaluationResultsEqual(results[0], results[1]) &&
		evaluationResultsEqual(results[4], results[5])
	return datasetCorrect && capacityComplete && accounting && deterministic, nil
}

func evaluationValidationHash(campaignID string, candidate evaluation.CandidateConfiguration,
	results []backtest.CanonicalResult) ([sha256.Size]byte, error) {
	hashInput := struct {
		CampaignID       string   `json:"campaign_id"`
		Strategy         string   `json:"strategy"`
		ConfigurationKey string   `json:"configuration_key"`
		ResultHashes     []string `json:"result_hashes"`
		CapacityComplete bool     `json:"capacity_complete"`
	}{CampaignID: campaignID, Strategy: string(candidate.Strategy),
		ConfigurationKey: candidate.ConfigurationKey, CapacityComplete: true}
	for _, result := range results {
		hashInput.ResultHashes = append(hashInput.ResultHashes, result.ResultHash)
	}
	canonical, err := json.Marshal(hashInput)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(canonical), nil
}

func evaluationCapacityEvidenceComplete(ctx context.Context, tx pgx.Tx, campaignID string,
	strategy evaluation.Strategy) (bool, error) {
	if strategy == evaluation.StrategyInventory {
		return true, nil
	}
	var count int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM evaluation_campaign_members
WHERE campaign_id=$1 AND strategy_id=$2 AND repeat_ordinal=0 AND cost_stress_bps=10000
  AND capital_micros IN (500000000,1000000000,1500000000) AND mode IN ('backtest','replay')
  AND state='SUCCEEDED'`, campaignID, string(strategy)).Scan(&count)
	return count == 6, err
}
