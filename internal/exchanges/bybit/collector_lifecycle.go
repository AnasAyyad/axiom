package bybit

import (
	"context"
	"time"
)

type collectorLifecycle interface {
	// Now returns the deterministic lifecycle clock.
	Now() time.Time
	// Wait advances or blocks that clock until the reconnect delay or cancellation.
	Wait(context.Context, time.Duration) error
}

type systemCollectorLifecycle struct{}

// Now returns the production wall clock used for lifecycle evidence.
func (systemCollectorLifecycle) Now() time.Time { return time.Now() }

// Wait blocks for a reconnect delay unless the lifecycle is canceled.
func (systemCollectorLifecycle) Wait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type generationRunner func(context.Context, time.Time) generationOutcome

type lifecycleState struct {
	attempt       uint32
	cycle         uint64
	resyncStarted time.Time
}

func (state lifecycleState) generationAttempt() (uint32, error) {
	if state.resyncStarted.IsZero() {
		if state.attempt == ^uint32(0) {
			return 0, streamError()
		}
		return state.attempt + 1, nil
	}
	if state.attempt == 0 {
		return 1, nil
	}
	return state.attempt, nil
}

func (collector *InstrumentCollector) runLifecycle(ctx context.Context, run generationRunner) error {
	state := lifecycleState{}
	for ctx.Err() == nil {
		attempt, err := state.generationAttempt()
		if err != nil {
			return err
		}
		collector.lifecycleCycle.Store(state.cycle)
		collector.lifecycleAttempt.Store(uint64(attempt))
		started := collector.lifecycle.Now()
		outcome := run(ctx, state.resyncStarted)
		duration := maxDuration(collector.lifecycle.Now().Sub(started), 0)
		if outcome.reason.valid() {
			collector.stats.recordReconnectReason(outcome.reason)
		}
		if outcome.fatal != nil {
			collector.recordDiagnostic(collector.outcomeDiagnostic(outcome, "fatal", duration, 0, 0))
			return outcome.fatal
		}
		if ctx.Err() != nil {
			return nil
		}
		if !outcome.reason.valid() {
			return streamError()
		}
		diagnostic, delay, advanceErr := collector.advanceLifecycle(&state, outcome, duration)
		if advanceErr != nil {
			return advanceErr
		}
		collector.recordDiagnostic(diagnostic)
		collector.stats.reconnects.Add(1)
		if err = collector.lifecycle.Wait(ctx, delay); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
	}
	return nil
}

func (collector *InstrumentCollector) advanceLifecycle(
	state *lifecycleState,
	outcome generationOutcome,
	attemptDuration time.Duration,
) (ReconnectDiagnostic, time.Duration, error) {
	backoffAttempt, phase := uint32(0), "attempt_failed"
	resyncElapsed := time.Duration(0)
	if !outcome.reachedHealthy && !state.resyncStarted.IsZero() {
		resyncElapsed = maxDuration(collector.lifecycle.Now().Sub(state.resyncStarted), 0)
	}
	if outcome.reachedHealthy {
		state.cycle++
		state.attempt, backoffAttempt, phase = 1, 1, "health_lost"
		collector.lifecycleCycle.Store(state.cycle)
		collector.lifecycleAttempt.Store(1)
		state.resyncStarted = outcome.lostHealthAt
		if state.resyncStarted.IsZero() {
			state.resyncStarted = collector.lifecycle.Now()
		}
	} else {
		if state.attempt == ^uint32(0) {
			return ReconnectDiagnostic{}, 0, streamError()
		}
		state.attempt++
		backoffAttempt = state.attempt
	}
	delay := reconnectBackoff(backoffAttempt, collector.config.MinimumBackoff, collector.config.MaximumBackoff)
	return collector.outcomeDiagnostic(outcome, phase, attemptDuration, delay, resyncElapsed), delay, nil
}

func (collector *InstrumentCollector) recordResynchronization(started time.Time, generation uint64) {
	duration := time.Duration(0)
	if !started.IsZero() {
		duration = maxDuration(collector.lifecycle.Now().Sub(started), 0)
		collector.stats.resync.record(duration)
	}
	collector.recordDiagnostic(collector.outcomeDiagnostic(generationOutcome{reachedHealthy: true,
		generation: generation, stage: "healthy", cause: "healthy"}, "health_restored", duration, 0, duration))
}

func maxDuration(left, right time.Duration) time.Duration {
	if left > right {
		return left
	}
	return right
}
