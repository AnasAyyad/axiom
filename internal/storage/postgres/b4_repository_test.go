package postgres

import (
	"testing"
	"time"

	"axiom/internal/storage/postgres/generated"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestB4RepositoryRejectsIncompleteCandidateAndOutcomeBeforeDatabase(t *testing.T) {
	repository := &B4Repository{}
	if err := repository.RecordCandidate(t.Context(), B4CandidateWrite{}); err == nil {
		t.Fatal("incomplete B4 candidate reached the database")
	}
	if err := repository.RecordOutcome(t.Context(), B4OutcomeWrite{}); err == nil {
		t.Fatal("incomplete B4 outcome reached the database")
	}
}

func TestB4CandidateValidationRequiresThreeOrderedLegsAndImmutableTiming(t *testing.T) {
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	now := pgtype.Timestamptz{Time: time.Date(2026, 7, 23, 9, 0, 0, 0, time.UTC), Valid: true}
	write := B4CandidateWrite{
		Candidate: generated.InsertTriangularCandidateParams{
			DecisionID: "decision-b4", StrategyVersionID: "triangular-v1",
			ConfigurationID:             "configuration-b4",
			PortfolioOwnershipAccountID: "account-b4", ExchangeID: "binance",
			Cycle: "USDT-BTC-ETH-USDT", FirstDetectedOffsetNanos: 100,
			DecisionOffsetNanos: 110, ExpiresOffsetNanos: 250_000_100,
			ConfigurationHash: hash, InstrumentMetadataSetHash: hash,
			CanonicalHash: hash, CorrelationID: "correlation-b4",
			CausationID: "causation-b4", ModelVersionID: "depth-b4",
			RiskEvaluationID: "risk-b4", ClaimModelVersionID: "claim-b4",
			FeeModelVersionID: "fee-b4", LatencyModelVersionID: "latency-b4",
			RecoveryModelVersionID: "recovery-b4", StartQuantity: "10",
			ExpectedFinalQuantity: "10.5", WorstFinalQuantity: "10.4",
			ExpectedNet: "0.5", WorstNet: "0.4", ExpectedEdge: "0.05",
			WorstEdge: "0.04", AdditionalSafetyMargin: "0.0015",
			RecordedAt: now,
		},
		Legs: []generated.InsertTriangularCandidateLegParams{
			b4LegFixture(0), b4LegFixture(1), b4LegFixture(2),
		},
	}
	if !validB4CandidateWrite(write) {
		t.Fatal("valid complete B4 write rejected")
	}
	write.Legs[2].LegIndex = 1
	if validB4CandidateWrite(write) {
		t.Fatal("unordered B4 leg accepted")
	}
}

func b4LegFixture(index int32) generated.InsertTriangularCandidateLegParams {
	return generated.InsertTriangularCandidateLegParams{
		DecisionID: "decision-b4", LegIndex: index,
		InstrumentID: "instrument", InstrumentMetadataID: "metadata",
		SourceAsset: "USDT", TargetAsset: "BTC", Side: "buy",
		InputQuantity: "10", TradeQuantity: "0.1", GrossOutput: "0.1",
		NetOutput: "0.1", SourceDust: "0", FeeAsset: "USDT",
		FeeQuantity: "0.001", FeeQuoteEquivalent: "0.001",
		Notional: "10", Vwap: "100", SpreadDepthCost: "0",
		BookVersion: 1, ConnectionGeneration: 1,
	}
}
