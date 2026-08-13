package postgres

import (
	"fmt"
	"testing"
	"time"

	"axiom/internal/storage/postgres/generated"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestCrossExchangeArbitrageRepositoryRejectsIncompleteCandidateOutcomeAndClaimBeforeDatabase(t *testing.T) {
	repository := &CrossExchangeArbitrageRepository{}
	if err := repository.RecordCandidate(t.Context(), CrossExchangeArbitrageCandidateWrite{}); err == nil {
		t.Fatal("incomplete cross-exchange arbitrage candidate reached the database")
	}
	if err := repository.RecordOutcome(t.Context(), CrossExchangeArbitrageOutcomeWrite{}); err == nil {
		t.Fatal("incomplete cross-exchange arbitrage outcome reached the database")
	}
	if err := repository.Claim(t.Context(), generated.ClaimCrossExchangeArbitrageResourcesParams{}); err == nil {
		t.Fatal("incomplete cross-exchange arbitrage claim reached the database")
	}
}

func TestCrossExchangeArbitrageCandidateValidationRequiresExactTwoMemberLegInventoryAggregate(t *testing.T) {
	write := crossExchangeArbitrageCandidateWriteFixture()
	if !validCrossExchangeArbitrageCandidateWrite(write) {
		t.Fatal("valid complete cross-exchange arbitrage candidate rejected")
	}
	write.Members[1].BookVersion = 0
	if validCrossExchangeArbitrageCandidateWrite(write) {
		t.Fatal("invalid coherent member accepted")
	}
	write = crossExchangeArbitrageCandidateWriteFixture()
	write.Legs[1].ExchangeID = write.Candidate.BuyExchangeID
	if validCrossExchangeArbitrageCandidateWrite(write) {
		t.Fatal("duplicate venue leg accepted")
	}
	write = crossExchangeArbitrageCandidateWriteFixture()
	write.Inventories[1].SnapshotRole = "buy_venue"
	if validCrossExchangeArbitrageCandidateWrite(write) {
		t.Fatal("duplicate inventory role accepted")
	}
}

func TestCrossExchangeArbitrageOutcomeValidationRequiresAllConcurrentAndAccountingEvidence(t *testing.T) {
	write := crossExchangeArbitrageOutcomeWriteFixture()
	if !validCrossExchangeArbitrageOutcomeWrite(write) {
		t.Fatal("valid complete cross-exchange arbitrage outcome rejected")
	}
	write.Legs[0].VerificationCount = -1
	if validCrossExchangeArbitrageOutcomeWrite(write) {
		t.Fatal("invalid verification count accepted")
	}
	write = crossExchangeArbitrageOutcomeWriteFixture()
	write.Journals[10].Category = write.Journals[0].Category
	if validCrossExchangeArbitrageOutcomeWrite(write) {
		t.Fatal("duplicate accounting category accepted")
	}
	write = crossExchangeArbitrageOutcomeWriteFixture()
	write.Rebalancing.AdvisoryOnly = false
	if validCrossExchangeArbitrageOutcomeWrite(write) {
		t.Fatal("executable rebalancing evidence accepted")
	}
}

func crossExchangeArbitrageCandidateWriteFixture() CrossExchangeArbitrageCandidateWrite {
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	now := crossExchangeArbitrageTimestamp()
	candidate := generated.InsertCrossExchangeCandidateParams{
		DecisionID: "decision-cross_exchange_arbitrage", StrategyVersionID: "cross-exchange-v1",
		ConfigurationID: "configuration-cross_exchange_arbitrage", CoherentViewID: hash,
		InstrumentID: "BTCUSDT", BuyExchangeID: "binance", SellExchangeID: "bybit",
		Direction: "buy_binance_sell_bybit", BuyOwnershipAccountID: "buy-account-cross_exchange_arbitrage",
		SellOwnershipAccountID: "sell-account-cross_exchange_arbitrage", QuoteBudget: "10", BaseQuantity: "0.1",
		GrossSpread: "0.4", BuyFee: "0.01", SellFee: "0.01", SpreadDepthCost: "0.01",
		LatencyDeterioration: "0.01", RecoveryAllowance: "0.01",
		ExpectedExecutionPnl: "0.35", MaximumOneLegLoss: "0.01",
		MarginalInventoryReplacement: "0.01", NaturalReversalCost: "0.01",
		AdvisoryRebalancingCost: "0.01", ExchangeConcentrationPenalty: "0.01",
		UsdtVenueConcentrationPenalty: "0.01", ExpectedClosedCycleProfit: "0.29",
		WorstClosedCycleProfit: "0.28", RestorationDelayNanos: 10,
		FirstDetectedOffsetNanos: 100, DecisionOffsetNanos: 110,
		ExpiresOffsetNanos: 250_000_100, ConfigurationHash: hash,
		InstrumentMetadataSetHash: hash, RiskEvaluationID: "risk-cross_exchange_arbitrage",
		PricingModelVersionID: "depth-cross_exchange_arbitrage", ClaimModelVersionID: "claim-cross_exchange_arbitrage",
		FeeModelVersionID: "fee-cross_exchange_arbitrage", LatencyModelVersionID: "latency-cross_exchange_arbitrage",
		RecoveryModelVersionID:        "recovery-cross_exchange_arbitrage",
		InventoryShadowModelVersionID: "shadow-cross_exchange_arbitrage",
		ConcentrationModelVersionID:   "concentration-cross_exchange_arbitrage",
		CorrelationID:                 "correlation-cross_exchange_arbitrage", CausationID: "causation-cross_exchange_arbitrage",
		CanonicalHash: hash, RecordedAt: now,
	}
	return CrossExchangeArbitrageCandidateWrite{
		Candidate: candidate,
		Members: []generated.InsertCrossExchangeCandidateMemberParams{
			crossExchangeArbitrageMemberFixture(candidate, 0, "binance"),
			crossExchangeArbitrageMemberFixture(candidate, 1, "bybit"),
		},
		Legs: []generated.InsertCrossExchangeCandidateLegParams{
			crossExchangeArbitrageLegFixture(candidate, 0, "binance", "buy-account-cross_exchange_arbitrage", "buy"),
			crossExchangeArbitrageLegFixture(candidate, 1, "bybit", "sell-account-cross_exchange_arbitrage", "sell"),
		},
		Inventories: []generated.InsertCrossExchangeInventorySnapshotParams{
			crossExchangeArbitrageInventoryFixture(candidate, 0, "binance", "buy-account-cross_exchange_arbitrage"),
			crossExchangeArbitrageInventoryFixture(candidate, 1, "bybit", "sell-account-cross_exchange_arbitrage"),
		},
	}
}

func crossExchangeArbitrageMemberFixture(
	candidate generated.InsertCrossExchangeCandidateParams,
	index int32,
	exchange string,
) generated.InsertCrossExchangeCandidateMemberParams {
	hash := candidate.CoherentViewID
	now := crossExchangeArbitrageTimestamp()
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

func crossExchangeArbitrageLegFixture(
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

func crossExchangeArbitrageInventoryFixture(
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
		OwnerLabel: "portfolio-cross_exchange_arbitrage", OwnershipRevision: 1,
		BaseBefore: before, BaseAfter: after, TotalEligibleBase: "100",
		BaseShareBefore: share, UsdtBefore: usdtBefore, UsdtAfter: usdtAfter,
		TotalEligibleUsdt: "200", UsdtShareBefore: "0.5",
		BandState: "preferred_natural_reverse", NaturalReversePreferred: true,
	}
}

func crossExchangeArbitrageOutcomeWriteFixture() CrossExchangeArbitrageOutcomeWrite {
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	now := crossExchangeArbitrageTimestamp()
	categories := []string{
		"execution_pnl", "btc_inventory_market_pnl", "eth_inventory_market_pnl",
		"stablecoin_valuation", "fees", "spread", "slippage", "latency", "recovery",
		"inventory_restoration", "combined_pnl",
	}
	journals := make([]generated.InsertCrossExchangeJournalLinkParams, len(categories))
	for index, category := range categories {
		journals[index] = generated.InsertCrossExchangeJournalLinkParams{
			DecisionID: "decision-cross_exchange_arbitrage", TransactionID: fmt.Sprintf("journal-cross_exchange_arbitrage-%d", index),
			Category: category,
		}
	}
	return CrossExchangeArbitrageOutcomeWrite{
		Simulation: generated.InsertCrossExchangeSimulationOutcomeParams{
			DecisionID: "decision-cross_exchange_arbitrage", PlanID: "plan-cross_exchange_arbitrage", Outcome: "both_filled",
			ActualUsdtNet: "0.3", VerificationCompleted: true,
			FinalDisposition: "all_legs_filled", RecoveryLoss: "0",
			LatencyModelVersionID: "latency-cross_exchange_arbitrage", CanonicalHash: hash,
			CorrelationID: "correlation-cross_exchange_arbitrage", CausationID: "causation-cross_exchange_arbitrage", RecordedAt: now,
		},
		Legs: []generated.InsertCrossExchangeSimulationLegParams{
			crossExchangeArbitrageSimulationLegFixture(0, "binance"),
			crossExchangeArbitrageSimulationLegFixture(1, "bybit"),
		},
		Rebalancing: generated.InsertCrossExchangeRebalancingNeedParams{
			DecisionID: "decision-cross_exchange_arbitrage", Required: true, AssetSymbol: "BTC",
			DepletedExchangeID: "bybit", OverweightExchangeID: "binance",
			PreferredAction: "prefer_natural_reverse_candidate", EstimatedCost: "0.01",
			EstimatedDelayNanos: 10, AdvisoryOnly: true, RecordedAt: now,
		},
		Journals: journals,
	}
}

func crossExchangeArbitrageSimulationLegFixture(index int32, exchange string) generated.InsertCrossExchangeSimulationLegParams {
	return generated.InsertCrossExchangeSimulationLegParams{
		DecisionID: "decision-cross_exchange_arbitrage", LegIndex: index, ExchangeID: exchange,
		ArrivalOffsetNanos: int64(120 + index), InitialState: "FILLED",
		VerifiedState: "FILLED", FinalState: "FILLED",
		InputQuantity: "10", FilledQuantity: "0.1",
	}
}

func crossExchangeArbitrageTimestamp() pgtype.Timestamptz {
	return pgtype.Timestamptz{
		Time: time.Date(2026, 7, 23, 9, 0, 0, 0, time.UTC), Valid: true,
	}
}
