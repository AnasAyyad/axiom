package triangular

import (
	"encoding/json"
	"testing"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/portfolio"
	"axiom/internal/risk"
)

func TestInputRebuildsCanonicalEvaluationAfterJSONRoundTrip(t *testing.T) {
	expected := profitableInput(t, false)
	input := inputFromEvaluation(t, expected)
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var restored Input
	if err = json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	actual, err := restored.EvaluationInput()
	if err != nil {
		t.Fatal(err)
	}
	before, err := Evaluate(expected)
	if err != nil {
		t.Fatal(err)
	}
	after, err := Evaluate(actual)
	if err != nil || len(before) != len(after) {
		t.Fatalf("replayed candidates=%#v error=%v", after, err)
	}
	for index := range before {
		if before[index].ID != after[index].ID {
			t.Fatalf("candidate %d changed: before=%#v after=%#v", index, before[index], after[index])
		}
	}
	if err = restored.ValidateEventBinding(restored.Ordinal, restored.LogicalTime); err != nil {
		t.Fatal(err)
	}
}

func TestInputRejectsMissingSnapshotIdentityAndMismatchedEnvelope(t *testing.T) {
	input := inputFromEvaluation(t, profitableInput(t, false))
	input.Markets[0].Snapshot.RawPayloadHash = ""
	if _, err := input.EvaluationInput(); err == nil {
		t.Fatal("snapshot without immutable payload identity was accepted")
	}
	input = inputFromEvaluation(t, profitableInput(t, false))
	if err := input.ValidateEventBinding(input.Ordinal+1, input.LogicalTime); err == nil {
		t.Fatal("mismatched replay envelope was accepted")
	}
}

func TestRecordedSimulationRestoresOnlyExactFutureBooksAndLatency(t *testing.T) {
	input := inputFromEvaluation(t, profitableInput(t, false))
	input.Simulation = &SimulationInput{Latency: testLatency()}
	for _, offset := range []uint64{input.LogicalTime + 10, input.LogicalTime + 30, input.LogicalTime + 60} {
		for _, market := range input.Markets {
			input.Simulation.Markets = append(input.Simulation.Markets, TimedMarketInput{Offset: offset, Market: market})
		}
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var restored Input
	if err = json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	timeline, latency, err := RecordedSagaSimulationInputs{}.SimulationInput(restored)
	if err != nil || latency != testLatency() {
		t.Fatalf("timeline=%#v latency=%#v error=%v", timeline, latency, err)
	}
	evaluation, err := restored.EvaluationInput()
	if err != nil {
		t.Fatal(err)
	}
	candidate := candidateFor(t, evaluation, CycleUSDTBTCETHUSDT, "10")
	result, err := Simulate(candidate, timeline, latency)
	if err != nil || result.Outcome != OutcomeFullSuccess {
		t.Fatalf("result=%#v error=%v", result, err)
	}
	missing := restored
	missing.Simulation.Markets = missing.Simulation.Markets[:len(missing.Simulation.Markets)-len(input.Markets)]
	if _, _, err = missing.RecordedSimulation(); err != nil {
		t.Fatal(err)
	}
	missingResult, simulationErr := Simulate(candidate, mustRecordedTimeline(t, missing), latency)
	if simulationErr != nil || missingResult.Outcome == OutcomeFullSuccess {
		t.Fatalf("simulation accepted a missing future book: %#v error=%v", missingResult, simulationErr)
	}
}

func TestRecordedRiskInputRestoresOnlyCapturedCentralRiskEvidence(t *testing.T) {
	input := inputFromEvaluation(t, profitableInput(t, false))
	input.CentralRisk = &RiskInput{Policies: []risk.Policy{{ID: "triangular-recorded-risk", Version: 1,
		Scope: risk.Scope{Kind: risk.ScopeStrategy, ID: "triangular"}, State: risk.StateNormal}},
		Observations: triangularHealthyRiskObservations(), EvaluatedAt: input.Now}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var restored Input
	if err = json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	riskInput, err := RecordedSagaRiskInputs{}.RiskInput(restored)
	if err != nil || len(riskInput.Policies) != 1 || riskInput.Policies[0].ID != "triangular-recorded-risk" {
		t.Fatalf("risk input=%#v error=%v", riskInput, err)
	}
	riskInput.Policies[0].ID = "mutated"
	if restored.CentralRisk.Policies[0].ID != "triangular-recorded-risk" {
		t.Fatal("recorded central-risk evidence was returned by reference")
	}
	missing := restored
	missing.CentralRisk = nil
	if _, err = missing.RecordedRiskInput(); err == nil {
		t.Fatal("missing recorded central-risk evidence was accepted")
	}
}

func TestRecordedReductionRestoresOnlyCapturedReconciliationEvidence(t *testing.T) {
	input := inputFromEvaluation(t, profitableInput(t, false))
	state := matchingSagaReconciliationState()
	input.Reduction = &ReductionInput{Reconciliation: ReconciliationInput{Scope: "triangular/recorded",
		Expected: state, Actual: state, At: input.Now}}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var restored Input
	if err = json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	reduction, err := restored.RecordedReduction()
	if err != nil || reduction.Reconciliation.Scope != "triangular/recorded" ||
		reduction.Reconciliation.Expected.Orders == "" {
		t.Fatalf("reduction=%#v error=%v", reduction, err)
	}
	reduction.Reconciliation.Expected.Duplicates = append(reduction.Reconciliation.Expected.Duplicates, "mutated")
	if len(restored.Reduction.Reconciliation.Expected.Duplicates) != 0 {
		t.Fatal("recorded reconciliation evidence was returned by reference")
	}
	missing := restored
	missing.Reduction = nil
	if _, err = missing.RecordedReduction(); err == nil {
		t.Fatal("missing recorded reduction evidence was accepted")
	}
}

func TestRecordedSagaClaimSetDerivesCapacityFromRecordedFacts(t *testing.T) {
	input := inputFromEvaluation(t, profitableInput(t, false))
	claims, err := NewRecordedSagaClaimSet(input, "portfolio-a")
	if err != nil {
		t.Fatal(err)
	}
	settlement, found := claims.Resource(portfolio.ClaimKey{Kind: portfolio.ClaimBalance, Owner: "portfolio-a",
		Exchange: input.Exchange, Resource: "usdt"})
	if !found || settlement.Available != input.AvailableSettlement {
		t.Fatalf("recorded settlement capacity=%#v found=%t", settlement, found)
	}
	liquidity, found := claims.Resource(portfolio.ClaimKey{Kind: portfolio.ClaimLiquidity, Owner: "portfolio-a",
		Exchange: input.Exchange, Resource: "btcusdt/buy/v1"})
	if !found || liquidity.Available.String() != "5" {
		t.Fatalf("recorded buy depth=%#v found=%t", liquidity, found)
	}
	invalid := input
	invalid.RecoveryAllowance = balance("0")
	if _, err = NewRecordedSagaClaimSet(invalid, "portfolio-a"); err == nil {
		t.Fatal("zero recorded recovery capacity was accepted")
	}
}

func mustRecordedTimeline(t *testing.T, input Input) Timeline {
	t.Helper()
	timeline, _, err := input.RecordedSimulation()
	if err != nil {
		t.Fatal(err)
	}
	return timeline
}

func inputFromEvaluation(t *testing.T, source EvaluationInput) Input {
	t.Helper()
	markets := make([]MarketInput, 0, len(source.Markets))
	for _, market := range source.Markets {
		view := market.Book
		observation := view.Observation()
		markets = append(markets, MarketInput{Snapshot: exchangecontracts.BookSnapshot{
			Exchange: exchangecontracts.ExchangeID(view.Exchange()), Instrument: view.Instrument(),
			LastSequence: view.Sequence(), ReceivedAt: observation.ReceivedAt,
			Bids: view.Bids(), Asks: view.Asks(), RawPayloadHash: "sha256:replay-" + view.Instrument().Symbol(),
		}, Observation: observation, Rules: market.Rules})
	}
	return Input{Ordinal: 9, LogicalTime: source.DecisionOffsetNanos,
		Now: time.Unix(11, 0).UTC(), Exchange: source.Exchange, Markets: markets,
		FirstDetectedOffset: source.FirstDetectedOffset,
		AvailableSettlement: source.AvailableSettlement, StrategyBudget: source.StrategyBudget,
		GlobalReserveFloor: source.GlobalReserveFloor, RecoveryAllowance: source.RecoveryAllowance,
		FeeBalances: source.FeeBalances, Configuration: source.Configuration,
		ConfigurationHash: source.ConfigurationHash, InstrumentMetadataID: source.InstrumentMetadataID}
}
