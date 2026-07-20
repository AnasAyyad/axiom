package binance

import (
	"context"
	"time"
)

// collectorLifecycle keeps reconnect timing deterministic in package tests
// without changing event timestamps or production scheduling.
type collectorLifecycle interface {
	Now() time.Time
	Wait(context.Context, time.Duration) error
}

type systemCollectorLifecycle struct{}

// Now returns the lifecycle wall-clock instant.
func (systemCollectorLifecycle) Now() time.Time { return time.Now() }

// Wait blocks for a reconnect delay or cancellation.
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

func (collector *InstrumentCollector) runLifecycle(ctx context.Context, run generationRunner) error {
	var attempt uint32
	var resyncStarted time.Time
	for ctx.Err() == nil {
		outcome := run(ctx, resyncStarted)
		if outcome.reason.valid() {
			collector.stats.recordReconnectReason(outcome.reason)
		}
		if outcome.fatal != nil {
			return outcome.fatal
		}
		if ctx.Err() != nil {
			return nil
		}
		if !outcome.reason.valid() {
			return streamError()
		}
		if outcome.reachedHealthy {
			attempt = 1
			resyncStarted = outcome.lostHealthAt
			if resyncStarted.IsZero() {
				resyncStarted = collector.lifecycle.Now()
			}
		} else {
			if attempt == ^uint32(0) {
				return streamError()
			}
			attempt++
		}
		collector.stats.reconnects.Add(1)
		delay, err := collector.config.ConnectionPolicy.Backoff(attempt)
		if err != nil {
			return err
		}
		if err = collector.lifecycle.Wait(ctx, delay); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
	}
	return nil
}

func (collector *InstrumentCollector) recordResynchronization(started time.Time) {
	if started.IsZero() {
		return
	}
	duration := collector.lifecycle.Now().Sub(started)
	collector.stats.resync.record(duration)
}

type recorderFailure struct{ error }

func isRecorderFailure(err error) bool {
	_, ok := err.(recorderFailure)
	return ok
}
