package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

func (driver *EvaluationCampaignDriver) loadEvaluationReportEvidence(ctx context.Context,
	campaignID string) (map[string]any, error) {
	events, err := driver.loadEvaluationReportEvents(ctx, campaignID)
	if err != nil {
		return nil, err
	}
	imports, err := driver.loadEvaluationReportImports(ctx, campaignID)
	if err != nil {
		return nil, err
	}
	audit, findings, err := driver.loadEvaluationReportAudit(ctx, campaignID)
	if err != nil {
		return nil, err
	}
	recorder, feeds, err := driver.loadEvaluationReportRecorder(ctx, campaignID)
	if err != nil {
		return nil, err
	}
	shadow, err := driver.loadEvaluationReportShadow(ctx, campaignID)
	if err != nil {
		return nil, err
	}
	datasets, err := driver.loadEvaluationReportDatasets(ctx, campaignID)
	if err != nil {
		return nil, err
	}
	locks, err := driver.loadEvaluationReportCandidateLocks(ctx, campaignID)
	if err != nil {
		return nil, err
	}
	combinedConfiguration, err := driver.loadEvaluationCombinedConfiguration(ctx, campaignID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"timeline": events, "historical_imports": imports, "data_audit": audit,
		"data_audit_findings": findings, "recorder": recorder, "recorder_feeds": feeds,
		"combined_shadow": shadow, "datasets": datasets, "candidate_locks": locks,
		"combined_configuration": combinedConfiguration}, nil
}

func (driver *EvaluationCampaignDriver) loadEvaluationCombinedConfiguration(ctx context.Context,
	campaignID string) (map[string]any, error) {
	var id, hash string
	var version int64
	err := driver.pool.QueryRow(ctx, `SELECT configuration.id,configuration.version,
  configuration.configuration_hash FROM evaluation_campaigns campaign
JOIN configuration_versions configuration ON configuration.id=campaign.combined_configuration_id
WHERE campaign.id=$1`, campaignID).Scan(&id, &version, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"profile": "balanced_full_v1_10000", "configuration_id": id,
		"configuration_version": version, "configuration_hash": hash,
		"capital_micros": int64(10_000_000_000), "protected_reserve_micros": int64(2_000_000_000),
		"member_ceiling_micros": int64(2_000_000_000)}, nil
}

func (driver *EvaluationCampaignDriver) loadEvaluationReportCandidateLocks(ctx context.Context,
	campaignID string) ([]map[string]any, error) {
	rows, err := driver.pool.Query(ctx, `SELECT strategy_id,state,COALESCE(configuration_key,''),
  COALESCE(configuration_id,''),COALESCE(dataset_id,''),
  COALESCE(encode(validation_result_hash,'hex'),''),COALESCE(reason_code,''),locked_at
FROM evaluation_campaign_candidate_locks WHERE campaign_id=$1 ORDER BY strategy_id`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var strategy, state, configuration, configurationID, datasetID, hash, reason string
		var lockedAt time.Time
		if err = rows.Scan(&strategy, &state, &configuration, &configurationID, &datasetID,
			&hash, &reason, &lockedAt); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{"strategy": strategy, "state": state,
			"configuration_key": configuration, "configuration_id": configurationID,
			"dataset_id": datasetID, "validation_result_hash": hash, "reason_code": reason,
			"locked_at": lockedAt.UTC()})
		if state == "SELECTED" {
			if err = driver.enrichEvaluationCandidateLock(ctx, campaignID, strategy, configuration,
				items[len(items)-1]); err != nil {
				return nil, err
			}
		}
	}
	return items, rows.Err()
}

func (driver *EvaluationCampaignDriver) enrichEvaluationCandidateLock(ctx context.Context, campaignID,
	strategy, configuration string, item map[string]any) error {
	var verdict, reason string
	var finalMetrics, stressMetrics []byte
	err := driver.pool.QueryRow(ctx, `SELECT COALESCE(baseline.verdict,''),
COALESCE(baseline.reason_code,''),COALESCE(baseline.metrics_payload,'{}'::bytea),
COALESCE(stress.metrics_payload,'{}'::bytea) FROM evaluation_campaign_members baseline
JOIN evaluation_campaign_members stress ON stress.campaign_id=baseline.campaign_id
 AND stress.strategy_id=baseline.strategy_id AND stress.configuration_key=baseline.configuration_key
 AND stress.mode='replay' AND stress.repeat_ordinal=2 AND stress.cost_stress_bps=15000
WHERE baseline.campaign_id=$1 AND baseline.strategy_id=$2 AND baseline.configuration_key=$3
 AND baseline.mode='replay' AND baseline.repeat_ordinal=2 AND baseline.cost_stress_bps=10000`,
		campaignID, strategy, configuration).Scan(&verdict, &reason, &finalMetrics, &stressMetrics)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if verdict != "" {
		item["pre_shadow_verdict"] = verdict
	}
	if reason != "" {
		item["pre_shadow_reason_code"] = reason
	}
	if json.Valid(finalMetrics) {
		item["final_test_metrics"] = json.RawMessage(append([]byte(nil), finalMetrics...))
	}
	if json.Valid(stressMetrics) {
		item["stress_1_5_metrics"] = json.RawMessage(append([]byte(nil), stressMetrics...))
	}
	return nil
}

func (driver *EvaluationCampaignDriver) loadEvaluationReportEvents(ctx context.Context,
	campaignID string) ([]map[string]any, error) {
	rows, err := driver.pool.Query(ctx, `SELECT ordinal,event_type,COALESCE(stage,''),
  COALESCE(reason_code,''),COALESCE(summary,''),occurred_at
FROM evaluation_campaign_events WHERE campaign_id=$1 ORDER BY ordinal`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var ordinal int64
		var eventType, stage, reason, summary string
		var occurredAt time.Time
		if err = rows.Scan(&ordinal, &eventType, &stage, &reason, &summary, &occurredAt); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{"ordinal": ordinal, "event_type": eventType, "stage": stage,
			"reason_code": reason, "summary": summary, "occurred_at": occurredAt.UTC()})
	}
	return items, rows.Err()
}

func (driver *EvaluationCampaignDriver) loadEvaluationReportImports(ctx context.Context,
	campaignID string) ([]map[string]any, error) {
	rows, err := driver.pool.Query(ctx, `SELECT exchange_id,instrument,interval,state,checkpoint_time,
  row_count,byte_count,gap_count,COALESCE(encode(raw_hash,'hex'),''),
  COALESCE(encode(normalized_hash,'hex'),''),COALESCE(normalized_dataset_id,''),
  COALESCE(reason_code,''),COALESCE(source_metadata,'{}'::bytea)
FROM evaluation_historical_imports WHERE campaign_id=$1 ORDER BY exchange_id,instrument,interval`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var exchange, instrument, interval, state, rawHash, normalizedHash, datasetID, reason string
		var checkpoint time.Time
		var rowCount, byteCount, gapCount int64
		var source []byte
		if err = rows.Scan(&exchange, &instrument, &interval, &state, &checkpoint, &rowCount,
			&byteCount, &gapCount, &rawHash, &normalizedHash, &datasetID, &reason, &source); err != nil {
			return nil, err
		}
		sourceValue := json.RawMessage(`{}`)
		if json.Valid(source) {
			sourceValue = append(json.RawMessage(nil), source...)
		}
		items = append(items, map[string]any{"exchange": exchange, "instrument": instrument,
			"interval": interval, "state": state, "checkpoint_time": checkpoint.UTC(), "row_count": rowCount,
			"byte_count": byteCount, "gap_count": gapCount, "raw_hash": rawHash,
			"normalized_hash": normalizedHash, "dataset_id": datasetID, "reason_code": reason,
			"source_metadata": sourceValue})
	}
	return items, rows.Err()
}

func (driver *EvaluationCampaignDriver) loadEvaluationReportAudit(ctx context.Context,
	campaignID string) (map[string]any, []map[string]any, error) {
	audit := map[string]any{}
	var id, state, reason string
	var baseline, updated time.Time
	err := driver.pool.QueryRow(ctx, `SELECT id,state,COALESCE(reason_code,''),baseline_at,updated_at
FROM evaluation_data_audits WHERE campaign_id=$1`, campaignID).Scan(&id, &state, &reason, &baseline, &updated)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, err
	}
	if err == nil {
		audit = map[string]any{"id": id, "state": state, "reason_code": reason,
			"baseline_at": baseline.UTC(), "updated_at": updated.UTC()}
	}
	rows, err := driver.pool.Query(ctx, `SELECT finding.ordinal,finding.dataset_id,
  COALESCE(finding.exchange_id,''),COALESCE(finding.instrument_id,''),finding.eligibility,
  finding.reason_code,COALESCE(encode(finding.manifest_hash,'hex'),''),finding.segment_count,
  finding.byte_count,finding.gap_count,finding.duplicate_count,finding.created_at
FROM evaluation_data_audit_findings finding
JOIN evaluation_data_audits audit ON audit.id=finding.audit_id
WHERE audit.campaign_id=$1 ORDER BY finding.ordinal`, campaignID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	findings := []map[string]any{}
	for rows.Next() {
		var ordinal, segments, bytes, gaps, duplicates int64
		var dataset, exchange, instrument, eligibility, itemReason, hash string
		var created time.Time
		if err = rows.Scan(&ordinal, &dataset, &exchange, &instrument, &eligibility, &itemReason,
			&hash, &segments, &bytes, &gaps, &duplicates, &created); err != nil {
			return nil, nil, err
		}
		findings = append(findings, map[string]any{"ordinal": ordinal, "dataset_id": dataset,
			"exchange": exchange, "instrument": instrument, "eligibility": eligibility,
			"reason_code": itemReason, "manifest_hash": hash, "segment_count": segments,
			"byte_count": bytes, "gap_count": gaps, "duplicate_count": duplicates,
			"created_at": created.UTC()})
	}
	return audit, findings, rows.Err()
}

func (driver *EvaluationCampaignDriver) loadEvaluationReportRecorder(ctx context.Context,
	campaignID string) (map[string]any, []map[string]any, error) {
	recorder, err := driver.loadEvaluationRecorderSummary(ctx, campaignID)
	if err != nil {
		return nil, nil, err
	}
	feeds, err := driver.loadEvaluationRecorderFeeds(ctx, campaignID)
	return recorder, feeds, err
}

func (driver *EvaluationCampaignDriver) loadEvaluationRecorderSummary(ctx context.Context,
	campaignID string) (map[string]any, error) {
	recorder := map[string]any{}
	var state, reason, binanceSession, bybitSession string
	var baseline, recorded, valid int64
	var rate, reserve *int64
	var requested time.Time
	err := driver.pool.QueryRow(ctx, `SELECT state,COALESCE(reason_code,''),binance_session_id,
  bybit_session_id,storage_baseline_bytes,recorded_bytes,valid_recording_seconds,
  measured_bytes_per_hour,shadow_reserved_bytes,requested_at
FROM evaluation_recorder_requests WHERE campaign_id=$1`, campaignID).Scan(&state, &reason, &binanceSession,
		&bybitSession, &baseline, &recorded, &valid, &rate, &reserve, &requested)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if err == nil {
		recorder = map[string]any{"state": state, "reason_code": reason, "binance_session_id": binanceSession,
			"bybit_session_id": bybitSession, "storage_baseline_bytes": baseline,
			"recorded_bytes": recorded, "valid_seconds": valid, "requested_at": requested.UTC()}
		if rate != nil {
			recorder["measured_bytes_per_hour"] = *rate
		}
		if reserve != nil {
			recorder["shadow_reserved_bytes"] = *reserve
		}
	}
	return recorder, nil
}

func (driver *EvaluationCampaignDriver) loadEvaluationRecorderFeeds(ctx context.Context,
	campaignID string) ([]map[string]any, error) {
	rows, err := driver.pool.Query(ctx, `SELECT instrument.exchange_id,instrument.instrument,
  instrument.eligible,instrument.book_fresh,instrument.clock_eligible,instrument.latest_event_at,
  instrument.message_count,instrument.queue_drop_count,instrument.gap_count,instrument.decoder_error_count
FROM evaluation_recorder_instrument_observations instrument
JOIN (SELECT campaign_id,max(ordinal) AS ordinal FROM evaluation_recorder_observations
  WHERE campaign_id=$1 GROUP BY campaign_id) latest
  ON latest.campaign_id=instrument.campaign_id AND latest.ordinal=instrument.observation_ordinal
ORDER BY instrument.exchange_id,instrument.instrument`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	feeds := []map[string]any{}
	for rows.Next() {
		var exchange, instrument string
		var eligible, bookFresh, clockEligible bool
		var latest time.Time
		var messages, drops, gaps, decoders int64
		if err = rows.Scan(&exchange, &instrument, &eligible, &bookFresh, &clockEligible, &latest,
			&messages, &drops, &gaps, &decoders); err != nil {
			return nil, err
		}
		feeds = append(feeds, map[string]any{"exchange": exchange, "instrument": instrument,
			"eligible": eligible, "book_fresh": bookFresh, "clock_eligible": clockEligible,
			"latest_event_at": latest.UTC(), "message_count": messages, "queue_drop_count": drops,
			"gap_count": gaps, "decoder_error_count": decoders})
	}
	return feeds, rows.Err()
}

func (driver *EvaluationCampaignDriver) loadEvaluationReportShadow(ctx context.Context,
	campaignID string) (map[string]any, error) {
	var state, recorderSession, reason, checkpointHash, manifestHash string
	var start, last, valid, capital, reserve, ceiling int64
	var started, updated time.Time
	err := driver.pool.QueryRow(ctx, `SELECT state,recorder_session_id,COALESCE(reason_code,''),
  start_ordinal,last_processed_ordinal,valid_seconds,shared_capital_micros,
  protected_reserve_micros,member_ceiling_micros,COALESCE(encode(checkpoint_hash,'hex'),''),
  COALESCE(encode(input_manifest_hash,'hex'),''),started_at,updated_at
FROM evaluation_shadow_sessions WHERE campaign_id=$1`, campaignID).Scan(&state, &recorderSession, &reason,
		&start, &last, &valid, &capital, &reserve, &ceiling, &checkpointHash, &manifestHash, &started, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"state": state, "recorder_session_id": recorderSession, "reason_code": reason,
		"start_ordinal": start, "last_processed_ordinal": last, "valid_seconds": valid,
		"shared_capital_micros": capital, "protected_reserve_micros": reserve,
		"member_ceiling_micros": ceiling, "checkpoint_hash": checkpointHash,
		"input_manifest_hash": manifestHash, "started_at": started.UTC(), "updated_at": updated.UTC()}, nil
}

func (driver *EvaluationCampaignDriver) loadEvaluationReportDatasets(ctx context.Context,
	campaignID string) ([]map[string]any, error) {
	rows, err := driver.pool.Query(ctx, `SELECT selected.strategy_id,selected.dataset_id,
  encode(selected.manifest_hash,'hex'),selected.first_ordinal,selected.last_ordinal,
  selected.split_ordinal,selected.classified_at,
  COALESCE(jsonb_agg(jsonb_build_object('ordinal',member.member_ordinal,'dataset_id',member.dataset_id,
    'evidence_role',member.evidence_role) ORDER BY member.member_ordinal)
    FILTER (WHERE member.dataset_id IS NOT NULL),'[]'::jsonb)
FROM evaluation_campaign_datasets selected
LEFT JOIN evaluation_campaign_dataset_members member
  ON member.campaign_id=selected.campaign_id AND member.strategy_id=selected.strategy_id
WHERE selected.campaign_id=$1
GROUP BY selected.campaign_id,selected.strategy_id,selected.dataset_id,selected.manifest_hash,
  selected.first_ordinal,selected.last_ordinal,selected.split_ordinal,selected.classified_at
ORDER BY selected.strategy_id`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var strategy, dataset, hash string
		var first, last, split int64
		var classified time.Time
		var members []byte
		if err = rows.Scan(&strategy, &dataset, &hash, &first, &last, &split, &classified, &members); err != nil {
			return nil, err
		}
		memberValue := json.RawMessage(`[]`)
		if json.Valid(members) {
			memberValue = append(json.RawMessage(nil), members...)
		}
		items = append(items, map[string]any{"strategy": strategy, "dataset_id": dataset,
			"manifest_hash": hash, "first_ordinal": first, "last_ordinal": last,
			"split_ordinal": split, "classified_at": classified.UTC(), "members": memberValue})
	}
	return items, rows.Err()
}
