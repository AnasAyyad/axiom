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

func loadEvaluationPublicDatasets(ctx context.Context, tx pgx.Tx,
	campaignID string) (evaluationDatasetSelection, error) {
	wanted, err := evaluationRecorderDatasetIDs(ctx, tx, campaignID)
	if err != nil {
		return evaluationDatasetSelection{}, err
	}
	members := make([]string, 0, 2)
	coverageStart, coverageEnd := time.Time{}, time.Time{}
	var first, last int64
	for _, recorderID := range wanted {
		member, loadErr := loadEvaluationPublicDatasetMember(ctx, tx, recorderID)
		if loadErr != nil {
			return evaluationDatasetSelection{}, loadErr
		}
		members = append(members, member.id)
		if first == 0 || member.first < first {
			first = member.first
		}
		if member.last > last {
			last = member.last
		}
		if coverageStart.IsZero() || member.start.Before(coverageStart) {
			coverageStart = member.start.UTC()
		}
		if member.end.After(coverageEnd) {
			coverageEnd = member.end.UTC()
		}
	}
	if last-first < 2 {
		return evaluationDatasetSelection{}, fmt.Errorf("evaluation_public_dataset_too_small")
	}
	hash, err := evaluationDatasetSetHash(ctx, tx, members)
	if err != nil {
		return evaluationDatasetSelection{}, err
	}
	return evaluationDatasetSelection{primaryID: members[0], members: members, role: "public_market",
		manifestHash: hash, first: first, last: last, split: chronologicalSplit(first, last),
		coverageStart: coverageStart, coverageEnd: coverageEnd}, nil
}

func evaluationRecorderDatasetIDs(ctx context.Context, tx pgx.Tx, campaignID string) ([]string, error) {
	var binanceSession, bybitSession string
	if err := tx.QueryRow(ctx, `SELECT binance_session_id,bybit_session_id
FROM evaluation_recorder_requests WHERE campaign_id=$1 AND state IN ('ACTIVE','PAUSED','COMPLETED')`,
		campaignID).Scan(&binanceSession, &bybitSession); errors.Is(err, pgx.ErrNoRows) {
		return nil, errEvaluationInputsPending
	} else if err != nil {
		return nil, err
	}
	return []string{"binance-public-recording-" + binanceSession,
		"bybit-public-recording-" + stringsTrimSuffix(bybitSession, "-bybit")}, nil
}

func loadEvaluationPublicDatasetMember(ctx context.Context, tx pgx.Tx,
	recorderID string) (evaluationPublicDatasetMember, error) {
	var member evaluationPublicDatasetMember
	err := tx.QueryRow(ctx, `SELECT manifest.id,manifest.coverage_start,manifest.coverage_end,
	  min(segment.first_ordinal),max(segment.last_ordinal)
FROM dataset_manifests manifest
JOIN dataset_segments selected ON selected.dataset_id=manifest.id
JOIN market_data_segments segment ON segment.id=selected.segment_id
WHERE manifest.recorder_dataset_id=$1 AND manifest.state IN ('ready','qualified')
  AND manifest.dataset_kind='public_market'
GROUP BY manifest.id,manifest.coverage_start,manifest.coverage_end,manifest.manifest_revision
ORDER BY manifest.manifest_revision DESC LIMIT 1`, recorderID).Scan(&member.id, &member.start, &member.end,
		&member.first, &member.last)
	if errors.Is(err, pgx.ErrNoRows) {
		return evaluationPublicDatasetMember{}, errEvaluationInputsPending
	}
	if err != nil || member.first <= 0 || member.last < member.first {
		return evaluationPublicDatasetMember{}, fmt.Errorf("evaluation_public_dataset_invalid")
	}
	return member, nil
}

func chronologicalSplit(first, last int64) int64 {
	count := last - first + 1
	split := first + count*80/100 - 1
	if split < first {
		split = first
	}
	if split >= last {
		split = last - 1
	}
	return split
}

func evaluationDatasetSetHash(ctx context.Context, tx pgx.Tx, members []string) ([]byte, error) {
	if len(members) == 0 {
		return nil, fmt.Errorf("evaluation_dataset_set_empty")
	}
	if len(members) == 1 {
		var value string
		if err := tx.QueryRow(ctx, `SELECT dataset_hash FROM dataset_manifests WHERE id=$1`,
			members[0]).Scan(&value); err != nil {
			return nil, err
		}
		return hex.DecodeString(value)
	}
	values := make([]string, 0)
	for _, member := range members {
		var manifestHash string
		if err := tx.QueryRow(ctx, `SELECT dataset_hash FROM dataset_manifests WHERE id=$1`,
			member).Scan(&manifestHash); err != nil {
			return nil, err
		}
		values = append(values, manifestHash)
		rows, err := tx.Query(ctx, `SELECT segment.checksum FROM dataset_segments selected
JOIN market_data_segments segment ON segment.id=selected.segment_id
WHERE selected.dataset_id=$1 ORDER BY selected.ordinal`, member)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var checksum string
			if err = rows.Scan(&checksum); err != nil {
				rows.Close()
				return nil, err
			}
			values = append(values, checksum)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	digest := sha256.Sum256([]byte(joinEvaluationHashes(values)))
	return digest[:], nil
}

func insertEvaluationDatasetSelection(ctx context.Context, tx pgx.Tx, campaignID string,
	selection evaluationDatasetSelection, now time.Time) error {
	if len(selection.manifestHash) != sha256.Size || selection.first <= 0 ||
		selection.split < selection.first || selection.split >= selection.last {
		return fmt.Errorf("evaluation_dataset_selection_invalid")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO evaluation_campaign_datasets(campaign_id,strategy_id,
  dataset_id,manifest_hash,first_ordinal,last_ordinal,split_ordinal,classified_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (campaign_id,strategy_id) DO NOTHING`,
		campaignID, string(selection.strategy), selection.primaryID, selection.manifestHash,
		selection.first, selection.last, selection.split, now); err != nil {
		return err
	}
	for index, member := range selection.members {
		if _, err := tx.Exec(ctx, `INSERT INTO evaluation_campaign_dataset_members(campaign_id,strategy_id,
  member_ordinal,dataset_id,evidence_role,created_at) VALUES($1,$2,$3,$4,$5,$6)
ON CONFLICT (campaign_id,strategy_id,member_ordinal) DO NOTHING`, campaignID,
			string(selection.strategy), index, member, selection.role, now); err != nil {
			return err
		}
	}
	return nil
}

func freezeEvaluationMetadata(ctx context.Context, tx pgx.Tx, campaignID string, now time.Time) error {
	var existing int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM evaluation_campaign_metadata WHERE campaign_id=$1`,
		campaignID).Scan(&existing); err != nil {
		return err
	}
	if existing == 6 {
		return nil
	}
	if existing != 0 {
		return fmt.Errorf("evaluation_metadata_partial")
	}
	for _, exchange := range []string{"binance", "bybit"} {
		for _, pair := range [][2]string{{"BTC", "USDT"}, {"ETH", "USDT"}, {"ETH", "BTC"}} {
			var instrumentID, metadataID string
			err := tx.QueryRow(ctx, `SELECT instrument.id,metadata.id
FROM instruments instrument
JOIN instrument_metadata_versions metadata ON metadata.instrument_id=instrument.id
WHERE metadata.exchange_id=$1 AND instrument.base_asset=$2 AND instrument.quote_asset=$3
  AND instrument.product='spot' AND metadata.effective_at<=$4
ORDER BY metadata.effective_at DESC,metadata.version DESC LIMIT 1`, exchange, pair[0], pair[1], now).
				Scan(&instrumentID, &metadataID)
			if errors.Is(err, pgx.ErrNoRows) {
				return errEvaluationInputsPending
			}
			if err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO evaluation_campaign_metadata(campaign_id,exchange_id,
  instrument_id,metadata_id,created_at) VALUES($1,$2,$3,$4,$5)`, campaignID, exchange,
				instrumentID, metadataID, now); err != nil {
				return err
			}
		}
	}
	return nil
}

func evaluationCampaignOwner(ctx context.Context, tx pgx.Tx, campaignID string) (string, error) {
	var owner string
	err := tx.QueryRow(ctx, `SELECT actor_id FROM evaluation_campaign_commands
WHERE target_id=$1 ORDER BY created_at,id LIMIT 1`, campaignID).Scan(&owner)
	if err != nil || owner == "" {
		return "", fmt.Errorf("evaluation_campaign_owner_unavailable")
	}
	return owner, nil
}

func (driver *EvaluationCampaignDriver) registerEvaluationConfiguration(ctx context.Context, tx pgx.Tx,
	configurationKey string, capitalMicros int64, now time.Time) (string, error) {
	configuration, err := evaluation.BalancedRunConfiguration(driver.base, configurationKey, capitalMicros)
	if configurationKey == "trend-balanced-01" && capitalMicros == evaluation.CombinedCapitalMicros {
		configuration, err = evaluation.BalancedCombinedConfiguration(driver.base)
	}
	if err != nil {
		return "", err
	}
	canonical, err := json.Marshal(configuration)
	if err != nil {
		return "", err
	}
	hash := ownerConsoleSHA256(canonical)
	id := "evaluation-configuration-" + hash[:24]
	var existing string
	err = tx.QueryRow(ctx, `SELECT id FROM configuration_versions WHERE configuration_hash=$1`, hash).Scan(&existing)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	var version int64
	if err = tx.QueryRow(ctx, `SELECT COALESCE(max(version),0)+1 FROM configuration_versions`).Scan(&version); err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `INSERT INTO configuration_versions(id,version,configuration_hash,
  canonical_payload,actor,recorded_at) VALUES($1,$2,$3,$4,'evaluation-campaign-worker',$5)`,
		id, version, hash, canonical, now)
	return id, err
}

func insertEvaluationJob(ctx context.Context, tx pgx.Tx, campaign evaluation.Campaign,
	planned evaluation.PlannedRun, memberID, owner, configurationID string,
	selection evaluationDatasetSelection, now time.Time) error {
	strategyVersion := evaluationStrategyVersion(planned.Strategy)
	if strategyVersion == "" {
		return fmt.Errorf("evaluation_strategy_version_invalid")
	}
	generationID, jobID, seed, err := registerEvaluationExperiment(ctx, tx, campaign, planned, memberID,
		configurationID, strategyVersion, selection, now)
	if err != nil {
		return err
	}
	first, last, err := evaluationRunWindow(selection, planned)
	if err != nil {
		return err
	}
	request := ownerConsoleOfflineRequest{ConfigurationID: configurationID, DatasetID: selection.primaryID,
		ResearchGenerationID: generationID, RootSeedHash: seed, StrategyVersion: strategyVersion,
		FirstOrdinal: ptrString(strconv.FormatInt(first, 10)), LastOrdinal: ptrString(strconv.FormatInt(last, 10)),
		EvaluationCampaignID: campaign.ID, EvaluationMemberID: memberID,
		ConfigurationKey: planned.ConfigurationKey, CapitalMicros: planned.CapitalMicros,
		CostStressBPS: planned.CostStressBPS}
	if planned.Mode == "replay" {
		request.Speed = ptrString("maximum")
	}
	return queueEvaluationJob(ctx, tx, campaign.ID, planned.Mode, memberID, owner, configurationID,
		selection.primaryID, generationID, jobID, request, now)
}

func registerEvaluationExperiment(ctx context.Context, tx pgx.Tx, campaign evaluation.Campaign,
	planned evaluation.PlannedRun, memberID, configurationID, strategyVersion string,
	selection evaluationDatasetSelection, now time.Time) (string, string, string, error) {
	var nextGeneration int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(generation),0)+1 FROM experiment_registrations
WHERE strategy_version_id=$1`, ownerConsoleStrategyVersionID(strategyVersion)).Scan(&nextGeneration); err != nil {
		return "", "", "", err
	}
	identity := evaluationStableID(campaign.ID + ":" + planned.ID)
	experimentID, generationID, jobID := "evaluation-experiment-"+identity,
		"evaluation-generation-"+identity, "evaluation-job-"+identity
	trainEnd, validationEnd := evaluationTimeSplits(selection.coverageStart, selection.coverageEnd)
	seed := ownerConsoleSHA256([]byte(campaign.ID + ":" + string(planned.Strategy) + ":" +
		planned.ConfigurationKey + ":" + planned.Mode + ":" + strconv.FormatInt(planned.CapitalMicros, 10) +
		":" + strconv.Itoa(int(planned.CostStressBPS))))
	registrationPayload, _ := json.Marshal(map[string]any{"campaign_id": campaign.ID,
		"member_id": memberID, "strategy": planned.Strategy, "configuration_key": planned.ConfigurationKey,
		"mode": planned.Mode, "capital_micros": planned.CapitalMicros,
		"repeat_ordinal": planned.RepeatOrdinal, "cost_stress_bps": planned.CostStressBPS,
		"dataset_hash": hex.EncodeToString(selection.manifestHash), "split_ordinal": selection.split})
	registrationHash := ownerConsoleSHA256(registrationPayload)
	finalHash := ownerConsoleSHA256([]byte(hex.EncodeToString(selection.manifestHash) + ":" +
		strconv.FormatInt(selection.split+1, 10) + ":" + strconv.FormatInt(selection.last, 10)))
	_, err := tx.Exec(ctx, `INSERT INTO experiment_registrations(id,strategy_version_id,
  configuration_id,dataset_id,hypothesis,status,registered_at,generation,primary_metric,
  train_start,train_end,validation_start,validation_end,final_test_start,final_test_end,
  search_space,parameter_neighborhood,model_assumptions,benchmark_assumptions,minimum_samples,
  stopping_rule,rejection_rule,promotion_rule,registered_seed_hash)
VALUES($1,$2,$3,$4,$5,'registered',$6,$7,'net_result_after_costs',$8,$9,$9,$10,$10,$11,
  '{}'::jsonb,'{}'::jsonb,$12::jsonb,$13::jsonb,20,$14,$15,$16,$17)`, experimentID,
		ownerConsoleStrategyVersionID(strategyVersion), configurationID, selection.primaryID,
		"Evaluate the server-owned balanced configuration without production orders.", now, nextGeneration,
		selection.coverageStart, trainEnd, validationEnd, selection.coverageEnd,
		`{"simulation_only":true,"spot_only":true}`, `{"chronological_split":"80_20"}`,
		"complete the registered immutable window", "reject correctness or safety failure",
		"candidate selection remains separate from promotion", seed)
	if err != nil {
		return "", "", "", err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO research_generations(id,experiment_id,generation,
  final_window_hash,registration_hash,registered_at) VALUES($1,$2,$3,$4,$5,$6)`, generationID,
		experimentID, nextGeneration, finalHash, registrationHash, now); err != nil {
		return "", "", "", err
	}
	return generationID, jobID, seed, nil
}
