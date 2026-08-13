package bootstrap

import (
	"context"
	"fmt"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/marketdata"
	runtimecore "axiom/internal/runtime"
)

func TestPublicShadowSagaMarketSourceBuildsCoherentUSDTFeeRules(t *testing.T) {
	now := time.Date(2026, 8, 9, 18, 0, 0, 0, time.UTC)
	source, keys, collectors := newPublicShadowSagaTestSource(t, now)
	for index, key := range keys {
		collectors[key.Instrument].(*publicShadowSagaCollector).health.ClockOffset = time.Duration(index+1) * time.Second
	}
	set, err := source.CaptureSandboxSagaMarketViews(context.Background(), keys, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range set.Members {
		if member.Clock.Offset != 0 || member.Clock.Uncertainty != time.Millisecond || !member.Clock.Eligible {
			t.Fatalf("member did not use the single capture clock: %#v", member.Clock)
		}
	}
	reader, err := NewSandboxSagaMarketInputReader(source)
	if err != nil {
		t.Fatal(err)
	}
	capture, err := reader.capture(context.Background(), keys, now)
	if err != nil || len(capture.members) != 3 || capture.coherent.Identity() == "" {
		t.Fatalf("coherent owner console capture=%#v error=%v", capture, err)
	}
	assertPublicShadowSagaFeeRules(t, capture)
	collectors[keys[1].Instrument].(*publicShadowSagaCollector).health.Eligible = false
	if _, err = reader.capture(context.Background(), keys, now); err == nil {
		t.Fatal("ineligible public member was accepted")
	}
}

func newPublicShadowSagaTestSource(t *testing.T, now time.Time) (
	*publicShadowSagaMarketSource, []runtimecore.MarketKey,
	map[domain.Instrument]shadowPublicCollector,
) {
	t.Helper()
	keys := []runtimecore.MarketKey{
		{Exchange: "binance", Instrument: sagaInstrument(t, "BTCUSDT")},
		{Exchange: "binance", Instrument: sagaInstrument(t, "ETHBTC")},
		{Exchange: "binance", Instrument: sagaInstrument(t, "ETHUSDT")},
	}
	collectors := make(map[domain.Instrument]shadowPublicCollector, len(keys))
	metadata := make(map[domain.Instrument]domain.InstrumentMetadata, len(keys))
	maximum := make(map[domain.Instrument]domain.Quantity, len(keys))
	prices := map[string][2]string{
		"BTCUSDT": {"29999", "30000"}, "ETHBTC": {"0.0666", "0.0667"}, "ETHUSDT": {"1999", "2000"},
	}
	for index, key := range keys {
		view := sagaBookViewPrices(t, key.Exchange, key.Instrument, now,
			uint64(900_000+index*10_000), uint64(index+1), prices[key.Instrument.Symbol()][0],
			prices[key.Instrument.Symbol()][1])
		collectors[key.Instrument] = &publicShadowSagaCollector{provider: publicShadowSagaViews{view: view},
			health: exchangecontracts.CollectorHealthSnapshot{Exchange: key.Exchange,
				Instrument: key.Instrument.Symbol(), BookHealth: "HEALTHY", BookHealthy: true,
				BookFresh: true, BookEligible: true, ClockEligible: true,
				ClockObservedAt: now.Add(-time.Second), ClockUncertainty: time.Millisecond, Eligible: true}}
		tick, _ := domain.ParsePrice("0.0001")
		step, _ := domain.ParseQuantity("0.0001")
		minimumNotional, _ := domain.ParseNotional("0.01")
		metadata[key.Instrument] = domain.InstrumentMetadata{Instrument: key.Instrument, Version: 1,
			EffectiveAt: now.Add(-time.Minute), PriceTick: tick, QuantityStep: step,
			MinimumQuantity: step, MinimumNotional: minimumNotional}
		maximum[key.Instrument], _ = domain.ParseQuantity("1000")
	}
	fee, _ := domain.ParseRate("0.001")
	source := &publicShadowSagaMarketSource{claimID: "triangle-shadow", exchange: "binance",
		monotonic: publicShadowSagaMonotonic(1_000_000),
		clock: publicShadowSagaClock{health: exchangecontracts.ClockHealth{ObservedAt: now.Add(-time.Second),
			Uncertainty: time.Millisecond, Eligible: true}}, collectors: collectors, metadata: metadata,
		maximumQuantity: maximum, feeVersion: "fixed-bps-v1", feeRate: fee, collectorRegion: "engine-local"}
	return source, keys, collectors
}

func assertPublicShadowSagaFeeRules(t *testing.T, capture validatedSandboxSagaMarketCapture) {
	t.Helper()
	for _, member := range capture.members {
		if member.rules.Fee.Asset != "USDT" || member.rules.Fee.Version != "fixed-bps-v1" ||
			member.rules.MaximumQuantity.String() != "1000" {
			t.Fatalf("public shadow rules=%#v", member.rules)
		}
		if member.view.Instrument().Symbol() == "ETHBTC" &&
			member.rules.Fee.ThirdAssetPriceInQuote.String() != "0.000033333333333333" {
			t.Fatalf("conservative settlement fee mark=%s", member.rules.Fee.ThirdAssetPriceInQuote)
		}
	}
}

type publicShadowSagaMonotonic uint64

func (value publicShadowSagaMonotonic) MonotonicOffset() uint64 { return uint64(value) }

type publicShadowSagaClock struct{ health exchangecontracts.ClockHealth }

func (clock publicShadowSagaClock) ClockHealth() exchangecontracts.ClockHealth { return clock.health }

type publicShadowSagaCollector struct {
	provider marketdata.MarketViewProvider
	health   exchangecontracts.CollectorHealthSnapshot
}

func (*publicShadowSagaCollector) Run(context.Context) error { return nil }
func (collector *publicShadowSagaCollector) Views() marketdata.MarketViewProvider {
	return collector.provider
}
func (collector *publicShadowSagaCollector) HealthSnapshot() exchangecontracts.CollectorHealthSnapshot {
	return collector.health
}

type publicShadowSagaViews struct{ view marketdata.BookView }

func (views publicShadowSagaViews) Book(exchange string, instrument domain.Instrument) (marketdata.BookView, error) {
	if views.view.Exchange() != exchange || views.view.Instrument() != instrument {
		return marketdata.BookView{}, fmt.Errorf("book unavailable")
	}
	return views.view, nil
}

func (publicShadowSagaViews) CompletedCandles(string, domain.Instrument, string) (marketdata.CandleView, error) {
	return marketdata.CandleView{}, fmt.Errorf("candles unavailable")
}
