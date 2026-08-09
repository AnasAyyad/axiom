package triangular

import (
	"reflect"
	"testing"
)

func TestAttachCleanRecordedReductionIsReproducibleAndTamperEvident(t *testing.T) {
	input := recordedReductionInput(t)
	prepared, projection, err := AttachCleanRecordedReduction(input, "shadow/triangular/session-a")
	if err != nil || projection == nil || prepared.Reduction == nil {
		t.Fatalf("prepared=%#v projection=%#v error=%v", prepared.Reduction, projection, err)
	}
	replayed, err := ValidateCleanRecordedReduction(prepared)
	if err != nil || !reflect.DeepEqual(projection, replayed) {
		t.Fatalf("replayed=%#v error=%v", replayed, err)
	}
	tampered := prepared
	tampered.Reduction = &ReductionInput{Reconciliation: cloneReductionInput(*prepared.Reduction).Reconciliation}
	tampered.Reduction.Reconciliation.Actual.Balances = prepared.Reduction.Reconciliation.Actual.Orders
	if _, err = ValidateCleanRecordedReduction(tampered); err == nil {
		t.Fatal("tampered terminal balance projection was accepted")
	}
}

func TestAttachCleanRecordedReductionLeavesOrdinaryNoActionUnreduced(t *testing.T) {
	input := recordedReductionInput(t)
	input.FeeBalances[asset("USDT")] = balance("0")
	prepared, projection, err := AttachCleanRecordedReduction(input, "shadow/triangular/session-a")
	if err != nil || projection != nil || prepared.Reduction != nil {
		t.Fatalf("prepared=%#v projection=%#v error=%v", prepared.Reduction, projection, err)
	}
}

func recordedReductionInput(t *testing.T) Input {
	t.Helper()
	input := inputFromEvaluation(t, profitableInput(t, false))
	input.Simulation = &SimulationInput{Latency: testLatency()}
	for _, offset := range []uint64{input.LogicalTime + 1, input.LogicalTime + 2,
		input.LogicalTime + 3, input.LogicalTime + 4} {
		for _, market := range input.Markets {
			input.Simulation.Markets = append(input.Simulation.Markets,
				TimedMarketInput{Offset: offset, Market: market})
		}
	}
	return input
}
