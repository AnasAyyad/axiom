package bootstrap

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/sandbox"
	"axiom/internal/strategies/arbitrage"
)

func TestSandboxSagaMarketCacheSetPublishesOneCompleteCrossVenueGeneration(t *testing.T) {
	now := time.Date(2026, 8, 9, 21, 0, 0, 0, time.UTC)
	clock, err := domain.NewReplayClock(now)
	if err != nil {
		t.Fatal(err)
	}
	instrument := sagaInstrument(t, "BTCUSDT")
	set := newCrossVenueSagaCacheSet(t, now, clock, instrument)
	if err = set.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	reader, err := NewSandboxSagaMarketInputReader(set)
	if err != nil {
		t.Fatal(err)
	}
	work := sagaPlanFacts(t, sandbox.StrategyCrossExchangeArbitrage, now).Coordinator.Work
	input, err := reader.ReadCrossExchange(context.Background(), work, now)
	if err != nil || len(input.Markets) != 2 || len(input.Coherent.Members) != 2 ||
		input.Trigger.MonotonicNanos == 0 || input.Trigger.UTC != now {
		t.Fatalf("input=%#v error=%v", input, err)
	}
	if _, err = set.CaptureSandboxSagaMarketViews(context.Background(), []runtimecore.MarketKey{{
		Exchange: "binance", Instrument: sagaInstrument(t, "ETHUSDT"),
	}}, now); err == nil {
		t.Fatal("unconfigured market escaped the complete cache set")
	}
}

func newCrossVenueSagaCacheSet(t *testing.T, now time.Time, clock domain.Clock,
	instrument domain.Instrument,
) *SandboxSagaMarketCacheSet {
	t.Helper()
	var monotonicValue atomic.Int64
	monotonicValue.Store(int64(100 * time.Millisecond))
	monotonic := func() time.Duration {
		return time.Duration(monotonicValue.Add(int64(time.Millisecond)))
	}
	caches := make([]*SandboxSagaMarketCache, 0, 2)
	for index, exchange := range []string{"binance", "bybit"} {
		view := sagaBookView(t, exchange, instrument, now, uint64(100_000+index*10_000), uint64(index+1))
		source := &sagaCacheSnapshots{values: map[string]exchangecontracts.BookSnapshot{
			instrument.Symbol(): {Exchange: exchangecontracts.ExchangeID(exchange), Instrument: instrument,
				LastSequence: view.Sequence(), ReceivedAt: domain.EventTime{
					UTC: now.Add(-20 * time.Millisecond), Sequence: uint64(index + 1)},
				Bids: view.Bids(), Asks: view.Asks(), RawPayloadHash: fmt.Sprintf("%064x", index+1)},
		}}
		cache, cacheErr := NewSandboxSagaMarketCache(source, clock, monotonic,
			sagaCacheClock{health: exchangecontracts.ClockHealth{ObservedAt: now.Add(-30 * time.Millisecond),
				Uncertainty: time.Millisecond, Eligible: true}}, exchange,
			[]arbitrage.InstrumentRules{sagaInstrumentRules(t, exchange, instrument, now)})
		if cacheErr != nil {
			t.Fatal(cacheErr)
		}
		caches = append(caches, cache)
	}
	set, err := NewSandboxSagaMarketCacheSet(caches, monotonic)
	if err != nil {
		t.Fatal(err)
	}
	return set
}
