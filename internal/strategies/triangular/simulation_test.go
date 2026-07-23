package triangular

import (
	"errors"
	"testing"

	"axiom/internal/domain"
	"axiom/internal/execution"
)

func TestSequentialSimulationUsesArrivalBooksAndActualLegOutput(t *testing.T) {
	input := profitableInput(t, false)
	candidate := candidateFor(t, input, CycleUSDTBTCETHUSDT, "10")
	timeline := &scriptedTimeline{markets: input.Markets}
	result, err := Simulate(candidate, timeline, testLatency())
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != OutcomeFullSuccess || len(result.Legs) != 3 ||
		result.Saga.State != execution.PlanCompleted || result.FinalUSDT.Compare(candidate.Start) <= 0 ||
		result.CanonicalHash == "" {
		t.Fatalf("unexpected full cycle: %#v", result)
	}
	if result.Legs[1].Input != result.Legs[0].NetOutput ||
		result.Legs[2].Input != result.Legs[1].NetOutput {
		t.Fatal("displayed input was reused instead of actual rounded output")
	}

	worse := profitableInput(t, false)
	worse.Markets[1] = triangleMarket(t, "binance", "ETH", "USDT",
		[][2]string{{"45", "10"}}, [][2]string{{"46", "10"}}, "USDT", "0.0001")
	result, err = Simulate(candidate, &scriptedTimeline{markets: worse.Markets}, testLatency())
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != OutcomeNegativeAfterLatency || result.Saga.State != execution.PlanCompleted {
		t.Fatalf("arrival deterioration was not recorded: %#v", result)
	}
}

func TestSequentialSimulationRecoversOrQuarantinesPartialCycle(t *testing.T) {
	input := profitableInput(t, false)
	candidate := candidateFor(t, input, CycleUSDTBTCETHUSDT, "10")
	timeline := &scriptedTimeline{
		markets: input.Markets, failures: map[string]bool{"BTC/ETH": true},
	}
	result, err := Simulate(candidate, timeline, testLatency())
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != OutcomePartialCycle || !result.Recovery.Recovered ||
		result.Recovery.Quarantined || result.Saga.State != execution.PlanRecovered ||
		len(result.Saga.RecoveryAttempts) != 1 {
		t.Fatalf("partial recovery was not persisted: %#v", result)
	}

	timeline.failures["BTC/USDT"] = true
	result, err = Simulate(candidate, timeline, testLatency())
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != OutcomeStrandedAsset || !result.Recovery.Quarantined ||
		result.Saga.State != execution.PlanQuarantined ||
		len(result.Saga.RemainingExposure) != 1 {
		t.Fatalf("unresolved exposure was not quarantined: %#v", result)
	}
}

func TestSequentialSimulationMissedFirstLegDoesNotTerminateAsCompleted(t *testing.T) {
	input := profitableInput(t, false)
	candidate := candidateFor(t, input, CycleUSDTBTCETHUSDT, "10")
	result, err := Simulate(candidate, &scriptedTimeline{
		markets: input.Markets, failures: map[string]bool{"USDT/BTC": true},
	}, testLatency())
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != OutcomeMissedLeg || result.Saga.State != execution.PlanFailed ||
		result.Saga.FinalDisposition != "no_leg_filled" {
		t.Fatalf("missed leg incorrectly completed plan: %#v", result)
	}
}

func TestSequentialSimulationIsRestartAndPermutationStable(t *testing.T) {
	input := profitableInput(t, false)
	candidate := candidateFor(t, input, CycleUSDTBTCETHUSDT, "25")
	first, err := Simulate(candidate, &scriptedTimeline{markets: input.Markets}, testLatency())
	if err != nil {
		t.Fatal(err)
	}
	input.Markets[0], input.Markets[2] = input.Markets[2], input.Markets[0]
	second, err := Simulate(candidate, &scriptedTimeline{markets: input.Markets}, testLatency())
	if err != nil {
		t.Fatal(err)
	}
	if first.CanonicalHash != second.CanonicalHash {
		t.Fatalf("permutation changed replay: %s != %s", first.CanonicalHash, second.CanonicalHash)
	}
}

type scriptedTimeline struct {
	markets  []Market
	failures map[string]bool
}

func (timeline *scriptedTimeline) MarketAt(
	exchange string,
	source, target domain.AssetSymbol,
	_ uint64,
) (Market, error) {
	if timeline.failures[string(source)+"/"+string(target)] {
		return Market{}, errors.New("scripted missing book")
	}
	return selectMarket(timeline.markets, source, target)
}

func candidateFor(t *testing.T, input EvaluationInput, cycle Cycle, start string) Candidate {
	t.Helper()
	candidates, err := Evaluate(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range candidates {
		if candidate.Cycle == cycle && candidate.Start.String() == start {
			return candidate
		}
	}
	t.Fatalf("candidate %s/%s missing", cycle, start)
	return Candidate{}
}

func testLatency() LatencyModel {
	return LatencyModel{Version: "latency.v1", LegNanos: [3]uint64{10, 20, 30}, RecoveryNanos: 10}
}
