package bybit

import (
	"context"
	"encoding/json"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/marketdata"
)

type observedResult struct {
	event exchangecontracts.ObservedStreamEvent
	err   error
}

func (collector *InstrumentCollector) consumeGeneration(ctx context.Context, stream ObservedStream) error {
	events, overflow := collector.startReceiver(ctx, stream)
	heartbeat := time.NewTicker(collector.config.HeartbeatEvery)
	stale := time.NewTicker(collector.config.StaleCheckEvery)
	defer heartbeat.Stop()
	defer stale.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-overflow:
			return collector.handleQueueOverflow(ctx, stream)
		case <-heartbeat.C:
			if err := stream.Ping(ctx); err != nil {
				return err
			}
		case <-stale.C:
			if collector.book.View().Version() > 0 && collector.book.MarkStale(collector.source.MonotonicOffset(),
				uint64(collector.config.MaximumBookAge.Nanoseconds())) != nil {
				return streamError()
			}
		case result := <-events:
			if err := collector.handleObservedResult(ctx, stream, result); err != nil {
				return err
			}
		}
	}
}

func (collector *InstrumentCollector) handleQueueOverflow(ctx context.Context, stream ObservedStream) error {
	collector.stats.queueOverflows.Add(1)
	sequence := collector.book.View().Sequence()
	_ = collector.book.Invalidate("queue_overflow", sequence)
	if sequence > 0 {
		if err := collector.recordGap(ctx, stream.ConnectionID(), stream.Generation(),
			sequence+1, ^uint64(0), "queue_overflow"); err != nil {
			return err
		}
	}
	return streamError()
}

func (collector *InstrumentCollector) handleObservedResult(
	ctx context.Context,
	stream ObservedStream,
	result observedResult,
) error {
	if result.err == nil {
		return collector.processObserved(ctx, result.event)
	}
	if ctx.Err() != nil {
		return nil
	}
	if exchangecontracts.KindOf(result.err) == exchangecontracts.ErrorValidation {
		collector.stats.decoderErrors.Add(1)
	}
	sequence := collector.book.View().Sequence()
	if sequence > 0 {
		if err := collector.recordGap(ctx, stream.ConnectionID(), stream.Generation(),
			sequence+1, ^uint64(0), "stream_interruption"); err != nil {
			return err
		}
	}
	return result.err
}

func (collector *InstrumentCollector) startReceiver(
	ctx context.Context,
	stream ObservedStream,
) (<-chan observedResult, <-chan struct{}) {
	events := make(chan observedResult, collector.config.QueueCapacity)
	overflow := make(chan struct{}, 1)
	go func() {
		for ctx.Err() == nil {
			observed, err := stream.ReceiveObserved(ctx)
			select {
			case events <- observedResult{event: observed, err: err}:
			case <-ctx.Done():
				return
			default:
				overflow <- struct{}{}
				_ = stream.Close()
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return events, overflow
}

func (collector *InstrumentCollector) processObserved(
	ctx context.Context,
	observed exchangecontracts.ObservedStreamEvent,
) error {
	switch observed.Event.Kind {
	case exchangecontracts.StreamLifecycle:
		if observed.Event.Lifecycle == nil {
			return streamError()
		}
		if observed.Event.Lifecycle.Reason == "heartbeat_pong" {
			collector.stats.heartbeats.Add(1)
		}
		return nil
	case exchangecontracts.StreamDepth:
		return collector.processDepth(ctx, observed)
	case exchangecontracts.StreamTrades:
		count := len(observed.Event.Trades)
		if observed.Event.Trade != nil {
			count++
		}
		if count == 0 || (observed.Event.Trade != nil && len(observed.Event.Trades) != 0) {
			return streamError()
		}
		collector.stats.trades.Add(uint64(count))
	case exchangecontracts.StreamTicker:
		if observed.Event.Ticker == nil {
			return streamError()
		}
		collector.stats.tickers.Add(1)
	case exchangecontracts.StreamCandle:
		return collector.processCandle(observed)
	default:
		return streamError()
	}
	return nil
}

func (collector *InstrumentCollector) processDepth(
	ctx context.Context,
	observed exchangecontracts.ObservedStreamEvent,
) error {
	if observed.Event.Snapshot != nil {
		wasActive := collector.book.View().Version() > 0
		snapshot := *observed.Event.Snapshot
		if err := collector.book.ReplaceSnapshot(snapshot,
			collector.observation(observed, time.Time{}, snapshot.LastSequence)); err != nil {
			return err
		}
		collector.stats.snapshots.Add(1)
		if wasActive || snapshot.LastSequence == 1 {
			collector.stats.resets.Add(1)
		}
		return collector.recordRebuild(ctx, observed, snapshot.LastSequence)
	}
	if observed.Event.Depth == nil {
		return streamError()
	}
	update := *observed.Event.Depth
	if err := collector.book.ApplyMonotonic(marketdata.DepthEvent{Update: update,
		Observation: collector.observation(observed, update.ExchangeTime, update.LastSequence)}); err != nil {
		return err
	}
	collector.stats.depthUpdates.Add(1)
	return nil
}

func (collector *InstrumentCollector) processCandle(observed exchangecontracts.ObservedStreamEvent) error {
	candle := observed.Event.Candle
	if candle == nil {
		return streamError()
	}
	if !candle.Closed {
		return nil
	}
	store, exists := collector.candles[candle.Interval]
	if !exists {
		return streamError()
	}
	sequence := uint64(candle.CloseTime.UnixMilli())
	if err := store.Add(*candle, collector.observation(observed, candle.CloseTime, sequence)); err != nil {
		return err
	}
	collector.stats.candles.Add(1)
	return nil
}

func (collector *InstrumentCollector) observation(
	observed exchangecontracts.ObservedStreamEvent,
	exchangeTime time.Time,
	sequence uint64,
) marketdata.Observation {
	processed, processedOffset := collector.clock.Now(), collector.source.MonotonicOffset()
	published, publishedOffset := collector.clock.Now(), collector.source.MonotonicOffset()
	if processedOffset < observed.ReceivedOffsetNanos {
		processedOffset = observed.ReceivedOffsetNanos
	}
	if publishedOffset < processedOffset {
		publishedOffset = processedOffset
	}
	ordinal := observed.RecordToken.IngestOrdinal
	if ordinal == 0 {
		ordinal = sequence
	}
	return marketdata.Observation{ExchangeTime: exchangeTime, ReceivedAt: observed.ReceivedAt,
		ProcessedAt: processed, PublishedAt: published, ConnectionID: observed.ConnectionID,
		ConnectionGeneration: observed.ConnectionGeneration, SourceSequence: sequence,
		IngestOrdinal: ordinal, ReceivedOffsetNanos: observed.ReceivedOffsetNanos,
		ProcessedOffsetNanos: processedOffset, PublishedOffsetNanos: publishedOffset}
}

func (collector *InstrumentCollector) recordLifecycle(
	ctx context.Context,
	stream ObservedStream,
	state string,
	reason string,
) error {
	fact := exchangecontracts.LifecycleEvent{Exchange: collectorExchange,
		Instrument: collector.config.Instrument, State: state, Reason: reason,
		ConnectionID: stream.ConnectionID(), ConnectionGeneration: stream.Generation(),
		ObservedAt: collector.clock.Now()}
	return collector.recordFact(ctx, exchangecontracts.RecordLifecycle, stream.ConnectionID(), stream.Generation(), fact)
}

func (collector *InstrumentCollector) recordRebuild(
	ctx context.Context,
	observed exchangecontracts.ObservedStreamEvent,
	sequence uint64,
) error {
	fact := struct {
		Sequence   uint64 `json:"sequence"`
		Generation uint64 `json:"generation"`
	}{Sequence: sequence, Generation: observed.ConnectionGeneration}
	return collector.recordFact(ctx, exchangecontracts.RecordRebuild, observed.ConnectionID,
		observed.ConnectionGeneration, fact)
}

func (collector *InstrumentCollector) recordGap(
	ctx context.Context,
	connectionID string,
	generation uint64,
	first uint64,
	last uint64,
	reason string,
) error {
	now := collector.clock.Now().UTC
	gap := exchangecontracts.SourceGap{Instrument: collector.config.Instrument,
		ConnectionGeneration: generation, FirstSequence: first,
		LastSequence: last, StartedAt: now, EndedAt: now, Reason: reason}
	if err := collector.recorder.RecordSourceGap(ctx, gap); err != nil {
		return recorderFailure{err}
	}
	collector.stats.sequenceGaps.Add(1)
	return collector.recordFact(ctx, exchangecontracts.RecordGap, connectionID, generation, gap)
}

func (collector *InstrumentCollector) recordFact(
	ctx context.Context,
	kind exchangecontracts.PublicRecordKind,
	connectionID string,
	generation uint64,
	fact any,
) error {
	payload, err := json.Marshal(fact)
	if err != nil {
		return streamError()
	}
	token, err := collector.recorder.RecordPublicRaw(ctx, exchangecontracts.PublicRawRecord{
		Kind: kind, Raw: payload, Instrument: collector.config.Instrument,
		ReceivedAt: collector.clock.Now(), ConnectionID: connectionID,
		ConnectionGeneration: generation, MonotonicOffsetNanos: collector.source.MonotonicOffset()})
	if err != nil {
		return recorderFailure{err}
	}
	if err = collector.recorder.RecordPublicCanonical(ctx, exchangecontracts.PublicCanonicalRecord{
		Kind: kind, Token: token, Canonical: payload}); err != nil {
		return recorderFailure{err}
	}
	return nil
}
