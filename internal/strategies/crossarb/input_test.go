package crossarb

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/execution"
	"axiom/internal/portfolio"
	"axiom/internal/risk"
)

func TestInputRebuildsExactCoherentEvaluationAfterJSONRoundTrip(t *testing.T) {
	expected := evaluationFixture(t, "BTC", false)
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
		if before[index].ID != after[index].ID ||
			before[index].CoherentViewID != after[index].CoherentViewID {
			t.Fatalf("candidate %d changed: before=%#v after=%#v", index, before[index], after[index])
		}
	}
	if err = restored.ValidateEventBinding(restored.Ordinal, restored.LogicalTime); err != nil {
		t.Fatal(err)
	}
}

func TestInputRejectsMissingSnapshotIdentityMismatchedCoherentBookAndEnvelope(t *testing.T) {
	input := inputFromEvaluation(t, evaluationFixture(t, "BTC", false))
	input.Markets[0].Snapshot.RawPayloadHash = ""
	if _, err := input.EvaluationInput(); err == nil {
		t.Fatal("snapshot without immutable source identity was accepted")
	}
	input = inputFromEvaluation(t, evaluationFixture(t, "BTC", false))
	input.Markets[0].Snapshot.Bids[0].Price = price("98")
	actual, err := input.EvaluationInput()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Evaluate(actual); err == nil {
		t.Fatal("book that mismatched its recorded coherent member was accepted")
	}
	if err = input.ValidateEventBinding(input.Ordinal+1, input.LogicalTime); err == nil {
		t.Fatal("mismatched replay envelope was accepted")
	}
}

func TestRecordedSimulationRestoresOnlyExactVenueBooksAndDirectives(t *testing.T) {
	input := inputFromEvaluation(t, evaluationFixture(t, "BTC", false))
	input.Simulation = &SimulationInput{Latency: testLatency(), Recovery: RecoveryPolicy{}}
	for _, offset := range []uint64{input.LogicalTime + 10, input.LogicalTime + 20} {
		for _, market := range input.Markets {
			input.Simulation.Markets = append(input.Simulation.Markets, TimedMarketInput{Offset: offset, Market: market})
		}
	}
	input.Simulation.Directives = []TimedDirective{
		{Exchange: "binance", Phase: PhaseArrival, Offset: input.LogicalTime + 10,
			Directive: LegDirective{State: execution.OrderFilled}},
		{Exchange: "bybit", Phase: PhaseArrival, Offset: input.LogicalTime + 20,
			Directive: LegDirective{State: execution.OrderFilled}},
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var restored Input
	if err = json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	timeline, latency, policy, err := RecordedSagaSimulationInputs{}.SimulationInput(restored)
	if err != nil || !reflect.DeepEqual(latency, testLatency()) || policy != (RecoveryPolicy{}) {
		t.Fatalf("timeline=%#v latency=%#v policy=%#v error=%v", timeline, latency, policy, err)
	}
	evaluation, err := restored.EvaluationInput()
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := Evaluate(evaluation)
	if err != nil || len(candidates) == 0 {
		t.Fatalf("candidates=%#v error=%v", candidates, err)
	}
	result, err := Simulate(candidates[0], timeline, latency, policy)
	if err != nil || result.Outcome != OutcomeBothFilled {
		t.Fatalf("result=%#v error=%v", result, err)
	}
	missing := restored
	missing.Simulation.Directives = missing.Simulation.Directives[:1]
	missingTimeline, missingLatency, missingPolicy, err := missing.RecordedSimulation()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Simulate(candidates[0], missingTimeline, missingLatency, missingPolicy); err == nil {
		t.Fatal("simulation accepted a missing recorded venue directive")
	}
}

func TestRecordedRiskInputRestoresOnlyCapturedCentralRiskEvidence(t *testing.T) {
	input := inputFromEvaluation(t, evaluationFixture(t, "BTC", false))
	input.CentralRisk = &RiskInput{Policies: []risk.Policy{{ID: "crossarb-recorded-risk", Version: 1,
		Scope: risk.Scope{Kind: risk.ScopeStrategy, ID: "crossarb"}, State: risk.StateNormal}},
		EvaluatedAt: input.Now}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var restored Input
	if err = json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	riskInput, err := RecordedSagaRiskInputs{}.RiskInput(restored)
	if err != nil || len(riskInput.Policies) != 1 || riskInput.Policies[0].ID != "crossarb-recorded-risk" {
		t.Fatalf("risk input=%#v error=%v", riskInput, err)
	}
	riskInput.Policies[0].ID = "mutated"
	if restored.CentralRisk.Policies[0].ID != "crossarb-recorded-risk" {
		t.Fatal("recorded central-risk evidence was returned by reference")
	}
	missing := restored
	missing.CentralRisk = nil
	if _, err = missing.RecordedRiskInput(); err == nil {
		t.Fatal("missing recorded central-risk evidence was accepted")
	}
}

func TestRecordedReductionRestoresOnlyCapturedAttributionAndReconciliationEvidence(t *testing.T) {
	input := inputFromEvaluation(t, evaluationFixture(t, "BTC", false))
	state := matchingSagaReconciliationState()
	input.Reduction = &ReductionInput{Attribution: PortfolioAttribution{Fees: balance("0.01")},
		Reconciliation: ReconciliationInput{Scope: "crossarb/recorded", Expected: state, Actual: state, At: input.Now}}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var restored Input
	if err = json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	reduction, err := restored.RecordedReduction()
	if err != nil || reduction.Reconciliation.Scope != "crossarb/recorded" ||
		reduction.Attribution.Fees.String() != "0.01" {
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

func TestRecordedSagaClaimSetDerivesCapacityFromRecordedOwnershipAndDepth(t *testing.T) {
	input := inputFromEvaluation(t, evaluationFixture(t, "BTC", false))
	claims, err := NewRecordedSagaClaimSet(input)
	if err != nil {
		t.Fatal(err)
	}
	first := input.Inventory[0]
	settlement, found := claims.Resource(portfolio.ClaimKey{Kind: portfolio.ClaimBalance, Owner: first.Owner,
		Exchange: first.Exchange, Resource: "usdt"})
	if !found || settlement.Available != first.OwnedUSDT {
		t.Fatalf("recorded USDT capacity=%#v found=%t", settlement, found)
	}
	liquidity, found := claims.Resource(portfolio.ClaimKey{Kind: portfolio.ClaimLiquidity, Owner: first.Owner,
		Exchange: "binance", Resource: "btcusdt-ask-v1"})
	if !found || liquidity.Available.String() != "100" {
		t.Fatalf("recorded ask depth=%#v found=%t", liquidity, found)
	}
	invalid := input
	invalid.Inventory = append([]VenueInventory(nil), input.Inventory...)
	invalid.Inventory[0].Owner = "other-owner"
	if _, err = NewRecordedSagaClaimSet(invalid); err == nil {
		t.Fatal("inconsistent recorded ownership was accepted")
	}
	sparse := input
	sparse.Inventory = append([]VenueInventory(nil), input.Inventory...)
	sparse.Inventory[0].OwnedBase = balance("0")
	claims, err = NewRecordedSagaClaimSet(sparse)
	if err != nil {
		t.Fatalf("zero unused capacity rejected every direction: %v", err)
	}
	if _, found = claims.Resource(portfolio.ClaimKey{Kind: portfolio.ClaimBalance, Owner: first.Owner,
		Exchange: first.Exchange, Resource: string(first.BaseAsset)}); found {
		t.Fatal("zero capacity was registered as claimable")
	}
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
			Bids: view.Bids(), Asks: view.Asks(), RawPayloadHash: "sha256:replay-" + view.Exchange(),
		}, Observation: observation, Rules: market.Rules})
	}
	view := source.CoherentView
	return Input{Ordinal: 11, LogicalTime: source.DecisionOffsetNanos, Now: time.Unix(11, 0).UTC(),
		Markets: markets, Coherent: CoherentViewInput{Identity: view.Identity(), Policy: view.Policy(),
			Trigger: view.Trigger(), Members: view.Members()}, Inventory: source.Inventory,
		QuoteBudget: source.QuoteBudget, FeeBalances: source.FeeBalances,
		Configuration: source.Configuration, ConfigurationHash: source.ConfigurationHash,
		InstrumentMetadataSetHash: source.InstrumentMetadataSetHash, Restoration: source.Restoration}
}
