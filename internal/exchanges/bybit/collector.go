package bybit

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/marketdata"
)

// InstrumentCollector owns one Bybit instrument's ordered public lifecycle.
type InstrumentCollector struct {
	config           CollectorConfig
	source           collectorSource
	recorder         exchangecontracts.PublicRecorder
	clock            domain.Clock
	book             *marketdata.Book
	candles          map[string]*marketdata.CandleStore
	provider         *marketdata.Provider
	stats            *collectorCounters
	marketUpdates    chan struct{}
	lifecycle        collectorLifecycle
	running          atomic.Bool
	healthMutex      sync.RWMutex
	clockHealth      ClockHealth
	clockEligible    bool
	degradedSince    time.Time
	evidenceSink     exchangecontracts.LifecycleEvidenceSink
	evidenceFailed   chan struct{}
	evidenceOnce     sync.Once
	evidenceMutex    sync.Mutex
	evidenceErr      error
	lifecycleCycle   atomic.Uint64
	lifecycleAttempt atomic.Uint64
}

// NewInstrumentCollector constructs a bounded raw-before-canonical collector.
func NewInstrumentCollector(
	config CollectorConfig,
	source collectorSource,
	recorder exchangecontracts.PublicRecorder,
	clock domain.Clock,
) (*InstrumentCollector, error) {
	if config.validate() != nil || source == nil || recorder == nil || clock == nil {
		return nil, streamError()
	}
	book, err := marketdata.NewBook(collectorExchange, config.Instrument,
		config.BookDepth, config.QueueCapacity, nil)
	if err != nil {
		return nil, err
	}
	provider := marketdata.NewProvider()
	if err = provider.RegisterBook(book); err != nil {
		return nil, err
	}
	stores, err := newCandleStores(config, provider)
	if err != nil {
		return nil, err
	}
	return &InstrumentCollector{config: config, source: source, recorder: recorder,
		clock: clock, book: book, candles: stores, provider: provider,
		marketUpdates: make(chan struct{}, 1),
		evidenceSink:  config.LifecycleEvidence, evidenceFailed: make(chan struct{}),
		stats: newCollectorCounters(), lifecycle: systemCollectorLifecycle{}}, nil
}

func newCandleStores(
	config CollectorConfig,
	provider *marketdata.Provider,
) (map[string]*marketdata.CandleStore, error) {
	stores := make(map[string]*marketdata.CandleStore, len(config.CandleIntervals))
	for _, interval := range config.CandleIntervals {
		store, err := marketdata.NewCandleStore(collectorExchange, config.Instrument,
			interval, config.CandleCapacity)
		if err != nil {
			return nil, err
		}
		if err = provider.RegisterCandles(store); err != nil {
			return nil, err
		}
		stores[interval] = store
	}
	return stores, nil
}

// Views exposes immutable book and completed-candle snapshots.
func (collector *InstrumentCollector) Views() marketdata.MarketViewProvider {
	return collector.provider
}

// MarketUpdates exposes coalesced commit notifications without blocking the collector hot path.
func (collector *InstrumentCollector) MarketUpdates() <-chan struct{} { return collector.marketUpdates }

func (collector *InstrumentCollector) notifyMarketUpdate() {
	select {
	case collector.marketUpdates <- struct{}{}:
	default:
	}
}

// Stats returns bounded qualification metrics.
func (collector *InstrumentCollector) Stats() CollectorStats { return collector.stats.snapshot() }

// Run reconnects until cancellation and never carries mutable book state across generations.
func (collector *InstrumentCollector) Run(ctx context.Context) error {
	if !collector.running.CompareAndSwap(false, true) {
		return streamError()
	}
	defer collector.running.Store(false)
	err := collector.runLifecycle(ctx, collector.runGeneration)
	diagnostic := collector.outcomeDiagnostic(generationOutcome{stage: "terminate",
		cause: "normal_termination"}, "terminal", 0, 0, 0)
	diagnostic.Action = exchangecontracts.RecoveryTerminate
	diagnostic.Attribution = string(exchangecontracts.AttributionRecovered)
	if err != nil && ctx.Err() == nil {
		diagnostic.Cause = "collector_failure"
		diagnostic.Attribution = string(exchangecontracts.AttributionInternal)
	}
	collector.recordDiagnostic(diagnostic)
	return err
}
