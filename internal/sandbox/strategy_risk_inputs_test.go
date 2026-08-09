package sandbox

import (
	"strings"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

func TestStrategyRiskInputsExposeOnlyExactCompleteSnapshot(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	work := validStrategyRiskWork(t, now)
	snapshot := validStrategyRiskSnapshot(t, work, now)
	instrument, err := domain.NewSpotInstrument("BTC", "USDT")
	if err != nil {
		t.Fatal(err)
	}
	market := StrategyMarketInput{Instrument: instrument,
		Metadata: exchangecontracts.InstrumentRecord{RawPayloadHash: strings.Repeat("6", 64)},
		Book:     exchangecontracts.BookSnapshot{RawPayloadHash: strings.Repeat("7", 64)},
		Candles: map[string][]exchangecontracts.Candle{
			"4h": {{RawPayloadHash: strings.Repeat("8", 64)}},
		}, ObservedAt: domain.EventTime{UTC: now, Sequence: 1}}
	facts := validStrategyRiskFacts(t, work, snapshot, now)
	observation := validStrategyRiskObservation(t, work, snapshot, market, facts, now)
	inputs, err := NewStrategyRiskInputs(work, snapshot, market, facts, observation, now)
	if err != nil {
		t.Fatal(err)
	}
	observations, policies, evaluatedAt, err := inputs.Current()
	if err != nil || len(policies) != 1 || policies[0].ID != facts.PolicyID ||
		!evaluatedAt.Equal(now) || observations.AccountDrawdown == nil || observations.Health.LeaseLost == nil {
		t.Fatalf("observations=%#v policies=%#v at=%s error=%v", observations, policies, evaluatedAt, err)
	}
	*observations.Health.LeaseLost = true
	second, _, _, err := inputs.Current()
	if err != nil || *second.Health.LeaseLost {
		t.Fatal("risk input caller mutated the immutable provider snapshot")
	}
	foreign := observation
	foreign.PolicyHash = strings.Repeat("9", 64)
	if _, err = NewStrategyRiskInputs(work, snapshot, market, facts, foreign, now); err == nil {
		t.Fatal("risk inputs accepted a policy-substituted observation")
	}
}
