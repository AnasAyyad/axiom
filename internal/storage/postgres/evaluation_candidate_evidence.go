package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/evaluation"

	"github.com/jackc/pgx/v5"
)

type evaluationEvidenceResults struct {
	baseline, validation, repeat, stress, backtestBaseline, backtestRepeat backtest.CanonicalResult
	baselineOK, validationOK, repeatOK, stressOK, backtestOK               bool
	backtestRepeatOK                                                       bool
}

func evaluationCandidateEvidence(ctx context.Context, tx pgx.Tx, campaignID string,
	candidate evaluation.CandidateConfiguration, stressPassed bool) (evaluation.EvidenceMetrics, error) {
	results, err := loadEvaluationEvidenceResults(ctx, tx, campaignID, candidate)
	if err != nil {
		return evaluation.EvidenceMetrics{}, err
	}
	datasetCorrect, err := evaluationDatasetCorrect(ctx, tx, campaignID, candidate.Strategy)
	if err != nil {
		return evaluation.EvidenceMetrics{}, err
	}
	metrics := evaluation.EvidenceMetrics{DatasetCorrect: datasetCorrect,
		RuntimeCorrect: results.allAvailable() && stressPassed}
	if metrics.RuntimeCorrect {
		accounting := results.values()
		metrics.AccountingReconciled = evaluationMetricFlagEvery(accounting, "accounting_reconciled", true)
		metrics.NoNegativeInventory = evaluationMetricFlagEvery(accounting, "negative_inventory_count", false)
		metrics.NoDuplicateFill = evaluationMetricFlagEvery(accounting, "duplicate_fill_count", false)
		metrics.NoUnsupportedSale = evaluationMetricFlagEvery(accounting, "unsupported_sale_count", false)
		metrics.DeterministicRepeat = evaluationResultsEqual(results.validation, results.repeat) &&
			evaluationResultsEqual(results.backtestBaseline, results.backtestRepeat)
		capital := evaluationCandidateCapital(candidate)
		metrics.NetResultMicros = metricMicros(results.baseline.Metrics.TotalNetReturn, capital)
		metrics.Stress15NetMicros = metricMicros(results.stress.Metrics.TotalNetReturn, capital)
		metrics.MaximumDrawdownBPS = metricBasisPoints(results.baseline.Metrics.MaximumDrawdown)
		metrics.TradeCount = int64(results.baseline.Metrics.Trades)
		metrics.GrossProfitMicros = metricDecimalMicros(results.baseline.Metrics.ByStrategy["gross_profit"])
		metrics.LargestWinMicros = metricDecimalMicros(results.baseline.Metrics.LargestWin)
		metrics.RouteEvidenceCount = metricMapInteger(results.baseline.Metrics.ByStrategy, "route_evidence")
		metrics.SnapshotEvidenceCount = metricMapInteger(results.baseline.Metrics.ByStrategy, "snapshot_evidence")
	}
	return metrics, nil
}

func loadEvaluationEvidenceResults(ctx context.Context, tx pgx.Tx, campaignID string,
	candidate evaluation.CandidateConfiguration) (evaluationEvidenceResults, error) {
	var values evaluationEvidenceResults
	var err error
	values.baseline, values.baselineOK, err = loadEvaluationResult(ctx, tx, campaignID, candidate, "replay", 2, 10_000)
	if err == nil {
		values.validation, values.validationOK, err = loadEvaluationResult(ctx, tx, campaignID, candidate, "replay", 0, 10_000)
	}
	if err == nil {
		values.repeat, values.repeatOK, err = loadEvaluationResult(ctx, tx, campaignID, candidate, "replay", 1, 10_000)
	}
	if err == nil {
		values.stress, values.stressOK, err = loadEvaluationResult(ctx, tx, campaignID, candidate, "replay", 2, 15_000)
	}
	if err == nil {
		values.backtestBaseline, values.backtestOK, err = loadEvaluationResult(ctx, tx, campaignID, candidate, "backtest", 0, 10_000)
	}
	if err == nil {
		values.backtestRepeat, values.backtestRepeatOK, err = loadEvaluationResult(ctx, tx, campaignID, candidate, "backtest", 1, 10_000)
	}
	return values, err
}

func (results evaluationEvidenceResults) allAvailable() bool {
	return results.baselineOK && results.validationOK && results.repeatOK && results.stressOK &&
		results.backtestOK && results.backtestRepeatOK
}

func (results evaluationEvidenceResults) values() []backtest.CanonicalResult {
	return []backtest.CanonicalResult{results.baseline, results.validation, results.repeat,
		results.stress, results.backtestBaseline, results.backtestRepeat}
}

func ensureEvaluationStressResults(ctx context.Context, tx pgx.Tx, campaignID string,
	now time.Time) ([]evaluation.FocusedStressResult, bool, error) {
	results := evaluation.RunFocusedStressSuite()
	passed := len(results) == 8
	for index, result := range results {
		payload, err := json.Marshal(result)
		if err != nil {
			return nil, false, err
		}
		digest, err := hex.DecodeString(result.EvidenceHash)
		if err != nil || len(digest) != sha256.Size {
			return nil, false, fmt.Errorf("evaluation_stress_hash_invalid")
		}
		state := "PASSED"
		if !result.Passed {
			state, passed = "FAILED", false
		}
		if _, err = tx.Exec(ctx, `INSERT INTO evaluation_campaign_stress_results(
  campaign_id,scenario,ordinal,state,reason_code,canonical_payload,evidence_hash,completed_at)
VALUES($1,$2,$3,$4,NULLIF($5,''),$6,$7,$8) ON CONFLICT (campaign_id,scenario) DO NOTHING`,
			campaignID, result.Scenario, index+1, state, result.ReasonCode, payload, digest, now); err != nil {
			return nil, false, err
		}
		var storedState string
		var storedHash []byte
		if err = tx.QueryRow(ctx, `SELECT state,evidence_hash FROM evaluation_campaign_stress_results
WHERE campaign_id=$1 AND scenario=$2`, campaignID, result.Scenario).Scan(&storedState, &storedHash); err != nil {
			return nil, false, err
		}
		if !bytes.Equal(storedHash, digest) {
			return nil, false, fmt.Errorf("evaluation_stress_evidence_conflict")
		}
		if storedState != "PASSED" {
			passed = false
		}
	}
	return results, passed, nil
}

func loadEvaluationResult(ctx context.Context, tx pgx.Tx, campaignID string,
	candidate evaluation.CandidateConfiguration, mode string, repeat int16,
	stress int32) (backtest.CanonicalResult, bool, error) {
	var state string
	var canonical []byte
	err := tx.QueryRow(ctx, `SELECT member.state,result.canonical_payload
FROM evaluation_campaign_members member
LEFT JOIN run_results result ON result.run_id=member.linked_run_id
WHERE member.campaign_id=$1 AND member.strategy_id=$2 AND member.configuration_key=$3
  AND member.mode=$4 AND member.capital_micros=$5 AND member.repeat_ordinal=$6
  AND member.cost_stress_bps=$7`, campaignID, string(candidate.Strategy), candidate.ConfigurationKey,
		mode, func() int64 {
			if candidate.OrderCapable {
				return evaluation.CombinedStrategyMicros
			}
			return evaluation.CombinedCapitalMicros
		}(), repeat, stress).Scan(&state, &canonical)
	if errors.Is(err, pgx.ErrNoRows) || state == "FAILED" || state == "CANCELED" {
		return backtest.CanonicalResult{}, false, nil
	}
	if err != nil {
		return backtest.CanonicalResult{}, false, err
	}
	var result backtest.CanonicalResult
	if state != "SUCCEEDED" || json.Unmarshal(canonical, &result) != nil || result.ResultHash == "" {
		return backtest.CanonicalResult{}, false, nil
	}
	return result, true, nil
}

func evaluationResultsEqual(left, right backtest.CanonicalResult) bool {
	left.ManifestHash, left.ResultHash = "", ""
	right.ManifestHash, right.ResultHash = "", ""
	left.Namespace, right.Namespace = backtest.ModelNamespace{}, backtest.ModelNamespace{}
	leftPayload, leftErr := json.Marshal(left)
	rightPayload, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftPayload) == string(rightPayload)
}

func evaluationDatasetCorrect(ctx context.Context, tx pgx.Tx, campaignID string,
	strategy evaluation.Strategy) (bool, error) {
	var publicGaps, otherGaps, rejected int
	err := tx.QueryRow(ctx, `SELECT
	  count(gap.id) FILTER (WHERE selected.evidence_role='public_market'),
	  count(gap.id) FILTER (WHERE selected.evidence_role<>'public_market'),
	  count(*) FILTER (WHERE manifest.state IN ('rejected','deleted'))
FROM evaluation_campaign_dataset_members selected
JOIN dataset_manifests manifest ON manifest.id=selected.dataset_id
LEFT JOIN dataset_gaps gap ON gap.dataset_id=manifest.id
WHERE selected.campaign_id=$1 AND selected.strategy_id=$2`, campaignID, string(strategy)).
		Scan(&publicGaps, &otherGaps, &rejected)
	if err != nil || otherGaps != 0 || rejected != 0 {
		return false, err
	}
	if publicGaps == 0 {
		return true, nil
	}
	return evaluationRecoveredPublicDatasetQualified(ctx, tx, campaignID)
}

// A public recording may retain explicit source gaps and still be correct for
// evaluation only when the campaign proved a later fully healthy interval and
// completed the 72-valid-hour qualification. Historical candle gaps and
// unrecovered public gaps remain ineligible.
func evaluationRecoveredPublicDatasetQualified(ctx context.Context, tx pgx.Tx,
	campaignID string) (bool, error) {
	var qualified bool
	err := tx.QueryRow(ctx, `SELECT
	  'RECORDER_QUALIFICATION'=ANY(campaign.completed_stages) AND
	  campaign.valid_recording_seconds>=$2 AND
	  request.state IN ('ACTIVE','PAUSED','FINALIZING','COMPLETED')
FROM evaluation_campaigns campaign
JOIN evaluation_recorder_requests request ON request.campaign_id=campaign.id
WHERE campaign.id=$1`, campaignID, int64(evaluation.RequiredRecordingValidTime/time.Second)).Scan(&qualified)
	return qualified, err
}

func insertSelectedShadowMembers(ctx context.Context, tx pgx.Tx, campaignID string, now time.Time) error {
	for _, strategy := range []evaluation.Strategy{evaluation.StrategyTrend, evaluation.StrategyMean,
		evaluation.StrategyTriangular, evaluation.StrategyCross} {
		var configurationKey, configurationID, datasetID string
		err := tx.QueryRow(ctx, `SELECT configuration_key,configuration_id,dataset_id
FROM evaluation_campaign_members WHERE campaign_id=$1 AND strategy_id=$2 AND verdict='CONTINUE'
  AND mode='replay' AND repeat_ordinal=0 AND cost_stress_bps=10000
ORDER BY (convert_from(metrics_payload,'UTF8')::jsonb#>>'{selection_metrics,net_result_micros}')::bigint DESC,
  configuration_key LIMIT 1`, campaignID, string(strategy)).Scan(&configurationKey, &configurationID, &datasetID)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		id := "evaluation-shadow-member-" + evaluationStableID(campaignID+":"+string(strategy))
		if _, err = tx.Exec(ctx, `INSERT INTO evaluation_campaign_members(campaign_id,id,strategy_id,
  configuration_key,mode,capital_micros,repeat_ordinal,cost_stress_bps,state,verdict,
  configuration_id,dataset_id,created_at,updated_at)
VALUES($1,$2,$3,$4,'shadow',2000000000,0,10000,'PENDING','CONTINUE',$5,$6,$7,$7)
ON CONFLICT (campaign_id,id) DO NOTHING`, campaignID, id, string(strategy), configurationKey,
			configurationID, datasetID, now); err != nil {
			return err
		}
	}
	return nil
}

func metricMicros(ratio string, capitalMicros int64) int64 {
	value, ok := new(big.Rat).SetString(ratio)
	if !ok {
		return 0
	}
	value.Mul(value, new(big.Rat).SetInt64(capitalMicros))
	return quotientInt64(value)
}

func metricDecimalMicros(value string) int64 {
	parsed, ok := new(big.Rat).SetString(value)
	if !ok {
		return 0
	}
	parsed.Mul(parsed, big.NewRat(1_000_000, 1))
	return quotientInt64(parsed)
}

func metricBasisPoints(value string) int64 {
	parsed, ok := new(big.Rat).SetString(value)
	if !ok {
		return 0
	}
	parsed.Mul(parsed, big.NewRat(10_000, 1))
	return quotientInt64(parsed)
}

func quotientInt64(value *big.Rat) int64 {
	if value == nil {
		return 0
	}
	if !value.IsInt() {
		integer := new(big.Int).Quo(value.Num(), value.Denom())
		return integer.Int64()
	}
	return value.Num().Int64()
}

func metricMapInteger(values map[string]string, key string) int64 {
	value, _ := strconv.ParseInt(values[key], 10, 64)
	return value
}

func evaluationMetricFlagEvery(results []backtest.CanonicalResult, key string, expectedTrue bool) bool {
	if len(results) == 0 {
		return false
	}
	for _, result := range results {
		value, exists := result.Metrics.ByStrategy[key]
		if !exists {
			return false
		}
		if expectedTrue {
			if value != "true" {
				return false
			}
			continue
		}
		count, err := strconv.ParseUint(value, 10, 64)
		if err != nil || count != 0 {
			return false
		}
	}
	return true
}

func (driver *EvaluationCampaignDriver) advanceCombinedShadow(ctx context.Context,
	campaign evaluation.Campaign) (evaluation.StageProgress, error) {
	return driver.shadow.Advance(ctx, campaign)
}

func evaluationStrategyVersion(strategy evaluation.Strategy) string {
	return string(strategy) + "@1.0.0"
}

func evaluationMemberID(runID string) string {
	digest := sha256.Sum256([]byte(runID))
	return "evaluation-member-" + hex.EncodeToString(digest[:16])
}

func evaluationStableID(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:16])
}

func ptrString(value string) *string { return &value }

func stringsTrimSuffix(value, suffix string) string {
	if len(value) >= len(suffix) && value[len(value)-len(suffix):] == suffix {
		return value[:len(value)-len(suffix)]
	}
	return value
}
