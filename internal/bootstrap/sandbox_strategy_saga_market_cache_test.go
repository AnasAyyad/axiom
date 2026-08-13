package bootstrap

import (
	"context"
	"fmt"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/strategies/arbitrage"
)

func TestSandboxSagaMarketCacheCapturesOnlyPreDecisionCompleteGeneration(t *testing.T) {
	now := time.Date(2026, 8, 9, 19, 0, 0, 0, time.UTC)
	clock, err := domain.NewReplayClock(now)
	if err != nil {
		t.Fatal(err)
	}
	keys := triangularSagaKeys(t)
	cache := newTriangularSagaMarketCache(t, now, clock, keys)
	if err = cache.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	reader, err := NewSandboxSagaMarketInputReader(cache)
	if err != nil {
		t.Fatal(err)
	}
	work := sagaPlanFacts(t, "triangular", now).Coordinator.Work
	input, err := reader.ReadTriangular(context.Background(), work, now)
	if err != nil || len(input.Markets) != 3 || input.Trigger.UTC != now ||
		input.FirstDetectedOffset == 0 || input.FirstDetectedOffset > input.Trigger.MonotonicNanos {
		t.Fatalf("input=%#v error=%v", input, err)
	}
	future := now.Add(time.Millisecond)
	if _, err = cache.CaptureSandboxSagaMarketViews(context.Background(), []runtimecore.MarketKey{
		{Exchange: "bybit", Instrument: keys[0].Instrument},
	}, future); err == nil {
		t.Fatal("another exchange reused the Binance cache")
	}
}

func newTriangularSagaMarketCache(t *testing.T, now time.Time, clock domain.Clock,
	keys []runtimecore.MarketKey,
) *SandboxSagaMarketCache {
	t.Helper()
	snapshots := make(map[string]exchangecontracts.BookSnapshot, len(keys))
	rules := make([]arbitrage.InstrumentRules, 0, len(keys))
	for index, key := range keys {
		view := sagaBookView(t, key.Exchange, key.Instrument, now,
			uint64(100_000+index*10_000), uint64(index+1))
		snapshots[key.Instrument.Symbol()] = exchangecontracts.BookSnapshot{
			Exchange: exchangecontracts.ExchangeID(key.Exchange), Instrument: key.Instrument,
			LastSequence: view.Sequence(), ReceivedAt: domain.EventTime{
				UTC: now.Add(-20 * time.Millisecond), Sequence: uint64(index + 1)},
			Bids: view.Bids(), Asks: view.Asks(), RawPayloadHash: fmt.Sprintf("%064x", index+1),
		}
		rules = append(rules, sagaInstrumentRules(t, key.Exchange, key.Instrument, now))
	}
	monotonicValue := 100 * time.Millisecond
	monotonic := func() time.Duration {
		monotonicValue += time.Millisecond
		return monotonicValue
	}
	cache, err := NewSandboxSagaMarketCache(&sagaCacheSnapshots{values: snapshots}, clock,
		monotonic, sagaCacheClock{health: exchangecontracts.ClockHealth{
			ObservedAt: now.Add(-30 * time.Millisecond), Uncertainty: time.Millisecond, Eligible: true,
		}}, "binance", rules)
	if err != nil {
		t.Fatal(err)
	}
	return cache
}

func TestSandboxSagaMarketCacheRejectsClockEvidenceAfterReceipt(t *testing.T) {
	now := time.Date(2026, 8, 9, 19, 0, 0, 0, time.UTC)
	clock, err := domain.NewReplayClock(now)
	if err != nil {
		t.Fatal(err)
	}
	key := triangularSagaKeys(t)[0]
	view := sagaBookView(t, key.Exchange, key.Instrument, now, 100_000, 1)
	snapshots := map[string]exchangecontracts.BookSnapshot{key.Instrument.Symbol(): {
		Exchange: exchangecontracts.ExchangeID(key.Exchange), Instrument: key.Instrument,
		LastSequence: view.Sequence(), ReceivedAt: domain.EventTime{UTC: now.Add(-20 * time.Millisecond), Sequence: 1},
		Bids: view.Bids(), Asks: view.Asks(), RawPayloadHash: fmt.Sprintf("%064x", 1),
	}}
	monotonicValue := 100 * time.Millisecond
	cache, err := NewSandboxSagaMarketCache(&sagaCacheSnapshots{values: snapshots}, clock,
		func() time.Duration { monotonicValue += time.Millisecond; return monotonicValue },
		sagaCacheClock{health: exchangecontracts.ClockHealth{
			ObservedAt: now.Add(-time.Millisecond), Uncertainty: time.Millisecond, Eligible: true,
		}}, key.Exchange, []arbitrage.InstrumentRules{
			sagaInstrumentRules(t, key.Exchange, key.Instrument, now),
		})
	if err != nil {
		t.Fatal(err)
	}
	if err = cache.Refresh(context.Background()); err == nil || err.Error() != "sandbox_saga_market_observation_invalid" {
		t.Fatalf("error=%v", err)
	}
}

type sagaCacheSnapshots struct {
	values map[string]exchangecontracts.BookSnapshot
}

func (source *sagaCacheSnapshots) Snapshot(
	_ context.Context,
	request exchangecontracts.SnapshotRequest,
) (exchangecontracts.BookSnapshot, error) {
	value, exists := source.values[request.Instrument.Symbol()]
	if !exists || request.Depth != 50 {
		return exchangecontracts.BookSnapshot{}, fmt.Errorf("snapshot unavailable")
	}
	return value, nil
}

type sagaCacheClock struct{ health exchangecontracts.ClockHealth }

func (source sagaCacheClock) SampleServerTime(context.Context) (exchangecontracts.ClockHealth, error) {
	return source.health, nil
}
