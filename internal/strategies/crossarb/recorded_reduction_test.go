package crossarb

import (
	"reflect"
	"testing"

	"axiom/internal/domain"
	"axiom/internal/execution"
)

func TestCleanRecordedReductionProjectsSeparateVenueBalancesAndRejectsTampering(t *testing.T) {
	input := inputFromEvaluation(t, evaluationFixture(t, "BTC", false))
	input.Simulation = filledRecordedSimulation(input)
	before := recordedVenueBalances(input)
	prepared, projection, err := AttachCleanRecordedReduction(input, "shadow/cross/test", before)
	if err != nil || projection == nil || prepared.Reduction == nil ||
		projection.Simulation.Outcome != OutcomeBothFilled ||
		projection.Simulation.Saga.State != execution.PlanCompleted {
		t.Fatalf("projection=%#v error=%v", projection, err)
	}
	if reflect.DeepEqual(before, projection.VenueBalances) ||
		projection.VenueBalances["binance"][asset("BTC")].Compare(before["binance"][asset("BTC")]) <= 0 ||
		projection.VenueBalances["bybit"][asset("BTC")].Compare(before["bybit"][asset("BTC")]) >= 0 {
		t.Fatalf("before=%#v after=%#v", before, projection.VenueBalances)
	}
	verified, err := ValidateCleanRecordedReduction(prepared, before)
	if err != nil || !reflect.DeepEqual(verified, projection) {
		t.Fatalf("verified=%#v error=%v", verified, err)
	}
	tampered := prepared
	tampered.Reduction = &ReductionInput{Attribution: prepared.Reduction.Attribution,
		Reconciliation: prepared.Reduction.Reconciliation}
	tampered.Reduction.Attribution.Fees, _ = domain.ParseBalance("999")
	if _, err = ValidateCleanRecordedReduction(tampered, before); err == nil {
		t.Fatal("tampered attribution accepted")
	}
}

func TestCleanRecordedReductionLeavesOrdinaryNoActionUnreduced(t *testing.T) {
	source := evaluationFixture(t, "BTC", false)
	source.Markets[1] = testMarket(t, "bybit", source.Markets[0].Book.Instrument(), "99", "100", 2)
	source.CoherentView = coherentFixture(t, source.Markets, 200)
	input := inputFromEvaluation(t, source)
	prepared, projection, err := AttachCleanRecordedReduction(input, "shadow/cross/test", recordedVenueBalances(input))
	if err != nil || projection != nil || prepared.Reduction != nil {
		t.Fatalf("prepared=%#v projection=%#v error=%v", prepared, projection, err)
	}
}

func filledRecordedSimulation(input Input) *SimulationInput {
	latency := testLatency()
	result := &SimulationInput{Latency: latency, Recovery: RecoveryPolicy{}}
	for _, offset := range []uint64{input.LogicalTime + latency.BuySamplesNanos[0],
		input.LogicalTime + latency.SellSamplesNanos[0]} {
		for _, market := range input.Markets {
			result.Markets = append(result.Markets, TimedMarketInput{Offset: offset, Market: market})
		}
	}
	result.Directives = []TimedDirective{
		{Exchange: "binance", Phase: PhaseArrival,
			Offset:    input.LogicalTime + latency.BuySamplesNanos[0],
			Directive: LegDirective{State: execution.OrderFilled}},
		{Exchange: "bybit", Phase: PhaseArrival,
			Offset:    input.LogicalTime + latency.SellSamplesNanos[0],
			Directive: LegDirective{State: execution.OrderFilled}},
	}
	return result
}

func recordedVenueBalances(input Input) VenueBalances {
	zero, _ := domain.ParseBalance("0")
	result := VenueBalances{}
	for _, inventory := range input.Inventory {
		result[inventory.Exchange] = map[domain.AssetSymbol]domain.Balance{
			asset("USDT"):       inventory.OwnedUSDT,
			inventory.BaseAsset: inventory.OwnedBase,
		}
		other := asset("ETH")
		if inventory.BaseAsset == other {
			other = asset("BTC")
		}
		result[inventory.Exchange][other] = zero
	}
	return result
}
