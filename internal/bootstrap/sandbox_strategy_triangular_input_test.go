package bootstrap

import (
	"context"
	"strings"
	"testing"
	"time"

	"axiom/internal/domain"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/sandbox"
)

func TestBuildTriangularSandboxInputBindsReviewedCapacityAndAggregateRisk(t *testing.T) {
	now := time.Date(2026, 8, 9, 17, 0, 0, 0, time.UTC)
	work, market, facts, riskInputs := triangularSandboxInputFixture(t, now)
	input, err := BuildTriangularSandboxInput(work, enabledSagaProduct(t), market, facts, riskInputs)
	if err != nil {
		t.Fatal(err)
	}
	if input.Ordinal != market.Trigger.IngestOrdinal || input.LogicalTime != market.Trigger.MonotonicNanos ||
		input.AvailableSettlement.String() != "10" || input.StrategyBudget.String() != "10" ||
		input.RecoveryAllowance.String() != "0.2" || input.GlobalReserveFloor.String() != "0" ||
		input.CentralRisk == nil || len(input.CentralRisk.Policies) != 1 ||
		input.ConfigurationHash != work.ConfigurationHash || len(input.Markets) != 3 {
		t.Fatalf("input=%#v", input)
	}
	if _, err = input.EvaluationInput(); err != nil {
		t.Fatalf("canonical input rejected: %v", err)
	}
	assertTriangularSandboxInsufficientCapital(t, work, market, facts, riskInputs)
}

func triangularSandboxInputFixture(t *testing.T, now time.Time) (sandbox.StrategySessionWork,
	SandboxTriangularMarketInput, SandboxSagaPlanFacts, *sandbox.StrategySagaRiskInputs,
) {
	t.Helper()
	facts := sagaPlanFacts(t, sandbox.StrategyTriangular, now)
	work := facts.Coordinator.Work
	source := sagaMarketSource(t, now, []runtimecore.MarketKey{
		{Exchange: "binance", Instrument: sagaInstrument(t, "BTCUSDT")},
		{Exchange: "binance", Instrument: sagaInstrument(t, "ETHBTC")},
		{Exchange: "binance", Instrument: sagaInstrument(t, "ETHUSDT")},
	})
	reader, err := NewSandboxSagaMarketInputReader(source)
	if err != nil {
		t.Fatal(err)
	}
	market, err := reader.ReadTriangular(context.Background(), work, now)
	if err != nil {
		t.Fatal(err)
	}
	riskMarket, err := market.RiskMarket(work)
	if err != nil {
		t.Fatal(err)
	}
	riskInputs := sagaRiskInputsForBuilder(t, facts, map[sandbox.AccountID]sandbox.StrategyMarketInput{
		work.Account.ID: riskMarket,
	}, now)
	return work, market, facts, riskInputs
}

func assertTriangularSandboxInsufficientCapital(t *testing.T, work sandbox.StrategySessionWork,
	market SandboxTriangularMarketInput, facts SandboxSagaPlanFacts,
	riskInputs *sandbox.StrategySagaRiskInputs,
) {
	t.Helper()
	insufficient := facts
	snapshot := insufficient.Snapshots[work.Account.ID]
	low, _ := domain.ParseBalance("0.1")
	snapshot.Balances[0].Available = low
	snapshot.SnapshotHash = strings.Repeat("e", 64)
	insufficient.Snapshots[work.Account.ID] = snapshot
	riskFacts := insufficient.RiskFacts[work.Account.ID]
	riskFacts.SnapshotHash = snapshot.SnapshotHash
	riskFacts.MinimumReserve = mustSizingFactsMoney(t, "0.1")
	insufficient.RiskFacts[work.Account.ID] = riskFacts
	if _, err := BuildTriangularSandboxInput(work, enabledSagaProduct(t), market, insufficient, riskInputs); err == nil {
		t.Fatal("zero post-reserve capacity accepted")
	}
}

func sagaRiskInputsForBuilder(
	t *testing.T,
	facts SandboxSagaPlanFacts,
	markets map[sandbox.AccountID]sandbox.StrategyMarketInput,
	now time.Time,
) *sandbox.StrategySagaRiskInputs {
	t.Helper()
	zero, err := domain.ParsePercent("0")
	if err != nil {
		t.Fatal(err)
	}
	one, err := domain.ParsePercent("1")
	if err != nil {
		t.Fatal(err)
	}
	members := make([]sandbox.StrategySagaRiskMember, 0, len(facts.Admissions))
	for _, admission := range facts.Admissions {
		work := admission.Work
		snapshot := facts.Snapshots[work.Account.ID]
		riskFacts := facts.RiskFacts[work.Account.ID]
		market, exists := markets[work.Account.ID]
		if !exists {
			t.Fatalf("risk market missing for %s", work.Account.ID)
		}
		observation := sandbox.StrategyRiskObservation{StrategySessionID: work.SessionID,
			StrategyRevision: work.StrategyRevision, AccountID: work.Account.ID,
			AccountEpoch: work.Account.Epoch, SnapshotHash: snapshot.SnapshotHash,
			MarketHash: sandbox.StrategyMarketEvidenceHash(market), Instrument: work.Instrument,
			PolicyID: riskFacts.PolicyID, PolicyVersion: riskFacts.PolicyVersion,
			PolicyHash: riskFacts.PolicyHash, AccountDrawdown: zero, UTCDayLoss: zero,
			Rolling24HourLoss: zero, StrategyLoss: zero, AssetExposure: zero,
			CombinedExposure: zero, ExchangeExposure: zero, Reserve: one,
			ReservedCapital: zero, Spread: zero, Slippage: zero,
			QualityScore: 100, ObservedAt: now}
		if err = observation.ValidFor(work, snapshot, market, riskFacts, now); err != nil {
			t.Fatalf("risk observation invalid: %v", err)
		}
		members = append(members, sandbox.StrategySagaRiskMember{Work: work, Snapshot: snapshot,
			Market: market, Facts: riskFacts, Observation: observation})
	}
	inputs, err := sandbox.NewStrategySagaRiskInputs(members, now)
	if err != nil {
		t.Fatal(err)
	}
	return inputs
}
