package postgres

import (
	"testing"
	"time"

	"axiom/internal/storage/postgres/generated"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestTriangularArbitrageRepositoryRejectsIncompleteCandidateAndOutcomeBeforeDatabase(t *testing.T) {
	repository := &TriangularArbitrageRepository{}
	if err := repository.RecordCandidate(t.Context(), TriangularArbitrageCandidateWrite{}); err == nil {
		t.Fatal("incomplete triangular arbitrage candidate reached the database")
	}
	if err := repository.RecordOutcome(t.Context(), TriangularArbitrageOutcomeWrite{}); err == nil {
		t.Fatal("incomplete triangular arbitrage outcome reached the database")
	}
}

func TestTriangularArbitrageCandidateValidationRequiresThreeOrderedLegsAndImmutableTiming(t *testing.T) {
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	now := pgtype.Timestamptz{Time: time.Date(2026, 7, 23, 9, 0, 0, 0, time.UTC), Valid: true}
	write := TriangularArbitrageCandidateWrite{
		Candidate: generated.InsertTriangularCandidateParams{
			DecisionID: "decision-triangular_arbitrage", StrategyVersionID: "triangular-v1",
			ConfigurationID:             "configuration-triangular_arbitrage",
			PortfolioOwnershipAccountID: "account-triangular_arbitrage", ExchangeID: "binance",
			Cycle: "USDT-BTC-ETH-USDT", FirstDetectedOffsetNanos: 100,
			DecisionOffsetNanos: 110, ExpiresOffsetNanos: 250_000_100,
			ConfigurationHash: hash, InstrumentMetadataSetHash: hash,
			CanonicalHash: hash, CorrelationID: "correlation-triangular_arbitrage",
			CausationID: "causation-triangular_arbitrage", ModelVersionID: "depth-triangular_arbitrage",
			RiskEvaluationID: "risk-triangular_arbitrage", ClaimModelVersionID: "claim-triangular_arbitrage",
			FeeModelVersionID: "fee-triangular_arbitrage", LatencyModelVersionID: "latency-triangular_arbitrage",
			RecoveryModelVersionID: "recovery-triangular_arbitrage", StartQuantity: "10",
			ExpectedFinalQuantity: "10.5", WorstFinalQuantity: "10.4",
			ExpectedNet: "0.5", WorstNet: "0.4", ExpectedEdge: "0.05",
			WorstEdge: "0.04", AdditionalSafetyMargin: "0.0015",
			RecordedAt: now,
		},
		Legs: []generated.InsertTriangularCandidateLegParams{
			triangularArbitrageLegFixture(0), triangularArbitrageLegFixture(1), triangularArbitrageLegFixture(2),
		},
	}
	if !validTriangularArbitrageCandidateWrite(write) {
		t.Fatal("valid complete triangular arbitrage write rejected")
	}
	write.Legs[2].LegIndex = 1
	if validTriangularArbitrageCandidateWrite(write) {
		t.Fatal("unordered triangular arbitrage leg accepted")
	}
}

func triangularArbitrageLegFixture(index int32) generated.InsertTriangularCandidateLegParams {
	return generated.InsertTriangularCandidateLegParams{
		DecisionID: "decision-triangular_arbitrage", LegIndex: index,
		InstrumentID: "instrument", InstrumentMetadataID: "metadata",
		SourceAsset: "USDT", TargetAsset: "BTC", Side: "buy",
		InputQuantity: "10", TradeQuantity: "0.1", GrossOutput: "0.1",
		NetOutput: "0.1", SourceDust: "0", FeeAsset: "USDT",
		FeeQuantity: "0.001", FeeQuoteEquivalent: "0.001",
		Notional: "10", Vwap: "100", SpreadDepthCost: "0",
		BookVersion: 1, ConnectionGeneration: 1,
	}
}
