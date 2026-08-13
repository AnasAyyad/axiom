package binance

import (
	"errors"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/marketdata"
)

// HealthSnapshot returns one immutable combined book-and-clock readiness fact.
func (collector *InstrumentCollector) HealthSnapshot() exchangecontracts.CollectorHealthSnapshot {
	view := collector.book.View()
	offset := collector.source.MonotonicOffset()
	bookFresh := view.Eligible(offset, collector.config.MaximumBookAge)
	bookHealthy := view.Health() == marketdata.HealthHealthy
	collector.healthMutex.RLock()
	clock := collector.clockHealth
	clockEligible := collector.clockEligible
	degradedSince := collector.degradedSince
	collector.healthMutex.RUnlock()
	if source, ok := collector.source.(interface{ TimeHealth() TimeHealth }); ok {
		shared := source.TimeHealth()
		if !shared.ObservedAt.IsZero() {
			clock = shared
			clockEligible = shared.Eligible &&
				(degradedSince.IsZero() || shared.ObservedAt.After(degradedSince))
			if clockEligible {
				degradedSince = time.Time{}
			}
		}
	}
	return exchangecontracts.CollectorHealthSnapshot{
		ObservedAt:       collector.lifecycle.Now().UTC(),
		Exchange:         collectorExchange,
		Instrument:       collector.config.Instrument.Symbol(),
		BookHealth:       string(view.Health()),
		BookHealthy:      bookHealthy,
		BookFresh:        bookFresh,
		BookEligible:     bookFresh,
		ClockEligible:    clockEligible,
		ClockObservedAt:  clock.ObservedAt,
		ClockOffset:      clock.Offset,
		ClockUncertainty: clock.Uncertainty,
		Eligible:         bookFresh && clockEligible,
		DegradedSince:    degradedSince,
	}
}

func (collector *InstrumentCollector) markClockDegraded(health TimeHealth, observed time.Time) {
	collector.healthMutex.Lock()
	defer collector.healthMutex.Unlock()
	collector.clockHealth = health
	collector.clockEligible = false
	if collector.degradedSince.IsZero() {
		collector.degradedSince = observed
	}
}

// setClockHealth returns the prior degradation start when this sample recovers it.
func (collector *InstrumentCollector) setClockHealth(health TimeHealth, recover bool) time.Time {
	collector.healthMutex.Lock()
	defer collector.healthMutex.Unlock()
	degraded := collector.degradedSince
	collector.clockHealth = health
	collector.clockEligible = health.Eligible
	if health.Eligible {
		collector.degradedSince = time.Time{}
	}
	if !recover {
		return time.Time{}
	}
	return degraded
}

func (collector *InstrumentCollector) recordClockResult(
	generation uint64,
	result clockSampleResult,
	eligible bool,
	backoff time.Duration,
) {
	outcome := generationOutcome{generation: generation, stage: "clock",
		clockOffset: result.health.Offset, clockUncertainty: result.health.Uncertainty}
	phase, cause := "operation_succeeded", "success"
	outcome.cause = cause
	if !eligible {
		phase, cause = "health_lost", "clock_uncertainty"
		if result.err != nil {
			cause = "clock_sample_failed"
		}
		outcome = generationFailure(outcome, "clock", cause, result.err)
	}
	diagnostic := collector.outcomeDiagnostic(outcome, phase, result.duration, backoff, 0)
	diagnostic.Action = exchangecontracts.RecoveryClockResample
	if eligible {
		diagnostic.Attribution = "recovered"
	}
	collector.recordDiagnostic(diagnostic)
}

func retryAfterOf(err error) time.Duration {
	var failure *exchangecontracts.Error
	if errors.As(err, &failure) && failure != nil {
		return failure.RetryAfter
	}
	return 0
}
