package postgres

import (
	"context"
	"errors"
	"strconv"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"

	"github.com/jackc/pgx/v5"
)

const multiExchangeConsoleInventorySQL = `WITH positions AS (
  SELECT snapshot.decision_id||':'||snapshot.snapshot_role||':'||snapshot.base_asset id,
    snapshot.exchange_id,snapshot.base_asset asset,candidate.strategy_version_id,
    snapshot.ownership_account_id,decision.run_id,snapshot.base_before::text before,
    snapshot.base_after::text after,snapshot.base_after::text available,
    snapshot.band_state,snapshot.ownership_revision,candidate.recorded_at
  FROM cross_exchange_inventory_snapshots snapshot
  JOIN cross_exchange_candidates candidate ON candidate.decision_id=snapshot.decision_id
  JOIN decisions decision ON decision.id=snapshot.decision_id
  UNION ALL
  SELECT snapshot.decision_id||':'||snapshot.snapshot_role||':USDT',
    snapshot.exchange_id,'USDT',candidate.strategy_version_id,
    snapshot.ownership_account_id,decision.run_id,snapshot.usdt_before::text,
    snapshot.usdt_after::text,snapshot.usdt_after::text,snapshot.band_state,
    snapshot.ownership_revision,candidate.recorded_at
  FROM cross_exchange_inventory_snapshots snapshot
  JOIN cross_exchange_candidates candidate ON candidate.decision_id=snapshot.decision_id
  JOIN decisions decision ON decision.id=snapshot.decision_id
)
SELECT id,exchange_id,asset,strategy_version_id,ownership_account_id,run_id,
  before,after,available,'0'::text,band_state,ownership_revision,recorded_at
FROM positions
WHERE ($1='' OR id>$1)
  AND ($2='' OR exchange_id=$2)
  AND ($3='' OR asset=$3)
  AND ($4='' OR strategy_version_id=$4)
  AND ($5='' OR ownership_account_id=$5)
ORDER BY id LIMIT $6`

const multiExchangeConsoleRebalancingSQL = `SELECT recommendation.id,recommendation.method,
  recommendation.source_exchange_id,recommendation.source_asset_symbol,
  recommendation.destination_exchange_id,recommendation.destination_asset_symbol,
  recommendation.quantity::text,recommendation.total_cost::text,
  recommendation.minimum_duration_nanos,recommendation.maximum_duration_nanos,
  recommendation.risk_score::text,recommendation.warnings,
  recommendation.advisory_only,recommendation.recorded_at,
  coalesce(min(fact.observed_at),recommendation.recorded_at),
  coalesce(min(fact.expires_at),recommendation.recorded_at),
  CASE WHEN min(fact.confidence)>=0.8 THEN 'high'
    WHEN min(fact.confidence)>=0.5 THEN 'medium' ELSE 'low' END
FROM rebalancing_recommendations recommendation
LEFT JOIN rebalancing_recommendation_steps step
  ON step.recommendation_id=recommendation.id
LEFT JOIN rebalancing_route_facts fact
  ON fact.fact_set_id=step.fact_set_id AND fact.fact_id=step.fact_id
WHERE $1::timestamptz IS NULL OR recommendation.recorded_at<$1 OR
  (recommendation.recorded_at=$1 AND recommendation.id<$2)
GROUP BY recommendation.id
ORDER BY recommendation.recorded_at DESC,recommendation.id DESC LIMIT $3`

const multiExchangeConsoleRebalancingRouteSQL = `SELECT step.step_index,fact.fact_id,fact.fact_version,
  fact.fact_kind,fact.from_exchange_id,fact.from_asset_symbol,fact.to_exchange_id,
  fact.to_asset_symbol,fact.network,fact.fee_cost+fact.spread_cost+fact.depth_cost+
    fact.delay_cost+fact.network_fee_cost+fact.compatibility_cost+
    fact.volatility_risk_cost+fact.operational_risk_cost,
  fact.minimum_duration_nanos,fact.maximum_duration_nanos,fact.confidence,
  fact.warnings,fact.approved,fact.provenance_hash
FROM rebalancing_recommendation_steps step
JOIN rebalancing_route_facts fact
  ON fact.fact_set_id=step.fact_set_id AND fact.fact_id=step.fact_id
WHERE step.recommendation_id=$1 ORDER BY step.step_index`

// Inventory returns isolated virtual positions without cross-owner netting.
func (store *OwnerConsoleStore) Inventory(
	ctx context.Context, cursor string, limit int, filters console.InventoryFilters,
) (generated.InventoryPage, error) {
	position, err := decodeMultiExchangeConsoleStringCursor(store.cursor, "multi_exchange_console-inventory", cursor)
	if err != nil {
		return generated.InventoryPage{}, err
	}
	rows, err := store.pool.Query(ctx, multiExchangeConsoleInventorySQL,
		position, filters.Exchange, filters.Asset, filters.Strategy, filters.Portfolio, limit+1)
	if err != nil {
		return generated.InventoryPage{}, err
	}
	defer rows.Close()
	now := store.clock.Now().UTC
	items := make([]generated.InventoryPosition, 0, limit+1)
	for rows.Next() {
		var item generated.InventoryPosition
		var revision int64
		var observed time.Time
		if err = rows.Scan(&item.Id, &item.Exchange, &item.Asset, &item.StrategyVersion,
			&item.PortfolioId, &item.ExperimentId, &item.Before, &item.After, &item.Available,
			&item.Reserved, &item.Status, &revision, &observed); err != nil {
			return generated.InventoryPage{}, err
		}
		item.Revision = strconv.FormatInt(revision, 10)
		item.UpdatedAt = observed.UTC()
		item.Virtual = true
		item.Quality = multiExchangeConsoleQuality("cross_exchange_inventory_snapshots", observed,
			generated.QualityEvidenceConfidenceHigh, multiExchangeConsoleFreshness(now, observed))
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return generated.InventoryPage{}, err
	}
	snapshot, err := multiExchangeConsoleSnapshotRevision(ctx, store.pool)
	if err != nil {
		return generated.InventoryPage{}, err
	}
	page := generated.InventoryPage{Items: items, Revision: snapshot, SnapshotRevision: snapshot,
		CombinedBalance: false,
		IsolationNotice: "Virtual inventory is isolated by exchange, asset, strategy version, experiment, and portfolio. No combined balance exists."}
	if len(items) > limit {
		page.HasMore = true
		items = items[:limit]
		page.Items = items
		next := encodeMultiExchangeConsoleStringCursor(store.cursor, "multi_exchange_console-inventory", items[len(items)-1].Id)
		page.NextCursor = &next
	}
	return page, nil
}

// Rebalancing returns reviewed advisory recommendations with no execution authority.
func (store *OwnerConsoleStore) Rebalancing(
	ctx context.Context, cursor string, limit int,
) (generated.RebalancingPage, error) {
	var cursorTime time.Time
	var cursorID string
	var err error
	if cursor != "" {
		cursorTime, cursorID, err = decodeMultiExchangeConsoleTimeCursor(store.cursor, "multi_exchange_console-rebalancing", cursor)
		if err != nil {
			return generated.RebalancingPage{}, err
		}
	}
	rows, err := store.pool.Query(ctx, multiExchangeConsoleRebalancingSQL,
		nullableMultiExchangeConsoleTime(cursorTime), cursorID, limit+1)
	if err != nil {
		return generated.RebalancingPage{}, err
	}
	defer rows.Close()
	items, err := scanMultiExchangeConsoleRebalancingRows(rows, store.clock.Now().UTC, limit+1)
	if err != nil {
		return generated.RebalancingPage{}, err
	}
	snapshot, err := multiExchangeConsoleSnapshotRevision(ctx, store.pool)
	if err != nil {
		return generated.RebalancingPage{}, err
	}
	page := generated.RebalancingPage{Items: items, Revision: snapshot,
		SnapshotRevision: snapshot, ExecutionAvailable: false}
	if len(items) > limit {
		page.HasMore = true
		items = items[:limit]
		page.Items = items
		last := items[len(items)-1]
		next := encodeMultiExchangeConsoleTimeCursor(store.cursor, "multi_exchange_console-rebalancing", last.RecordedAt, last.Id)
		page.NextCursor = &next
	}
	return page, nil
}

func scanMultiExchangeConsoleRebalancingRows(rows pgx.Rows, now time.Time, capacity int) (
	[]generated.RebalancingSummary, error,
) {
	var err error
	items := make([]generated.RebalancingSummary, 0, capacity)
	for rows.Next() {
		var item generated.RebalancingSummary
		var advisory bool
		var observed, expires time.Time
		var confidence string
		if err = rows.Scan(&item.Id, &item.Method, &item.SourceExchange, &item.SourceAsset,
			&item.DestinationExchange, &item.DestinationAsset, &item.Quantity, &item.TotalCost,
			&item.MinimumDurationNanos, &item.MaximumDurationNanos, &item.RiskScore,
			&item.Warnings, &advisory, &item.RecordedAt, &observed, &expires,
			&confidence); err != nil {
			return nil, err
		}
		item.AdvisoryOnly = generated.RebalancingSummaryAdvisoryOnly(advisory)
		item.Revision = strconv.FormatInt(item.RecordedAt.UnixNano(), 10)
		item.Quality = multiExchangeConsoleRebalancingQuality(now, observed, expires, confidence)
		items = append(items, item)
	}
	return items, rows.Err()
}

// RebalancingDetail returns reviewed route facts and a manual-only checklist.
func (store *OwnerConsoleStore) RebalancingDetail(
	ctx context.Context, id string,
) (generated.RebalancingDetail, error) {
	page, err := store.multiExchangeConsoleRebalancingByID(ctx, id)
	if err != nil {
		return generated.RebalancingDetail{}, err
	}
	rows, err := store.pool.Query(ctx, multiExchangeConsoleRebalancingRouteSQL, id)
	if err != nil {
		return generated.RebalancingDetail{}, err
	}
	route := []generated.RebalancingRouteStep{}
	for rows.Next() {
		var item generated.RebalancingRouteStep
		var network *string
		if err = rows.Scan(&item.Index, &item.FactId, &item.FactVersion, &item.Role,
			&item.FromExchange, &item.FromAsset, &item.ToExchange, &item.ToAsset,
			&network, &item.ExpectedCost, &item.MinimumDurationNanos,
			&item.MaximumDurationNanos, &item.Confidence, &item.Warnings,
			&item.Approved, &item.ProvenanceHash); err != nil {
			rows.Close()
			return generated.RebalancingDetail{}, err
		}
		item.Network = network
		route = append(route, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return generated.RebalancingDetail{}, err
	}
	checkRows, err := store.pool.Query(ctx, `SELECT step_index,instruction
	FROM rebalancing_checklist_steps WHERE recommendation_id=$1 ORDER BY step_index`, id)
	if err != nil {
		return generated.RebalancingDetail{}, err
	}
	defer checkRows.Close()
	checklist := []generated.ManualChecklistStep{}
	for checkRows.Next() {
		var item generated.ManualChecklistStep
		if err = checkRows.Scan(&item.Index, &item.Instruction); err != nil {
			return generated.RebalancingDetail{}, err
		}
		item.ManualOnly = true
		checklist = append(checklist, item)
	}
	return generated.RebalancingDetail{Summary: page, Route: route, Checklist: checklist,
		ExecutionAvailable: false}, checkRows.Err()
}

func (store *OwnerConsoleStore) multiExchangeConsoleRebalancingByID(
	ctx context.Context, id string,
) (generated.RebalancingSummary, error) {
	var item generated.RebalancingSummary
	var advisory bool
	var observed, expires time.Time
	var confidence string
	err := store.pool.QueryRow(ctx, `SELECT recommendation.id,recommendation.method,
	  recommendation.source_exchange_id,recommendation.source_asset_symbol,
	  recommendation.destination_exchange_id,recommendation.destination_asset_symbol,
	  recommendation.quantity::text,recommendation.total_cost::text,
	  recommendation.minimum_duration_nanos,recommendation.maximum_duration_nanos,
	  recommendation.risk_score::text,recommendation.warnings,
	  recommendation.advisory_only,recommendation.recorded_at,
	  coalesce(min(fact.observed_at),recommendation.recorded_at),
	  coalesce(min(fact.expires_at),recommendation.recorded_at),
	  CASE WHEN min(fact.confidence)>=0.8 THEN 'high'
	    WHEN min(fact.confidence)>=0.5 THEN 'medium' ELSE 'low' END
	FROM rebalancing_recommendations recommendation
	LEFT JOIN rebalancing_recommendation_steps step
	  ON step.recommendation_id=recommendation.id
	LEFT JOIN rebalancing_route_facts fact
	  ON fact.fact_set_id=step.fact_set_id AND fact.fact_id=step.fact_id
	WHERE recommendation.id=$1 GROUP BY recommendation.id`, id).Scan(
		&item.Id, &item.Method, &item.SourceExchange, &item.SourceAsset,
		&item.DestinationExchange, &item.DestinationAsset, &item.Quantity, &item.TotalCost,
		&item.MinimumDurationNanos, &item.MaximumDurationNanos, &item.RiskScore,
		&item.Warnings, &advisory, &item.RecordedAt, &observed, &expires, &confidence)
	if errors.Is(err, pgx.ErrNoRows) {
		return generated.RebalancingSummary{}, console.ErrNotFound
	}
	if err != nil {
		return generated.RebalancingSummary{}, err
	}
	item.AdvisoryOnly = generated.RebalancingSummaryAdvisoryOnly(advisory)
	item.Revision = strconv.FormatInt(item.RecordedAt.UnixNano(), 10)
	item.Quality = multiExchangeConsoleRebalancingQuality(store.clock.Now().UTC, observed, expires, confidence)
	return item, nil
}

func multiExchangeConsoleRebalancingQuality(now, observed, expires time.Time, confidence string) generated.QualityEvidence {
	label := generated.QualityEvidenceConfidence(confidence)
	freshness := multiExchangeConsoleFreshness(now, observed)
	if !expires.After(now) {
		freshness = generated.QualityEvidenceFreshnessExpired
	}
	result := multiExchangeConsoleQuality("rebalancing_route_facts", observed, label, freshness)
	expires = expires.UTC()
	result.ExpiresAt = &expires
	return result
}
