package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"axiom/internal/evaluation"

	"github.com/jackc/pgx/v5"
)

var errEvaluationInputsPending = errors.New("evaluation_execution_inputs_pending")

type evaluationDatasetSelection struct {
	strategy                   evaluation.Strategy
	primaryID                  string
	members                    []string
	role                       string
	manifestHash               []byte
	first, last, split         int64
	coverageStart, coverageEnd time.Time
}

type evaluationMatrixSummary struct {
	total, queued, running, succeeded, failed, canceled int
}

type evaluationPublicDatasetMember struct {
	id          string
	first, last int64
	start, end  time.Time
}

func (driver *EvaluationCampaignDriver) advanceOfflineMatrix(ctx context.Context,
	campaign evaluation.Campaign, mode string) (evaluation.StageProgress, error) {
	if mode != "backtest" && mode != "replay" {
		return evaluation.StageProgress{}, fmt.Errorf("evaluation_offline_mode_invalid")
	}
	tx, err := driver.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return evaluation.StageProgress{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = driver.ensureEvaluationMatrix(ctx, tx, campaign, mode); err != nil {
		if errors.Is(err, errEvaluationInputsPending) {
			return evaluation.StageProgress{State: evaluation.ProgressPause,
				Reason:  evaluation.ReasonDataUnavailable,
				Summary: "Immutable recorder datasets or public instrument metadata are not yet available."}, nil
		}
		return evaluation.StageProgress{}, err
	}
	if err = syncEvaluationMembers(ctx, tx, campaign.ID, mode, driver.clock.Now().UTC); err != nil {
		return evaluation.StageProgress{}, err
	}
	summary, err := readEvaluationMatrixSummary(ctx, tx, campaign.ID, mode)
	if err != nil {
		return evaluation.StageProgress{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return evaluation.StageProgress{}, err
	}
	checkpoint, _ := json.Marshal(map[string]int{"total": summary.total, "queued": summary.queued,
		"running": summary.running, "succeeded": summary.succeeded, "failed": summary.failed,
		"canceled": summary.canceled})
	if summary.total == 0 {
		return evaluation.StageProgress{}, fmt.Errorf("evaluation_offline_matrix_empty")
	}
	if summary.succeeded+summary.failed+summary.canceled == summary.total {
		return evaluation.StageProgress{State: evaluation.ProgressComplete,
			Summary:    fmt.Sprintf("The %s matrix completed; individual failures remain preserved for selection.", mode),
			Checkpoint: checkpoint, LinkedResourceType: "offline_matrix", LinkedResourceID: campaign.ID + ":" + mode}, nil
	}
	return evaluation.StageProgress{State: evaluation.ProgressWaiting,
		Summary:    fmt.Sprintf("The %s matrix is executing through durable credential-free jobs.", mode),
		Checkpoint: checkpoint, LinkedResourceType: "offline_matrix", LinkedResourceID: campaign.ID + ":" + mode}, nil
}

func (driver *EvaluationCampaignDriver) ensureEvaluationMatrix(ctx context.Context, tx pgx.Tx,
	campaign evaluation.Campaign, mode string) error {
	plannedRuns := plannedEvaluationRuns(mode)
	if (mode != "backtest" && mode != "replay") || len(plannedRuns) == 0 {
		return fmt.Errorf("evaluation_matrix_mode_invalid")
	}
	linked, err := linkedEvaluationMatrixMembers(ctx, tx, campaign.ID, mode)
	if err != nil {
		return err
	}
	if linked == len(plannedRuns) {
		return nil
	}
	if linked != 0 {
		return fmt.Errorf("evaluation_matrix_partial_initialization")
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('axiom:evaluation-matrix'))`); err != nil {
		return err
	}
	selections, err := driver.prepareEvaluationMatrixInputs(ctx, tx, campaign.ID, mode)
	if err != nil {
		return err
	}
	owner, err := evaluationCampaignOwner(ctx, tx, campaign.ID)
	if err != nil {
		return err
	}
	if mode == "backtest" {
		if err = driver.freezeCombinedEvaluationConfiguration(ctx, tx, campaign.ID); err != nil {
			return err
		}
	}
	return driver.insertEvaluationMatrixJobs(ctx, tx, campaign, plannedRuns, selections, owner)
}

func plannedEvaluationRuns(mode string) []evaluation.PlannedRun {
	plannedRuns := make([]evaluation.PlannedRun, 0, len(evaluation.BalancedFullRuns())/2)
	for _, planned := range evaluation.BalancedFullRuns() {
		if planned.Mode == mode {
			plannedRuns = append(plannedRuns, planned)
		}
	}
	return plannedRuns
}

func linkedEvaluationMatrixMembers(ctx context.Context, tx pgx.Tx, campaignID, mode string) (int, error) {
	var linked int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM evaluation_campaign_members
WHERE campaign_id=$1 AND mode=$2 AND repeat_ordinal<2 AND linked_job_id IS NOT NULL`, campaignID,
		mode).Scan(&linked)
	return linked, err
}

func (driver *EvaluationCampaignDriver) prepareEvaluationMatrixInputs(ctx context.Context, tx pgx.Tx,
	campaignID, mode string) (map[evaluation.Strategy]evaluationDatasetSelection, error) {
	if mode == "backtest" {
		selections, err := loadEvaluationDatasetSelections(ctx, tx, campaignID, driver.clock.Now().UTC)
		if err == nil {
			err = freezeEvaluationMetadata(ctx, tx, campaignID, driver.clock.Now().UTC)
		}
		return selections, err
	}
	selections := make(map[evaluation.Strategy]evaluationDatasetSelection, 5)
	for _, strategy := range evaluationStrategies() {
		selection, err := loadStoredEvaluationDatasetSelection(ctx, tx, campaignID, strategy)
		if err != nil {
			return nil, err
		}
		selections[strategy] = selection
	}
	var metadataCount int
	var combinedConfigurationID string
	err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM evaluation_campaign_metadata
WHERE campaign_id=$1),COALESCE(combined_configuration_id,'') FROM evaluation_campaigns WHERE id=$1`, campaignID).
		Scan(&metadataCount, &combinedConfigurationID)
	if err != nil || metadataCount != 6 || combinedConfigurationID == "" {
		return nil, fmt.Errorf("evaluation_frozen_inputs_missing")
	}
	return selections, nil
}

func evaluationStrategies() []evaluation.Strategy {
	return []evaluation.Strategy{evaluation.StrategyTrend, evaluation.StrategyMean,
		evaluation.StrategyTriangular, evaluation.StrategyCross, evaluation.StrategyInventory}
}

func (driver *EvaluationCampaignDriver) freezeCombinedEvaluationConfiguration(ctx context.Context,
	tx pgx.Tx, campaignID string) error {
	now := driver.clock.Now().UTC
	configurationID, err := driver.registerEvaluationConfiguration(ctx, tx, "trend-balanced-01",
		evaluation.CombinedCapitalMicros, now)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE evaluation_campaigns SET combined_configuration_id=$2,
updated_at=GREATEST(updated_at,$3) WHERE id=$1
AND (combined_configuration_id IS NULL OR combined_configuration_id=$2)`, campaignID, configurationID, now)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("evaluation_combined_configuration_conflict")
	}
	return nil
}

func (driver *EvaluationCampaignDriver) insertEvaluationMatrixJobs(ctx context.Context, tx pgx.Tx,
	campaign evaluation.Campaign, plannedRuns []evaluation.PlannedRun,
	selections map[evaluation.Strategy]evaluationDatasetSelection, owner string) error {
	configurationIDs := make(map[string]string)
	var err error
	for _, planned := range plannedRuns {
		selection, ok := selections[planned.Strategy]
		if !ok {
			return errEvaluationInputsPending
		}
		memberID := evaluationMemberID(planned.ID)
		configurationCacheKey := planned.ConfigurationKey + ":" + strconv.FormatInt(planned.CapitalMicros, 10)
		configurationID := configurationIDs[configurationCacheKey]
		if configurationID == "" {
			configurationID, err = driver.registerEvaluationConfiguration(ctx, tx,
				planned.ConfigurationKey, planned.CapitalMicros, driver.clock.Now().UTC)
			if err != nil {
				return err
			}
			configurationIDs[configurationCacheKey] = configurationID
		}
		if err = insertEvaluationJob(ctx, tx, campaign, planned, memberID, owner,
			configurationID, selection, driver.clock.Now().UTC); err != nil {
			return err
		}
	}
	return nil
}

func loadEvaluationDatasetSelections(ctx context.Context, tx pgx.Tx,
	campaignID string, now time.Time) (map[evaluation.Strategy]evaluationDatasetSelection, error) {
	result, err := loadEvaluationHistoricalSelections(ctx, tx, campaignID)
	if err != nil {
		return nil, err
	}
	public, err := loadEvaluationPublicDatasets(ctx, tx, campaignID)
	if err != nil {
		return nil, err
	}
	for _, strategy := range []evaluation.Strategy{evaluation.StrategyTriangular,
		evaluation.StrategyCross, evaluation.StrategyInventory} {
		selection, selectionErr := publicSelectionForStrategy(ctx, tx, strategy, public)
		if selectionErr != nil {
			return nil, selectionErr
		}
		result[strategy] = selection
	}
	for _, selection := range result {
		if err = insertEvaluationDatasetSelection(ctx, tx, campaignID, selection, now); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func loadEvaluationHistoricalSelections(ctx context.Context, tx pgx.Tx,
	campaignID string) (map[evaluation.Strategy]evaluationDatasetSelection, error) {
	result := make(map[evaluation.Strategy]evaluationDatasetSelection, 5)
	historical := []struct {
		strategy   evaluation.Strategy
		instrument string
		interval   string
	}{{evaluation.StrategyTrend, "BTC/USDT", "4h"}, {evaluation.StrategyMean, "BTC/USDT", "1h"}}
	for _, wanted := range historical {
		var selection evaluationDatasetSelection
		var hash string
		selection.strategy, selection.role = wanted.strategy, "historical_candles"
		err := tx.QueryRow(ctx, `SELECT imported.normalized_dataset_id,manifest.dataset_hash,
  imported.row_count,manifest.coverage_start,manifest.coverage_end
FROM evaluation_historical_imports imported
JOIN dataset_manifests manifest ON manifest.id=imported.normalized_dataset_id
WHERE imported.campaign_id=$1 AND imported.exchange_id='binance' AND imported.instrument=$2
  AND imported.interval=$3 AND imported.state='COMPLETED'
  AND manifest.state IN ('ready','qualified') AND manifest.dataset_kind='public_market'`,
			campaignID, wanted.instrument, wanted.interval).Scan(&selection.primaryID, &hash,
			&selection.last, &selection.coverageStart, &selection.coverageEnd)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errEvaluationInputsPending
		}
		if err != nil || selection.last < 2 {
			return nil, fmt.Errorf("evaluation_historical_dataset_invalid")
		}
		selection.first, selection.members = 1, []string{selection.primaryID}
		selection.split = chronologicalSplit(selection.first, selection.last)
		selection.manifestHash, err = hex.DecodeString(hash)
		if err != nil || len(selection.manifestHash) != sha256.Size {
			return nil, fmt.Errorf("evaluation_historical_dataset_invalid")
		}
		result[wanted.strategy] = selection
	}
	return result, nil
}

func publicSelectionForStrategy(ctx context.Context, tx pgx.Tx, strategy evaluation.Strategy,
	public evaluationDatasetSelection) (evaluationDatasetSelection, error) {
	selection := public
	selection.strategy = strategy
	if strategy != evaluation.StrategyTriangular {
		return selection, nil
	}
	selection.members = selection.members[:1]
	selection.primaryID = selection.members[0]
	var err error
	selection.first, selection.last, selection.coverageStart, selection.coverageEnd, err =
		evaluationDatasetBounds(ctx, tx, selection.primaryID)
	if err != nil || selection.last-selection.first < 2 {
		return evaluationDatasetSelection{}, fmt.Errorf("evaluation_triangular_dataset_invalid")
	}
	selection.split = chronologicalSplit(selection.first, selection.last)
	selection.manifestHash, err = evaluationDatasetSetHash(ctx, tx, selection.members)
	return selection, err
}

func evaluationDatasetBounds(ctx context.Context, tx pgx.Tx, datasetID string) (int64, int64,
	time.Time, time.Time, error) {
	var first, last int64
	var start, end time.Time
	err := tx.QueryRow(ctx, `SELECT min(segment.first_ordinal),max(segment.last_ordinal),
  manifest.coverage_start,manifest.coverage_end
FROM dataset_manifests manifest
JOIN dataset_segments selected ON selected.dataset_id=manifest.id
JOIN market_data_segments segment ON segment.id=selected.segment_id
WHERE manifest.id=$1 GROUP BY manifest.id,manifest.coverage_start,manifest.coverage_end`, datasetID).
		Scan(&first, &last, &start, &end)
	return first, last, start.UTC(), end.UTC(), err
}
