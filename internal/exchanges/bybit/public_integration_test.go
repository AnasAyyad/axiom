package bybit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	marketrecorder "axiom/internal/recorder"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/storage/segments"
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

func TestProductionPublicBybitWebSocketRecording(t *testing.T) {
	if os.Getenv("AXIOM_B1_LIVE_PUBLIC") != "1" {
		t.Skip("AXIOM_B1_LIVE_PUBLIC=1 is required")
	}
	client, err := NewPublicClient(publicEndpointSet, &domain.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	sink := &liveBybitSink{raw: make(map[uint64]exchangecontracts.PublicRawRecord)}
	stream, err := client.SubscribeRecorded(ctx, exchangecontracts.StreamRequest{
		Instrument: approvedInstruments()[0],
		Kinds: []exchangecontracts.StreamKind{
			exchangecontracts.StreamDepth, exchangecontracts.StreamTrades,
			exchangecontracts.StreamTicker, exchangecontracts.StreamCandle,
		},
		CandleIntervals: []string{"15m", "1h", "4h"},
	}, sink)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	wanted := map[exchangecontracts.StreamKind]bool{
		exchangecontracts.StreamDepth: false, exchangecontracts.StreamTrades: false,
		exchangecontracts.StreamTicker: false, exchangecontracts.StreamCandle: false,
	}
	subscribed := false
	for !subscribed || !allLiveKindsSeen(wanted) {
		observed, receiveErr := stream.ReceiveObserved(ctx)
		if receiveErr != nil {
			t.Fatalf("public WebSocket receive failed after %s: %v", sink.lastSummary(), receiveErr)
		}
		if observed.ConnectionID == "" || observed.ConnectionGeneration == 0 || len(observed.Raw) == 0 {
			t.Fatalf("missing observed connection evidence: %#v", observed)
		}
		if observed.Event.Kind == exchangecontracts.StreamLifecycle {
			subscribed = subscribed || (observed.Event.Lifecycle != nil &&
				observed.Event.Lifecycle.Reason == "subscription_acknowledged")
		} else if _, exists := wanted[observed.Event.Kind]; exists {
			wanted[observed.Event.Kind] = true
		}
	}
	if err = stream.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if raw, canonical := sink.counts(); raw == 0 || raw != canonical {
		t.Fatalf("live raw/canonical linkage=%d/%d", raw, canonical)
	}
}

func TestProductionPublicBybitRecorderManifest(t *testing.T) {
	if os.Getenv("AXIOM_B1_LIVE_PUBLIC") != "1" {
		t.Skip("AXIOM_B1_LIVE_PUBLIC=1 is required")
	}
	harness := newBybitLiveRecorderHarness(t)
	manifest := recordBybitDepthManifest(t, harness)
	assertBybitRecorderManifest(t, harness, manifest)
	t.Logf("B1_MANIFEST root=%s hash=%s raw=%d canonical=%d", harness.root, manifest.Hash,
		manifest.RawRecordCount, manifest.CanonicalCount)
}

type bybitLiveRecorderHarness struct {
	root      string
	recorder  *marketrecorder.Recorder
	sink      *marketrecorder.PublicStreamSink
	committed []segments.Manifest
}

func newBybitLiveRecorderHarness(t *testing.T) *bybitLiveRecorderHarness {
	t.Helper()
	evidenceRoot := os.Getenv("AXIOM_B1_LIVE_EVIDENCE_ROOT")
	if evidenceRoot == "" {
		evidenceRoot = t.TempDir()
	}
	session := fmt.Sprintf("b1-live-%d", time.Now().UTC().UnixNano())
	harness := &bybitLiveRecorderHarness{root: filepath.Join(evidenceRoot, session),
		committed: make([]segments.Manifest, 0, 2)}
	if err := os.MkdirAll(harness.root, 0o700); err != nil {
		t.Fatal(err)
	}
	recorder, err := marketrecorder.New(harness.root, "bybit-short-public", session, "bybit",
		&runtimecore.IngestOrdinals{}, func(manifest segments.Manifest) error {
			harness.committed = append(harness.committed, manifest)
			return nil
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	sink, err := marketrecorder.NewPublicStreamSink(recorder,
		"bybit-public-parser.v1", "bybit-public-normalizer.v1")
	if err != nil {
		t.Fatal(err)
	}
	harness.recorder = recorder
	harness.sink = sink
	return harness
}

func recordBybitDepthManifest(t *testing.T, harness *bybitLiveRecorderHarness) marketrecorder.DatasetManifest {
	t.Helper()
	client, err := NewPublicClient(publicEndpointSet, &domain.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stream, err := client.SubscribeRecorded(ctx, exchangecontracts.StreamRequest{
		Instrument: approvedInstruments()[0],
		Kinds:      []exchangecontracts.StreamKind{exchangecontracts.StreamDepth},
	}, harness.sink)
	if err != nil {
		t.Fatal(err)
	}
	for {
		observed, receiveErr := stream.ReceiveObserved(ctx)
		if receiveErr != nil {
			t.Fatal(receiveErr)
		}
		if observed.Event.Snapshot != nil {
			break
		}
	}
	if err = stream.Close(); err != nil {
		t.Fatal(err)
	}
	manifest, err := harness.recorder.Flush()
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func assertBybitRecorderManifest(
	t *testing.T,
	harness *bybitLiveRecorderHarness,
	manifest marketrecorder.DatasetManifest,
) {
	t.Helper()
	if manifest.RawRecordCount == 0 || manifest.RawRecordCount != manifest.CanonicalCount ||
		len(harness.committed) != 2 {
		t.Fatalf("live recorder manifest=%#v committed=%d", manifest, len(harness.committed))
	}
	records, err := marketrecorder.ValidateDataset(harness.root, manifest)
	if err != nil || uint64(len(records)) != manifest.CanonicalCount {
		t.Fatalf("live recorder validation records=%d error=%v", len(records), err)
	}
}

func allLiveKindsSeen(seen map[exchangecontracts.StreamKind]bool) bool {
	for _, value := range seen {
		if !value {
			return false
		}
	}
	return true
}

type liveBybitSink struct {
	mutex     sync.Mutex
	ordinal   uint64
	raw       map[uint64]exchangecontracts.PublicRawRecord
	canonical uint64
}

func (sink *liveBybitSink) RecordPublicRaw(
	_ context.Context,
	record exchangecontracts.PublicRawRecord,
) (exchangecontracts.StreamRecordToken, error) {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	sink.ordinal++
	sink.raw[sink.ordinal] = record
	return exchangecontracts.StreamRecordToken{IngestOrdinal: sink.ordinal}, nil
}

func (sink *liveBybitSink) RecordPublicCanonical(
	_ context.Context,
	record exchangecontracts.PublicCanonicalRecord,
) error {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	if _, exists := sink.raw[record.Token.IngestOrdinal]; !exists {
		return streamError()
	}
	sink.canonical++
	return nil
}

func (sink *liveBybitSink) RecordSourceGap(context.Context, exchangecontracts.SourceGap) error {
	return nil
}

func (sink *liveBybitSink) counts() (uint64, uint64) {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	return uint64(len(sink.raw)), sink.canonical
}

func (sink *liveBybitSink) lastSummary() string {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	record := sink.raw[sink.ordinal]
	var envelope map[string]json.RawMessage
	if json.Unmarshal(record.Raw, &envelope) != nil {
		return "non-JSON frame"
	}
	keys := make([]string, 0, len(envelope))
	for key := range envelope {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := make([]string, 0, 3)
	for _, key := range []string{"op", "topic", "type"} {
		if value, exists := envelope[key]; exists {
			values = append(values, key+"="+string(value))
		}
	}
	return "keys=" + strings.Join(keys, ",") + " " + strings.Join(values, " ")
}
