package operationalReadiness

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresTelemetrySource reads one coherent aggregate through a read-only
// repeatable-read transaction. It never writes or returns row identifiers.
type PostgresTelemetrySource struct{ Pool *pgxpool.Pool }

func (source PostgresTelemetrySource) Observe(
	ctx context.Context,
	windowStart time.Time,
	observedAt time.Time,
) (DatabaseTelemetry, error) {
	if source.Pool == nil || windowStart.IsZero() || observedAt.IsZero() || !windowStart.Before(observedAt) {
		return DatabaseTelemetry{}, fmt.Errorf("operational_readiness_database_source_invalid")
	}
	tx, err := source.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return DatabaseTelemetry{}, fmt.Errorf("operational_readiness_database_source_unavailable")
	}
	defer tx.Rollback(ctx)
	result := DatabaseTelemetry{}
	if err = tx.QueryRow(ctx, "SELECT CURRENT_TIMESTAMP").Scan(&result.ObservedAt); err != nil {
		return DatabaseTelemetry{}, fmt.Errorf("operational_readiness_database_source_unavailable")
	}
	var stale, gaps, lost, doublePosted, duplicates, unbalanced, production int64
	if err = tx.QueryRow(ctx, `
SELECT
  count(*) FILTER (WHERE stale_data),
  count(*) FILTER (WHERE gap)
FROM sandbox_strategy_risk_observations
WHERE observed_at >= $1 AND observed_at <= $2`, windowStart.UTC(), result.ObservedAt).Scan(&stale, &gaps); err != nil {
		return DatabaseTelemetry{}, fmt.Errorf("operational_readiness_database_source_unavailable")
	}
	if err = tx.QueryRow(ctx, `
SELECT
  count(*) FILTER (WHERE lost_fill),
  coalesce(sum(double_posted_fills),0)
FROM sandbox_qualification_order_observations
WHERE approved_at >= $1 AND approved_at <= $2`, windowStart.UTC(), result.ObservedAt).Scan(&lost, &doublePosted); err != nil {
		return DatabaseTelemetry{}, fmt.Errorf("operational_readiness_database_source_unavailable")
	}
	if err = tx.QueryRow(ctx, `
SELECT coalesce(sum(request_count-1),0)
FROM (
  SELECT count(*)::bigint AS request_count
  FROM sandbox_runtime_authenticated_request_evidence
  WHERE recorded_at >= $1 AND recorded_at <= $2 AND method='POST'
    AND path IN ('/api/v3/order','/v5/order/create')
  GROUP BY request_hash HAVING count(*)>1
) repeated`, windowStart.UTC(), result.ObservedAt).Scan(&duplicates); err != nil {
		return DatabaseTelemetry{}, fmt.Errorf("operational_readiness_database_source_unavailable")
	}
	if err = tx.QueryRow(ctx, `
SELECT count(*) FROM (
  SELECT transaction_id,asset_symbol
  FROM ledger_entries
  GROUP BY transaction_id,asset_symbol
  HAVING sum(CASE direction WHEN 'debit' THEN quantity ELSE -quantity END)<>0
) imbalance`).Scan(&unbalanced); err != nil {
		return DatabaseTelemetry{}, fmt.Errorf("operational_readiness_database_source_unavailable")
	}
	if err = tx.QueryRow(ctx, `
SELECT count(*)
FROM sandbox_runtime_exchange_accounts
WHERE environment NOT IN ('testnet','demo')`).Scan(&production); err != nil {
		return DatabaseTelemetry{}, fmt.Errorf("operational_readiness_database_source_unavailable")
	}
	if err = tx.QueryRow(ctx, `
SELECT level,observed_at
FROM owner_console_storage_pressure_state
WHERE scope_id='market-data'`).Scan(&result.DiskLevel, &result.DiskObservedAt); err != nil {
		return DatabaseTelemetry{}, fmt.Errorf("operational_readiness_database_source_unavailable")
	}
	values := []*uint64{
		&result.StaleDecisions, &result.UninvalidatedGaps, &result.DuplicateOrders,
		&result.LostFills, &result.DoublePostedFills, &result.UnbalancedJournals,
	}
	for index, value := range []int64{stale, gaps, duplicates, lost, doublePosted, unbalanced} {
		if value < 0 {
			return DatabaseTelemetry{}, fmt.Errorf("operational_readiness_database_source_invalid")
		}
		*values[index] = uint64(value)
	}
	result.ObservedAt = result.ObservedAt.UTC()
	result.DiskObservedAt = result.DiskObservedAt.UTC()
	result.ProductionTargetObserved = production > 0
	if err = tx.Commit(ctx); err != nil {
		return DatabaseTelemetry{}, fmt.Errorf("operational_readiness_database_source_unavailable")
	}
	return result, nil
}
