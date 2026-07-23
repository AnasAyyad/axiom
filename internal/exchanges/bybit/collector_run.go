package bybit

import (
	"context"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
)

func (collector *InstrumentCollector) runGeneration(ctx context.Context, resyncStarted time.Time) generationOutcome {
	request := exchangecontracts.StreamRequest{Instrument: collector.config.Instrument,
		Kinds: []exchangecontracts.StreamKind{exchangecontracts.StreamDepth,
			exchangecontracts.StreamTrades, exchangecontracts.StreamTicker, exchangecontracts.StreamCandle},
		CandleIntervals: append([]string(nil), collector.config.CandleIntervals...)}
	subscriptionStarted := collector.lifecycle.Now()
	stream, err := collector.source.SubscribeRecorded(ctx, request, collector.recorder)
	if err != nil {
		outcome := generationFailure(generationOutcome{reason: reconnectSubscription,
			lostHealthAt: collector.lifecycle.Now()}, "subscription", "subscription_failed", err)
		if isRecorderFailure(err) {
			outcome.fatal = err
		}
		return outcome
	}
	defer stream.Close()
	collector.stats.connections.Add(1)
	generation := stream.Generation()
	collector.recordOperationDiagnostic("subscription", generation,
		collector.lifecycle.Now().Sub(subscriptionStarted), 0)
	if err = collector.book.BeginGeneration(stream.ConnectionID(), stream.Generation()); err != nil {
		return generationFailure(generationOutcome{fatal: err, generation: generation},
			"generation", "generation_begin_failed", err)
	}
	clockStarted := collector.lifecycle.Now()
	health, _, err := collector.source.SampleServerTimeRecorded(ctx, collector.config.Instrument,
		stream.ConnectionID(), generation, collector.recorder)
	if err != nil {
		outcome := generationFailure(generationOutcome{reason: reconnectClock,
			lostHealthAt: collector.lifecycle.Now(), generation: generation}, "clock", "clock_sample_failed", err)
		if isRecorderFailure(err) {
			outcome.fatal = err
		}
		return outcome
	}
	collector.recordOperationDiagnostic("clock", generation,
		collector.lifecycle.Now().Sub(clockStarted), health.Uncertainty)
	if !health.Eligible {
		return generationOutcome{reason: reconnectClock, lostHealthAt: collector.lifecycle.Now(),
			generation: generation, stage: "clock", cause: "clock_uncertainty",
			failureKind: exchangecontracts.ErrorTimestamp,
			operation:   exchangecontracts.OperationMetadata, clockUncertainty: health.Uncertainty}
	}
	if err = collector.recordLifecycle(ctx, stream, "SYNCING", "connected"); err != nil {
		return generationFailure(generationOutcome{fatal: err, generation: generation},
			"recorder", "recorder", err)
	}
	return collector.consumeGeneration(ctx, stream, resyncStarted)
}

func reconnectBackoff(attempt uint32, minimum, maximum time.Duration) time.Duration {
	if attempt == 0 {
		attempt = 1
	}
	delay := minimum
	for count := uint32(1); count < attempt && delay < maximum; count++ {
		if delay > maximum/2 {
			return maximum
		}
		delay *= 2
	}
	if delay > maximum {
		return maximum
	}
	return delay
}
