package runs

import "testing"

func TestDefaultCatalogueAdvertisesEveryStrategyAndRejectsLive(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	choices, blocker := registry.Catalogue(Selection{})
	if blocker != nil || len(choices) == 0 {
		t.Fatalf("choices=%d blocker=%+v", len(choices), blocker)
	}
	seen := map[string]bool{}
	for _, choice := range choices {
		seen[choice.StrategyID] = true
	}
	for _, strategy := range []string{"trend-following", "mean-reversion", "triangular-arbitrage", "cross-exchange-arbitrage", "inventory-rebalancing"} {
		if !seen[strategy] {
			t.Fatalf("missing strategy %s", strategy)
		}
	}
	if _, blocker = registry.Catalogue(Selection{Mode: "live"}); blocker == nil || blocker.Code != "LIVE_MODE_FORBIDDEN" {
		t.Fatalf("live blocker=%+v", blocker)
	}
}

func TestCatalogueReturnsPlainBlockersAndDoesNotSubstituteSelection(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if _, blocker := registry.Catalogue(Selection{StrategyID: "inventory-rebalancing", Mode: ModeDemo}); blocker == nil || blocker.Code != "MODE_UNSUPPORTED" {
		t.Fatalf("mode blocker=%+v", blocker)
	}
	if _, blocker := registry.Catalogue(Selection{StrategyID: "trend-following", Instrument: "SOL/USDT"}); blocker == nil || blocker.Code != "INSTRUMENT_UNSUPPORTED" {
		t.Fatalf("instrument blocker=%+v", blocker)
	}
	if _, blocker := registry.Catalogue(Selection{StrategyID: "trend-following", Mode: ModeTestnet, Exchanges: []Exchange{ExchangeBybit}}); blocker == nil || blocker.Code != "EXCHANGE_UNSUPPORTED" {
		t.Fatalf("sandbox-exchange blocker=%+v", blocker)
	}
	choices, blocker := registry.Catalogue(Selection{StrategyID: "cross-exchange-arbitrage", Mode: ModeShadow, Exchanges: []Exchange{ExchangeBinance, ExchangeBybit}, Instrument: "BTC/USDT"})
	if blocker != nil || len(choices) != 1 || choices[0].StrategyVersion != "cross-exchange-arbitrage@1.0.0" {
		t.Fatalf("choices=%+v blocker=%+v", choices, blocker)
	}
}

func TestRegistryRejectsLiveAndIncompleteStrategyMetadata(t *testing.T) {
	if _, err := NewRegistry([]Strategy{{ID: "unsafe", Name: "Unsafe", Explanation: "Unsafe", Version: "unsafe@1", Modes: []Mode{"live"}, Exchanges: []Exchange{ExchangeBinance}, Instruments: []string{"BTC/USDT"}, Cadence: "now", Warmup: "none"}}); err == nil {
		t.Fatal("live metadata accepted")
	}
	if _, err := NewRegistry([]Strategy{{ID: "bad"}}); err == nil {
		t.Fatal("incomplete strategy accepted")
	}
}
