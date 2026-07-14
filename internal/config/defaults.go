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
		Capabilities: UnsupportedCapabilities(),
	}
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
