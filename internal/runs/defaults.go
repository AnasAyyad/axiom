package runs

import "errors"

var errInvalidStrategy = errors.New("run_strategy_invalid")

// DefaultRegistry declares only combinations with an installed, shared durable
// runtime. Strategy pages may describe the reviewed future families, but this
// launch catalogue must never offer a workflow that would fail after selection.
func DefaultRegistry() (*Registry, error) {
	return NewRegistry([]Strategy{
		{ID: "trend-following", Name: "Trend Following", Explanation: "Researches sustained price direction after a finalized four-hour candle.", Version: "trend-following@1.0.0", Modes: []Mode{ModeBacktest, ModeReplay, ModeShadow, ModeSandbox}, Exchanges: []Exchange{ExchangeBinance, ExchangeBybit}, Instruments: []string{"BTC/USDT", "ETH/USDT"}, Cadence: "After each finalized 4-hour candle", Warmup: "Required candle history", OrderCapable: true, IndependentExchanges: true},
		{ID: "mean-reversion", Name: "Mean Reversion", Explanation: "Researches one-hour mean reversion only when its four-hour regime permits it.", Version: "mean-reversion@1.0.0", Modes: []Mode{ModeBacktest, ModeReplay, ModeShadow, ModeSandbox}, Exchanges: []Exchange{ExchangeBinance, ExchangeBybit}, Instruments: []string{"BTC/USDT", "ETH/USDT"}, Cadence: "After each finalized 1-hour candle with 4-hour context", Warmup: "Required 1-hour and 4-hour history", OrderCapable: true, IndependentExchanges: true},
		{ID: "triangular-arbitrage", Name: "Triangular Arbitrage", Explanation: "Researches exact three-leg spot conversion cycles using synchronized public or recorded books.", Version: "triangular-arbitrage@1.0.0", Modes: []Mode{ModeBacktest, ModeReplay, ModeShadow, ModeSandbox}, Exchanges: []Exchange{ExchangeBinance, ExchangeBybit}, Instruments: []string{"BTC/USDT", "ETH/USDT"}, Cadence: "When a complete three-market book cycle is available", Warmup: "Three synchronized spot books", OrderCapable: true, IndependentExchanges: true},
		{ID: "cross-exchange-arbitrage", Name: "Cross-Exchange Arbitrage", Explanation: "Researches prefunded two-venue spot opportunities with recovery economics.", Version: "cross-exchange-arbitrage@1.0.0", Modes: []Mode{ModeBacktest, ModeReplay, ModeShadow, ModeSandbox}, Exchanges: []Exchange{ExchangeBinance, ExchangeBybit}, Instruments: []string{"BTC/USDT", "ETH/USDT"}, Cadence: "When coherent Binance and Bybit books are available", Warmup: "Paired synchronized spot books and inventory evidence", OrderCapable: true, ExactExchangeSet: true},
		{ID: "inventory-rebalancing", Name: "Inventory Rebalancing", Explanation: "Evaluates an owned inventory imbalance and explains a manual recommendation without moving assets or creating orders.", Version: "inventory-rebalancing@1.0.0", Modes: []Mode{ModeBacktest, ModeReplay}, Exchanges: []Exchange{ExchangeBinance, ExchangeBybit}, Instruments: []string{"BTC/USDT", "ETH/USDT"}, Cadence: "When a reviewed inventory snapshot and route-fact set are available", Warmup: "Owned inventory snapshot and complete reviewed route facts", OrderCapable: false, ExactExchangeSet: true},
	})
}

func unavailableBlocker() *Blocker {
	return &Blocker{Code: "RUN_CATALOGUE_UNAVAILABLE", Summary: "Run choices are temporarily unavailable.", Detail: "The server cannot safely validate a run selection right now.", SuggestedAction: "Wait for the service to recover; do not guess raw identifiers."}
}
