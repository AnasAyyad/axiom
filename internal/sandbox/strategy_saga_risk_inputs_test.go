package sandbox

import (
	"strings"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

func TestStrategySagaRiskInputsConservativelyAggregatePairedEvidence(t *testing.T) {
	now := time.Date(2026, 8, 9, 16, 0, 0, 0, time.UTC)
	binance, bybit := pairedSagaRiskMembers(t, now)
	inputs, err := NewStrategySagaRiskInputs([]StrategySagaRiskMember{bybit, binance}, now)
	if err != nil {
		t.Fatal(err)
	}
	assertConservativeSagaRiskAggregate(t, inputs, now)
	assertSagaRiskEvidenceIdentity(t, inputs)
}

func pairedSagaRiskMembers(t *testing.T, now time.Time) (StrategySagaRiskMember, StrategySagaRiskMember) {
	t.Helper()
	binance := sagaRiskMember(t, ExchangeBinance, "binance-risk-account", now)
	bybit := sagaRiskMember(t, ExchangeBybit, "bybit-risk-account", now)
	binance.Observation.AssetExposure = sagaPercent(t, "0.10")
	bybit.Observation.AssetExposure = sagaPercent(t, "0.20")
	binance.Observation.CombinedExposure = sagaPercent(t, "0.15")
	bybit.Observation.CombinedExposure = sagaPercent(t, "0.25")
	binance.Observation.ExchangeExposure = sagaPercent(t, "0.20")
	bybit.Observation.ExchangeExposure = sagaPercent(t, "0.30")
	binance.Observation.ReservedCapital = sagaPercent(t, "0.05")
	bybit.Observation.ReservedCapital = sagaPercent(t, "0.07")
	binance.Observation.Reserve = sagaPercent(t, "0.40")
	bybit.Observation.Reserve = sagaPercent(t, "0.25")
	binance.Observation.UTCDayLoss = sagaPercent(t, "0.01")
	bybit.Observation.UTCDayLoss = sagaPercent(t, "0.02")
	binance.Observation.OpenOrders = 1
	bybit.Observation.OpenOrders = 2
	binance.Observation.BookAge = 10 * time.Millisecond
	bybit.Observation.BookAge = 20 * time.Millisecond
	binance.Observation.ClockDrift = -5 * time.Millisecond
	bybit.Observation.ClockDrift = 8 * time.Millisecond
	binance.Observation.QualityScore = 99
	bybit.Observation.QualityScore = 91
	bybit.Observation.APIError = true
	return binance, bybit
}

func assertConservativeSagaRiskAggregate(t *testing.T, inputs *StrategySagaRiskInputs, now time.Time) {
	t.Helper()
	observations, policies, evaluatedAt, err := inputs.Current()
	if err != nil {
		t.Fatalf("current policies=%#v at=%v error=%v", policies, evaluatedAt, err)
	}
	if len(policies) != 1 || !evaluatedAt.Equal(now) {
		t.Fatalf("current policies=%#v at=%v", policies, evaluatedAt)
	}
	if observations.AssetExposure.String() != "0.3" || observations.CombinedExposure.String() != "0.4" ||
		observations.ExchangeExposure.String() != "0.5" || observations.ReservedCapital.String() != "0.12" ||
		observations.Reserve.String() != "0.25" || observations.UTCDayLoss.String() != "0.02" ||
		*observations.OpenOrders != 3 || *observations.BookAge != 20*time.Millisecond ||
		*observations.ClockDrift != 8*time.Millisecond || *observations.QualityScore != 91 ||
		observations.Health.APIError == nil || !*observations.Health.APIError {
		t.Fatalf("non-conservative aggregate: %#v", observations)
	}
	*observations.OpenOrders = 99
	again, _, _, _ := inputs.Current()
	if *again.OpenOrders != 3 {
		t.Fatal("caller mutated the immutable aggregate")
	}
}

func assertSagaRiskEvidenceIdentity(t *testing.T, inputs *StrategySagaRiskInputs) {
	t.Helper()
	evidence, identity := inputs.Evidence()
	if len(evidence) != 2 || len(identity) != 64 || evidence[0].Exchange != ExchangeBinance ||
		evidence[1].Exchange != ExchangeBybit || evidence[0].ObservationHash == evidence[1].ObservationHash {
		t.Fatalf("evidence=%#v identity=%q", evidence, identity)
	}
}

func TestStrategySagaRiskInputsRejectIncompleteTopologyAndCrossPolicy(t *testing.T) {
	now := time.Date(2026, 8, 9, 16, 5, 0, 0, time.UTC)
	binance := sagaRiskMember(t, ExchangeBinance, "binance-risk-account", now)
	bybit := sagaRiskMember(t, ExchangeBybit, "bybit-risk-account", now)
	for name, members := range map[string][]StrategySagaRiskMember{
		"missing_peer": {binance},
		"duplicate_exchange": {binance, func() StrategySagaRiskMember {
			item := bybit
			item.Work.Account.Exchange = ExchangeBinance
			return item
		}()},
		"stale_observation": {binance, func() StrategySagaRiskMember {
			item := bybit
			item.Observation.ObservedAt = now.Add(-time.Second)
			return item
		}()},
		"different_policy": {binance, func() StrategySagaRiskMember {
			item := bybit
			item.Facts.PolicyHash = strings.Repeat("9", 64)
			item.Observation.PolicyHash = item.Facts.PolicyHash
			return item
		}()},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewStrategySagaRiskInputs(members, now); err == nil {
				t.Fatal("unsafe aggregate accepted")
			}
		})
	}
}

func TestStrategySagaRiskInputsAcceptSingleTriangularAccount(t *testing.T) {
	now := time.Date(2026, 8, 9, 16, 10, 0, 0, time.UTC)
	member := sagaRiskMember(t, ExchangeBybit, "triangular-risk-account", now)
	member.Work.Strategy = StrategyTriangular
	member.Observation = validStrategyRiskObservation(t, member.Work, member.Snapshot,
		member.Market, member.Facts, now)
	inputs, err := NewStrategySagaRiskInputs([]StrategySagaRiskMember{member}, now)
	if err != nil {
		t.Fatal(err)
	}
	evidence, identity := inputs.Evidence()
	if len(evidence) != 1 || len(identity) != 64 {
		t.Fatalf("evidence=%#v identity=%q", evidence, identity)
	}
}

func sagaRiskMember(t *testing.T, exchange Exchange, account AccountID, now time.Time) StrategySagaRiskMember {
	t.Helper()
	work := validStrategyRiskWork(t, now)
	work.Strategy = StrategyCrossExchangeArbitrage
	work.Account.ID = account
	work.Account.Exchange = exchange
	snapshot := validStrategyRiskSnapshot(t, work, now)
	facts := validStrategyRiskFacts(t, work, snapshot, now)
	instrument, err := domain.NewSpotInstrument("BTC", "USDT")
	if err != nil {
		t.Fatal(err)
	}
	metadataDigit, bookDigit := "6", "7"
	if exchange == ExchangeBybit {
		metadataDigit, bookDigit = "8", "9"
	}
	market := StrategyMarketInput{Instrument: instrument,
		Metadata:   exchangecontracts.InstrumentRecord{RawPayloadHash: strings.Repeat(metadataDigit, 64)},
		Book:       exchangecontracts.BookSnapshot{RawPayloadHash: strings.Repeat(bookDigit, 64)},
		ObservedAt: domain.EventTime{UTC: now, Sequence: 1}}
	observation := validStrategyRiskObservation(t, work, snapshot, market, facts, now)
	return StrategySagaRiskMember{Work: work, Snapshot: snapshot, Market: market,
		Facts: facts, Observation: observation}
}

func sagaPercent(t *testing.T, value string) domain.Percent {
	t.Helper()
	parsed, err := domain.ParsePercent(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
