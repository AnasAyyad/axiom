package postgres

import (
	"os"
	"strings"
	"testing"
)

func TestReviewedQueriesCoverA4RepositoryBoundaries(t *testing.T) {
	files := []string{"queries/accounting.sql", "queries/catalog.sql", "queries/coordination.sql", "queries/a8_execution.sql", "queries/a9_portfolio_risk.sql"}
	var source strings.Builder
	for _, file := range files {
		contents, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		source.Write(contents)
	}
	for _, query := range []string{
		"LockVirtualBalance", "ReserveVirtualBalance", "InsertReservation", "InsertJournalTransaction",
		"RebuildAccountProjection", "InsertMarketDataSegment", "InsertDatasetGap", "TransitionDatasetManifest",
		"InsertRun", "TransitionRun", "LatestRunCheckpoint", "InsertAuditEvent", "ConsumeInbox", "InsertOutbox",
		"AdvanceConsumerCursor", "ClaimNextJob", "RenewJobClaim", "CompleteJob", "AcquireLease", "RenewLease",
		"InsertRunManifest", "InsertCanonicalOutput", "ReduceCanonicalOrder", "InsertCanonicalFill",
		"InsertFillJournalPosting", "InsertA8Checkpoint", "UpdateVirtualBalanceProjection",
		"UpsertPositionProjection", "UpsertProjectionRevision", "SettleReservationFill",
		"InsertPortfolioOwnership", "InsertA9AccountSnapshot", "InsertAllocationCandidate",
		"InsertAllocationScoreComponent", "ReserveLiquidityDomain", "InsertLiquidityReservation",
		"CloseAllocationCandidate", "SettleAllocationCandidateFill", "CloseLiquidityReservation",
		"SettleLiquidityReservationFill", "ReleaseLiquidityDomain", "UpdateLiquidityDomainProjection",
		"InsertRiskPolicy", "InsertRiskPolicyLimits", "InsertRiskStateEvent",
		"InsertA9RiskEvaluation", "InsertRiskEvaluationPolicy", "InsertCircuitBreakerEvent",
		"InsertA9ReconciliationCase", "InsertReconciliationDifference", "QuarantineScope",
		"InsertStartupRecoveryAttempt", "InsertStartupRecoveryEvidence", "CompleteStartupRecoveryAttempt",
	} {
		if !strings.Contains(source.String(), "-- name: "+query+" ") {
			t.Fatalf("reviewed query missing: %s", query)
		}
	}
	for _, required := range []string{"first_source_sequence", "last_source_sequence", "FOR UPDATE SKIP LOCKED"} {
		if !strings.Contains(source.String(), required) {
			t.Fatalf("query invariant missing: %s", required)
		}
	}
}
