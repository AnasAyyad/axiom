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
	}
	collector.notifyMarketUpdate()
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

type healthyTimers struct {
	renewal *time.Timer
	clock   *time.Ticker
	stale   *time.Ticker
}

func newHealthyTimers(config CollectorConfig) healthyTimers {
	return healthyTimers{
		renewal: time.NewTimer(config.ConnectionPolicy.Renewal),
		clock:   time.NewTicker(config.ClockSyncEvery),
		stale:   time.NewTicker(config.StaleCheckEvery),
	}
}

func (timers healthyTimers) stop() {
	timers.renewal.Stop()
	timers.clock.Stop()
	timers.stale.Stop()
}

type clockRecoveryState struct {
	results      chan clockSampleResult
	inFlight     bool
	attempt      uint32
	retry        *time.Timer
	retryChannel <-chan time.Time
}

func newClockRecoveryState() *clockRecoveryState {
	return &clockRecoveryState{results: make(chan clockSampleResult, 1)}
}

func (state *clockRecoveryState) stop() {
	if state.retry != nil {
		state.retry.Stop()
	}
}

func (state *clockRecoveryState) scheduleRetry(delay time.Duration) {
	if state.retry == nil {
		state.retry = time.NewTimer(delay)
	} else {
		state.retry.Reset(delay)
	}
	state.retryChannel = state.retry.C
}

func (state *clockRecoveryState) start(
	ctx context.Context,
	collector *InstrumentCollector,
	connectionID string,
	generation uint64,
) {
	if state.inFlight {
		return
	}
	state.inFlight = true
	go collector.sampleClock(ctx, connectionID, generation, state.results)
}

func (state *clockRecoveryState) handle(
	collector *InstrumentCollector,
	generation uint64,
	result clockSampleResult,
) (generationOutcome, bool) {
	state.inFlight = false
	if isRecorderFailure(result.err) {
		return generationFailure(generationOutcome{fatal: result.err, generation: generation},
			"clock", "recorder", result.err), true
	}
	if result.err == nil && result.health.Eligible {
		degraded := collector.setClockHealth(result.health, true)
		collector.recordClockResult(generation, result, true, 0)
		if !degraded.IsZero() {
			collector.recordClockResynchronization(degraded, generation)
		}
		state.attempt = 0
		if state.retry != nil && state.retry.Stop() {
			state.retryChannel = nil
		}
		return generationOutcome{}, false
	}
	collector.markClockDegraded(result.health, collector.lifecycle.Now())
	if state.attempt != ^uint32(0) {
		state.attempt++
	}
	delay, err := collector.config.ConnectionPolicy.Backoff(max(state.attempt, 1))
	if err != nil {
		return generationOutcome{fatal: err, generation: generation}, true
	}
	if retry := retryAfterOf(result.err); retry > delay {
		delay = retry
	}
	collector.recordClockResult(generation, result, false, delay)
	state.scheduleRetry(delay)
	return generationOutcome{}, false
}

func (collector *InstrumentCollector) runHealthy(
	ctx context.Context,
	events <-chan observedResult,
	overflow <-chan struct{},
	connectionID string,
	generation uint64,
	initialClockRetry time.Duration,
) generationOutcome {
	timers := newHealthyTimers(collector.config)
	defer timers.stop()
	clock := newClockRecoveryState()
	defer clock.stop()
	if initialClockRetry > 0 {
		clock.attempt = 1
		clock.scheduleRetry(initialClockRetry)
	}
	for {
		select {
		case <-ctx.Done():
			return generationOutcome{}
		case <-collector.evidenceFailed:
			return generationOutcome{fatal: collector.lifecycleEvidenceError(), generation: generation}
		case <-timers.renewal.C:
			return collector.pauseOutcome(ctx, connectionID, generation, collector.book.View().Sequence(), reconnectScheduledRenewal)
		case <-overflow:
			return generationFailure(collector.pauseOutcome(ctx, connectionID, generation,
				collector.book.View().Sequence(), reconnectQueue), "healthy_queue", "queue_overflow", nil)
		case <-timers.clock.C:
			clock.start(ctx, collector, connectionID, generation)
		case <-clock.retryChannel:
			clock.retryChannel = nil
			clock.start(ctx, collector, connectionID, generation)
		case result := <-clock.results:
			if outcome, failed := clock.handle(collector, generation, result); failed {
				return outcome
			}
		case <-timers.stale.C:
			// Book age and transport health are independent facts. A quiet spot book
			// remains ineligible through View.Eligible and HealthSnapshot, but silence
			// alone does not prove a stream fault and must not consume REST snapshot
			// budget. Stream errors, sequence gaps, invalid events, and queue failures
			// continue to invalidate the generation and reconnect fail closed.
			_ = collector.book.View().Eligible(collector.source.MonotonicOffset(),
				collector.config.MaximumBookAge)
		case result := <-events:
			if outcome, failed := collector.healthyEventOutcome(ctx, result, len(events), connectionID, generation); failed {
				return outcome
			}
		}
	}
}

func (collector *InstrumentCollector) healthyEventOutcome(
	ctx context.Context,
	result observedResult,
	queueDepth int,
	connectionID string,
	generation uint64,
) (generationOutcome, bool) {
	if result.err != nil {
		return collector.streamFailureOutcome(ctx, connectionID, generation, result.err), true
	}
	collector.stats.observeQueue(queueDepth)
	priorGaps := collector.stats.gaps.Load()
	if err := collector.processObserved(ctx, result.event, generation); err != nil {
		if isFatalCollectorError(err) {
			return generationOutcome{fatal: err}, true
		}
		reason, cause := reconnectInvalidEvent, "invalid_event"
		if collector.stats.gaps.Load() > priorGaps {
			reason, cause = reconnectSequenceGap, "sequence_gap"
		}
		return generationFailure(collector.pauseOutcome(ctx, connectionID, generation,
			collector.book.View().Sequence(), reason), "event_process", cause, err), true
	}
	return generationOutcome{}, false
}

func (collector *InstrumentCollector) sampleClock(
	ctx context.Context,
	connectionID string,
	generation uint64,
	results chan<- clockSampleResult,
) {
	started := collector.lifecycle.Now()
	health, _, err := collector.source.SampleServerTimeRecorded(ctx, collector.config.Instrument,
		connectionID, generation, collector.recorder)
	result := clockSampleResult{health: health, err: err, duration: collector.lifecycle.Now().Sub(started)}
	select {
	case results <- result:
	case <-ctx.Done():
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
		collector.notifyMarketUpdate()
	case exchangecontracts.StreamTrades:
		if observed.Event.Trade == nil {
			return streamError()
		}
		collector.stats.trades.Add(1)
	case exchangecontracts.StreamCandle:
		if err := collector.processCandle(observed); err != nil {
			return err
		}
		collector.notifyMarketUpdate()
	case exchangecontracts.StreamTicker:
		if observed.Event.Ticker == nil {
			return streamError()
		}
	default:
		return streamError()
	}
	collector.stats.hotPath.record(time.Duration(observed.DecodeNanos) + time.Since(started))
	return nil
}

func (collector *InstrumentCollector) processCandle(observed ObservedStreamEvent) error {
	if observed.Event.Candle == nil {
		return streamError()
	}
	if !observed.Event.Candle.Closed {
		return nil
	}
	sequence := uint64(observed.Event.Candle.CloseTime.UnixMilli())
	store := collector.candleStores[observed.Event.Candle.Interval]
	if store == nil {
		return streamError()
	}
	if err := store.Add(*observed.Event.Candle,
		collector.streamObservation(observed, observed.Event.Candle.CloseTime, sequence)); err != nil {
		return err
	}
	collector.stats.candles.Add(1)
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
