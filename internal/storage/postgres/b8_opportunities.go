package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"

	"github.com/jackc/pgx/v5"
)

const b8OpportunitiesCTE = `WITH opportunities AS (
  SELECT candidate.decision_id id,'triangular'::text kind,candidate.strategy_version_id,
    candidate.cycle label,candidate.exchange_id exchange,
    ''::text buy_exchange,''::text sell_exchange,
    ''::text instrument,candidate.start_quantity::text maximum_size,
    candidate.start_quantity::text tested_size,candidate.expected_edge::text gross_metric,
    candidate.expected_net::text net_metric,candidate.expected_net::text expected_profit,
    candidate.worst_net::text worst_profit,candidate.recorded_at,
    coalesce(lifetime.total_lifetime_nanos,0) lifetime_nanos,
    coalesce(outcome.outcome,'') outcome,coalesce(outcome.quarantined,false) quarantined
  FROM triangular_candidates candidate
  LEFT JOIN triangular_opportunity_lifetimes lifetime ON lifetime.decision_id=candidate.decision_id
  LEFT JOIN triangular_simulation_outcomes outcome ON outcome.decision_id=candidate.decision_id
  UNION ALL
  SELECT candidate.decision_id,'cross_exchange',candidate.strategy_version_id,
    candidate.direction,candidate.buy_exchange_id,candidate.buy_exchange_id,
    candidate.sell_exchange_id,candidate.instrument_id,candidate.quote_budget::text,
    candidate.quote_budget::text,candidate.gross_spread::text,
    candidate.expected_closed_cycle_profit::text,candidate.expected_closed_cycle_profit::text,
    candidate.worst_closed_cycle_profit::text,candidate.recorded_at,
    candidate.expires_offset_nanos-candidate.first_detected_offset_nanos,
    coalesce(outcome.outcome,''),coalesce(outcome.quarantined,false)
  FROM cross_exchange_candidates candidate
  LEFT JOIN cross_exchange_simulation_outcomes outcome ON outcome.decision_id=candidate.decision_id
)
SELECT id,kind,strategy_version_id,label,exchange,buy_exchange,sell_exchange,instrument,
  maximum_size,tested_size,gross_metric,net_metric,expected_profit,worst_profit,
  recorded_at,lifetime_nanos,outcome,quarantined
FROM opportunities`

const b8OpportunitiesPageSQL = b8OpportunitiesCTE + `
WHERE ($1='' OR kind=$1) AND (
  $2::timestamptz IS NULL OR recorded_at<$2 OR (recorded_at=$2 AND id<$3)
)
ORDER BY recorded_at DESC,id DESC LIMIT $4`

const b8OpportunityByIDSQL = b8OpportunitiesCTE + ` WHERE id=$1`

const b8TriangularLegsSQL = `SELECT leg.leg_index,candidate.exchange_id,leg.instrument_id,leg.side,
  leg.input_quantity::text,leg.trade_quantity::text,leg.gross_output::text,
  leg.net_output::text,leg.fee_asset,leg.fee_quantity::text,
  leg.fee_quote_equivalent::text,leg.vwap::text,leg.spread_depth_cost::text,
  leg.source_asset,leg.target_asset,0::bigint,'simulated'::text,leg.book_version
FROM triangular_candidate_legs leg
JOIN triangular_candidates candidate ON candidate.decision_id=leg.decision_id
WHERE leg.decision_id=$1 ORDER BY leg.leg_index`

const b8CrossExchangeLegsSQL = `SELECT leg.leg_index,leg.exchange_id,leg.instrument_id,leg.side,
  leg.input_quantity::text,leg.trade_quantity::text,leg.gross_output::text,
  leg.net_output::text,leg.fee_asset,leg.fee_quantity::text,
  leg.fee_quote_equivalent::text,leg.vwap::text,leg.spread_depth_cost::text,
  ''::text,''::text,coalesce(sim.arrival_offset_nanos,0),
  coalesce(sim.final_state,'simulated'),leg.book_version
FROM cross_exchange_candidate_legs leg
LEFT JOIN cross_exchange_simulation_legs sim
  ON sim.decision_id=leg.decision_id AND sim.leg_index=leg.leg_index
WHERE leg.decision_id=$1 ORDER BY leg.leg_index`

type b8OpportunityRow struct {
	id, kind, strategy, label, exchange, buyExchange, sellExchange, instrument   string
	maximumSize, testedSize, grossMetric, netMetric, expectedProfit, worstProfit string
	recordedAt                                                                   time.Time
	lifetimeNanos                                                                int64
	outcome                                                                      string
	quarantined                                                                  bool
}

// Opportunities returns immutable triangular and cross-exchange simulation evidence.
func (store *A11ConsoleStore) Opportunities(
	ctx context.Context, cursor string, limit int, kind string,
) (generated.OpportunityPage, error) {
	var cursorTime time.Time
	var cursorID string
	var err error
	if cursor != "" {
		cursorTime, cursorID, err = decodeB8TimeCursor(store.cursor, "b8-opportunities-"+kind, cursor)
		if err != nil {
			return generated.OpportunityPage{}, err
		}
	}
	rows, err := store.pool.Query(ctx, b8OpportunitiesPageSQL,
		kind, nullableB8Time(cursorTime), cursorID, limit+1)
	if err != nil {
		return generated.OpportunityPage{}, err
	}
	defer rows.Close()
	now := store.clock.Now().UTC
	items := make([]generated.OpportunitySummary, 0, limit+1)
	for rows.Next() {
		var row b8OpportunityRow
		if err = rows.Scan(&row.id, &row.kind, &row.strategy, &row.label, &row.exchange,
			&row.buyExchange, &row.sellExchange, &row.instrument, &row.maximumSize,
			&row.testedSize, &row.grossMetric, &row.netMetric, &row.expectedProfit,
			&row.worstProfit, &row.recordedAt, &row.lifetimeNanos, &row.outcome,
			&row.quarantined); err != nil {
			return generated.OpportunityPage{}, err
		}
		items = append(items, b8OpportunitySummary(row, now))
	}
	if err = rows.Err(); err != nil {
		return generated.OpportunityPage{}, err
	}
	snapshot, err := b8SnapshotRevision(ctx, store.pool)
	if err != nil {
		return generated.OpportunityPage{}, err
	}
	page := generated.OpportunityPage{Items: items, Revision: snapshot, SnapshotRevision: snapshot}
	if len(items) > limit {
		page.HasMore = true
		items = items[:limit]
		page.Items = items
		last := items[len(items)-1]
		next := encodeB8TimeCursor(store.cursor, "b8-opportunities-"+kind, last.RecordedAt, last.Id)
		page.NextCursor = &next
	}
	return page, nil
}

func nullableB8Time(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func b8OpportunitySummary(row b8OpportunityRow, now time.Time) generated.OpportunitySummary {
	status := generated.OpportunitySummaryStatusDetected
	switch {
	case row.quarantined:
		status = generated.OpportunitySummaryStatusQuarantined
	case row.outcome != "":
		status = generated.OpportunitySummaryStatusSimulated
	}
	result := generated.OpportunitySummary{
		Id: row.id, Kind: generated.OpportunitySummaryKind(row.kind), Label: row.label,
		MaximumSize: row.maximumSize, TestedSize: row.testedSize,
		GrossMetric: row.grossMetric, NetMetric: row.netMetric,
		ExpectedProfit: row.expectedProfit, WorstCaseProfit: row.worstProfit,
		RecordedAt: row.recordedAt.UTC(), StrategyVersion: row.strategy,
		SimulationOnly: true, Status: status,
		Quality: b8Quality("immutable_simulation_evidence", row.recordedAt,
			generated.QualityEvidenceConfidenceHigh, generated.QualityEvidenceFreshnessHistorical),
		Revision: strconv.FormatInt(row.recordedAt.UnixNano(), 10),
	}
	age := generated.Revision(b8PositiveAge(now, row.recordedAt))
	lifetime := generated.Revision(strconv.FormatInt(row.lifetimeNanos, 10))
	result.OpportunityAgeNanos, result.LifetimeNanos = &age, &lifetime
	if row.kind == "triangular" {
		result.Exchange = &row.exchange
		path := []string{"USDT", "BTC", "ETH", "USDT"}
		if row.label == "USDT-ETH-BTC-USDT" {
			path = []string{"USDT", "ETH", "BTC", "USDT"}
		}
		result.CyclePath = &path
	} else {
		result.BuyExchange, result.SellExchange, result.Instrument =
			&row.buyExchange, &row.sellExchange, &row.instrument
	}
	return result
}

// Opportunity returns one simulation candidate with immutable leg and recovery evidence.
func (store *A11ConsoleStore) Opportunity(ctx context.Context, id string) (generated.OpportunityDetail, error) {
	summary, kind, err := store.b8OpportunityByID(ctx, id)
	if err != nil {
		return generated.OpportunityDetail{}, err
	}
	legs, err := store.b8OpportunityLegs(ctx, id, kind)
	if err != nil {
		return generated.OpportunityDetail{}, err
	}
	inventory, err := store.b8OpportunityInventory(ctx, id, kind)
	if err != nil {
		return generated.OpportunityDetail{}, err
	}
	recovery, costs, outcomeAt, outcomeCorrelation, err := store.b8OpportunityOutcome(ctx, id, kind)
	if err != nil {
		return generated.OpportunityDetail{}, err
	}
	timeline := []generated.EvidenceTimelineEvent{{
		Index: 0, EventType: kind + ".candidate", Label: "Immutable candidate recorded",
		OccurredAt: summary.RecordedAt, Revision: summary.Revision, CorrelationId: id,
	}}
	if !outcomeAt.IsZero() {
		timeline = append(timeline, generated.EvidenceTimelineEvent{
			Index: 1, EventType: kind + ".simulation", Label: "Simulation outcome recorded",
			OccurredAt: outcomeAt.UTC(), Revision: strconv.FormatInt(outcomeAt.UnixNano(), 10),
			CorrelationId: outcomeCorrelation,
		})
	}
	return generated.OpportunityDetail{Summary: summary, Legs: legs, Inventory: inventory,
		Recovery: recovery, CostAttribution: costs, Timeline: timeline, RawEvidenceAvailable: true}, nil
}

func (store *A11ConsoleStore) b8OpportunityByID(
	ctx context.Context, id string,
) (generated.OpportunitySummary, string, error) {
	var row b8OpportunityRow
	err := store.pool.QueryRow(ctx, b8OpportunityByIDSQL, id).Scan(&row.id, &row.kind, &row.strategy,
		&row.label, &row.exchange, &row.buyExchange, &row.sellExchange, &row.instrument,
		&row.maximumSize, &row.testedSize, &row.grossMetric, &row.netMetric,
		&row.expectedProfit, &row.worstProfit, &row.recordedAt, &row.lifetimeNanos,
		&row.outcome, &row.quarantined)
	if errors.Is(err, pgx.ErrNoRows) {
		return generated.OpportunitySummary{}, "", console.ErrNotFound
	}
	if err != nil {
		return generated.OpportunitySummary{}, "", err
	}
	return b8OpportunitySummary(row, store.clock.Now().UTC), row.kind, nil
}

func (store *A11ConsoleStore) b8OpportunityLegs(
	ctx context.Context, id, kind string,
) ([]generated.OpportunityLeg, error) {
	query := b8TriangularLegsSQL
	if kind == "cross_exchange" {
		query = b8CrossExchangeLegsSQL
	}
	rows, err := store.pool.Query(ctx, query, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []generated.OpportunityLeg{}
	for rows.Next() {
		var item generated.OpportunityLeg
		var source, target string
		var arrival, revision int64
		if err = rows.Scan(&item.Index, &item.Exchange, &item.Instrument, &item.Side,
			&item.InputQuantity, &item.TradeQuantity, &item.GrossOutput, &item.NetOutput,
			&item.FeeAsset, &item.FeeQuantity, &item.FeeQuoteEquivalent, &item.Vwap,
			&item.DepthCost, &source, &target, &arrival, &item.State, &revision); err != nil {
			return nil, err
		}
		item.Revision = strconv.FormatInt(revision, 10)
		if source != "" {
			item.SourceAsset, item.TargetAsset = &source, &target
		}
		if arrival > 0 {
			value := generated.Revision(strconv.FormatInt(arrival, 10))
			item.ArrivalOffsetNanos = &value
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *A11ConsoleStore) b8OpportunityInventory(
	ctx context.Context, id, kind string,
) ([]generated.InventoryImpact, error) {
	if kind != "cross_exchange" {
		return []generated.InventoryImpact{}, nil
	}
	rows, err := store.pool.Query(ctx, `SELECT exchange_id,base_asset,base_before::text,
	  base_after::text,band_state,natural_reverse_preferred
	FROM cross_exchange_inventory_snapshots WHERE decision_id=$1 ORDER BY snapshot_role`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []generated.InventoryImpact{}
	for rows.Next() {
		var item generated.InventoryImpact
		if err = rows.Scan(&item.Exchange, &item.Asset, &item.Before, &item.After,
			&item.BandState, &item.NaturalReversePreferred); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *A11ConsoleStore) b8OpportunityOutcome(
	ctx context.Context, id, kind string,
) (generated.RecoveryAnalysis, map[string]string, time.Time, string, error) {
	if kind == "triangular" {
		return store.b8TriangularOpportunityOutcome(ctx, id)
	}
	return store.b8CrossExchangeOpportunityOutcome(ctx, id)
}

func (store *A11ConsoleStore) b8TriangularOpportunityOutcome(
	ctx context.Context, id string,
) (generated.RecoveryAnalysis, map[string]string, time.Time, string, error) {
	recovery := generated.RecoveryAnalysis{Disposition: "not_simulated",
		Explanation: "No immutable simulation outcome has been recorded.", RecoveryLoss: "0"}
	costs := map[string]string{}
	var recorded time.Time
	var correlation string
	err := store.pool.QueryRow(ctx, `SELECT outcome,recovery_attempted,recovery_succeeded,
	  quarantined,recovery_loss::text,recorded_at,correlation_id
	FROM triangular_simulation_outcomes WHERE decision_id=$1`, id).Scan(
		&recovery.Disposition, &recovery.Attempted, &recovery.Succeeded,
		&recovery.Quarantined, &recovery.RecoveryLoss, &recorded, &correlation)
	if errors.Is(err, pgx.ErrNoRows) {
		return recovery, costs, recorded, correlation, nil
	}
	if err != nil {
		return recovery, nil, time.Time{}, "", err
	}
	var expectedEdge, worstEdge, safetyMargin string
	err = store.pool.QueryRow(ctx, `SELECT expected_edge::text,worst_edge::text,
	  additional_safety_margin::text FROM triangular_candidates WHERE decision_id=$1`, id).
		Scan(&expectedEdge, &worstEdge, &safetyMargin)
	costs["expected_edge"], costs["worst_edge"], costs["safety_margin"] =
		expectedEdge, worstEdge, safetyMargin
	recovery.Explanation = fmt.Sprintf("Triangular simulation disposition: %s.", recovery.Disposition)
	return recovery, costs, recorded, correlation, err
}

func (store *A11ConsoleStore) b8CrossExchangeOpportunityOutcome(
	ctx context.Context, id string,
) (generated.RecoveryAnalysis, map[string]string, time.Time, string, error) {
	recovery := generated.RecoveryAnalysis{Disposition: "not_simulated",
		Explanation: "No immutable simulation outcome has been recorded.", RecoveryLoss: "0"}
	costs := map[string]string{}
	var recorded time.Time
	var correlation string
	var retryAttempted, retrySucceeded, unwindAttempted, unwindSucceeded bool
	err := store.pool.QueryRow(ctx, `SELECT outcome,retry_attempted,retry_succeeded,
	  unwind_attempted,unwind_succeeded,quarantined,recovery_loss::text,recorded_at,
	  correlation_id FROM cross_exchange_simulation_outcomes WHERE decision_id=$1`, id).Scan(
		&recovery.Disposition, &retryAttempted, &retrySucceeded, &unwindAttempted,
		&unwindSucceeded, &recovery.Quarantined, &recovery.RecoveryLoss, &recorded, &correlation)
	if errors.Is(err, pgx.ErrNoRows) {
		return recovery, costs, recorded, correlation, nil
	}
	if err != nil {
		return recovery, nil, time.Time{}, "", err
	}
	recovery.Attempted = retryAttempted || unwindAttempted
	recovery.Succeeded = retrySucceeded || unwindSucceeded
	recovery.Explanation = fmt.Sprintf("Cross-exchange simulation disposition: %s.", recovery.Disposition)
	var buyFee, sellFee, spreadDepth, latency, recoveryCost string
	var inventoryReplacement, naturalReversal, advisoryRebalancing string
	var exchangeConcentration, stablecoinConcentration string
	err = store.pool.QueryRow(ctx, `SELECT buy_fee::text,sell_fee::text,
	  spread_depth_cost::text,latency_deterioration::text,recovery_allowance::text,
	  marginal_inventory_replacement::text,natural_reversal_cost::text,
	  advisory_rebalancing_cost::text,exchange_concentration_penalty::text,
	  usdt_venue_concentration_penalty::text
	FROM cross_exchange_candidates WHERE decision_id=$1`, id).Scan(
		&buyFee, &sellFee, &spreadDepth, &latency, &recoveryCost,
		&inventoryReplacement, &naturalReversal, &advisoryRebalancing,
		&exchangeConcentration, &stablecoinConcentration)
	costs["buy_fee"], costs["sell_fee"], costs["spread_depth"] = buyFee, sellFee, spreadDepth
	costs["latency"], costs["recovery"] = latency, recoveryCost
	costs["inventory_replacement"], costs["natural_reversal"] = inventoryReplacement, naturalReversal
	costs["advisory_rebalancing"] = advisoryRebalancing
	costs["exchange_concentration"] = exchangeConcentration
	costs["stablecoin_concentration"] = stablecoinConcentration
	return recovery, costs, recorded, correlation, err
}
