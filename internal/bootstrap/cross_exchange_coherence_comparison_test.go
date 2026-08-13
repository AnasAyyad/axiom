package bootstrap

import (
	"context"
	"testing"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
	runtimecore "axiom/internal/runtime"
)

func TestCrossExchangeComparisonKeepsStrictAndActionableVerdictsSeparate(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	keys := []runtimecore.MarketKey{
		{Exchange: "binance", Instrument: sagaInstrument(t, "BTCUSDT")},
		{Exchange: "bybit", Instrument: sagaInstrument(t, "BTCUSDT")},
	}
	set := sagaMarketSource(t, now, keys).set
	set.Members[1].Clock.Offset = 100 * time.Millisecond
	trigger := ownerConsoleCrossExchangeSetTrigger(t, set, "bybit")
	strict, comparison := compareCrossExchangeCapture(context.Background(), keys, now, trigger, set)
	if strict.coherent.Identity() != "" || comparison.Strict.Passed || comparison.Strict.Reason != "interval" {
		t.Fatalf("strict verdict=%#v capture=%#v", comparison.Strict, strict)
	}
	if !comparison.Actionable.Passed || comparison.Actionable.ViewID == "" || len(comparison.Members) != 2 ||
		comparison.CorrectedOverlap >= 0 {
		t.Fatalf("actionable comparison=%#v", comparison)
	}
}

func TestCrossExchangeComparisonRejectsUnsafeSourceTiming(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 5, 0, 0, time.UTC)
	keys := []runtimecore.MarketKey{
		{Exchange: "binance", Instrument: sagaInstrument(t, "BTCUSDT")},
		{Exchange: "bybit", Instrument: sagaInstrument(t, "BTCUSDT")},
	}
	for name, offset := range map[string]time.Duration{
		"source_delay": 300 * time.Millisecond,
		"future":       -100 * time.Millisecond,
	} {
		t.Run(name, func(t *testing.T) {
			set := sagaMarketSource(t, now, keys).set
			set.Members[1].Clock.Offset = offset
			trigger := ownerConsoleCrossExchangeSetTrigger(t, set, "bybit")
			_, comparison := compareCrossExchangeCapture(context.Background(), keys, now, trigger, set)
			want := "source_delay"
			if name == "future" {
				want = "exchange_time_future"
			}
			if comparison.Actionable.Passed || comparison.Actionable.Reason != want {
				t.Fatalf("comparison=%#v want=%s", comparison, want)
			}
		})
	}
}

func ownerConsoleCrossExchangeSetTrigger(t *testing.T, set SandboxSagaMarketViewSet,
	exchange string,
) exchangecontracts.BookCommit {
	t.Helper()
	for _, member := range set.Members {
		if member.View.Exchange() != exchange {
			continue
		}
		observation := member.View.Observation()
		return exchangecontracts.BookCommit{Exchange: exchange, Instrument: member.View.Instrument(),
			ConnectionGeneration: member.View.Generation(), BookVersion: member.View.Version(),
			IngestOrdinal: observation.IngestOrdinal, ReceivedOffsetNanos: observation.ReceivedOffsetNanos,
			PublishedOffsetNanos: observation.PublishedOffsetNanos}
	}
	t.Fatal("trigger member missing")
	return exchangecontracts.BookCommit{}
}
