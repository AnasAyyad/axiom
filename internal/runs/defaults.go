package runs

import "errors"

var errInvalidStrategy = errors.New("run_strategy_invalid")

// DefaultRegistry declares the reviewed V1 strategy support matrix. The
// runtime binds each metadata record to its existing production package before
// a run can start; this catalogue never supplies an alternate evaluator.
func DefaultRegistry() (*Registry, error) {
	allResearch := []Mode{ModeDemonstration, ModeBacktest, ModeReplay, ModeShadow}
	return NewRegistry([]Strategy{
		{ID: "trend-following", Name: "Trend Following", Explanation: "Researches sustained price direction after a finalized four-hour candle.", Version: "trend-following@1.0.0", Modes: append(append([]Mode(nil), allResearch...), ModeTestnet, ModeDemo), Exchanges: []Exchange{ExchangeBinance, ExchangeBybit}, ModeExchanges: map[Mode][]Exchange{ModeTestnet: []Exchange{ExchangeBinance}, ModeDemo: []Exchange{ExchangeBybit}}, Instruments: []string{"BTC/USDT", "ETH/USDT"}, Cadence: "After each finalized 4-hour candle", Warmup: "Required candle history", OrderCapable: true},
		{ID: "mean-reversion", Name: "Mean Reversion", Explanation: "Researches one-hour mean reversion only when its four-hour regime permits it.", Version: "mean-reversion@1.0.0", Modes: append(append([]Mode(nil), allResearch...), ModeTestnet, ModeDemo), Exchanges: []Exchange{ExchangeBinance, ExchangeBybit}, ModeExchanges: map[Mode][]Exchange{ModeTestnet: []Exchange{ExchangeBinance}, ModeDemo: []Exchange{ExchangeBybit}}, Instruments: []string{"BTC/USDT", "ETH/USDT"}, Cadence: "After each finalized 1-hour candle with 4-hour context", Warmup: "Required 1-hour and 4-hour history", OrderCapable: true},
		{ID: "triangular-arbitrage", Name: "Triangular Arbitrage", Explanation: "Researches synchronized three-market spot conversion cycles on one exchange.", Version: "triangular-arbitrage@1.0.0", Modes: append(append([]Mode(nil), allResearch...), ModeTestnet, ModeDemo), Exchanges: []Exchange{ExchangeBinance, ExchangeBybit}, ModeExchanges: map[Mode][]Exchange{ModeTestnet: []Exchange{ExchangeBinance}, ModeDemo: []Exchange{ExchangeBybit}}, Instruments: []string{"BTC/USDT", "ETH/USDT", "ETH/BTC"}, Cadence: "When synchronized order books change", Warmup: "Healthy synchronized three-market books", OrderCapable: true},
		{ID: "cross-exchange-arbitrage", Name: "Cross-Exchange Arbitrage", Explanation: "Researches paired spot opportunities while preserving separately owned venue inventory.", Version: "cross-exchange-arbitrage@1.0.0", Modes: allResearch, Exchanges: []Exchange{ExchangeBinance, ExchangeBybit}, Instruments: []string{"BTC/USDT", "ETH/USDT"}, Cadence: "When coherent Binance and Bybit books change", Warmup: "Healthy coherent paired books and inventory", OrderCapable: true},
		{ID: "inventory-rebalancing", Name: "Inventory Rebalancing", Explanation: "Explains imbalances and recommends manual routes; it never submits a transfer or order.", Version: "inventory-rebalancing@1.0.0", Modes: allResearch, Exchanges: []Exchange{ExchangeBinance, ExchangeBybit}, Instruments: []string{"BTC/USDT", "ETH/USDT"}, Cadence: "When inventory or route facts change", Warmup: "Current inventory and reviewed route facts", OrderCapable: false},
	})
}

func unavailableBlocker() *Blocker {
	return &Blocker{Code: "RUN_CATALOGUE_UNAVAILABLE", Summary: "Run choices are temporarily unavailable.", Detail: "The server cannot safely validate a run selection right now.", SuggestedAction: "Wait for the service to recover; do not guess raw identifiers."}
}
