package binance

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"time"

	"axiom/internal/domain"
	"axiom/internal/marketdata"
)

// InstrumentCollector owns the single ordered writer for one public spot instrument.
type InstrumentCollector struct {
	config   CollectorConfig
	source   collectorSource
	recorder PublicRecorder
	clock    domain.Clock
	book     *marketdata.Book
	candles  *marketdata.CandleStore
	provider *marketdata.Provider
	stats    *CollectorStats
	running  atomic.Bool
}

// NewInstrumentCollector constructs one fail-closed bounded collector.
func NewInstrumentCollector(
	config CollectorConfig,
	source collectorSource,
	recorder PublicRecorder,
	clock domain.Clock,
) (*InstrumentCollector, error) {
	if config.validate() != nil || source == nil || recorder == nil || clock == nil {
		return nil, streamError()
	}
	book, err := marketdata.NewBook(collectorExchange, config.Instrument, config.BookDepth, config.QueueCapacity, nil)
	if err != nil {
		return nil, streamError()
	}
	candles, err := marketdata.NewCandleStore(collectorExchange, config.Instrument, "4h", config.CandleCapacity)
	if err != nil {
		return nil, streamError()
	}
	provider := marketdata.NewProvider()
	if provider.RegisterBook(book) != nil || provider.RegisterCandles(candles) != nil {
		return nil, streamError()
	}
	return &InstrumentCollector{config: config, source: source, recorder: recorder, clock: clock,
		book: book, candles: candles, provider: provider, stats: newCollectorStats()}, nil
}

// Views exposes immutable book and candle snapshots.
func (collector *InstrumentCollector) Views() marketdata.MarketViewProvider {
	return collector.provider
}

// Stats returns bounded qualification metrics.
func (collector *InstrumentCollector) Stats() CollectorStatsSnapshot {
	return collector.stats.Snapshot()
}

// Run reconnects until cancellation while every generation starts ineligible.
func (collector *InstrumentCollector) Run(ctx context.Context) error {
	if !collector.running.CompareAndSwap(false, true) {
		return streamError()
	}
	defer collector.running.Store(false)
	var attempt uint32
	resyncStarted := time.Now()
	for ctx.Err() == nil {
		outcome := collector.runGeneration(ctx, resyncStarted)
		if outcome.fatal != nil {
			return outcome.fatal
		}
		if ctx.Err() != nil {
			return nil
		}
		attempt++
		resyncStarted = time.Now()
		collector.stats.reconnects.Add(1)
		delay, err := collector.config.ConnectionPolicy.Backoff(attempt)
		if err != nil {
			return err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil
		case <-timer.C:
		}
	}
	return nil
}

type generationOutcome struct{ fatal error }

type fatalCollectorError struct{ error }

func isFatalCollectorError(err error) bool {
	_, ok := err.(fatalCollectorError)
	return ok
}

type lifecycleFact struct {
	State      string `json:"state"`
	Reason     string `json:"reason,omitempty"`
	Generation uint64 `json:"generation"`
}

type subscriptionFact struct {
	Streams    []string `json:"streams"`
	Generation uint64   `json:"generation"`
}

type rebuildFact struct {
	SnapshotSequence uint64 `json:"snapshot_sequence"`
	BufferedDepth    int    `json:"buffered_depth"`
	Generation       uint64 `json:"generation"`
}

func (collector *InstrumentCollector) pauseOutcome(
	ctx context.Context,
	connectionID string,
	generation, sequence uint64,
	reason string,
) generationOutcome {
	_ = collector.book.Invalidate(reason, sequence)
	if _, err := collector.recordFact(ctx, RecordLifecycle, connectionID, generation,
		lifecycleFact{State: "PAUSED", Reason: reason, Generation: generation}); err != nil && ctx.Err() == nil {
		return generationOutcome{fatal: err}
	}
	return generationOutcome{}
}

func (collector *InstrumentCollector) recordFact(
	ctx context.Context,
	kind PublicRecordKind,
	connectionID string,
	generation uint64,
	fact any,
) (StreamRecordToken, error) {
	payload, err := json.Marshal(fact)
	if err != nil {
		return StreamRecordToken{}, streamError()
	}
	now := collector.clock.Now()
	token, err := collector.recorder.RecordPublicRaw(ctx, PublicRawRecord{Kind: kind, Raw: payload,
		Instrument: collector.config.Instrument, ReceivedAt: now, ConnectionID: connectionID,
		ConnectionGeneration: generation, MonotonicOffsetNanos: collector.source.MonotonicOffset()})
	if err != nil {
		return StreamRecordToken{}, err
	}
	if err = collector.recorder.RecordPublicCanonical(ctx, PublicCanonicalRecord{Kind: kind, Token: token,
		Canonical: payload}); err != nil {
		return StreamRecordToken{}, err
	}
	return token, nil
}
