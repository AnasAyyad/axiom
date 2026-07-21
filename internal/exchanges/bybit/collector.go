package bybit

import (
	"context"
	"sync/atomic"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/marketdata"
)

// InstrumentCollector owns one Bybit instrument's ordered public lifecycle.
type InstrumentCollector struct {
	config   CollectorConfig
	source   collectorSource
	recorder exchangecontracts.PublicRecorder
	clock    domain.Clock
	book     *marketdata.Book
	candles  map[string]*marketdata.CandleStore
	provider *marketdata.Provider
	stats    collectorCounters
	running  atomic.Bool
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
		clock: clock, book: book, candles: stores, provider: provider}, nil
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

// Stats returns bounded qualification metrics.
func (collector *InstrumentCollector) Stats() CollectorStats { return collector.stats.snapshot() }

// Run reconnects until cancellation and never carries mutable book state across generations.
func (collector *InstrumentCollector) Run(ctx context.Context) error {
	if !collector.running.CompareAndSwap(false, true) {
		return streamError()
	}
	defer collector.running.Store(false)
	return collector.runLifecycle(ctx)
}
