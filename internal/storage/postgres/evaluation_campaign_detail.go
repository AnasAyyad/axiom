package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"
	"axiom/internal/evaluation"

	"github.com/jackc/pgx/v5"
)

func (store *OwnerConsoleStore) loadEvaluationCampaignDetail(ctx context.Context,
	campaign *generated.EvaluationCampaign) error {
	if err := store.loadEvaluationStages(ctx, campaign); err != nil {
		return err
	}
	if err := store.loadEvaluationImports(ctx, campaign); err != nil {
		return err
	}
	if err := store.loadEvaluationCoverage(ctx, campaign); err != nil {
		return err
	}
	if err := store.loadEvaluationMatrix(ctx, campaign); err != nil {
		return err
	}
	if err := store.loadEvaluationFeedHealth(ctx, campaign); err != nil {
		return err
	}
	return store.loadEvaluationShadow(ctx, campaign)
}

func (store *OwnerConsoleStore) loadEvaluationStages(ctx context.Context,
	campaign *generated.EvaluationCampaign) error {
	rows, err := store.pool.Query(ctx, `SELECT stage,state,attempt,reason_code,started_at,completed_at,updated_at
FROM evaluation_campaign_stages WHERE campaign_id=$1 ORDER BY ordinal`, campaign.Id)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := make([]generated.EvaluationStageProgress, 0, len(evaluation.Stages()))
	for rows.Next() {
		var stage, state string
		var attempt int
		var reason *string
		var started, completed *generated.Timestamp
		var updated generated.Timestamp
		if err = rows.Scan(&stage, &state, &attempt, &reason, &started, &completed, &updated); err != nil {
			return err
		}
		items = append(items, generated.EvaluationStageProgress{Stage: generated.EvaluationStageProgressStage(stage),
			State: generated.EvaluationStageProgressState(state), Attempt: attempt, ReasonCode: reason,
			StartedAt: started, CompletedAt: completed, UpdatedAt: updated})
	}
	campaign.Stages = &items
	return rows.Err()
}

func (store *OwnerConsoleStore) loadEvaluationImports(ctx context.Context,
	campaign *generated.EvaluationCampaign) error {
	rows, err := store.pool.Query(ctx, `SELECT exchange_id,instrument,interval,state,window_start,
window_end,checkpoint_time,row_count,byte_count,gap_count,reason_code
FROM evaluation_historical_imports WHERE campaign_id=$1 ORDER BY exchange_id,instrument,interval`, campaign.Id)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := make([]generated.EvaluationHistoricalImportProgress, 0, 12)
	for rows.Next() {
		var exchange, instrument, interval, state string
		var start, end, checkpoint generated.Timestamp
		var rowCount, byteCount, gapCount int64
		var reason *string
		if err = rows.Scan(&exchange, &instrument, &interval, &state, &start, &end, &checkpoint,
			&rowCount, &byteCount, &gapCount, &reason); err != nil {
			return err
		}
		items = append(items, generated.EvaluationHistoricalImportProgress{
			Exchange:   generated.EvaluationHistoricalImportProgressExchange(exchange),
			Instrument: generated.EvaluationHistoricalImportProgressInstrument(instrument),
			Interval:   generated.EvaluationHistoricalImportProgressInterval(interval),
			State:      generated.EvaluationHistoricalImportProgressState(state), WindowStart: start,
			WindowEnd: end, CheckpointTime: checkpoint, RowCount: rowCount, ByteCount: byteCount,
			GapCount: gapCount, ReasonCode: reason})
	}
	campaign.HistoricalImports = &items
	return rows.Err()
}

func (store *OwnerConsoleStore) loadEvaluationCoverage(ctx context.Context,
	campaign *generated.EvaluationCampaign) error {
	rows, err := store.pool.Query(ctx, `SELECT dataset_id,exchange_id,instrument_id,eligibility,reason_code,
segment_count,byte_count,gap_count,duplicate_count FROM evaluation_data_audit_findings
WHERE audit_id=(SELECT id FROM evaluation_data_audits WHERE campaign_id=$1 ORDER BY created_at DESC,id DESC LIMIT 1)
ORDER BY ordinal`, campaign.Id)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := make([]generated.EvaluationCoverage, 0)
	for rows.Next() {
		var item generated.EvaluationCoverage
		var eligibility string
		if err = rows.Scan(&item.DatasetId, &item.Exchange, &item.Instrument, &eligibility, &item.ReasonCode,
			&item.SegmentCount, &item.ByteCount, &item.GapCount, &item.DuplicateCount); err != nil {
			return err
		}
		item.Eligibility = generated.EvaluationCoverageEligibility(eligibility)
		items = append(items, item)
	}
	campaign.Coverage = &items
	return rows.Err()
}

func (store *OwnerConsoleStore) loadEvaluationMatrix(ctx context.Context,
	campaign *generated.EvaluationCampaign) error {
	rows, err := store.pool.Query(ctx, evaluationMemberSelect+`
FROM evaluation_campaign_members WHERE campaign_id=$1 AND mode<>'shadow'
ORDER BY strategy_id,configuration_key,mode,capital_micros,repeat_ordinal,cost_stress_bps`, campaign.Id)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := make([]generated.EvaluationMatrixMember, 0, len(evaluation.BalancedFullRuns()))
	for rows.Next() {
		item, scanErr := scanEvaluationMatrixMember(rows)
		if scanErr != nil {
			return scanErr
		}
		items = append(items, item)
	}
	campaign.Matrix = &items
	return rows.Err()
}

const evaluationMemberSelect = `SELECT id,strategy_id,configuration_key,mode,capital_micros,
repeat_ordinal,cost_stress_bps,state,verdict,reason_code,COALESCE(encode(result_hash,'hex'),''),
COALESCE(metrics_payload,'{}'::bytea) `

func scanEvaluationMatrixMember(row campaignScanner) (generated.EvaluationMatrixMember, error) {
	var item generated.EvaluationMatrixMember
	var strategy, mode, state string
	var verdict, reason *string
	var resultHash string
	var metricsPayload []byte
	err := row.Scan(&item.Id, &strategy, &item.Configuration, &mode, &item.CapitalMicros,
		&item.RepeatOrdinal, &item.CostStressBps, &state, &verdict, &reason, &resultHash, &metricsPayload)
	if err != nil {
		return generated.EvaluationMatrixMember{}, err
	}
	item.Strategy = generated.EvaluationMatrixMemberStrategy(strategy)
	item.Mode = generated.EvaluationMatrixMemberMode(mode)
	item.State = generated.EvaluationMatrixMemberState(state)
	item.ReasonCode = reason
	if verdict != nil {
		value := generated.EvaluationMatrixMemberVerdict(*verdict)
		item.Verdict = &value
	}
	if resultHash != "" {
		item.ResultHash = &resultHash
	}
	var metrics map[string]interface{}
	if json.Valid(metricsPayload) && json.Unmarshal(metricsPayload, &metrics) == nil {
		item.Metrics = &metrics
	}
	return item, nil
}

func (store *OwnerConsoleStore) loadEvaluationFeedHealth(ctx context.Context,
	campaign *generated.EvaluationCampaign) error {
	rows, err := store.pool.Query(ctx, `SELECT exchange_id,instrument,eligible,book_fresh,clock_eligible,
latest_event_at,message_count,queue_drop_count,gap_count,decoder_error_count
FROM evaluation_recorder_instrument_observations WHERE campaign_id=$1 AND observation_ordinal=(
SELECT max(ordinal) FROM evaluation_recorder_observations WHERE campaign_id=$1)
ORDER BY exchange_id,instrument`, campaign.Id)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := make([]generated.EvaluationFeedHealth, 0, 6)
	for rows.Next() {
		var exchange, instrument string
		var item generated.EvaluationFeedHealth
		if err = rows.Scan(&exchange, &instrument, &item.Eligible, &item.BookFresh, &item.ClockEligible,
			&item.LatestEventAt, &item.MessageCount, &item.QueueDropCount, &item.GapCount,
			&item.DecoderErrorCount); err != nil {
			return err
		}
		item.Exchange = generated.EvaluationFeedHealthExchange(exchange)
		item.Instrument = generated.EvaluationFeedHealthInstrument(instrument)
		items = append(items, item)
	}
	campaign.FeedHealth = &items
	return rows.Err()
}

func (store *OwnerConsoleStore) loadEvaluationShadow(ctx context.Context,
	campaign *generated.EvaluationCampaign) error {
	var shadow generated.EvaluationShadowProgress
	var state string
	err := store.pool.QueryRow(ctx, `SELECT state,valid_seconds,start_ordinal,last_processed_ordinal,
shared_capital_micros,protected_reserve_micros,member_ceiling_micros,reason_code
FROM evaluation_shadow_sessions WHERE campaign_id=$1`, campaign.Id).Scan(&state, &shadow.ValidSeconds,
		&shadow.StartOrdinal, &shadow.LastProcessedOrdinal, &shadow.SharedCapitalMicros,
		&shadow.ProtectedReserveMicros, &shadow.MemberCeilingMicros, &shadow.ReasonCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	shadow.State = generated.EvaluationShadowProgressState(state)
	rows, err := store.pool.Query(ctx, `SELECT member.id,checkpoint.strategy_id,member.configuration_key,
'shadow',member.capital_micros,member.repeat_ordinal,member.cost_stress_bps,checkpoint.state,
member.verdict,checkpoint.reason_code,COALESCE(encode(checkpoint.result_hash,'hex'),''),checkpoint.metrics_payload
FROM evaluation_shadow_member_checkpoints checkpoint JOIN evaluation_campaign_members member
ON member.campaign_id=checkpoint.campaign_id AND member.id=checkpoint.member_id
WHERE checkpoint.campaign_id=$1 ORDER BY checkpoint.strategy_id,checkpoint.member_id`, campaign.Id)
	if err != nil {
		return err
	}
	defer rows.Close()
	shadow.Members = make([]generated.EvaluationMatrixMember, 0, 4)
	for rows.Next() {
		item, scanErr := scanEvaluationMatrixMember(rows)
		if scanErr != nil {
			return scanErr
		}
		shadow.Members = append(shadow.Members, item)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	campaign.Shadow = &shadow
	return nil
}

func fmtHex(value []byte) string {
	const hex = "0123456789abcdef"
	out := make([]byte, len(value)*2)
	for i, b := range value {
		out[i*2] = hex[b>>4]
		out[i*2+1] = hex[b&15]
	}
	return string(out)
}

var _ console.EvaluationCampaignService = (*OwnerConsoleStore)(nil)
