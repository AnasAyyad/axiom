package bootstrap

import (
	"context"
	"fmt"
	"sync"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
	runtimecore "axiom/internal/runtime"
)

// SandboxSagaMarketCacheSet joins one or two credential-free venue caches and
// publishes a new capture generation only after every venue refreshed in the
// same cycle. It prevents a successful Binance refresh from replacing the
// last complete cross-exchange generation when Bybit failed, and vice versa.
type SandboxSagaMarketCacheSet struct {
	caches    []*SandboxSagaMarketCache
	monotonic exchangecontracts.MonotonicSource

	mutex      sync.RWMutex
	configured map[string]struct{}
	members    map[string]SandboxSagaMarketMember
}

// NewSandboxSagaMarketCacheSet combines credential-isolated exchange caches.
func NewSandboxSagaMarketCacheSet(
	caches []*SandboxSagaMarketCache,
	monotonic exchangecontracts.MonotonicSource,
) (*SandboxSagaMarketCacheSet, error) {
	if len(caches) == 0 || len(caches) > 2 || monotonic == nil {
		return nil, fmt.Errorf("sandbox_saga_market_cache_set_invalid")
	}
	configured := make(map[string]struct{}, 6)
	exchanges := make(map[string]struct{}, len(caches))
	for _, cache := range caches {
		if cache == nil || cache.exchange == "" || len(cache.rules) == 0 {
			return nil, fmt.Errorf("sandbox_saga_market_cache_set_invalid")
		}
		if _, duplicate := exchanges[cache.exchange]; duplicate {
			return nil, fmt.Errorf("sandbox_saga_market_cache_set_invalid")
		}
		exchanges[cache.exchange] = struct{}{}
		for symbol := range cache.rules {
			identity := sandboxSagaCacheMemberIdentity(cache.exchange, symbol)
			if _, duplicate := configured[identity]; duplicate {
				return nil, fmt.Errorf("sandbox_saga_market_cache_set_invalid")
			}
			configured[identity] = struct{}{}
		}
	}
	return &SandboxSagaMarketCacheSet{caches: append([]*SandboxSagaMarketCache(nil), caches...),
		monotonic: monotonic, configured: configured,
		members: make(map[string]SandboxSagaMarketMember, len(configured))}, nil
}

// Refresh publishes only a complete all-venue cycle. Individual cache state
// may advance internally, but it is invisible to captures until every member
// in this set succeeded and was copied into one atomic immutable map.
func (set *SandboxSagaMarketCacheSet) Refresh(ctx context.Context) error {
	if set == nil || ctx == nil || len(set.caches) == 0 {
		return fmt.Errorf("sandbox_saga_market_cache_set_invalid")
	}
	results := make(chan error, len(set.caches))
	for _, cache := range set.caches {
		cache := cache
		go func() { results <- cache.Refresh(ctx) }()
	}
	for range set.caches {
		if err := <-results; err != nil {
			return fmt.Errorf("sandbox_saga_market_cache_set_unavailable")
		}
	}
	next := make(map[string]SandboxSagaMarketMember, len(set.configured))
	for _, cache := range set.caches {
		cache.mutex.RLock()
		if len(cache.members) != len(cache.rules) {
			cache.mutex.RUnlock()
			return fmt.Errorf("sandbox_saga_market_cache_set_unavailable")
		}
		for symbol, member := range cache.members {
			identity := sandboxSagaCacheMemberIdentity(cache.exchange, symbol)
			if _, expected := set.configured[identity]; !expected ||
				member.View.Exchange() != cache.exchange || member.View.Instrument().Symbol() != symbol {
				cache.mutex.RUnlock()
				return fmt.Errorf("sandbox_saga_market_cache_set_unavailable")
			}
			next[identity] = member
		}
		cache.mutex.RUnlock()
	}
	if len(next) != len(set.configured) {
		return fmt.Errorf("sandbox_saga_market_cache_set_unavailable")
	}
	set.mutex.Lock()
	set.members = next
	set.mutex.Unlock()
	return nil
}

// CaptureSandboxSagaMarketViews returns one complete cross-cache generation.
func (set *SandboxSagaMarketCacheSet) CaptureSandboxSagaMarketViews(
	ctx context.Context,
	keys []runtimecore.MarketKey,
	now time.Time,
) (SandboxSagaMarketViewSet, error) {
	if set == nil || ctx == nil || now.IsZero() || now.Location() != time.UTC ||
		len(keys) == 0 || len(keys) > len(set.configured) {
		return SandboxSagaMarketViewSet{}, fmt.Errorf("sandbox_saga_market_cache_set_invalid")
	}
	trigger := positiveSagaCacheOffset(set.monotonic())
	result := SandboxSagaMarketViewSet{Trigger: runtimecore.AsOfTrigger{
		MonotonicNanos: trigger, IngestOrdinal: trigger, UTC: now,
	}, Members: make([]SandboxSagaMarketMember, 0, len(keys))}
	seen := make(map[string]struct{}, len(keys))
	set.mutex.RLock()
	defer set.mutex.RUnlock()
	for _, key := range keys {
		identity := sandboxSagaCacheMemberIdentity(key.Exchange, key.Instrument.Symbol())
		if _, duplicate := seen[identity]; duplicate {
			return SandboxSagaMarketViewSet{}, fmt.Errorf("sandbox_saga_market_cache_set_unavailable")
		}
		member, exists := set.members[identity]
		if !exists || member.View.Exchange() != key.Exchange ||
			member.View.Instrument() != key.Instrument || member.View.Observation().PublishedAt.UTC.After(now) {
			return SandboxSagaMarketViewSet{}, fmt.Errorf("sandbox_saga_market_cache_set_unavailable")
		}
		seen[identity] = struct{}{}
		published := member.View.Observation().PublishedOffsetNanos
		if published > result.FirstDetectedOffset {
			result.FirstDetectedOffset = published
		}
		result.Members = append(result.Members, member)
	}
	if result.FirstDetectedOffset == 0 || result.FirstDetectedOffset > trigger {
		return SandboxSagaMarketViewSet{}, fmt.Errorf("sandbox_saga_market_cache_set_unavailable")
	}
	return result, nil
}

func sandboxSagaCacheMemberIdentity(exchange, symbol string) string {
	return exchange + "\x00" + symbol
}

var _ SandboxSagaMarketViewSource = (*SandboxSagaMarketCacheSet)(nil)
var _ sandboxSagaMarketRefresher = (*SandboxSagaMarketCacheSet)(nil)
