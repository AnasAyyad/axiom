package binance

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/marketdata"
)

// InstrumentCollector owns the single ordered writer for one public spot instrument.
type InstrumentCollector struct {
	config           CollectorConfig
	source           collectorSource
	recorder         PublicRecorder
	clock            domain.Clock
	book             *marketdata.Book
	candles          *marketdata.CandleStore
	candleStores     map[string]*marketdata.CandleStore
	provider         *marketdata.Provider
	stats            *CollectorStats
	running          atomic.Bool
	lifecycle        collectorLifecycle
	lifecycleCycle   atomic.Uint64
	lifecycleAttempt atomic.Uint64
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
	provider := marketdata.NewProvider()
	if provider.RegisterBook(book) != nil {
		return nil, streamError()
	}
	stores := make(map[string]*marketdata.CandleStore, len(config.CandleIntervals))
	for _, interval := range config.CandleIntervals {
		store, storeErr := marketdata.NewCandleStore(collectorExchange, config.Instrument,
			interval, config.CandleCapacity)
		if storeErr != nil || provider.RegisterCandles(store) != nil {
			return nil, streamError()
		}
		stores[interval] = store
	}
	return &InstrumentCollector{config: config, source: source, recorder: recorder, clock: clock,
		book: book, candles: stores["4h"], candleStores: stores, provider: provider, stats: newCollectorStats(),
		lifecycle: systemCollectorLifecycle{}}, nil
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
	return collector.runLifecycle(ctx, collector.runGeneration)
}

type generationOutcome struct {
	fatal            error
	reachedHealthy   bool
	reason           reconnectReason
	lostHealthAt     time.Time
	generation       uint64
	stage            string
	cause            string
	failureKind      exchangecontracts.ErrorKind
	operation        exchangecontracts.Operation
	retryAfter       time.Duration
	httpStatus       int
	failureMetadata  exchangecontracts.FailureMetadata
	clockOffset      time.Duration
	clockUncertainty time.Duration
}

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
	reason reconnectReason,
) generationOutcome {
	lostHealthAt := collector.lifecycle.Now()
	_ = collector.book.Invalidate(reason.String(), sequence)
	if _, err := collector.recordFact(ctx, RecordLifecycle, connectionID, generation,
		lifecycleFact{State: "PAUSED", Reason: reason.String(), Generation: generation}); err != nil && ctx.Err() == nil {
		return generationFailure(generationOutcome{fatal: err, reason: reason, lostHealthAt: lostHealthAt,
			generation: generation}, "recorder", "recorder", err)
	}
	return generationOutcome{reason: reason, lostHealthAt: lostHealthAt, generation: generation,
		stage: reason.String(), cause: reason.String()}
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
