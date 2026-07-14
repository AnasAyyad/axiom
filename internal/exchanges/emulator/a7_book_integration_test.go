package emulator

import (
	"context"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/marketdata"
)

func TestA7EmulatorSnapshotBridgeAndGapInvalidation(t *testing.T) {
	instrument := emulatorInstrument(t)
	snapshotBody := []byte(`{"lastUpdateId":100,"bids":[["100","2"]],"asks":[["101","2"]]}`)
	bridge := []byte(`{"e":"depthUpdate","E":1700000000000,"s":"BTCUSDT","U":101,"u":101,"b":[["100","3"]],"a":[]}`)
	gap := []byte(`{"e":"depthUpdate","E":1700000000001,"s":"BTCUSDT","U":103,"u":103,"b":[],"a":[]}`)
	scenario := Scenario{Name: "a7-book", Seed: "a7-book-v1",
		REST: []RESTStep{getStep("/api/v3/depth", "limit=100&symbol=BTCUSDT", snapshotBody)},
		StreamSessions: []StreamSession{{Path: "/ws/btcusdt@depth",
			Frames: []StreamFrame{{Body: bridge}, {Body: gap}, {Close: true}}}},
	}
	server, err := NewServer(scenario)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	clock, _ := domain.NewReplayClock(time.UnixMilli(1_700_000_000_000).UTC())
	adapter, err := NewAdapter(server, clock, clock.Now().UTC)
	if err != nil {
		t.Fatal(err)
	}
	exerciseA7BookScenario(t, adapter, server, clock, instrument)
}

func exerciseA7BookScenario(
	t *testing.T,
	adapter *Adapter,
	server *Server,
	clock domain.Clock,
	instrument domain.Instrument,
) {
	t.Helper()
	stream, err := adapter.Subscribe(context.Background(), exchangecontracts.StreamRequest{
		Instrument: instrument, Kinds: []exchangecontracts.StreamKind{exchangecontracts.StreamDepth}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	book, err := marketdata.NewBook("binance", instrument, 10, 20, nil)
	if err != nil || book.BeginGeneration("emulator-connection", 1) != nil {
		t.Fatal(err)
	}
	first, err := stream.Receive(context.Background())
	if err != nil || first.Depth == nil {
		t.Fatal(err)
	}
	if err = book.Buffer(marketdata.DepthEvent{Update: *first.Depth,
		Observation: emulatorObservation(clock, 101, 1, 10)}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := adapter.Snapshot(context.Background(), exchangecontracts.SnapshotRequest{
		Instrument: instrument, Depth: 100})
	if err != nil {
		t.Fatal(err)
	}
	if err = book.InstallSnapshot(snapshot, emulatorObservation(clock, 100, 2, 20)); err != nil {
		t.Fatal(err)
	}
	if view := book.View(); view.Health() != marketdata.HealthHealthy || view.Sequence() != 101 ||
		view.Bids()[0].Quantity.String() != "3" {
		t.Fatalf("bridge failed: sequence=%d health=%s", view.Sequence(), view.Health())
	}
	assertA7GapInvalidates(t, stream, book, server, clock)
}

func assertA7GapInvalidates(
	t *testing.T,
	stream exchangecontracts.Stream,
	book *marketdata.Book,
	server *Server,
	clock domain.Clock,
) {
	t.Helper()
	second, err := stream.Receive(context.Background())
	if err != nil || second.Depth == nil {
		t.Fatal(err)
	}
	if err = book.Apply(marketdata.DepthEvent{Update: *second.Depth,
		Observation: emulatorObservation(clock, 103, 3, 30)}); err == nil ||
		book.View().Health() != marketdata.HealthPaused {
		t.Fatalf("gap remained eligible: err=%v health=%s", err, book.View().Health())
	}
	if !server.Complete() {
		t.Fatal("A7 emulator scenario was not fully consumed")
	}
}

func emulatorObservation(clock domain.Clock, sequence, ordinal, offset uint64) marketdata.Observation {
	received, processed, published := clock.Now(), clock.Now(), clock.Now()
	return marketdata.Observation{ExchangeTime: received.UTC, ReceivedAt: received, ProcessedAt: processed,
		PublishedAt: published, ConnectionID: "emulator-connection", ConnectionGeneration: 1,
		SourceSequence: sequence, IngestOrdinal: ordinal, ReceivedOffsetNanos: offset,
		ProcessedOffsetNanos: offset + 1, PublishedOffsetNanos: offset + 2}
}
