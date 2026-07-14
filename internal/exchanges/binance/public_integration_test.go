package binance

import (
	"context"
	"os"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

func TestProductionPublicBinanceSurface(t *testing.T) {
	if os.Getenv("AXIOM_BINANCE_PUBLIC_INTEGRATION") != "1" {
		t.Skip("set AXIOM_BINANCE_PUBLIC_INTEGRATION=1 to probe compiled public endpoints")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	clock := &domain.SystemClock{}
	client, err := NewPublicClient(publicEndpointSet, clock)
	if err != nil {
		t.Fatal(err)
	}
	btc, _ := domain.NewSpotInstrument("BTC", "USDT")
	eth, _ := domain.NewSpotInstrument("ETH", "USDT")
	exerciseProductionPublicREST(t, ctx, client, btc, eth)
	exerciseProductionPublicStream(t, ctx, client, btc)
}

func exerciseProductionPublicREST(
	t *testing.T,
	ctx context.Context,
	client *PublicClient,
	btc, eth domain.Instrument,
) {
	t.Helper()
	if err := client.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	health, err := client.SampleServerTime(ctx)
	if err != nil || !health.Eligible {
		t.Fatalf("public clock health = %#v, %v", health, err)
	}
	records, err := client.Instruments(ctx, []domain.Instrument{btc, eth})
	if err != nil || len(records) != 2 {
		t.Fatalf("metadata count=%d err=%v", len(records), err)
	}
	for _, instrument := range []domain.Instrument{btc, eth} {
		snapshot, snapshotErr := client.Snapshot(ctx, exchangecontracts.SnapshotRequest{
			Instrument: instrument, Depth: 100})
		if snapshotErr != nil || snapshot.LastSequence == 0 || len(snapshot.Bids) == 0 || len(snapshot.Asks) == 0 {
			t.Fatalf("%s snapshot invalid: %#v %v", instrument.Symbol(), snapshot, snapshotErr)
		}
		trades, tradeErr := client.Trades(ctx, exchangecontracts.HistoryRequest{Instrument: instrument, Limit: 5})
		if tradeErr != nil || len(trades) == 0 {
			t.Fatalf("%s trades count=%d err=%v", instrument.Symbol(), len(trades), tradeErr)
		}
	}
	end := time.Now().UTC()
	candles, err := client.Candles(ctx, exchangecontracts.CandleRequest{HistoryRequest: exchangecontracts.HistoryRequest{
		Instrument: btc, Start: end.Add(-12 * time.Hour), End: end, Limit: 3}, Interval: "4h"})
	if err != nil || len(candles) == 0 {
		t.Fatalf("candle count=%d err=%v", len(candles), err)
	}
}

func exerciseProductionPublicStream(
	t *testing.T,
	ctx context.Context,
	client *PublicClient,
	btc domain.Instrument,
) {
	t.Helper()
	stream, err := client.SubscribeObserved(ctx, exchangecontracts.StreamRequest{Instrument: btc,
		Kinds: []exchangecontracts.StreamKind{exchangecontracts.StreamDepth, exchangecontracts.StreamTrades,
			exchangecontracts.StreamCandle}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	seen := make(map[exchangecontracts.StreamKind]bool)
	for len(seen) < 3 {
		observed, receiveErr := stream.ReceiveObserved(ctx)
		if receiveErr != nil {
			t.Fatal(receiveErr)
		}
		if len(observed.Raw) == 0 || observed.ReceivedOffsetNanos == 0 || observed.ConnectionGeneration == 0 {
			t.Fatal("stream omitted raw or ordering evidence")
		}
		seen[observed.Event.Kind] = true
	}
}
