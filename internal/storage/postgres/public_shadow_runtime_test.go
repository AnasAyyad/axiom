package postgres

import (
	"testing"

	"axiom/internal/config"
)

func TestPublicShadowSelectionRequiresConfiguredPublicVenueAndInstrument(t *testing.T) {
	configuration := config.DefaultMultiStrategyConfiguration()
	if !publicShadowSelectionConfigured(configuration, "bybit", "BTCUSDT") ||
		!publicShadowSelectionConfigured(configuration, "binance", "ETHUSDT") {
		t.Fatal("reviewed public shadow selection was not found in the configuration")
	}
	if publicShadowSelectionConfigured(configuration, "bybit", "SOLUSDT") ||
		publicShadowSelectionConfigured(config.DefaultConfiguration(), "bybit", "BTCUSDT") {
		t.Fatal("unconfigured public shadow selection was accepted")
	}
}

func TestPublicShadowClaimAcceptsOnlyInstalledConfiguredStrategyRuntime(t *testing.T) {
	configuration := config.DefaultMultiStrategyConfiguration()
	for _, claim := range []PublicShadowClaim{
		{StrategyID: "trend-following-1-0-0", StrategyVersion: "trend-following@1.0.0", Configuration: configuration},
		{StrategyID: "mean-reversion-1-0-0", StrategyVersion: "mean-reversion@1.0.0", Configuration: configuration},
		{StrategyID: "triangular-arbitrage-1-0-0", StrategyVersion: "triangular-arbitrage@1.0.0", Configuration: configuration},
		{StrategyID: "cross-exchange-arbitrage-1-0-0", StrategyVersion: "cross-exchange-arbitrage@1.0.0", Configuration: configuration},
	} {
		if !publicShadowStrategyConfigured(claim) {
			t.Fatalf("installed shadow strategy rejected: %+v", claim)
		}
	}
	if publicShadowStrategyConfigured(PublicShadowClaim{StrategyID: "cross-exchange-arbitrage-1-0-0",
		StrategyVersion: "wrong", Configuration: configuration}) {
		t.Fatal("mismatched paired-venue shadow runtime accepted")
	}
}

func TestPublicShadowClaimRequiresExactImmutablePairedVenueScope(t *testing.T) {
	configuration := config.DefaultMultiStrategyConfiguration()
	claim := PublicShadowClaim{StrategyID: "cross-exchange-arbitrage-1-0-0", StrategyVersion: "cross-exchange-arbitrage@1.0.0",
		ExchangeID: "binance", InstrumentID: "BTCUSDT", MarketScopeRequired: true,
		Configuration: configuration, MarketScopes: []PublicShadowMarketScope{
			{Ordinal: 1, ExchangeID: "binance", InstrumentID: "BTCUSDT", Purpose: "paired_market"},
			{Ordinal: 2, ExchangeID: "bybit", InstrumentID: "BTCUSDT", Purpose: "paired_market"},
		}}
	if !publicShadowMarketScopesConfigured(claim) {
		t.Fatal("exact paired venue scope was rejected")
	}
	claim.MarketScopes[1].ExchangeID = "binance"
	if publicShadowMarketScopesConfigured(claim) {
		t.Fatal("same-venue pair was accepted")
	}
}

func TestPublicShadowClaimRequiresExactImmutableTriangleMarketScope(t *testing.T) {
	configuration := config.DefaultMultiStrategyConfiguration()
	claim := PublicShadowClaim{StrategyID: "triangular-arbitrage-1-0-0", StrategyVersion: "triangular-arbitrage@1.0.0",
		ExchangeID: "binance", InstrumentID: "BTCUSDT", MarketScopeRequired: true,
		Configuration: configuration, MarketScopes: []PublicShadowMarketScope{
			{Ordinal: 1, ExchangeID: "binance", InstrumentID: "BTCUSDT", Purpose: "triangle_market"},
			{Ordinal: 2, ExchangeID: "binance", InstrumentID: "ETHBTC", Purpose: "triangle_market"},
			{Ordinal: 3, ExchangeID: "binance", InstrumentID: "ETHUSDT", Purpose: "triangle_market"},
		}}
	if !publicShadowMarketScopesConfigured(claim) {
		t.Fatal("exact reviewed Triangle scope was rejected")
	}
	claim.MarketScopes[1].InstrumentID = "BTCUSDT"
	if publicShadowMarketScopesConfigured(claim) {
		t.Fatal("duplicate Triangle market was accepted")
	}
}

func TestPublicShadowClaimRequiresExactImmutableSingleMarketScope(t *testing.T) {
	configuration := config.DefaultMultiStrategyConfiguration()
	claim := PublicShadowClaim{StrategyID: "trend-following-1-0-0", StrategyVersion: "trend-following@1.0.0",
		ExchangeID: "bybit", InstrumentID: "BTCUSDT", MarketScopeRequired: true,
		Configuration: configuration, MarketScopes: []PublicShadowMarketScope{{
			Ordinal: 1, ExchangeID: "bybit", InstrumentID: "BTCUSDT", Purpose: "primary",
		}}}
	if !publicShadowMarketScopesConfigured(claim) {
		t.Fatal("exact reviewed single-market scope was rejected")
	}
	claim.MarketScopes[0].ExchangeID = "binance"
	if publicShadowMarketScopesConfigured(claim) {
		t.Fatal("scope that disagrees with its session parent was accepted")
	}
	claim.MarketScopes = nil
	if publicShadowMarketScopesConfigured(claim) {
		t.Fatal("required market scope was omitted")
	}
}
