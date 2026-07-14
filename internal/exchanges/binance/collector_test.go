package binance

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/marketdata"
)

func TestInstrumentCollectorBridgesSnapshotAndPublishesImmutableView(t *testing.T) {
	instrument := approvedBTC(t)
	clock := &domain.SystemClock{}
	recorder := &collectorRecorder{}
	source := newCollectorSource(t, instrument, clock, 101)
	collector, err := NewInstrumentCollector(testCollectorConfig(instrument), source, recorder, clock)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- collector.Run(ctx) }()
	waitFor(t, func() bool {
		view, viewErr := collector.Views().Book(collectorExchange, instrument)
		return viewErr == nil && view.Health() == marketdata.HealthHealthy && view.Sequence() == 101
	})
	view, _ := collector.Views().Book(collectorExchange, instrument)
	bids := view.Bids()
	bids[0].Quantity = mustQuantity(t, "999")
	current, _ := collector.Views().Book(collectorExchange, instrument)
	if current.Bids()[0].Quantity.String() == "999" || !current.Eligible(source.MonotonicOffset(), time.Second) {
		t.Fatal("collector exposed mutable or stale view")
	}
	cancel()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	stats := collector.Stats()
	if stats.DepthUpdates != 1 || stats.Rebuilds != 1 || recorder.raw.Load() == 0 || recorder.canonical.Load() == 0 {
		t.Fatalf("unexpected collector evidence: %#v raw=%d canonical=%d", stats, recorder.raw.Load(), recorder.canonical.Load())
	}
}

func TestInstrumentCollectorFailsClosedAndRecordsSequenceGap(t *testing.T) {
	instrument := approvedBTC(t)
	clock := &domain.SystemClock{}
	recorder := &collectorRecorder{}
	source := newCollectorSource(t, instrument, clock, 103)
	config := testCollectorConfig(instrument)
	config.ConnectionPolicy.MinimumBackoff = 50 * time.Millisecond
	config.ConnectionPolicy.MaximumBackoff = 100 * time.Millisecond
	collector, err := NewInstrumentCollector(config, source, recorder, clock)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- collector.Run(ctx) }()
	waitFor(t, func() bool { return recorder.gapCount.Load() > 0 })
	view, _ := collector.Views().Book(collectorExchange, instrument)
	if view.Health() != marketdata.HealthPaused || recorder.lastGap.FirstSequence != 101 ||
		recorder.lastGap.LastSequence != 103 {
		t.Fatalf("gap did not fail closed: health=%s gap=%#v", view.Health(), recorder.lastGap)
	}
	cancel()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

func TestInstrumentCollectorReconnectsAndResynchronizesAfterGap(t *testing.T) {
	instrument := approvedBTC(t)
	clock := &domain.SystemClock{}
	recorder := &collectorRecorder{}
	source := newCollectorSource(t, instrument, clock, 103, 101)
	collector, err := NewInstrumentCollector(testCollectorConfig(instrument), source, recorder, clock)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- collector.Run(ctx) }()
	waitFor(t, func() bool {
		view, viewErr := collector.Views().Book(collectorExchange, instrument)
		return viewErr == nil && view.Health() == marketdata.HealthHealthy && view.Generation() == 2 &&
			view.Sequence() == 101
	})
	stats := collector.Stats()
	if stats.Reconnects == 0 || stats.Rebuilds == 0 || stats.Gaps == 0 || stats.ResyncP95 <= 0 {
		t.Fatalf("resynchronization evidence = %#v", stats)
	}
	cancel()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

type collectorSourceFixture struct {
	testing       *testing.T
	instrument    domain.Instrument
	clock         domain.Clock
	lastSequences []uint64
	offset        atomic.Uint64
	generation    atomic.Uint64
}

func newCollectorSource(t *testing.T, instrument domain.Instrument, clock domain.Clock, sequences ...uint64) *collectorSourceFixture {
	return &collectorSourceFixture{testing: t, instrument: instrument, clock: clock, lastSequences: sequences}
}

func (source *collectorSourceFixture) MonotonicOffset() uint64 {
	return source.offset.Add(uint64(time.Millisecond))
}

func (source *collectorSourceFixture) SubscribeRecorded(
	_ context.Context,
	_ exchangecontracts.StreamRequest,
	recorder PublicRecorder,
) (ObservedStream, error) {
	generation := source.generation.Add(1)
	return &collectorStreamFixture{source: source, recorder: recorder, id: fmt.Sprintf("fixture-connection-%d", generation),
		generation: generation, closed: make(chan struct{})}, nil
}

func (source *collectorSourceFixture) SnapshotRecorded(
	ctx context.Context,
	_ exchangecontracts.SnapshotRequest,
	connectionID string,
	generation uint64,
	recorder PublicRecorder,
) (exchangecontracts.BookSnapshot, StreamRecordToken, error) {
	received := source.clock.Now()
	payload := []byte(`{"lastUpdateId":100}`)
	token, err := recorder.RecordPublicRaw(ctx, PublicRawRecord{Kind: RecordSnapshot, Raw: payload,
		Instrument: source.instrument, ReceivedAt: received, ConnectionID: connectionID,
		ConnectionGeneration: generation, MonotonicOffsetNanos: source.MonotonicOffset()})
	if err == nil {
		err = recorder.RecordPublicCanonical(ctx, PublicCanonicalRecord{Kind: RecordSnapshot, Token: token,
			Canonical: payload, SourceSequence: "100"})
	}
	return exchangecontracts.BookSnapshot{Exchange: collectorExchange, Instrument: source.instrument,
		LastSequence: 100, ReceivedAt: received, Bids: collectorLevels(source.testing, [][2]string{{"100", "2"}}),
		Asks: collectorLevels(source.testing, [][2]string{{"101", "2"}}), RawPayloadHash: repeatedHash("a")}, token, err
}

func (source *collectorSourceFixture) SampleServerTimeRecorded(
	ctx context.Context,
	instrument domain.Instrument,
	connectionID string,
	generation uint64,
	recorder PublicRecorder,
) (TimeHealth, StreamRecordToken, error) {
	received := source.clock.Now()
	payload := []byte(`{"serverTime":1700000000000}`)
	token, err := recorder.RecordPublicRaw(ctx, PublicRawRecord{Kind: RecordClockSample, Raw: payload,
		Instrument: instrument, ReceivedAt: received, ConnectionID: connectionID,
		ConnectionGeneration: generation, MonotonicOffsetNanos: source.MonotonicOffset()})
	if err == nil {
		err = recorder.RecordPublicCanonical(ctx, PublicCanonicalRecord{Kind: RecordClockSample,
			Token: token, Canonical: payload})
	}
	return TimeHealth{ObservedAt: received.UTC, Uncertainty: time.Millisecond, Eligible: true}, token, err
}

type collectorStreamFixture struct {
	source     *collectorSourceFixture
	recorder   PublicRecorder
	id         string
	generation uint64
	sent       atomic.Bool
	closed     chan struct{}
	closeOnce  sync.Once
}

func (stream *collectorStreamFixture) Receive(ctx context.Context) (exchangecontracts.StreamEvent, error) {
	observed, err := stream.ReceiveObserved(ctx)
	return observed.Event, err
}

func (stream *collectorStreamFixture) ReceiveObserved(ctx context.Context) (ObservedStreamEvent, error) {
	if stream.sent.CompareAndSwap(false, true) {
		received := stream.source.clock.Now()
		sequenceIndex := int(stream.generation - 1)
		if sequenceIndex >= len(stream.source.lastSequences) {
			sequenceIndex = len(stream.source.lastSequences) - 1
		}
		lastSequence := stream.source.lastSequences[sequenceIndex]
		update := exchangecontracts.DepthUpdate{Exchange: collectorExchange, Instrument: stream.source.instrument,
			ExchangeTime: received.UTC, FirstSequence: lastSequence,
			LastSequence: lastSequence, ReceivedAt: received,
			Bids: collectorLevels(stream.source.testing, [][2]string{{"100", "3"}}), RawPayloadHash: repeatedHash("b")}
		payload, _ := json.Marshal(update)
		token, err := stream.recorder.RecordPublicRaw(ctx, PublicRawRecord{Kind: RecordStreamFrame, Raw: payload,
			Instrument: stream.source.instrument, ReceivedAt: received, ConnectionID: stream.id,
			ConnectionGeneration: stream.generation, MonotonicOffsetNanos: stream.source.MonotonicOffset()})
		if err != nil {
			return ObservedStreamEvent{}, err
		}
		_ = stream.recorder.RecordPublicCanonical(ctx, PublicCanonicalRecord{Kind: RecordStreamFrame,
			Token: token, Canonical: payload})
		return ObservedStreamEvent{Raw: payload, ReceivedAt: received, ConnectionID: stream.id,
			ConnectionGeneration: stream.generation, Event: exchangecontracts.StreamEvent{Kind: exchangecontracts.StreamDepth,
				Depth: &update}, RecordToken: token, DecodeNanos: 1, ReceivedOffsetNanos: stream.source.MonotonicOffset()}, nil
	}
	select {
	case <-ctx.Done():
		return ObservedStreamEvent{}, ctx.Err()
	case <-stream.closed:
		return ObservedStreamEvent{}, context.Canceled
	}
}

func (stream *collectorStreamFixture) ConnectionID() string { return stream.id }
func (stream *collectorStreamFixture) Generation() uint64   { return stream.generation }
func (stream *collectorStreamFixture) Close() error {
	stream.closeOnce.Do(func() {
		close(stream.closed)
	})
	return nil
}

type collectorRecorder struct {
	ordinal   atomic.Uint64
	raw       atomic.Uint64
	canonical atomic.Uint64
	gapCount  atomic.Uint64
	mutex     sync.Mutex
	lastGap   SourceGap
}

func (recorder *collectorRecorder) RecordPublicRaw(_ context.Context, record PublicRawRecord) (StreamRecordToken, error) {
	recorder.raw.Add(1)
	return StreamRecordToken{IngestOrdinal: recorder.ordinal.Add(1)}, nil
}
func (recorder *collectorRecorder) RecordPublicCanonical(_ context.Context, _ PublicCanonicalRecord) error {
	recorder.canonical.Add(1)
	return nil
}
func (recorder *collectorRecorder) RecordSourceGap(_ context.Context, gap SourceGap) error {
	recorder.mutex.Lock()
	recorder.lastGap = gap
	recorder.mutex.Unlock()
	recorder.gapCount.Add(1)
	return nil
}

func testCollectorConfig(instrument domain.Instrument) CollectorConfig {
	config := DefaultCollectorConfig(instrument)
	config.SnapshotDepth, config.BookDepth, config.QueueCapacity = 100, 2, 4
	config.ClockSyncEvery, config.StaleCheckEvery = time.Hour, time.Hour
	config.ConnectionPolicy = ConnectionPolicy{MinimumBackoff: time.Millisecond, MaximumBackoff: 2 * time.Millisecond,
		Renewal: time.Hour, Seed: "fixture"}
	return config
}

func collectorLevels(t *testing.T, values [][2]string) []exchangecontracts.PriceLevel {
	t.Helper()
	result := make([]exchangecontracts.PriceLevel, 0, len(values))
	for _, value := range values {
		price, priceErr := domain.ParsePrice(value[0])
		quantity, quantityErr := domain.ParseQuantity(value[1])
		if priceErr != nil || quantityErr != nil {
			t.Fatal(priceErr, quantityErr)
		}
		result = append(result, exchangecontracts.PriceLevel{Price: price, Quantity: quantity})
	}
	return result
}

func mustQuantity(t *testing.T, value string) domain.Quantity {
	t.Helper()
	quantity, err := domain.ParseQuantity(value)
	if err != nil {
		t.Fatal(err)
	}
	return quantity
}

func repeatedHash(value string) string {
	result := value
	for len(result) < 64 {
		result += value
	}
	return result[:64]
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()
	for {
		if condition() {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("condition not reached")
		case <-ticker.C:
		}
	}
}
