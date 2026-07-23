package config

import "axiom/internal/domain"

// MeanReversionParameterCount is the immutable size of the B3 baseline graph.
const MeanReversionParameterCount = 27

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

func defaultMeanReversionConfiguration() MeanReversionConfiguration {
	parameter := func(id, description, algorithm, value, unit, minimum, maximum string,
		minimumInclusive, maximumInclusive bool, scale uint8, rounding, warmUp string,
		dependencies ...string,
	) StrategyParameter {
		return StrategyParameter{ID: id, Description: description, Value: value, Unit: unit,
			Minimum: minimum, Maximum: maximum, MinimumInclusive: minimumInclusive,
			MaximumInclusive: maximumInclusive, Scale: scale, Rounding: rounding,
			Cadence: "completed_1h_candle", WarmUp: warmUp, Mutability: "immutable_per_run",
			ModelDependencies: dependencies, AlgorithmVersion: algorithm,
			EvaluationTimezone: "UTC", ChangeBehavior: "existing positions retain their opening configuration; new decisions require a new snapshot",
			ApprovalActor: "authoritative_specification", ApprovalReference: "AX-V1B-B03-FUN-001/AX-V1B-B03-SAF-001",
			ApprovedAt: "2026-07-22T00:00:00Z", ChangeReason: "initial immutable B3 baseline"}
	}
	parameters := []StrategyParameter{
		parameter("mean_reversion.primary_timeframe_hours", "UTC-aligned completed primary candle duration.", "utc-candle-hours.v1", "1", "hours", "1", "1", true, true, 0, "half_even", "28_primary_candles", "candle_model"),
		parameter("mean_reversion.higher_timeframe_hours", "UTC-aligned completed higher-timeframe candle duration.", "utc-candle-hours.v1", "4", "hours", "4", "4", true, true, 0, "half_even", "210_higher_timeframe_candles", "candle_model"),
		parameter("mean_reversion.zscore_period", "Completed primary closes used by the rolling deviation model.", "population-zscore.v1", "20", "completed_candles", "2", "1000", true, true, 0, "half_even", "20_primary_candles", "candle_model"),
		parameter("mean_reversion.adx_period", "Wilder directional-movement period.", "wilder-adx.v1", "14", "completed_candles", "2", "1000", true, true, 0, "half_even", "28_primary_candles", "candle_model"),
		parameter("mean_reversion.adx_threshold", "Entry requires ADX strictly below this value.", "wilder-adx.v1", "25", "adx_points", "0", "100", false, true, 18, "half_even", "28_primary_candles", "candle_model"),
		parameter("mean_reversion.ema_regime_period", "Higher-timeframe EMA period for dangerous-downtrend rejection.", "simple-mean-seeded-ema.v1", "200", "completed_candles", "2", "10000", true, true, 0, "half_even", "210_higher_timeframe_candles", "candle_model"),
		parameter("mean_reversion.ema_decline_lookback", "Completed higher-timeframe candles across which EMA decline is measured.", "ema-decline-fraction.v1", "10", "completed_candles", "1", "1000", true, true, 0, "half_even", "210_higher_timeframe_candles", "candle_model"),
		parameter("mean_reversion.ema_decline_threshold", "Inclusive EMA decline fraction that is strongly declining.", "ema-decline-fraction.v1", "0.005", "decimal_fraction", "0", "1", false, true, 18, "half_even", "210_higher_timeframe_candles", "candle_model"),
		parameter("mean_reversion.entry_zscore", "Inclusive downside z-score entry boundary.", "population-zscore.v1", "-2", "standard_deviations", "-100", "0", true, false, 18, "half_even", "20_primary_candles", "candle_model"),
		parameter("mean_reversion.normal_exit_zscore", "Inclusive normalized z-score exit boundary.", "population-zscore.v1", "-0.25", "standard_deviations", "-100", "100", true, true, 18, "half_even", "20_primary_candles", "candle_model"),
		parameter("mean_reversion.protective_exit_zscore", "Inclusive adverse z-score protective exit boundary.", "population-zscore.v1", "-3.5", "standard_deviations", "-100", "0", true, false, 18, "half_even", "20_primary_candles", "candle_model", "gap_model"),
		parameter("mean_reversion.atr_period", "Wilder ATR period used by the protective stop and stressed sizing.", "wilder-atr.v1", "14", "completed_candles", "1", "1000", true, true, 0, "half_even", "28_primary_candles", "candle_model"),
		parameter("mean_reversion.protective_stop_atr_multiplier", "ATR multiple below entry used by the nominal protective stop.", "mean-reversion-stressed-stop.v1", "2.5", "decimal_multiplier", "0", "100", false, true, 18, "half_even", "28_primary_candles", "candle_model", "gap_model"),
		parameter("mean_reversion.maximum_holding_period", "Maximum completed primary candles held before a required exit.", "bounded-holding.v1", "12", "completed_candles", "1", "1000", true, true, 0, "half_even", "0_candles", "position_model"),
		parameter("mean_reversion.protective_loss_cooldown", "Completed primary candles blocked after a protective loss.", "protective-cooldown.v1", "3", "completed_candles", "0", "1000", true, true, 0, "half_even", "0_candles", "position_model"),
		parameter("mean_reversion.trade_risk_budget", "Maximum stressed loss as a fraction of mean-reversion equity.", "mean-reversion-stressed-sizing.v1", "0.0025", "decimal_fraction", "0", "0.0025", false, true, 18, "down", "210_higher_timeframe_candles", "fee_model", "slippage_model", "gap_model"),
		parameter("mean_reversion.maximum_positions", "Maximum open mean-reversion positions per asset and portfolio.", "single-position.v1", "1", "count", "1", "1", true, true, 0, "down", "0_candles", "position_model", "portfolio_policy"),
		parameter("mean_reversion.candidate_lifetime", "Maximum age of a candidate before allocation.", "post-signal-timing.v1", "5", "seconds", "0", "5", false, true, 0, "down", "210_higher_timeframe_candles", "latency_model"),
		parameter("mean_reversion.marketable_limit_validity", "Maximum lifetime of the simulated marketable-limit order.", "post-signal-timing.v1", "5", "seconds", "0", "5", false, true, 0, "down", "210_higher_timeframe_candles", "fill_model", "latency_model"),
		parameter("mean_reversion.candle_finalization_delay", "Delay after final publication before evaluation.", "completed-candle-finality.v1", "2", "seconds", "0", "60", true, true, 0, "down", "210_higher_timeframe_candles", "candle_model"),
		parameter("mean_reversion.signal_evaluation_window", "Evaluation must finish strictly before this post-finalization duration.", "completed-candle-finality.v1", "5", "seconds", "0", "5", false, true, 0, "down", "210_higher_timeframe_candles", "candle_model"),
		parameter("mean_reversion.arrival_book_max_age", "Arrival market view age must be strictly below this value.", "arrival-view-age.v1", "250", "milliseconds", "0", "250", false, true, 0, "down", "210_higher_timeframe_candles", "market_view_model"),
		parameter("mean_reversion.maximum_spread", "Inclusive spread limit for entry eligibility.", "spread-limit.v1", "0.001", "decimal_fraction", "0", "0.001", true, true, 18, "down", "210_higher_timeframe_candles", "spread_model", "risk_policy"),
		parameter("mean_reversion.maximum_simulated_slippage", "Inclusive arrival-price slippage and marketable-limit cap.", "slippage-limit.v1", "0.005", "decimal_fraction", "0", "0.005", true, true, 18, "down", "210_higher_timeframe_candles", "slippage_model"),
		parameter("mean_reversion.maximum_gap_allowance", "Inclusive stressed gap allowance below the nominal stop.", "gap-stress-limit.v1", "0.01", "decimal_fraction", "0", "0.01", true, true, 18, "down", "210_higher_timeframe_candles", "gap_model"),
		parameter("mean_reversion.maximum_notional", "Maximum notional for one mean-reversion simulated order.", "mean-reversion-allocation-cap.v1", "75", "USDT", "0", "75", false, true, 18, "down", "210_higher_timeframe_candles", "instrument_metadata", "portfolio_policy"),
		parameter("mean_reversion.maximum_allocation", "Maximum fraction of mean-reversion equity allocated to one position.", "mean-reversion-allocation-cap.v1", "0.10", "decimal_fraction", "0", "0.10", false, true, 18, "down", "210_higher_timeframe_candles", "portfolio_policy", "risk_policy"),
	}
	return MeanReversionConfiguration{StrategyVersion: "mean-reversion.v1b.1", PrimaryTimeframe: "1h",
		HigherTimeframe: "4h", Parameters: parameters}
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
