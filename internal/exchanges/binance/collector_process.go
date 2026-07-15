package binance

import (
	"context"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/marketdata"
)

func (collector *InstrumentCollector) installSnapshot(
	ctx context.Context,
	result snapshotResult,
	pending []ObservedStreamEvent,
	connectionID string,
	generation uint64,
) error {
	started := time.Now()
	offset := collector.source.MonotonicOffset()
	observation := collector.observation(result.snapshot.ReceivedAt, time.Time{}, connectionID, generation,
		result.snapshot.LastSequence, result.token.IngestOrdinal, offset)
	for _, observed := range pending {
		if observed.Event.Depth == nil {
			continue
		}
		collector.stats.messages.Add(1)
		if err := collector.book.Buffer(marketdata.DepthEvent{Update: *observed.Event.Depth,
			Observation: collector.streamObservation(observed, observed.Event.Depth.ExchangeTime,
				observed.Event.Depth.LastSequence)}); err != nil {
			if recordErr := collector.recordSequenceGap(ctx, connectionID, generation, result.snapshot.LastSequence+1,
				observed.Event.Depth.LastSequence, "snapshot_bridge_gap"); recordErr != nil {
				return fatalCollectorError{recordErr}
			}
			return err
		}
	}
	if err := collector.book.InstallSnapshot(result.snapshot, observation); err != nil {
		if first, last, gap := snapshotBridgeGap(result.snapshot.LastSequence, pending); gap {
			if recordErr := collector.recordSequenceGap(ctx, connectionID, generation, first, last,
				"snapshot_bridge_gap"); recordErr != nil {
				return fatalCollectorError{recordErr}
			}
		}
		return err
	}
	for _, observed := range pending {
		if observed.Event.Depth == nil {
			continue
		}
		collector.stats.depthUpdates.Add(1)
		collector.stats.hotPath.record(time.Duration(observed.DecodeNanos) + time.Since(started))
	}
	return nil
}

func snapshotBridgeGap(snapshotSequence uint64, pending []ObservedStreamEvent) (uint64, uint64, bool) {
	sequence := snapshotSequence
	for _, observed := range pending {
		if observed.Event.Depth == nil || observed.Event.Depth.LastSequence <= sequence {
			continue
		}
		next := sequence + 1
		if observed.Event.Depth.FirstSequence > next || observed.Event.Depth.LastSequence < next {
			return next, observed.Event.Depth.LastSequence, true
		}
		sequence = observed.Event.Depth.LastSequence
	}
	return 0, 0, false
}

func (collector *InstrumentCollector) runHealthy(
	ctx context.Context,
	events <-chan observedResult,
	overflow <-chan struct{},
	connectionID string,
	generation uint64,
) generationOutcome {
	renewal := time.NewTimer(collector.config.ConnectionPolicy.Renewal)
	defer renewal.Stop()
	clockTicker := time.NewTicker(collector.config.ClockSyncEvery)
	defer clockTicker.Stop()
	staleTicker := time.NewTicker(collector.config.StaleCheckEvery)
	defer staleTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return generationOutcome{}
		case <-renewal.C:
			return collector.pauseOutcome(ctx, connectionID, generation, collector.book.View().Sequence(), "scheduled_renewal")
		case <-overflow:
			return collector.pauseOutcome(ctx, connectionID, generation, collector.book.View().Sequence(), "queue_overflow")
		case <-clockTicker.C:
			health, _, err := collector.source.SampleServerTimeRecorded(ctx, collector.config.Instrument,
				connectionID, generation, collector.recorder)
			if err != nil || !health.Eligible {
				return collector.pauseOutcome(ctx, connectionID, generation, collector.book.View().Sequence(), "clock_ineligible")
			}
		case <-staleTicker.C:
			if collector.book.MarkStale(collector.source.MonotonicOffset(),
				uint64(collector.config.MaximumBookAge.Nanoseconds())) != nil {
				return collector.pauseOutcome(ctx, connectionID, generation, collector.book.View().Sequence(), "book_stale")
			}
		case result := <-events:
			if result.err != nil {
				if exchangecontracts.KindOf(result.err) == exchangecontracts.ErrorValidation {
					collector.stats.decoderErrors.Add(1)
				}
				return collector.pauseOutcome(ctx, connectionID, generation, collector.book.View().Sequence(), "stream_failed")
			}
			collector.stats.observeQueue(len(events))
			if err := collector.processObserved(ctx, result.event, generation); err != nil {
				if isFatalCollectorError(err) {
					return generationOutcome{fatal: err}
				}
				return collector.pauseOutcome(ctx, connectionID, generation, collector.book.View().Sequence(), "event_invalid")
			}
		}
	}
}

func (collector *InstrumentCollector) processObserved(ctx context.Context, observed ObservedStreamEvent, generation uint64) error {
	started := time.Now()
	collector.stats.messages.Add(1)
	switch observed.Event.Kind {
	case exchangecontracts.StreamDepth:
		if observed.Event.Depth == nil {
			return streamError()
		}
		before := collector.book.View().Sequence()
		err := collector.book.Apply(marketdata.DepthEvent{Update: *observed.Event.Depth,
			Observation: collector.streamObservation(observed, observed.Event.Depth.ExchangeTime,
				observed.Event.Depth.LastSequence)})
		if err != nil {
			first := before + 1
			if first == 1 {
				first = observed.Event.Depth.FirstSequence
			}
			if recordErr := collector.recordSequenceGap(ctx, observed.ConnectionID, generation, first,
				observed.Event.Depth.LastSequence, "sequence_gap"); recordErr != nil {
				return fatalCollectorError{recordErr}
			}
			return err
		}
		collector.stats.depthUpdates.Add(1)
	case exchangecontracts.StreamTrades:
		if observed.Event.Trade == nil {
			return streamError()
		}
		collector.stats.trades.Add(1)
	case exchangecontracts.StreamCandle:
		if observed.Event.Candle == nil {
			return streamError()
		}
		if observed.Event.Candle.Closed {
			sequence := uint64(observed.Event.Candle.CloseTime.UnixMilli())
			if err := collector.candles.Add(*observed.Event.Candle,
				collector.streamObservation(observed, observed.Event.Candle.CloseTime, sequence)); err != nil {
				return err
			}
			collector.stats.candles.Add(1)
		}
	default:
		return streamError()
	}
	collector.stats.hotPath.record(time.Duration(observed.DecodeNanos) + time.Since(started))
	return nil
}

func (collector *InstrumentCollector) streamObservation(
	observed ObservedStreamEvent,
	exchangeTime time.Time,
	sequence uint64,
) marketdata.Observation {
	return collector.observation(observed.ReceivedAt, exchangeTime, observed.ConnectionID,
		observed.ConnectionGeneration, sequence, observed.RecordToken.IngestOrdinal, observed.ReceivedOffsetNanos)
}

func (collector *InstrumentCollector) observation(
	received domain.EventTime,
	exchangeTime time.Time,
	connectionID string,
	generation, sequence, ordinal, receivedOffset uint64,
) marketdata.Observation {
	processed, processedOffset := collector.clock.Now(), collector.source.MonotonicOffset()
	published, publishedOffset := collector.clock.Now(), collector.source.MonotonicOffset()
	return marketdata.Observation{ExchangeTime: exchangeTime, ReceivedAt: received, ProcessedAt: processed,
		PublishedAt: published, ConnectionID: connectionID, ConnectionGeneration: generation,
		SourceSequence: sequence, IngestOrdinal: ordinal, ReceivedOffsetNanos: receivedOffset,
		ProcessedOffsetNanos: processedOffset, PublishedOffsetNanos: publishedOffset}
}

func (collector *InstrumentCollector) recordSequenceGap(
	ctx context.Context,
	connectionID string,
	generation, first, last uint64,
	reason string,
) error {
	if first == 0 || last < first {
		last = first
	}
	now := collector.clock.Now().UTC
	gap := SourceGap{Instrument: collector.config.Instrument, ConnectionGeneration: generation,
		FirstSequence: first, LastSequence: last, StartedAt: now, EndedAt: now, Reason: reason}
	if err := collector.recorder.RecordSourceGap(ctx, gap); err != nil {
		return err
	}
	collector.stats.gaps.Add(1)
	_, err := collector.recordFact(ctx, RecordGap, connectionID, generation, gap)
	return err
}
