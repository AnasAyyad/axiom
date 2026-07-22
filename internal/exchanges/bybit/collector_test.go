package bybit

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/marketdata"
)

func TestB1InstrumentCollectorAppliesSnapshotsMonotonicDeltasAndPublicEvents(t *testing.T) {
	clock := &domain.SystemClock{}
	instrument := approvedInstruments()[0]
	source := &bybitCollectorSource{clock: clock, generations: [][]exchangecontracts.StreamEvent{{
		bybitSnapshotEvent(t, clock, instrument, 10, "100", "101"),
		bybitDepthEvent(t, clock, instrument, 15),
		bybitSnapshotEvent(t, clock, instrument, 1, "98", "102"),
		bybitLifecycleEvent(clock), bybitTradeEvent(t, clock, instrument),
		bybitTickerEvent(t, clock, instrument), bybitCandleEvent(t, clock, instrument),
	}}}
	recorder := &bybitCollectorRecorder{}
	config := DefaultCollectorConfig(instrument)
	config.MinimumBackoff, config.MaximumBackoff = time.Millisecond, 2*time.Millisecond
	config.HeartbeatEvery, config.StaleCheckEvery = time.Hour, time.Hour
	collector, err := NewInstrumentCollector(config, source, recorder, clock)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- collector.Run(ctx) }()
	waitForBybitCollector(t, func() bool {
		stats := collector.Stats()
		view, viewErr := collector.Views().Book(collectorExchange, instrument)
		return viewErr == nil && view.Sequence() == 1 && view.Health() == marketdata.HealthHealthy &&
			stats.Snapshots == 2 && stats.Resets == 1 && stats.DepthUpdates == 1 && stats.Trades == 1 &&
			stats.Tickers == 1 && stats.Candles == 1 && stats.Heartbeats == 1
	})
	cancel()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if recorder.raw.Load() == 0 || recorder.canonical.Load() == 0 || recorder.canonical.Load() > recorder.raw.Load() {
		t.Fatalf("collector linkage raw=%d canonical=%d", recorder.raw.Load(), recorder.canonical.Load())
	}
}

func TestB1InstrumentCollectorRecordsConservativeGapAndReconnects(t *testing.T) {
	clock := &domain.SystemClock{}
	instrument := approvedInstruments()[0]
	source := &bybitCollectorSource{clock: clock, generations: [][]exchangecontracts.StreamEvent{
		{bybitSnapshotEvent(t, clock, instrument, 20, "100", "101")},
		{bybitSnapshotEvent(t, clock, instrument, 30, "99", "102")},
	}, failGeneration: 1}
	recorder := &bybitCollectorRecorder{}
	config := DefaultCollectorConfig(instrument)
	config.MinimumBackoff, config.MaximumBackoff = time.Millisecond, 2*time.Millisecond
	config.HeartbeatEvery, config.StaleCheckEvery = time.Hour, time.Hour
	collector, err := NewInstrumentCollector(config, source, recorder, clock)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- collector.Run(ctx) }()
	waitForBybitCollector(t, func() bool {
		view, viewErr := collector.Views().Book(collectorExchange, instrument)
		stats := collector.Stats()
		return viewErr == nil && view.Generation() == 2 && view.Sequence() == 30 &&
			stats.Reconnects >= 1 && recorder.gaps.Load() == 1
	})
	cancel()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	recorder.mutex.Lock()
	gap := recorder.lastGap
	recorder.mutex.Unlock()
	if gap.FirstSequence != 21 || gap.LastSequence != ^uint64(0) || gap.Reason != "stream_interruption" {
		t.Fatalf("conservative gap=%#v", gap)
	}
}

func TestB1InstrumentCollectorReceiverQueueIsBounded(t *testing.T) {
	clock := &domain.SystemClock{}
	instrument := approvedInstruments()[0]
	events := make([]exchangecontracts.StreamEvent, 1001)
	for index := range events {
		events[index] = bybitLifecycleEvent(clock)
	}
	source := &bybitCollectorSource{clock: clock, generations: [][]exchangecontracts.StreamEvent{events}}
	recorder := &bybitCollectorRecorder{}
	config := DefaultCollectorConfig(instrument)
	config.QueueCapacity = 1000
	collector, err := NewInstrumentCollector(config, source, recorder, clock)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := source.SubscribeRecorded(context.Background(), exchangecontracts.StreamRequest{}, recorder)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, overflow := collector.startReceiver(ctx, stream)
	select {
	case <-overflow:
	case <-time.After(5 * time.Second):
		t.Fatal("bounded receiver did not signal overflow")
	}
}

type bybitCollectorSource struct {
	clock          domain.Clock
	generations    [][]exchangecontracts.StreamEvent
	failGeneration uint64
	generation     atomic.Uint64
	offset         atomic.Uint64
}

func (source *bybitCollectorSource) MonotonicOffset() uint64 {
	return source.offset.Add(uint64(time.Millisecond))
}

func (source *bybitCollectorSource) SubscribeRecorded(
	_ context.Context,
	_ exchangecontracts.StreamRequest,
	recorder exchangecontracts.PublicRecorder,
) (ObservedStream, error) {
	generation := source.generation.Add(1)
	index := int(generation - 1)
	if index >= len(source.generations) {
		index = len(source.generations) - 1
	}
	return &bybitCollectorStream{source: source, recorder: recorder,
		id: fmt.Sprintf("bybit-fixture-%d", generation), generation: generation,
		events:          append([]exchangecontracts.StreamEvent(nil), source.generations[index]...),
		failAfterEvents: source.failGeneration == generation, closed: make(chan struct{})}, nil
}

func (source *bybitCollectorSource) SampleServerTimeRecorded(
	ctx context.Context,
	instrument domain.Instrument,
	connectionID string,
	generation uint64,
	recorder exchangecontracts.PublicRecorder,
) (ClockHealth, exchangecontracts.StreamRecordToken, error) {
	received := source.clock.Now()
	payload := []byte(`{"time":"fixture"}`)
	token, err := recorder.RecordPublicRaw(ctx, exchangecontracts.PublicRawRecord{
		Kind: exchangecontracts.RecordClockSample, Raw: payload, Instrument: instrument,
		ReceivedAt: received, ConnectionID: connectionID, ConnectionGeneration: generation,
		MonotonicOffsetNanos: source.MonotonicOffset(),
	})
	if err == nil {
		err = recorder.RecordPublicCanonical(ctx, exchangecontracts.PublicCanonicalRecord{
			Kind: exchangecontracts.RecordClockSample, Token: token, Canonical: payload,
		})
	}
	return ClockHealth{ObservedAt: received.UTC, Uncertainty: time.Millisecond, Eligible: true}, token, err
}

type bybitCollectorStream struct {
	source          *bybitCollectorSource
	recorder        exchangecontracts.PublicRecorder
	id              string
	generation      uint64
	events          []exchangecontracts.StreamEvent
	failAfterEvents bool
	closed          chan struct{}
	closeOnce       sync.Once
}

func (stream *bybitCollectorStream) Receive(ctx context.Context) (exchangecontracts.StreamEvent, error) {
	observed, err := stream.ReceiveObserved(ctx)
	return observed.Event, err
}

func (stream *bybitCollectorStream) ReceiveObserved(ctx context.Context) (exchangecontracts.ObservedStreamEvent, error) {
	if len(stream.events) != 0 {
		event := stream.events[0]
		stream.events = stream.events[1:]
		received := stream.source.clock.Now()
		payload, _ := json.Marshal(event)
		token, err := stream.recorder.RecordPublicRaw(ctx, exchangecontracts.PublicRawRecord{
			Kind: exchangecontracts.RecordStreamFrame, Raw: payload, Instrument: approvedInstruments()[0],
			ReceivedAt: received, ConnectionID: stream.id, ConnectionGeneration: stream.generation,
			MonotonicOffsetNanos: stream.source.MonotonicOffset(),
		})
		if err != nil {
			return exchangecontracts.ObservedStreamEvent{}, err
		}
		if err = stream.recorder.RecordPublicCanonical(ctx, exchangecontracts.PublicCanonicalRecord{
			Kind: exchangecontracts.RecordStreamFrame, Token: token, Canonical: payload,
		}); err != nil {
			return exchangecontracts.ObservedStreamEvent{}, err
		}
		return exchangecontracts.ObservedStreamEvent{Raw: payload, ReceivedAt: received,
			ConnectionID: stream.id, ConnectionGeneration: stream.generation, Event: event,
			RecordToken: token, ReceivedOffsetNanos: stream.source.MonotonicOffset(), DecodeNanos: 1}, nil
	}
	if stream.failAfterEvents {
		stream.failAfterEvents = false
		return exchangecontracts.ObservedStreamEvent{}, exchangecontracts.NewDetailedError(
			exchangecontracts.ErrorTransient, exchangecontracts.OperationStream, 0, 0, "fixture_disconnect")
	}
	select {
	case <-ctx.Done():
		return exchangecontracts.ObservedStreamEvent{}, ctx.Err()
	case <-stream.closed:
		return exchangecontracts.ObservedStreamEvent{}, context.Canceled
	}
}

func (stream *bybitCollectorStream) Ping(context.Context) error { return nil }
func (stream *bybitCollectorStream) ConnectionID() string       { return stream.id }
func (stream *bybitCollectorStream) Generation() uint64         { return stream.generation }
func (stream *bybitCollectorStream) Close() error {
	stream.closeOnce.Do(func() { close(stream.closed) })
	return nil
}

type bybitCollectorRecorder struct {
	ordinal   atomic.Uint64
	raw       atomic.Uint64
	canonical atomic.Uint64
	gaps      atomic.Uint64
	mutex     sync.Mutex
	lastGap   exchangecontracts.SourceGap
}

func (recorder *bybitCollectorRecorder) RecordPublicRaw(_ context.Context, _ exchangecontracts.PublicRawRecord) (exchangecontracts.StreamRecordToken, error) {
	recorder.raw.Add(1)
	return exchangecontracts.StreamRecordToken{IngestOrdinal: recorder.ordinal.Add(1)}, nil
}

func (recorder *bybitCollectorRecorder) RecordPublicCanonical(_ context.Context, _ exchangecontracts.PublicCanonicalRecord) error {
	recorder.canonical.Add(1)
	return nil
}

func (recorder *bybitCollectorRecorder) RecordSourceGap(_ context.Context, gap exchangecontracts.SourceGap) error {
	recorder.mutex.Lock()
	recorder.lastGap = gap
	recorder.mutex.Unlock()
	recorder.gaps.Add(1)
	return nil
}

func bybitSnapshotEvent(t *testing.T, clock domain.Clock, instrument domain.Instrument, sequence uint64, bid, ask string) exchangecontracts.StreamEvent {
	t.Helper()
	return exchangecontracts.StreamEvent{Kind: exchangecontracts.StreamDepth, Snapshot: &exchangecontracts.BookSnapshot{
		Exchange: collectorExchange, Instrument: instrument, LastSequence: sequence, ReceivedAt: clock.Now(),
		Bids: bybitLevels(t, [][2]string{{bid, "2"}}), Asks: bybitLevels(t, [][2]string{{ask, "2"}}),
		RawPayloadHash: bybitHash("a"),
	}}
}

func bybitDepthEvent(t *testing.T, clock domain.Clock, instrument domain.Instrument, sequence uint64) exchangecontracts.StreamEvent {
	t.Helper()
	return exchangecontracts.StreamEvent{Kind: exchangecontracts.StreamDepth, Depth: &exchangecontracts.DepthUpdate{
		Exchange: collectorExchange, Instrument: instrument, FirstSequence: sequence, LastSequence: sequence,
		ExchangeTime: clock.Now().UTC, ReceivedAt: clock.Now(), Bids: bybitLevels(t, [][2]string{{"100", "3"}}),
		RawPayloadHash: bybitHash("b"),
	}}
}

func bybitLifecycleEvent(clock domain.Clock) exchangecontracts.StreamEvent {
	lifecycle := exchangecontracts.LifecycleEvent{Exchange: collectorExchange, State: "HEALTHY",
		Reason: "heartbeat_pong", ObservedAt: clock.Now()}
	return exchangecontracts.StreamEvent{Kind: exchangecontracts.StreamLifecycle, Lifecycle: &lifecycle}
}

func bybitTradeEvent(t *testing.T, clock domain.Clock, instrument domain.Instrument) exchangecontracts.StreamEvent {
	t.Helper()
	trade := exchangecontracts.Trade{Exchange: collectorExchange, Instrument: instrument, NativeID: "trade-1",
		Price: bybitPrice(t, "100"), Quantity: bybitQuantity(t, "1"), ExchangeTime: clock.Now().UTC,
		ReceivedAt: clock.Now(), RawPayloadHash: bybitHash("c")}
	return exchangecontracts.StreamEvent{Kind: exchangecontracts.StreamTrades, Trade: &trade}
}

func bybitTickerEvent(t *testing.T, clock domain.Clock, instrument domain.Instrument) exchangecontracts.StreamEvent {
	t.Helper()
	ticker := exchangecontracts.Ticker{Exchange: collectorExchange, Instrument: instrument,
		BidPrice: bybitPrice(t, "100"), BidQuantity: bybitQuantity(t, "2"),
		AskPrice: bybitPrice(t, "101"), AskQuantity: bybitQuantity(t, "3"), LastPrice: bybitPrice(t, "100.5"),
		ExchangeTime: clock.Now().UTC, ReceivedAt: clock.Now(), RawPayloadHash: bybitHash("d")}
	return exchangecontracts.StreamEvent{Kind: exchangecontracts.StreamTicker, Ticker: &ticker}
}

func bybitCandleEvent(t *testing.T, clock domain.Clock, instrument domain.Instrument) exchangecontracts.StreamEvent {
	t.Helper()
	open := time.Now().UTC().Truncate(15 * time.Minute).Add(-15 * time.Minute)
	candle := exchangecontracts.Candle{Exchange: collectorExchange, Instrument: instrument, Interval: "15m",
		OpenTime: open, CloseTime: open.Add(15*time.Minute - time.Millisecond), Open: bybitPrice(t, "100"),
		High: bybitPrice(t, "102"), Low: bybitPrice(t, "99"), Close: bybitPrice(t, "101"),
		Volume: bybitQuantity(t, "4"), Closed: true, ReceivedAt: clock.Now(), RawPayloadHash: bybitHash("e")}
	return exchangecontracts.StreamEvent{Kind: exchangecontracts.StreamCandle, Candle: &candle}
}

func bybitLevels(t *testing.T, values [][2]string) []exchangecontracts.PriceLevel {
	t.Helper()
	levels := make([]exchangecontracts.PriceLevel, 0, len(values))
	for _, value := range values {
		levels = append(levels, exchangecontracts.PriceLevel{Price: bybitPrice(t, value[0]), Quantity: bybitQuantity(t, value[1])})
	}
	return levels
}

func bybitPrice(t *testing.T, value string) domain.Price {
	t.Helper()
	parsed, err := domain.ParsePrice(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func bybitQuantity(t *testing.T, value string) domain.Quantity {
	t.Helper()
	parsed, err := domain.ParseQuantity(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func bybitHash(value string) string { return strings.Repeat(value, 64) }

func waitForBybitCollector(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()
	for {
		if condition() {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("collector condition not reached")
		case <-ticker.C:
		}
	}
}
