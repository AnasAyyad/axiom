package runs

import "testing"

func TestDefaultCatalogueAdvertisesOnlyInstalledRuntimesAndRejectsLive(t *testing.T) {
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
	for _, strategy := range []string{"trend-following", "mean-reversion", "triangular-arbitrage", "cross-exchange-arbitrage"} {
		choices, sandboxBlocker := registry.Catalogue(Selection{StrategyID: strategy, Mode: ModeSandbox})
		if sandboxBlocker != nil || len(choices) == 0 {
			t.Fatalf("automatic sandbox strategy %s choices=%+v blocker=%+v", strategy, choices, sandboxBlocker)
		}
	}
	choices, advisoryBlocker := registry.Catalogue(Selection{StrategyID: "inventory-rebalancing", Mode: ModeBacktest,
		Exchanges: []Exchange{ExchangeBinance, ExchangeBybit}})
	if advisoryBlocker != nil || len(choices) != 1 || choices[0].OrderCapable {
		t.Fatalf("inventory advisory choices=%+v blocker=%+v", choices, advisoryBlocker)
	}
	if _, advisoryBlocker = registry.Catalogue(Selection{StrategyID: "inventory-rebalancing", Mode: ModeSandbox}); advisoryBlocker == nil || advisoryBlocker.Code != "MODE_UNSUPPORTED" {
		t.Fatalf("inventory sandbox blocker=%+v", advisoryBlocker)
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
	if choices, blocker := registry.Catalogue(Selection{StrategyID: "mean-reversion", Mode: ModeShadow,
		Exchanges: []Exchange{ExchangeBybit}, Instrument: "BTC/USDT"}); blocker != nil || len(choices) != 1 ||
		choices[0].Exchanges[0] != ExchangeBybit {
		t.Fatalf("mean-reversion Bybit shadow choices=%+v blocker=%+v", choices, blocker)
	}
	if _, blocker := registry.Catalogue(Selection{StrategyID: "trend-following", Instrument: "SOL/USDT"}); blocker == nil || blocker.Code != "INSTRUMENT_UNSUPPORTED" {
		t.Fatalf("instrument blocker=%+v", blocker)
	}
	if choices, blocker := registry.Catalogue(Selection{StrategyID: "trend-following", Mode: ModeShadow, Exchanges: []Exchange{ExchangeBybit}}); blocker != nil || len(choices) != 1 || choices[0].Exchanges[0] != ExchangeBybit {
		t.Fatalf("bybit shadow choices=%+v blocker=%+v", choices, blocker)
	}
	if choices, blocker := registry.Catalogue(Selection{StrategyID: "triangular-arbitrage", Mode: ModeShadow,
		Exchanges: []Exchange{ExchangeBinance}, Instrument: "BTC/USDT"}); blocker != nil || len(choices) != 1 ||
		choices[0].Exchanges[0] != ExchangeBinance {
		t.Fatalf("Triangle shadow choices=%+v blocker=%+v", choices, blocker)
	}
	if choices, blocker := registry.Catalogue(Selection{StrategyID: "cross-exchange-arbitrage", Mode: ModeShadow, Exchanges: []Exchange{ExchangeBinance, ExchangeBybit}, Instrument: "BTC/USDT"}); blocker != nil || len(choices) != 1 {
		t.Fatalf("cross-exchange shadow choices=%+v blocker=%+v", choices, blocker)
	}
	choices, blocker := registry.Catalogue(Selection{StrategyID: "triangular-arbitrage", Mode: ModeBacktest})
	if blocker != nil || len(choices) != 2 || len(choices[0].Exchanges) != 1 || len(choices[1].Exchanges) != 1 {
		t.Fatalf("triangular independent choices=%+v blocker=%+v", choices, blocker)
	}
	if _, blocker = registry.Catalogue(Selection{StrategyID: "cross-exchange-arbitrage", Mode: ModeBacktest, Exchanges: []Exchange{ExchangeBinance}}); blocker == nil || blocker.Code != "EXCHANGE_UNSUPPORTED" {
		t.Fatalf("cross-exchange partial venue blocker=%+v", blocker)
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
