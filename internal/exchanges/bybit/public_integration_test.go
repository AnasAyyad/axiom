package bybit

import (
	"context"
	"os"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

func TestProductionPublicBybitSurface(t *testing.T) {
	if os.Getenv("AXIOM_B1_LIVE_PUBLIC") != "1" {
		t.Skip("AXIOM_B1_LIVE_PUBLIC=1 is required")
	}
	client, err := NewPublicClient("bybit-public-v1", &domain.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if health, healthErr := client.SampleServerTime(ctx); healthErr != nil || health.ObservedAt.IsZero() {
		t.Fatalf("server time = %#v, %v", health, healthErr)
	}
	instrument := approvedInstruments()[0]
	if records, metadataErr := client.Instruments(ctx, []domain.Instrument{instrument}); metadataErr != nil || len(records) != 1 {
		t.Fatalf("metadata count = %d, %v", len(records), metadataErr)
	}
	if snapshot, snapshotErr := client.Snapshot(ctx,
		(exchangecontracts.SnapshotRequest{Instrument: instrument, Depth: 1000})); snapshotErr != nil || snapshot.LastSequence == 0 || len(snapshot.Bids) == 0 || len(snapshot.Asks) == 0 {
		t.Fatalf("snapshot = %#v, %v", snapshot, snapshotErr)
	}
	if ticker, tickerErr := client.Ticker(ctx, instrument); tickerErr != nil || ticker.Exchange != "bybit" {
		t.Fatalf("ticker = %#v, %v", ticker, tickerErr)
	}
	end := time.Now().UTC()
	if candles, candleErr := client.Candles(ctx, exchangecontracts.CandleRequest{HistoryRequest: exchangecontracts.HistoryRequest{Instrument: instrument, Start: end.Add(-4 * time.Hour),
		End: end, Limit: 2}, Interval: "1h"}); candleErr != nil || len(candles) == 0 {
		t.Fatalf("candles = %d, %v", len(candles), candleErr)
	}
}
