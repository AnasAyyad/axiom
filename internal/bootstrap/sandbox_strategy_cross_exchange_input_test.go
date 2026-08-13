package bootstrap

import (
	"context"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/sandbox"
	"axiom/internal/strategies/crossarb"
)

func TestBuildCrossExchangeSandboxInputBindsOwnedInventoryAndNonzeroRestoration(t *testing.T) {
	now := time.Date(2026, 8, 9, 22, 0, 0, 0, time.UTC)
	work, market, facts, riskInputs := crossExchangeSandboxInputFixture(t, now)
	input, err := BuildCrossExchangeSandboxInput(work, enabledSagaProduct(t), market, facts, riskInputs)
	if err != nil {
		t.Fatal(err)
	}
	if input.Ordinal != market.Trigger.IngestOrdinal || input.LogicalTime != market.Trigger.MonotonicNanos ||
		input.QuoteBudget.String() != "10" || len(input.Inventory) != 2 || len(input.Markets) != 2 ||
		len(input.FeeBalances) != 2 || input.CentralRisk == nil ||
		input.Restoration.ModelVersion != "closed-inventory-cycle.v1" ||
		input.Restoration.RecoveryAllowance.String() == "0" ||
		input.Restoration.MarginalInventoryReplacement.String() == "0" ||
		input.Restoration.NaturalReversalCost.String() == "0" ||
		input.Restoration.MaximumOneLegLoss.String() == "0" ||
		input.Restoration.EstimatedRestorationDelayNanos != uint64(250*time.Millisecond) {
		t.Fatalf("input=%#v", input)
	}
	if _, err = input.EvaluationInput(); err != nil {
		t.Fatalf("canonical input rejected: %v", err)
	}
	if _, err = crossarb.Evaluate(inputFromCrossInput(t, input)); err == nil {
		t.Fatal("equal books unexpectedly overcame explicit restoration charges")
	}
}

func crossExchangeSandboxInputFixture(t *testing.T, now time.Time) (sandbox.StrategySessionWork,
	SandboxCrossExchangeMarketInput, SandboxSagaPlanFacts, *sandbox.StrategySagaRiskInputs,
) {
	t.Helper()
	facts := sagaPlanFacts(t, sandbox.StrategyCrossExchangeArbitrage, now)
	bybit := facts.Admissions[sandbox.ExchangeBybit]
	bybitSnapshot := facts.Snapshots[bybit.Work.Account.ID]
	ten, _ := domain.ParseBalance("10")
	zero, _ := domain.ParseBalance("0")
	bybitSnapshot.Balances = append(bybitSnapshot.Balances,
		sandbox.Balance{Asset: "USDT", Available: ten, Reserved: zero})
	facts.Snapshots[bybit.Work.Account.ID] = bybitSnapshot
	work := facts.Coordinator.Work
	instrument := sagaInstrument(t, work.Instrument)
	source := sagaMarketSource(t, now, []runtimecore.MarketKey{
		{Exchange: "binance", Instrument: instrument},
		{Exchange: "bybit", Instrument: instrument},
	})
	reader, err := NewSandboxSagaMarketInputReader(source)
	if err != nil {
		t.Fatal(err)
	}
	market, err := reader.ReadCrossExchange(context.Background(), work, now)
	if err != nil {
		t.Fatal(err)
	}
	riskMarkets := make(map[sandbox.AccountID]sandbox.StrategyMarketInput, 2)
	for _, admission := range facts.Admissions {
		riskMarket, riskErr := market.RiskMarket(admission.Work)
		if riskErr != nil {
			t.Fatal(riskErr)
		}
		riskMarkets[admission.Work.Account.ID] = riskMarket
	}
	riskInputs := sagaRiskInputsForBuilder(t, facts, riskMarkets, now)
	return work, market, facts, riskInputs
}

func favorableCrossMarketSource(
	t *testing.T,
	now time.Time,
	instrument domain.Instrument,
) *sagaMarketViewSource {
	t.Helper()
	values := []struct {
		exchange string
		bid      string
		ask      string
	}{{exchange: "binance", bid: "99", ask: "100"},
		{exchange: "bybit", bid: "110", ask: "111"}}
	members := make([]SandboxSagaMarketMember, 0, len(values))
	for index, value := range values {
		view := sagaBookViewPrices(t, value.exchange, instrument, now,
			uint64(900_000+index*10_000), uint64(index+1), value.bid, value.ask)
		members = append(members, SandboxSagaMarketMember{View: view,
			Clock: exchangecontracts.ClockHealth{ObservedAt: now.Add(-time.Second),
				Uncertainty: time.Millisecond, Eligible: true},
			Rules:             sagaInstrumentRules(t, value.exchange, instrument, now),
			CollectorInstance: "sandbox-collector-" + value.exchange,
			CollectorRegion:   "local-region"})
	}
	return &sagaMarketViewSource{set: SandboxSagaMarketViewSet{
		Trigger: runtimecore.AsOfTrigger{MonotonicNanos: 1_000_000,
			IngestOrdinal: 100, UTC: now},
		FirstDetectedOffset: 990_000, Members: members}}
}

func inputFromCrossInput(t *testing.T, input crossarb.Input) crossarb.EvaluationInput {
	t.Helper()
	value, err := input.EvaluationInput()
	if err != nil {
		t.Fatal(err)
	}
	return value
}
