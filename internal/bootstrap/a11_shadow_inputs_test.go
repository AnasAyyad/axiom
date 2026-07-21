package bootstrap

import (
	"testing"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/config"
	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	postgresstore "axiom/internal/storage/postgres"
)

func TestA11ShadowProcessorUsesSameOperationalComposition(t *testing.T) {
	configuration := config.DefaultConfiguration()
	configSnapshot, snapshotErr := config.NewSnapshot(configuration, config.SourceDefault, "test", &domain.SystemClock{})
	if snapshotErr != nil {
		t.Fatal(snapshotErr)
	}
	processor, configured, snapshot, err := newA11ShadowProcessor(postgresstore.A11ShadowClaim{
		ID: "shadow-a11", RunID: "shadow-a11", AccountID: "shadow-account-a11",
		PortfolioID: "shadow-portfolio-a11", Configuration: configuration, ConfigurationHash: configSnapshot.Hash(),
		Models: backtest.ModelNamespace{ID: "shadow-models", MarketContext: "production-public",
			LiquidityDomain: "shadow-liquidity", FeeDomain: configuration.Models.Fee,
			LatencyDomain: configuration.Models.Latency, FillDomain: "fill-v1"},
	})
	if err != nil || processor == nil || configured.Version != "trend.v1a.1" || snapshot.Revision != 1 {
		t.Fatalf("shadow processor = %#v %#v %#v %v", processor, configured, snapshot, err)
	}
}

func TestA11ShadowCandleMergeIsChronologicalAndLiveWins(t *testing.T) {
	instrument, _ := domain.NewSpotInstrument("BTC", "USDT")
	start := time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)
	candle := func(offset int, marker string) exchangecontracts.Candle {
		return exchangecontracts.Candle{Instrument: instrument, Interval: "4h", OpenTime: start.Add(time.Duration(offset) * 4 * time.Hour),
			CloseTime: start.Add(time.Duration(offset+1) * 4 * time.Hour), Closed: true, RawPayloadHash: marker}
	}
	merged := mergeA11Candles([]exchangecontracts.Candle{candle(1, "history-1"), candle(0, "history-0")},
		[]exchangecontracts.Candle{candle(1, "live-1")}, start.Add(12*time.Hour))
	if len(merged) != 2 || merged[0].RawPayloadHash != "history-0" || merged[1].RawPayloadHash != "live-1" {
		t.Fatalf("merged candles = %#v", merged)
	}
}

func TestA11ShadowBookAgeFailsClosed(t *testing.T) {
	if a11BookAge(100, 90) != 10 || a11BookAge(90, 100) != time.Duration(1<<63-1) {
		t.Fatal("shadow book age did not fail closed")
	}
}
