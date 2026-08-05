package runs

import "errors"

var errInvalidStrategy = errors.New("run_strategy_invalid")

// DefaultRegistry declares only combinations with an installed, shared durable
// runtime. Strategy pages may describe the reviewed future families, but this
// launch catalogue must never offer a workflow that would fail after selection.
func DefaultRegistry() (*Registry, error) {
	return NewRegistry([]Strategy{
		{ID: "trend-following", Name: "Trend Following", Explanation: "Researches sustained price direction after a finalized four-hour candle.", Version: "trend-following@1.0.0", Modes: []Mode{ModeBacktest, ModeReplay, ModeShadow}, Exchanges: []Exchange{ExchangeBinance}, Instruments: []string{"BTC/USDT", "ETH/USDT"}, Cadence: "After each finalized 4-hour candle", Warmup: "Required candle history", OrderCapable: true},
		{ID: "mean-reversion", Name: "Mean Reversion", Explanation: "Researches one-hour mean reversion only when its four-hour regime permits it.", Version: "mean-reversion@1.0.0", Modes: []Mode{ModeBacktest, ModeReplay}, Exchanges: []Exchange{ExchangeBinance}, Instruments: []string{"BTC/USDT", "ETH/USDT"}, Cadence: "After each finalized 1-hour candle with 4-hour context", Warmup: "Required 1-hour and 4-hour history", OrderCapable: true},
	})
}

func unavailableBlocker() *Blocker {
	return &Blocker{Code: "RUN_CATALOGUE_UNAVAILABLE", Summary: "Run choices are temporarily unavailable.", Detail: "The server cannot safely validate a run selection right now.", SuggestedAction: "Wait for the service to recover; do not guess raw identifiers."}
}
