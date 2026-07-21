package bybit

import (
	"context"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
)

func (collector *InstrumentCollector) runLifecycle(ctx context.Context) error {
	attempt := uint32(0)
	for ctx.Err() == nil {
		attempt++
		err := collector.runGeneration(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if isRecorderFailure(err) {
			return err
		}
		collector.stats.reconnects.Add(1)
		if err = waitContext(ctx, reconnectBackoff(attempt,
			collector.config.MinimumBackoff, collector.config.MaximumBackoff)); err != nil {
			return nil
		}
	}
	return nil
}

func (collector *InstrumentCollector) runGeneration(ctx context.Context) error {
	request := exchangecontracts.StreamRequest{Instrument: collector.config.Instrument,
		Kinds: []exchangecontracts.StreamKind{exchangecontracts.StreamDepth,
			exchangecontracts.StreamTrades, exchangecontracts.StreamTicker, exchangecontracts.StreamCandle},
		CandleIntervals: append([]string(nil), collector.config.CandleIntervals...)}
	stream, err := collector.source.SubscribeRecorded(ctx, request, collector.recorder)
	if err != nil {
		return err
	}
	defer stream.Close()
	collector.stats.connections.Add(1)
	if err = collector.book.BeginGeneration(stream.ConnectionID(), stream.Generation()); err != nil {
		return err
	}
	if _, _, err = collector.source.SampleServerTimeRecorded(ctx, collector.config.Instrument,
		stream.ConnectionID(), stream.Generation(), collector.recorder); err != nil {
		return err
	}
	if err = collector.recordLifecycle(ctx, stream, "SYNCING", "connected"); err != nil {
		return err
	}
	return collector.consumeGeneration(ctx, stream)
}

func reconnectBackoff(attempt uint32, minimum, maximum time.Duration) time.Duration {
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

func waitContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
