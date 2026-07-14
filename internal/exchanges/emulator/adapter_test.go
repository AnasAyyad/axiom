package emulator

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

func TestAdapterConformsToPublicContracts(t *testing.T) {
	t.Parallel()
	instrument := emulatorInstrument(t)
	start := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	end := start.Add(4 * time.Hour)
	scenario := Scenario{
		Name: "public-contracts", Seed: "public-contracts-v1",
		REST: []RESTStep{
			getStep("/api/v3/exchangeInfo", "showPermissionSets=false&symbol=BTCUSDT", fixture(t, "exchange-info.json")),
			getStep("/api/v3/depth", "limit=100&symbol=BTCUSDT", fixture(t, "depth-snapshot.json")),
			getStep("/api/v3/trades", "limit=1&symbol=BTCUSDT", fixture(t, "trades.json")),
			getStep("/api/v3/klines", "endTime=1784001600000&interval=4h&limit=1&startTime=1783987200000&symbol=BTCUSDT&timeZone=0", fixture(t, "candles.json")),
		},
		StreamSessions: []StreamSession{{
			Path:   "/ws/btcusdt@depth",
			Frames: []StreamFrame{{Body: fixture(t, "depth-update.json")}, {Close: true}},
		}},
	}
	server, err := NewServer(scenario)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	clock, err := domain.NewReplayClock(start)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewAdapter(server, clock, start)
	if err != nil {
		t.Fatal(err)
	}
	exerciseRESTContracts(t, adapter, instrument, start, end)
	exerciseStreamContract(t, adapter, instrument)
	if !server.Complete() {
		t.Fatal("emulator scenario was not fully consumed")
	}
}

func exerciseRESTContracts(t *testing.T, adapter *Adapter, instrument domain.Instrument, start, end time.Time) {
	t.Helper()
	records, err := adapter.Instruments(context.Background(), []domain.Instrument{instrument})
	if err != nil || len(records) != 1 || records[0].NativeSymbol != "BTCUSDT" {
		t.Fatalf("metadata conformance: records=%+v err=%v", records, err)
	}
	snapshot, err := adapter.Snapshot(context.Background(), exchangecontracts.SnapshotRequest{Instrument: instrument, Depth: 100})
	if err != nil || snapshot.LastSequence != 1027024 {
		t.Fatalf("snapshot conformance: %+v err=%v", snapshot, err)
	}
	history := exchangecontracts.HistoryRequest{Instrument: instrument, Start: start, End: end, Limit: 1}
	trades, err := adapter.Trades(context.Background(), history)
	if err != nil || len(trades) != 1 || trades[0].NativeID != "28457" {
		t.Fatalf("trade conformance: %+v err=%v", trades, err)
	}
	candles, err := adapter.Candles(context.Background(), exchangecontracts.CandleRequest{HistoryRequest: history, Interval: "4h"})
	if err != nil || len(candles) != 1 || !candles[0].Closed {
		t.Fatalf("candle conformance: %+v err=%v", candles, err)
	}
}

func exerciseStreamContract(t *testing.T, adapter *Adapter, instrument domain.Instrument) {
	t.Helper()
	stream, err := adapter.Subscribe(context.Background(), exchangecontracts.StreamRequest{
		Instrument: instrument, Kinds: []exchangecontracts.StreamKind{exchangecontracts.StreamDepth},
	})
	if err != nil {
		t.Fatal(err)
	}
	event, err := stream.Receive(context.Background())
	if err != nil || event.Depth == nil || event.Depth.FirstSequence != 157 {
		t.Fatalf("stream conformance: %+v err=%v", event, err)
	}
	_ = stream.Close()
}

func TestCapabilityDescriptorIsDefensive(t *testing.T) {
	t.Parallel()
	server, err := NewServer(Scenario{Name: "copy", Seed: "copy-v1"})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	start := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	clock, _ := domain.NewReplayClock(start)
	adapter, err := NewAdapter(server, clock, start)
	if err != nil {
		t.Fatal(err)
	}
	first := adapter.Capabilities()
	first.Capabilities[0].Support = exchangecontracts.Unsupported
	second := adapter.Capabilities()
	if first.Capabilities[0].Support == second.Capabilities[0].Support {
		t.Fatal("capability mutation escaped defensive copy")
	}
	_, err = adapter.Snapshot(context.Background(), exchangecontracts.SnapshotRequest{
		Instrument: domain.Instrument{Product: domain.ProductSpot}, Depth: 101,
	})
	if exchangecontracts.KindOf(err) != exchangecontracts.ErrorValidation || !server.Complete() {
		t.Fatalf("invalid request reached the emulator: %v", err)
	}
}

func getStep(path, query string, body []byte) RESTStep {
	return RESTStep{Method: "GET", Path: path, RawQuery: query, Status: 200,
		Headers: []Header{{Name: "Content-Type", Value: "application/json"}}, Body: body}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "exchanges", "binance", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func emulatorInstrument(t *testing.T) domain.Instrument {
	t.Helper()
	instrument, err := domain.NewSpotInstrument("BTC", "USDT")
	if err != nil {
		t.Fatal(err)
	}
	return instrument
}
