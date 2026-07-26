package bybit

import (
	"context"
	"time"
)

type generationTimers struct {
	heartbeat *time.Ticker
	clock     *time.Ticker
	stale     *time.Ticker
	renewal   *time.Timer
}

func newGenerationTimers(config CollectorConfig) generationTimers {
	return generationTimers{
		heartbeat: time.NewTicker(config.HeartbeatEvery),
		clock:     time.NewTicker(config.ClockSyncEvery),
		stale:     time.NewTicker(config.StaleCheckEvery),
		renewal:   time.NewTimer(config.Renewal),
	}
}

func (timers generationTimers) stop() {
	timers.heartbeat.Stop()
	timers.clock.Stop()
	timers.stale.Stop()
	timers.renewal.Stop()
}

type bybitClockRecovery struct {
	results      chan clockSampleResult
	inFlight     bool
	attempt      uint32
	retry        *time.Timer
	retryChannel <-chan time.Time
}

func newBybitClockRecovery() *bybitClockRecovery {
	return &bybitClockRecovery{results: make(chan clockSampleResult, 1)}
}

func (state *bybitClockRecovery) stop() {
	if state.retry != nil {
		state.retry.Stop()
	}
}

func (state *bybitClockRecovery) start(
	ctx context.Context,
	collector *InstrumentCollector,
	stream ObservedStream,
) {
	if state.inFlight {
		return
	}
	state.inFlight = true
	go collector.sampleClock(ctx, stream, state.results)
}

func (state *bybitClockRecovery) handle(
	ctx context.Context,
	collector *InstrumentCollector,
	stream ObservedStream,
	result clockSampleResult,
	reachedHealthy bool,
) (generationOutcome, bool) {
	generation := stream.Generation()
	state.inFlight = false
	if isRecorderFailure(result.err) {
		return generationFailure(generationOutcome{fatal: result.err, generation: generation},
			"clock", "recorder", result.err), true
	}
	if result.err == nil && result.health.Eligible {
		degraded := collector.setClockHealth(result.health, true)
		collector.recordClockResult(generation, result, true, 0)
		if reachedHealthy && !degraded.IsZero() {
			if err := collector.recordLifecycle(ctx, stream, "HEALTHY", "clock_recovered"); err != nil {
				return generationFailure(generationOutcome{fatal: err, generation: generation},
					"recorder", "recorder", err), true
			}
			collector.recordResynchronization(degraded, generation)
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
	delay := reconnectBackoff(max(state.attempt, 1),
		collector.config.MinimumBackoff, collector.config.MaximumBackoff)
	if retry := retryAfterOf(result.err); retry > delay {
		delay = retry
	}
	collector.recordClockResult(generation, result, false, delay)
	if reachedHealthy {
		if err := collector.recordLifecycle(ctx, stream, "DEGRADED_CLOCK", "clock_invalid"); err != nil {
			return generationFailure(generationOutcome{fatal: err, generation: generation},
				"recorder", "recorder", err), true
		}
	}
	state.scheduleRetry(delay)
	return generationOutcome{}, false
}

func (state *bybitClockRecovery) scheduleRetry(delay time.Duration) {
	if state.retry == nil {
		state.retry = time.NewTimer(delay)
	} else {
		state.retry.Reset(delay)
	}
	state.retryChannel = state.retry.C
}

func (collector *InstrumentCollector) heartbeatOutcome(
	ctx context.Context,
	stream ObservedStream,
	reachedHealthy bool,
) (generationOutcome, bool) {
	err := stream.Ping(ctx)
	if err == nil {
		return generationOutcome{}, false
	}
	if ctx.Err() != nil {
		return generationOutcome{reachedHealthy: reachedHealthy, generation: stream.Generation()}, true
	}
	return collector.failedGeneration(ctx, stream, reachedHealthy,
		reconnectHeartbeat, "heartbeat", "heartbeat_failed", err), true
}

func (collector *InstrumentCollector) staleOutcome(
	ctx context.Context,
	stream ObservedStream,
	reachedHealthy bool,
) (generationOutcome, bool) {
	if collector.book.View().Version() == 0 ||
		collector.book.MarkStale(collector.source.MonotonicOffset(),
			uint64(collector.config.MaximumBookAge.Nanoseconds())) == nil {
		return generationOutcome{}, false
	}
	return collector.failedGeneration(ctx, stream, reachedHealthy,
		reconnectStaleBook, "stale_book", "book_stale", nil), true
}

func (collector *InstrumentCollector) queueOverflowOutcome(
	ctx context.Context,
	stream ObservedStream,
	reachedHealthy bool,
) generationOutcome {
	err := collector.handleQueueOverflow(ctx, stream)
	return collector.failedGeneration(ctx, stream, reachedHealthy,
		reconnectQueue, "receiver_queue", "queue_overflow", err)
}
