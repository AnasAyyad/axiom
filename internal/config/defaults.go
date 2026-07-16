package config

import "axiom/internal/domain"

// DefaultConfiguration returns a credential-free, paused V1A paper configuration.
func DefaultConfiguration() Configuration {
	return Configuration{
		SchemaVersion: SchemaVersion,
		Revision:      1,
		Environment:   EnvironmentLocal,
		Mode:          ModePaper,
		Product:       domain.ProductSpot,
		Safety: SafetyConfiguration{
			FailClosed: true, RiskInitialState: "PAUSED", AutoUnpause: false,
		},
		Endpoint: EndpointConfiguration{
			Set:       "market-data-only-v1",
			REST:      "https://data-api.binance.vision",
			WebSocket: "wss://data-stream.binance.vision",
		},
		Assets: domain.DefaultAssets(),
		Instruments: []Instrument{
			{Base: "BTC", Quote: "USDT", Product: "spot"},
			{Base: "ETH", Quote: "USDT", Product: "spot"},
		},
		Risk: RiskConfiguration{
			MaximumAssetAllocation: percentValue("0.25"),
			MaximumOrderNotional:   moneyValue("1000"),
			MaximumDailyLoss:       moneyValue("100"),
		},
		Portfolio:    PortfolioConfiguration{SettlementAsset: "USDT", StartingCapital: moneyValue("500")},
		Models:       ModelConfiguration{Fee: "fixed-bps-v1", Latency: "fixed-zero-v1"},
		Trend:        defaultTrendConfiguration(),
		Capabilities: UnsupportedCapabilities(),
	}
}

func defaultTrendConfiguration() TrendConfiguration {
	parameter := func(id, description, value, unit, minimum, maximum string, minimumInclusive, maximumInclusive bool, scale uint8, rounding, warmUp string, dependencies ...string) StrategyParameter {
		return StrategyParameter{ID: id, Description: description, Value: value, Unit: unit,
			Minimum: minimum, Maximum: maximum, MinimumInclusive: minimumInclusive,
			MaximumInclusive: maximumInclusive, Scale: scale, Rounding: rounding,
			Cadence: "completed_4h_candle", WarmUp: warmUp, Mutability: "immutable_per_run",
			ModelDependencies: dependencies}
	}
	return TrendConfiguration{StrategyVersion: "trend.v1a.1", Timeframe: "4h", Parameters: []StrategyParameter{
		parameter("trend.ema_confirmation_period", "Completed-candle EMA used for trend confirmation and the secondary exit.", "50", "completed_candles", "1", "10000", true, true, 0, "half_even", "50_candles", "candle_model"),
		parameter("trend.ema_regime_period", "Completed-candle EMA used for the long-term regime.", "200", "completed_candles", "2", "10000", true, true, 0, "half_even", "200_candles", "candle_model"),
		parameter("trend.breakout_lookback", "Prior completed candles used by the strict breakout test.", "20", "completed_candles", "1", "1000", true, true, 0, "half_even", "20_candles", "candle_model"),
		parameter("trend.atr_period", "Wilder ATR period used by stops and stressed sizing.", "14", "completed_candles", "1", "1000", true, true, 0, "half_even", "15_candles", "candle_model"),
		parameter("trend.initial_stop_atr_multiplier", "ATR multiple below actual simulated entry for the initial protective stop.", "2.5", "decimal_multiplier", "0", "100", false, true, 18, "half_even", "200_candles", "candle_model", "fill_model"),
		parameter("trend.trailing_stop_atr_multiplier", "ATR multiple below the highest favorable completed close for the trailing stop.", "3", "decimal_multiplier", "0", "100", false, true, 18, "half_even", "200_candles", "candle_model", "fill_model"),
		parameter("trend.protective_loss_cooldown", "Completed candles blocked after a protective loss.", "3", "completed_candles", "0", "1000", true, true, 0, "half_even", "0_candles", "position_model"),
		parameter("trend.trade_risk_budget", "Maximum stressed loss as a fraction of current Trend virtual equity.", "0.005", "decimal_fraction", "0", "0.01", false, true, 18, "down", "200_candles", "fee_model", "latency_model", "gap_model"),
		parameter("trend.maximum_notional", "Maximum Trend notional for one simulated order.", "150", "USDT", "0", "150", false, true, 18, "down", "200_candles", "instrument_metadata", "portfolio_policy"),
		parameter("trend.maximum_simulated_slippage", "Inclusive marketable-limit protection around the arrival reference.", "0.005", "decimal_fraction", "0", "0.005", true, true, 18, "down", "200_candles", "slippage_model"),
		parameter("trend.candidate_lifetime", "Maximum age of a Trend candidate before allocation.", "5", "seconds", "0", "5", false, true, 0, "down", "200_candles", "latency_model"),
		parameter("trend.marketable_limit_validity", "Maximum lifetime of the simulated marketable-limit order.", "5", "seconds", "0", "5", false, true, 0, "down", "200_candles", "fill_model", "latency_model"),
		parameter("trend.arrival_book_max_age", "Arrival book age must be strictly below this value.", "250", "milliseconds", "0", "250", false, true, 0, "down", "200_candles", "market_view_model"),
		parameter("trend.signal_evaluation_window", "Decision evaluation must finish strictly before this duration after final publication.", "5", "seconds", "0", "5", false, true, 0, "down", "200_candles", "candle_model"),
		parameter("trend.candle_finalization_delay", "Delay after close publication before a candle becomes decision eligible.", "2", "seconds", "0", "60", true, true, 0, "down", "200_candles", "candle_model"),
		parameter("trend.maximum_positions", "Maximum open Trend positions per instrument and portfolio.", "1", "count", "1", "1", true, true, 0, "down", "200_candles", "position_model"),
	}}
}

func percentValue(value string) FinancialValue {
	return FinancialValue{
		Value: value, Unit: "decimal_fraction", Minimum: "0", Maximum: "1",
		MinimumInclusive: true, MaximumInclusive: true, Scale: 8, Rounding: "down",
	}
}

func moneyValue(value string) FinancialValue {
	return FinancialValue{
		Value: value, Unit: "USDT", Minimum: "0", Maximum: "1000000",
		MinimumInclusive: false, MaximumInclusive: true, Scale: 8, Rounding: "half_even",
	}
}
