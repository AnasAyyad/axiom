package bootstrap

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/marketdata"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/strategies/arbitrage"
)

// SandboxSagaSnapshotSource retrieves allowlisted public order-book snapshots.
type SandboxSagaSnapshotSource interface {
	Snapshot(context.Context, exchangecontracts.SnapshotRequest) (exchangecontracts.BookSnapshot, error)
}

// SandboxSagaClockHealthSource samples an exchange's public server clock.
type SandboxSagaClockHealthSource interface {
	SampleServerTime(context.Context) (exchangecontracts.ClockHealth, error)
}

// SandboxSagaMarketCache continuously normalizes credential-free public REST
// snapshots before a scheduler decision. Capture is read-only and never makes
// a network request, so its as-of instant cannot precede a response fetched in
// the same evaluation.
type SandboxSagaMarketCache struct {
	data        SandboxSagaSnapshotSource
	clock       domain.Clock
	monotonic   exchangecontracts.MonotonicSource
	clockHealth SandboxSagaClockHealthSource
	exchange    string
	rules       map[string]arbitrage.InstrumentRules

	mutex      sync.RWMutex
	members    map[string]SandboxSagaMarketMember
	generation atomic.Uint64
}

type sandboxSagaSnapshotResult struct {
	symbol   string
	snapshot exchangecontracts.BookSnapshot
	err      error
}

// NewSandboxSagaMarketCache constructs one exchange-local coherent market cache.
func NewSandboxSagaMarketCache(
	data SandboxSagaSnapshotSource,
	clock domain.Clock,
	monotonic exchangecontracts.MonotonicSource,
	clockHealth SandboxSagaClockHealthSource,
	exchange string,
	rules []arbitrage.InstrumentRules,
) (*SandboxSagaMarketCache, error) {
	if data == nil || clock == nil || monotonic == nil || clockHealth == nil || exchange == "" ||
		len(rules) == 0 || len(rules) > 3 {
		return nil, fmt.Errorf("sandbox_saga_market_cache_invalid")
	}
	bySymbol := make(map[string]arbitrage.InstrumentRules, len(rules))
	now := clock.Now().UTC
	for _, rule := range rules {
		key := runtimecore.MarketKey{Exchange: exchange, Instrument: rule.Metadata.Instrument}
		if !validSandboxSagaInstrumentRules(rule, key, now) {
			return nil, fmt.Errorf("sandbox_saga_market_cache_invalid")
		}
		symbol := rule.Metadata.Instrument.Symbol()
		if _, duplicate := bySymbol[symbol]; duplicate {
			return nil, fmt.Errorf("sandbox_saga_market_cache_invalid")
		}
		bySymbol[symbol] = rule
	}
	return &SandboxSagaMarketCache{data: data, clock: clock, monotonic: monotonic,
		clockHealth: clockHealth, exchange: exchange, rules: bySymbol,
		members: make(map[string]SandboxSagaMarketMember, len(rules))}, nil
}

// Refresh obtains every configured book concurrently and atomically publishes
// the complete generation. A partial refresh leaves the prior generation in
// place; freshness then expires naturally instead of mixing capture cycles.
func (cache *SandboxSagaMarketCache) Refresh(ctx context.Context) error {
	if cache == nil || cache.data == nil || cache.clock == nil || cache.monotonic == nil ||
		cache.clockHealth == nil || ctx == nil {
		return fmt.Errorf("sandbox_saga_market_cache_invalid")
	}
	health, err := cache.clockHealth.SampleServerTime(ctx)
	if err != nil || !health.Eligible || health.ObservedAt.IsZero() || health.ObservedAt.Location() != time.UTC {
		return fmt.Errorf("sandbox_saga_market_clock_unavailable")
	}
	loaded, err := cache.loadSagaMarketSnapshots(ctx)
	if err != nil {
		return err
	}
	next, err := cache.normalizeSagaMarketGeneration(loaded, health)
	if err != nil {
		return err
	}
	cache.mutex.Lock()
	cache.members = next
	cache.mutex.Unlock()
	return nil
}

func (cache *SandboxSagaMarketCache) loadSagaMarketSnapshots(ctx context.Context) (
	map[string]exchangecontracts.BookSnapshot, error,
) {
	results := make(chan sandboxSagaSnapshotResult, len(cache.rules))
	for symbol, rule := range cache.rules {
		symbol, instrument := symbol, rule.Metadata.Instrument
		go func() {
			snapshot, loadErr := cache.data.Snapshot(ctx, exchangecontracts.SnapshotRequest{
				Instrument: instrument, Depth: 50,
			})
			results <- sandboxSagaSnapshotResult{symbol: symbol, snapshot: snapshot, err: loadErr}
		}()
	}
	loaded := make(map[string]exchangecontracts.BookSnapshot, len(cache.rules))
	for range cache.rules {
		item := <-results
		if item.err != nil || item.snapshot.Exchange != exchangecontracts.ExchangeID(cache.exchange) ||
			item.snapshot.Instrument != cache.rules[item.symbol].Metadata.Instrument ||
			item.snapshot.LastSequence == 0 || item.snapshot.ReceivedAt.Validate() != nil ||
			len(item.snapshot.Bids) == 0 || len(item.snapshot.Asks) == 0 ||
			item.snapshot.RawPayloadHash == "" {
			return nil, fmt.Errorf("sandbox_saga_market_snapshot_unavailable")
		}
		loaded[item.symbol] = item.snapshot
	}
	return loaded, nil
}

func (cache *SandboxSagaMarketCache) normalizeSagaMarketGeneration(
	loaded map[string]exchangecontracts.BookSnapshot,
	health exchangecontracts.ClockHealth,
) (map[string]SandboxSagaMarketMember, error) {
	generation := cache.generation.Add(1)
	next := make(map[string]SandboxSagaMarketMember, len(loaded))
	symbols := make([]string, 0, len(loaded))
	for symbol := range loaded {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)
	for index, symbol := range symbols {
		member, err := cache.normalizeSagaMarketMember(symbol, index, len(symbols), generation,
			loaded[symbol], health)
		if err != nil {
			return nil, err
		}
		next[symbol] = member
	}
	return next, nil
}

func (cache *SandboxSagaMarketCache) normalizeSagaMarketMember(symbol string, index, total int,
	generation uint64, snapshot exchangecontracts.BookSnapshot,
	health exchangecontracts.ClockHealth,
) (SandboxSagaMarketMember, error) {
	receivedOffset := positiveSagaCacheOffset(cache.monotonic())
	processed, processedOffset := cache.clock.Now(), positiveSagaCacheOffset(cache.monotonic())
	published, publishedOffset := cache.clock.Now(), positiveSagaCacheOffset(cache.monotonic())
	if processed.UTC.Before(snapshot.ReceivedAt.UTC) || published.UTC.Before(processed.UTC) ||
		processedOffset < receivedOffset || publishedOffset < processedOffset {
		return SandboxSagaMarketMember{}, fmt.Errorf("sandbox_saga_market_observation_invalid")
	}
	baseSequence := generation*uint64(total*3) + uint64(index*3)
	observation := marketdata.Observation{
		ReceivedAt:           domain.EventTime{UTC: snapshot.ReceivedAt.UTC, Sequence: baseSequence + 1},
		ProcessedAt:          domain.EventTime{UTC: processed.UTC, Sequence: baseSequence + 2},
		PublishedAt:          domain.EventTime{UTC: published.UTC, Sequence: baseSequence + 3},
		ConnectionID:         fmt.Sprintf("sandbox-saga-%s-%d", cache.exchange, generation),
		ConnectionGeneration: generation, SourceSequence: snapshot.LastSequence,
		IngestOrdinal:       generation*uint64(total) + uint64(index+1),
		ReceivedOffsetNanos: receivedOffset, ProcessedOffsetNanos: processedOffset,
		PublishedOffsetNanos: publishedOffset,
	}
	book, err := marketdata.NewBook(cache.exchange, snapshot.Instrument, 50, 50, nil)
	if err != nil || book.BeginGeneration(observation.ConnectionID, generation) != nil ||
		book.ReplaceSnapshot(snapshot, observation) != nil {
		return SandboxSagaMarketMember{}, fmt.Errorf("sandbox_saga_market_normalization_failed")
	}
	view := book.View()
	instance, region := "sandbox-saga-cache-"+cache.exchange, "engine-local"
	if _, err = marketdata.CoherentInput(view, health, instance, region); err != nil {
		return SandboxSagaMarketMember{}, fmt.Errorf("sandbox_saga_market_observation_invalid")
	}
	return SandboxSagaMarketMember{View: view, Clock: health, Rules: cache.rules[symbol],
		CollectorInstance: instance, CollectorRegion: region}, nil
}

// Run refreshes the complete cache until the engine context is cancelled.
// Refresh failures do not publish partial state; callers observe a semantic
// stale/unavailable wait once the last complete generation ages out.
func (cache *SandboxSagaMarketCache) Run(ctx context.Context, interval time.Duration) {
	if cache == nil || ctx == nil || interval < 100*time.Millisecond {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = cache.Refresh(ctx)
		}
	}
}

// CaptureSandboxSagaMarketViews returns one complete cached generation.
func (cache *SandboxSagaMarketCache) CaptureSandboxSagaMarketViews(
	_ context.Context,
	keys []runtimecore.MarketKey,
	now time.Time,
) (SandboxSagaMarketViewSet, error) {
	if cache == nil || now.IsZero() || now.Location() != time.UTC || len(keys) == 0 || len(keys) > len(cache.rules) {
		return SandboxSagaMarketViewSet{}, fmt.Errorf("sandbox_saga_market_cache_invalid")
	}
	cache.mutex.RLock()
	defer cache.mutex.RUnlock()
	trigger := positiveSagaCacheOffset(cache.monotonic())
	set := SandboxSagaMarketViewSet{Trigger: runtimecore.AsOfTrigger{
		MonotonicNanos: trigger, IngestOrdinal: trigger, UTC: now,
	}, Members: make([]SandboxSagaMarketMember, 0, len(keys))}
	for _, key := range keys {
		member, exists := cache.members[key.Instrument.Symbol()]
		if !exists || key.Exchange != cache.exchange || member.View.Exchange() != key.Exchange ||
			member.View.Instrument() != key.Instrument || member.View.Observation().PublishedAt.UTC.After(now) {
			return SandboxSagaMarketViewSet{}, fmt.Errorf("sandbox_saga_market_cache_unavailable")
		}
		published := member.View.Observation().PublishedOffsetNanos
		if published > set.FirstDetectedOffset {
			set.FirstDetectedOffset = published
		}
		set.Members = append(set.Members, member)
	}
	if set.FirstDetectedOffset == 0 || set.FirstDetectedOffset > trigger {
		return SandboxSagaMarketViewSet{}, fmt.Errorf("sandbox_saga_market_cache_unavailable")
	}
	return set, nil
}

func positiveSagaCacheOffset(value time.Duration) uint64 {
	if value <= 0 {
		return 1
	}
	return uint64(value)
}

var _ SandboxSagaMarketViewSource = (*SandboxSagaMarketCache)(nil)
