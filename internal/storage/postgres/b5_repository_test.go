package postgres

import (
	"fmt"
	"testing"
	"time"

	"axiom/internal/storage/postgres/generated"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestB5RepositoryRejectsIncompleteCandidateOutcomeAndClaimBeforeDatabase(t *testing.T) {
	repository := &B5Repository{}
	if err := repository.RecordCandidate(t.Context(), B5CandidateWrite{}); err == nil {
		t.Fatal("incomplete B5 candidate reached the database")
	}
	if err := repository.RecordOutcome(t.Context(), B5OutcomeWrite{}); err == nil {
		t.Fatal("incomplete B5 outcome reached the database")
	}
	if err := repository.Claim(t.Context(), generated.ClaimB5ResourcesParams{}); err == nil {
		t.Fatal("incomplete B5 claim reached the database")
	}
}

func TestB5CandidateValidationRequiresExactTwoMemberLegInventoryAggregate(t *testing.T) {
	write := b5CandidateWriteFixture()
	if !validB5CandidateWrite(write) {
		t.Fatal("valid complete B5 candidate rejected")
	}
	write.Members[1].BookVersion = 0
	if validB5CandidateWrite(write) {
		t.Fatal("invalid coherent member accepted")
	}
	write = b5CandidateWriteFixture()
	write.Legs[1].ExchangeID = write.Candidate.BuyExchangeID
	if validB5CandidateWrite(write) {
		t.Fatal("duplicate venue leg accepted")
	}
	write = b5CandidateWriteFixture()
	write.Inventories[1].SnapshotRole = "buy_venue"
	if validB5CandidateWrite(write) {
		t.Fatal("duplicate inventory role accepted")
	}
}

func TestB5OutcomeValidationRequiresAllConcurrentAndAccountingEvidence(t *testing.T) {
	write := b5OutcomeWriteFixture()
	if !validB5OutcomeWrite(write) {
		t.Fatal("valid complete B5 outcome rejected")
	}
	write.Legs[0].VerificationCount = -1
	if validB5OutcomeWrite(write) {
		t.Fatal("invalid verification count accepted")
	}
	write = b5OutcomeWriteFixture()
	write.Journals[10].Category = write.Journals[0].Category
	if validB5OutcomeWrite(write) {
		t.Fatal("duplicate accounting category accepted")
	}
	write = b5OutcomeWriteFixture()
	write.Rebalancing.AdvisoryOnly = false
	if validB5OutcomeWrite(write) {
		t.Fatal("executable rebalancing evidence accepted")
	}
}

func b5CandidateWriteFixture() B5CandidateWrite {
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	now := b5Timestamp()
	candidate := generated.InsertCrossExchangeCandidateParams{
		DecisionID: "decision-b5", StrategyVersionID: "cross-exchange-v1",
		ConfigurationID: "configuration-b5", CoherentViewID: hash,
		InstrumentID: "BTCUSDT", BuyExchangeID: "binance", SellExchangeID: "bybit",
		Direction: "buy_binance_sell_bybit", BuyOwnershipAccountID: "buy-account-b5",
		SellOwnershipAccountID: "sell-account-b5", QuoteBudget: "10", BaseQuantity: "0.1",
		GrossSpread: "0.4", BuyFee: "0.01", SellFee: "0.01", SpreadDepthCost: "0.01",
		LatencyDeterioration: "0.01", RecoveryAllowance: "0.01",
		ExpectedExecutionPnl: "0.35", MaximumOneLegLoss: "0.01",
		MarginalInventoryReplacement: "0.01", NaturalReversalCost: "0.01",
		AdvisoryRebalancingCost: "0.01", ExchangeConcentrationPenalty: "0.01",
		UsdtVenueConcentrationPenalty: "0.01", ExpectedClosedCycleProfit: "0.29",
		WorstClosedCycleProfit: "0.28", RestorationDelayNanos: 10,
		FirstDetectedOffsetNanos: 100, DecisionOffsetNanos: 110,
		ExpiresOffsetNanos: 250_000_100, ConfigurationHash: hash,
		InstrumentMetadataSetHash: hash, RiskEvaluationID: "risk-b5",
		PricingModelVersionID: "depth-b5", ClaimModelVersionID: "claim-b5",
		FeeModelVersionID: "fee-b5", LatencyModelVersionID: "latency-b5",
		RecoveryModelVersionID:        "recovery-b5",
		InventoryShadowModelVersionID: "shadow-b5",
		ConcentrationModelVersionID:   "concentration-b5",
		CorrelationID:                 "correlation-b5", CausationID: "causation-b5",
		CanonicalHash: hash, RecordedAt: now,
	}
	return B5CandidateWrite{
		Candidate: candidate,
		Members: []generated.InsertCrossExchangeCandidateMemberParams{
			b5MemberFixture(candidate, 0, "binance"),
			b5MemberFixture(candidate, 1, "bybit"),
		},
		Legs: []generated.InsertCrossExchangeCandidateLegParams{
			b5LegFixture(candidate, 0, "binance", "buy-account-b5", "buy"),
			b5LegFixture(candidate, 1, "bybit", "sell-account-b5", "sell"),
		},
		Inventories: []generated.InsertCrossExchangeInventorySnapshotParams{
			b5InventoryFixture(candidate, 0, "binance", "buy-account-b5"),
			b5InventoryFixture(candidate, 1, "bybit", "sell-account-b5"),
		},
	}
}

func b5MemberFixture(
	candidate generated.InsertCrossExchangeCandidateParams,
	index int32,
	exchange string,
) generated.InsertCrossExchangeCandidateMemberParams {
	hash := candidate.CoherentViewID
	now := b5Timestamp()
	return generated.InsertCrossExchangeCandidateMemberParams{
		DecisionID: candidate.DecisionID, CoherentViewID: hash, MemberOrdinal: index,
		ExchangeID: exchange, InstrumentID: candidate.InstrumentID, BookVersion: 1,
		ConnectionGeneration: 1, ReceiveMonotonicNanos: int64(100 + index),
		ReceiveUtc: now, ReceiveUtcUnixNanos: now.Time.UnixNano(),
		IngestOrdinal: int64(index + 1), ClockOffsetNanos: 0, ClockUncertaintyNanos: 1,
		ClockIntervalStart: now, ClockIntervalEnd: now, StateHash: hash,
		CollectorInstance: "collector-" + exchange, CollectorRegion: "test-region",
	}
}

func b5LegFixture(
	candidate generated.InsertCrossExchangeCandidateParams,
	index int32,
	exchange, account, side string,
) generated.InsertCrossExchangeCandidateLegParams {
	return generated.InsertCrossExchangeCandidateLegParams{
		DecisionID: candidate.DecisionID, LegIndex: index, ExchangeID: exchange,
		OwnershipAccountID: account, InstrumentID: candidate.InstrumentID,
		InstrumentMetadataID: "metadata-" + exchange, Side: side,
		InputQuantity: "10", TradeQuantity: "0.1", GrossOutput: "10",
		NetOutput: "9.9", SourceDust: "0", FeeAsset: "USDT", FeeQuantity: "0.01",
		FeeQuoteEquivalent: "0.01", Notional: "10", Vwap: "100",
		SpreadDepthCost: "0.01", BookVersion: 1, ConnectionGeneration: 1,
	}
}

func b5InventoryFixture(
	candidate generated.InsertCrossExchangeCandidateParams,
	index int,
	exchange, account string,
) generated.InsertCrossExchangeInventorySnapshotParams {
	role, before, after, share := "buy_venue", "20", "20.1", "0.2"
	usdtBefore, usdtAfter := "100", "90"
	if index == 1 {
		role, before, after, share = "sell_venue", "80", "79.9", "0.8"
		usdtBefore, usdtAfter = "100", "110"
	}
	return generated.InsertCrossExchangeInventorySnapshotParams{
		DecisionID: candidate.DecisionID, SnapshotRole: role,
		OwnershipAccountID: account, ExchangeID: exchange, BaseAsset: "BTC",
		OwnerLabel: "portfolio-b5", OwnershipRevision: 1,
		BaseBefore: before, BaseAfter: after, TotalEligibleBase: "100",
		BaseShareBefore: share, UsdtBefore: usdtBefore, UsdtAfter: usdtAfter,
		TotalEligibleUsdt: "200", UsdtShareBefore: "0.5",
		BandState: "preferred_natural_reverse", NaturalReversePreferred: true,
	}
}

func b5OutcomeWriteFixture() B5OutcomeWrite {
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	now := b5Timestamp()
	categories := []string{
		"execution_pnl", "btc_inventory_market_pnl", "eth_inventory_market_pnl",
		"stablecoin_valuation", "fees", "spread", "slippage", "latency", "recovery",
		"inventory_restoration", "combined_pnl",
	}
	journals := make([]generated.InsertCrossExchangeJournalLinkParams, len(categories))
	for index, category := range categories {
		journals[index] = generated.InsertCrossExchangeJournalLinkParams{
			DecisionID: "decision-b5", TransactionID: fmt.Sprintf("journal-b5-%d", index),
			Category: category,
		}
	}
	return B5OutcomeWrite{
		Simulation: generated.InsertCrossExchangeSimulationOutcomeParams{
			DecisionID: "decision-b5", PlanID: "plan-b5", Outcome: "both_filled",
			ActualUsdtNet: "0.3", VerificationCompleted: true,
			FinalDisposition: "all_legs_filled", RecoveryLoss: "0",
			LatencyModelVersionID: "latency-b5", CanonicalHash: hash,
			CorrelationID: "correlation-b5", CausationID: "causation-b5", RecordedAt: now,
		},
		Legs: []generated.InsertCrossExchangeSimulationLegParams{
			b5SimulationLegFixture(0, "binance"),
			b5SimulationLegFixture(1, "bybit"),
		},
		Rebalancing: generated.InsertCrossExchangeRebalancingNeedParams{
			DecisionID: "decision-b5", Required: true, AssetSymbol: "BTC",
			DepletedExchangeID: "bybit", OverweightExchangeID: "binance",
			PreferredAction: "prefer_natural_reverse_candidate", EstimatedCost: "0.01",
			EstimatedDelayNanos: 10, AdvisoryOnly: true, RecordedAt: now,
		},
		Journals: journals,
	}
}

func b5SimulationLegFixture(index int32, exchange string) generated.InsertCrossExchangeSimulationLegParams {
	return generated.InsertCrossExchangeSimulationLegParams{
		DecisionID: "decision-b5", LegIndex: index, ExchangeID: exchange,
		ArrivalOffsetNanos: int64(120 + index), InitialState: "FILLED",
		VerifiedState: "FILLED", FinalState: "FILLED",
		InputQuantity: "10", FilledQuantity: "0.1",
	}
}

func b5Timestamp() pgtype.Timestamptz {
	return pgtype.Timestamptz{
		Time: time.Date(2026, 7, 23, 9, 0, 0, 0, time.UTC), Valid: true,
	}
}
