package postgres

import (
	"context"

	"axiom/internal/api/generated"
)

const sandboxRuntimeConsoleQualificationSLOSQL = `
SELECT
 count(*)::bigint,
 coalesce(max(critical_alert_latency_ms),0),
 coalesce(max(recovery_duration_ms),0),
 coalesce(max(duplicate_creates),0),
 coalesce(max(lost_fills),0),
 coalesce(max(double_posted_fills),0),
 coalesce(max(unknown_orders),0),
 coalesce(max(reconciliation_mismatches),0),
 coalesce(max(suspense_items),0),
 coalesce(max(reconnects),0),
 coalesce(max(restarts),0),
 coalesce((
   SELECT resident_memory_bytes FROM sandbox_qualification_samples
   WHERE run_id=$1 ORDER BY sample_ordinal LIMIT 1
 ),0),
 coalesce((
   SELECT resident_memory_bytes FROM sandbox_qualification_samples
   WHERE run_id=$1 ORDER BY sample_ordinal DESC LIMIT 1
 ),0),
 coalesce(bool_and(all_accounts_fresh),false),
 coalesce(bool_and(all_leases_held),false),
 coalesce(bool_and(persistence_healthy),false),
 coalesce(bool_and(restart_safe),false),
 coalesce(bool_and(entry_safe),false),
 coalesce(bool_or(production_target_observed),false),
 coalesce(max(daily_submitted_microunits),0),
 coalesce(max(largest_order_microunits),0),
 coalesce(max(maximum_account_open),0),
 coalesce(max(global_open),0)
FROM sandbox_qualification_samples WHERE run_id=$1`

type sandboxQualificationSLOFacts struct {
	firstMemory, lastMemory                               int64
	allFresh, allLeases, persistence                      bool
	restartSafe, entrySafe, productionTarget              bool
	dailySubmitted, largestOrder, accountOpen, globalOpen int64
}

func (store *OwnerConsoleStore) sandboxRuntimeConsoleQualificationSLO(
	ctx context.Context,
	runID string,
) (generated.SandboxSLOSummary, error) {
	var result generated.SandboxSLOSummary
	var facts sandboxQualificationSLOFacts
	err := store.pool.QueryRow(ctx, sandboxRuntimeConsoleQualificationSLOSQL, runID).Scan(
		&result.Samples,
		&result.CriticalAlertLatencyMs,
		&result.RecoveryDurationMs,
		&result.DuplicateCreates,
		&result.LostFills,
		&result.DoublePostedFills,
		&result.UnknownOrders,
		&result.ReconciliationMismatches,
		&result.SuspenseItems,
		&result.Reconnects,
		&result.Restarts,
		&facts.firstMemory, &facts.lastMemory,
		&facts.allFresh, &facts.allLeases, &facts.persistence,
		&facts.restartSafe, &facts.entrySafe, &facts.productionTarget,
		&facts.dailySubmitted, &facts.largestOrder,
		&facts.accountOpen, &facts.globalOpen,
	)
	if err != nil {
		return generated.SandboxSLOSummary{}, err
	}
	result.ResidentMemoryDeltaBytes = facts.lastMemory - facts.firstMemory
	result.PositiveMemoryLeakTrend = facts.firstMemory > 0 &&
		facts.lastMemory > facts.firstMemory+64*1024*1024 &&
		facts.lastMemory > facts.firstMemory+facts.firstMemory/10
	result.Passing = sandboxQualificationSLOPassing(result, facts)
	return result, nil
}

func sandboxQualificationSLOPassing(result generated.SandboxSLOSummary, facts sandboxQualificationSLOFacts) bool {
	return result.Samples > 0 &&
		result.CriticalAlertLatencyMs <= 5000 &&
		result.RecoveryDurationMs <= 600000 &&
		result.DuplicateCreates == 0 &&
		result.LostFills == 0 &&
		result.DoublePostedFills == 0 &&
		result.UnknownOrders == 0 &&
		result.ReconciliationMismatches == 0 &&
		result.SuspenseItems == 0 &&
		facts.allFresh && facts.allLeases && facts.persistence &&
		facts.restartSafe && facts.entrySafe &&
		!facts.productionTarget && facts.dailySubmitted <= 50_000_000 &&
		facts.largestOrder <= 10_000_000 &&
		facts.accountOpen <= 1 && facts.globalOpen <= 2 &&
		!result.PositiveMemoryLeakTrend
}
