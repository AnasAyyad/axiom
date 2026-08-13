package bootstrap

import (
	"testing"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/config"
	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	marketrecorder "axiom/internal/recorder"
	runtimecore "axiom/internal/runtime"
	postgresstore "axiom/internal/storage/postgres"
	"axiom/internal/storage/segments"
	"axiom/internal/strategies/meanreversion"
	"axiom/internal/strategies/trend"
)

func TestPublicShadowProcessorUsesSameOperationalComposition(t *testing.T) {
	configuration := config.DefaultConfiguration()
	configSnapshot, snapshotErr := config.NewSnapshot(configuration, config.SourceDefault, "test", &domain.SystemClock{})
	if snapshotErr != nil {
		t.Fatal(snapshotErr)
	}
	processor, configured, _, _, snapshot, err := newPublicShadowProcessor(postgresstore.PublicShadowClaim{
		ID: "shadow-owner_console", RunID: "shadow-owner_console", AccountID: "shadow-account-owner_console",
		PortfolioID: "shadow-portfolio-owner_console", StrategyID: "trend-following-1-0-0", StrategyVersion: "trend-following@1.0.0",
		Configuration: configuration, ConfigurationHash: configSnapshot.Hash(),
		Models: backtest.ModelNamespace{ID: "shadow-models", MarketContext: "production-public",
			LiquidityDomain: "shadow-liquidity", FeeDomain: configuration.Models.Fee,
			LatencyDomain: configuration.Models.Latency, FillDomain: "fill-v1"},
	})
	if err != nil || processor == nil || configured.Version != "trend-following@1.0.0" || snapshot.Revision != 1 {
		t.Fatalf("shadow processor = %#v %#v %#v %v", processor, configured, snapshot, err)
	}
}

func TestOwnerConsoleMeanReversionShadowProcessorUsesSameOperationalComposition(t *testing.T) {
	configuration := config.DefaultMultiStrategyConfiguration()
	configSnapshot, snapshotErr := config.NewSnapshot(configuration, config.SourceDefault, "test", &domain.SystemClock{})
	if snapshotErr != nil {
		t.Fatal(snapshotErr)
	}
	processor, _, configured, _, snapshot, err := newPublicShadowProcessor(postgresstore.PublicShadowClaim{
		ID: "mean-shadow-owner_console", RunID: "mean-shadow-owner_console", AccountID: "mean-shadow-account-owner_console",
		PortfolioID: "mean-shadow-portfolio-owner_console", StrategyID: "mean-reversion-1-0-0",
		StrategyVersion: "mean-reversion@1.0.0", ExchangeID: "bybit",
		Configuration: configuration, ConfigurationHash: configSnapshot.Hash(),
		Models: backtest.ModelNamespace{ID: "shadow-models", MarketContext: "production-public",
			LiquidityDomain: "shadow-liquidity", FeeDomain: configuration.Models.Fee,
			LatencyDomain: configuration.Models.Latency, FillDomain: "fill-v1"},
	})
	if err != nil || processor == nil || configured.Version != "mean-reversion@1.0.0" || snapshot.Revision != 1 {
		t.Fatalf("mean shadow processor = %#v %#v %#v %v", processor, configured, snapshot, err)
	}
}

func TestOwnerConsoleTriangularShadowProcessorHasExplicitSingleVenueOwnership(t *testing.T) {
	configuration := config.DefaultMultiStrategyConfiguration()
	configSnapshot, snapshotErr := config.NewSnapshot(configuration, config.SourceDefault, "test", &domain.SystemClock{})
	if snapshotErr != nil {
		t.Fatal(snapshotErr)
	}
	processor, _, _, configured, snapshot, err := newPublicShadowProcessor(postgresstore.PublicShadowClaim{
		ID: "triangular-shadow-owner_console", RunID: "triangular-shadow-owner_console",
		AccountID: "triangular-shadow-account-owner_console", PortfolioID: "triangular-shadow-portfolio-owner_console",
		StrategyID: "triangular-arbitrage-1-0-0", StrategyVersion: "triangular-arbitrage@1.0.0", ExchangeID: "bybit",
		Configuration: configuration, ConfigurationHash: configSnapshot.Hash(),
		Models: backtest.ModelNamespace{ID: "shadow-models", MarketContext: "production-public",
			LiquidityDomain: "shadow-liquidity", FeeDomain: configuration.Models.Fee,
			LatencyDomain: configuration.Models.Latency, FillDomain: "fill-v1"},
	})
	if err != nil || processor == nil || configured.StrategyVersion != "triangular-arbitrage@1.0.0" ||
		snapshot.Revision != 1 || snapshot.Ownership.Strategy != "triangular" ||
		snapshot.Ownership.Exchange != "bybit" || snapshot.Balances["USDT"].Available.String() != "500" ||
		snapshot.Balances["BTC"].Available.String() != "0" || snapshot.Balances["ETH"].Available.String() != "0" {
		t.Fatalf("triangular shadow processor=%T configured=%#v snapshot=%#v error=%v",
			processor, configured, snapshot, err)
	}
}

func TestTriangularShadowConsumesEachETHBTCBookVersionOnce(t *testing.T) {
	session := &ownerConsoleLiveShadowSession{}
	if session.consumeTriangularTrigger(0, 1) || session.consumeTriangularTrigger(1, 0) {
		t.Fatal("invalid trigger identity was consumed")
	}
	if !session.consumeTriangularTrigger(1, 1) {
		t.Fatal("first ETHBTC book version was not consumed")
	}
	if session.consumeTriangularTrigger(1, 1) {
		t.Fatal("same ETHBTC book version was consumed twice")
	}
	if !session.consumeTriangularTrigger(1, 2) {
		t.Fatal("next ETHBTC book version was not consumed")
	}
	if session.consumeTriangularTrigger(1, 1) {
		t.Fatal("regressed ETHBTC book version was consumed")
	}
	if !session.consumeTriangularTrigger(2, 1) {
		t.Fatal("first version after reconnect was not consumed")
	}
	if session.consumeTriangularTrigger(1, 3) {
		t.Fatal("regressed ETHBTC connection generation was consumed")
	}
}

func TestPublicShadowCandleMergeIsChronologicalAndLiveWins(t *testing.T) {
	instrument, _ := domain.NewSpotInstrument("BTC", "USDT")
	start := time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)
	candle := func(offset int, marker string) exchangecontracts.Candle {
		return exchangecontracts.Candle{Instrument: instrument, Interval: "4h", OpenTime: start.Add(time.Duration(offset) * 4 * time.Hour),
			CloseTime: start.Add(time.Duration(offset+1) * 4 * time.Hour), Closed: true, RawPayloadHash: marker}
	}
	merged := mergeOwnerConsoleCandles([]exchangecontracts.Candle{candle(1, "history-1"), candle(0, "history-0")},
		[]exchangecontracts.Candle{candle(1, "live-1")}, start.Add(12*time.Hour))
	if len(merged) != 2 || merged[0].RawPayloadHash != "history-0" || merged[1].RawPayloadHash != "live-1" {
		t.Fatalf("merged candles = %#v", merged)
	}
}

func TestPublicShadowBookAgeFailsClosed(t *testing.T) {
	if ownerConsoleBookAge(100, 90) != 10 || ownerConsoleBookAge(90, 100) != time.Duration(1<<63-1) {
		t.Fatal("shadow book age did not fail closed")
	}
}

func TestPublicShadowSimulationBookRetainsSelectedPublicVenue(t *testing.T) {
	instrument, err := domain.NewSpotInstrument("BTC", "USDT")
	if err != nil {
		t.Fatal(err)
	}
	price, err := domain.ParsePrice("100")
	if err != nil {
		t.Fatal(err)
	}
	timeline := ownerConsoleInputTimeline{input: trend.Input{Instrument: instrument, LogicalTime: 10,
		Sizing: trend.SizingState{FirstExecutablePrice: price}, Evidence: trend.InputEvidence{MarketViewRevision: 3}},
		exchange: "bybit"}
	state, found, err := timeline.AtOrAfter(instrument, 10)
	if err != nil || !found || state.Exchange != "bybit" || state.Instrument != instrument {
		t.Fatalf("state=%+v found=%t error=%v", state, found, err)
	}
}

func TestOwnerConsoleMeanReversionShadowSimulationBookRetainsSelectedPublicVenue(t *testing.T) {
	instrument, err := domain.NewSpotInstrument("BTC", "USDT")
	if err != nil {
		t.Fatal(err)
	}
	price, err := domain.ParsePrice("100")
	if err != nil {
		t.Fatal(err)
	}
	timeline := ownerConsoleMeanReversionInputTimeline{input: meanreversion.Input{Instrument: instrument, LogicalTime: 10,
		Sizing:   meanreversion.SizingState{FirstExecutablePrice: price},
		Evidence: meanreversion.InputEvidence{MarketViewRevision: 3}}, exchange: "bybit"}
	state, found, err := timeline.AtOrAfter(instrument, 10)
	if err != nil || !found || state.Exchange != "bybit" || state.Instrument != instrument {
		t.Fatalf("state=%+v found=%t error=%v", state, found, err)
	}
}

func TestPublicShadowMarketRuntimeInstallsExactInstrumentOnBothPublicVenues(t *testing.T) {
	configuration := config.DefaultMultiStrategyConfiguration()
	recorderRuntime := config.RecorderRuntime{QueueCapacity: 4096, BookDepth: 1000}
	for _, exchange := range []string{"binance", "bybit"} {
		t.Run(exchange, func(t *testing.T) {
			recorder, err := marketrecorder.New(t.TempDir(), "shadow-public-"+exchange,
				"shadow-session-"+exchange, exchange, &runtimecore.IngestOrdinals{},
				func(segments.Manifest) error { return nil }, nil)
			if err != nil {
				t.Fatal(err)
			}
			client, collectors, err := newPublicShadowMarketRuntime(postgresstore.PublicShadowClaim{
				ExchangeID: exchange, InstrumentID: "BTCUSDT", Configuration: configuration,
			}, recorderRuntime, recorder, &domain.SystemClock{})
			if err != nil || client == nil || len(collectors) != 1 {
				t.Fatalf("client=%T collectors=%d error=%v", client, len(collectors), err)
			}
			for instrument := range collectors {
				if instrument.Symbol() != "BTCUSDT" {
					t.Fatalf("unexpected selected instrument %s", instrument.Symbol())
				}
			}
		})
	}
}

func TestPublicShadowScopeInstrumentsUsesExactImmutableMembership(t *testing.T) {
	claim := postgresstore.PublicShadowClaim{ExchangeID: "binance", InstrumentID: "BTCUSDT",
		MarketScopes: []postgresstore.PublicShadowMarketScope{
			{Ordinal: 1, ExchangeID: "binance", InstrumentID: "BTCUSDT", Purpose: "triangle_market"},
			{Ordinal: 2, ExchangeID: "binance", InstrumentID: "ETHUSDT", Purpose: "triangle_market"},
			{Ordinal: 3, ExchangeID: "binance", InstrumentID: "ETHBTC", Purpose: "triangle_market"},
		}}
	selected, err := publicShadowScopeInstruments(claim, "binance")
	if err != nil || len(selected) != 3 || !selected["BTCUSDT"] || !selected["ETHUSDT"] || !selected["ETHBTC"] {
		t.Fatalf("selected scopes=%v error=%v", selected, err)
	}
	claim.MarketScopes[2].ExchangeID = "bybit"
	if _, err = publicShadowScopeInstruments(claim, "binance"); err == nil {
		t.Fatal("cross-venue scope was accepted by a single-venue collector runtime")
	}
}
