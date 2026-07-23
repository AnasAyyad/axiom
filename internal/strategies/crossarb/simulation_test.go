package crossarb

import (
	"testing"

	"axiom/internal/domain"
	"axiom/internal/execution"
)

type fixtureTimeline struct {
	markets         map[string]Market
	recoveryMarkets map[string]Market
	recoveryAfter   uint64
	directives      map[string]LegDirective
}

func (timeline *fixtureTimeline) MarketAt(
	exchange string,
	_ domain.Instrument,
	offset uint64,
) (Market, error) {
	if offset >= timeline.recoveryAfter {
		if market, ok := timeline.recoveryMarkets[exchange]; ok {
			return market, nil
		}
	}
	market, ok := timeline.markets[exchange]
	if !ok {
		return Market{}, strategyError("timeline_market_missing")
	}
	return market, nil
}

func (timeline *fixtureTimeline) DirectiveAt(
	exchange string,
	phase TimelinePhase,
	_ uint64,
) (LegDirective, error) {
	directive, ok := timeline.directives[exchange+":"+string(phase)]
	if !ok {
		return LegDirective{}, strategyError("timeline_directive_missing")
	}
	return directive, nil
}

type b5SimulationOutcomeCase struct {
	name       string
	directives map[string]LegDirective
	policy     RecoveryPolicy
	want       SimulationOutcome
	recovery   bool
	quarantine bool
}

func b5SimulationOutcomeCases() []b5SimulationOutcomeCase {
	return []b5SimulationOutcomeCase{
		{name: "both filled", directives: arrivalStates(execution.OrderFilled, execution.OrderFilled),
			policy: RecoveryPolicy{}, want: OutcomeBothFilled},
		{name: "buy only", directives: arrivalStates(execution.OrderFilled, execution.OrderExpired),
			policy: RecoveryPolicy{}, want: OutcomeBuyOnly, recovery: true},
		{name: "sell only", directives: arrivalStates(execution.OrderExpired, execution.OrderFilled),
			policy: RecoveryPolicy{}, want: OutcomeSellOnly, recovery: true},
		{name: "partial buy", directives: arrivalInputs(
			LegDirective{State: execution.OrderPartiallyFilled, Input: quantity("5")},
			LegDirective{State: execution.OrderExpired}),
			policy: RecoveryPolicy{}, want: OutcomePartialBuy, recovery: true},
		{name: "partial sell", directives: arrivalInputs(
			LegDirective{State: execution.OrderExpired},
			LegDirective{State: execution.OrderPartiallyFilled, Input: quantity("0.04")}),
			policy: RecoveryPolicy{}, want: OutcomePartialSell, recovery: true},
		{name: "partial both", directives: arrivalInputs(
			LegDirective{State: execution.OrderPartiallyFilled, Input: quantity("5")},
			LegDirective{State: execution.OrderPartiallyFilled, Input: quantity("0.04")}),
			policy: RecoveryPolicy{}, want: OutcomePartialBoth, recovery: true},
		{name: "both missed", directives: arrivalStates(execution.OrderExpired, execution.OrderRejected),
			policy: RecoveryPolicy{}, want: OutcomeBothMissed},
		{name: "unknown verified retry", directives: unknownRetryStates(),
			policy: RecoveryPolicy{RiskAllowsRetry: true, MaximumRetries: 1}, want: OutcomeBothFilled},
		{name: "unknown unresolved", directives: unknownUnresolvedStates(),
			policy: RecoveryPolicy{}, want: OutcomeDelayedUnknown, quarantine: true},
	}
}

func TestConcurrentSimulationAllOutcomeClassesAndProtectedRecovery(t *testing.T) {
	for _, test := range b5SimulationOutcomeCases() {
		t.Run(test.name, func(t *testing.T) {
			candidate, timeline := simulationFixture(t)
			timeline.directives = test.directives
			result, err := Simulate(candidate, timeline, testLatency(), test.policy)
			if err != nil {
				t.Fatal(err)
			}
			if result.Outcome != test.want || result.Recovery.UnwindSucceeded != test.recovery ||
				result.Recovery.Quarantined != test.quarantine {
				t.Fatalf("result = %#v", result)
			}
			if test.recovery && result.Saga.State != execution.PlanRecovered {
				t.Fatalf("recovered saga = %#v", result.Saga)
			}
			if test.quarantine && result.Saga.State != execution.PlanQuarantined {
				t.Fatalf("quarantined saga = %#v", result.Saga)
			}
			restored, restoreErr := RestoreSimulation(result)
			if restoreErr != nil || restored.CanonicalHash != result.CanonicalHash {
				t.Fatalf("restore = %#v, %v", restored, restoreErr)
			}
		})
	}
}

func TestConcurrentSimulationRejectsNegativeBeforeArrivalAndTampering(t *testing.T) {
	candidate, timeline := simulationFixture(t)
	timeline.markets["binance"] = testMarket(t, "binance", candidate.Instrument, "105", "106", 3)
	timeline.markets["bybit"] = testMarket(t, "bybit", candidate.Instrument, "104", "105", 4)
	timeline.directives = arrivalStates(execution.OrderFilled, execution.OrderFilled)
	result, err := Simulate(candidate, timeline, testLatency(), RecoveryPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != OutcomeNegativeBeforeArrival || result.Saga.State != execution.PlanFailed {
		t.Fatalf("negative arrival = %#v", result)
	}
	result.Outcome = OutcomeBothFilled
	if _, err = RestoreSimulation(result); err == nil {
		t.Fatal("tampered simulation checkpoint accepted")
	}
}

func TestConcurrentSimulationIsDeterministicAcrossRuns(t *testing.T) {
	candidate, timeline := simulationFixture(t)
	timeline.directives = arrivalStates(execution.OrderFilled, execution.OrderExpired)
	first, err := Simulate(candidate, timeline, testLatency(), RecoveryPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Simulate(candidate, timeline, testLatency(), RecoveryPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if first.CanonicalHash != second.CanonicalHash {
		t.Fatalf("hashes differ: %s != %s", first.CanonicalHash, second.CanonicalHash)
	}
}

func simulationFixture(t *testing.T) (Candidate, *fixtureTimeline) {
	t.Helper()
	input := evaluationFixture(t, "BTC", false)
	candidates, err := Evaluate(input)
	if err != nil {
		t.Fatal(err)
	}
	candidate := candidates[0]
	timeline := &fixtureTimeline{
		markets: map[string]Market{
			"binance": input.Markets[0], "bybit": input.Markets[1],
		},
		recoveryMarkets: map[string]Market{
			"binance": testMarket(t, "binance", candidate.Instrument, "99", "100", 3),
			"bybit":   testMarket(t, "bybit", candidate.Instrument, "89", "90", 4),
		},
		recoveryAfter: 240,
	}
	return candidate, timeline
}

func arrivalStates(buy, sell execution.OrderState) map[string]LegDirective {
	return arrivalInputs(LegDirective{State: buy}, LegDirective{State: sell})
}

func arrivalInputs(buy, sell LegDirective) map[string]LegDirective {
	return map[string]LegDirective{
		"binance:arrival": buy,
		"bybit:arrival":   sell,
	}
}

func unknownRetryStates() map[string]LegDirective {
	return map[string]LegDirective{
		"binance:arrival":      {State: execution.OrderUnknown},
		"binance:verification": {State: execution.OrderUnknown},
		"binance:retry":        {State: execution.OrderFilled},
		"bybit:arrival":        {State: execution.OrderFilled},
	}
}

func unknownUnresolvedStates() map[string]LegDirective {
	return map[string]LegDirective{
		"binance:arrival":      {State: execution.OrderUnknown},
		"binance:verification": {State: execution.OrderUnknown},
		"bybit:arrival":        {State: execution.OrderExpired},
	}
}

func testLatency() LatencyDistribution {
	return LatencyDistribution{
		Version:         "latency-distribution-b5.v1",
		BuySamplesNanos: []uint64{10}, SellSamplesNanos: []uint64{20},
		SampleOrdinal: 0, VerificationNanos: 5, RetryNanos: 5, RecoveryNanos: 30,
	}
}
