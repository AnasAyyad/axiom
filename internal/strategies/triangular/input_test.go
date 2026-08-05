package triangular

import (
	"encoding/json"
	"testing"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
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
