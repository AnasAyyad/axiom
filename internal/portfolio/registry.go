package portfolio

import (
	"sync"

	"axiom/internal/domain"
)

// AssetEligibility is one versioned current registry fact.
type AssetEligibility struct {
	Asset   domain.AssetSymbol
	Status  domain.AssetStatus
	Version uint64
}

// AssetRegistry supplies the authoritative current eligibility fact.
type AssetRegistry interface {
	Current(domain.AssetSymbol) (AssetEligibility, bool)
}

// MemoryAssetRegistry is a concurrency-safe versioned conformance registry.
type MemoryAssetRegistry struct {
	mutex sync.RWMutex
	items map[domain.AssetSymbol]AssetEligibility
}

// NewAssetRegistry constructs the required approved V1A registry at version one.
func NewAssetRegistry() *MemoryAssetRegistry {
	items := make(map[domain.AssetSymbol]AssetEligibility)
	for _, asset := range domain.DefaultAssets() {
		items[asset.Symbol] = AssetEligibility{Asset: asset.Symbol, Status: asset.Status, Version: 1}
	}
	return &MemoryAssetRegistry{items: items}
}

// Current returns the exact current asset fact.
func (registry *MemoryAssetRegistry) Current(asset domain.AssetSymbol) (AssetEligibility, bool) {
	registry.mutex.RLock()
	defer registry.mutex.RUnlock()
	value, exists := registry.items[asset]
	return value, exists
}

// Set appends a strictly newer in-memory registry fact for model tests.
func (registry *MemoryAssetRegistry) Set(asset domain.AssetSymbol, status domain.AssetStatus, version uint64) error {
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	prior, exists := registry.items[asset]
	if !exists || version != prior.Version+1 || !knownStatus(status) {
		return portfolioError("asset_registry_transition_rejected")
	}
	registry.items[asset] = AssetEligibility{Asset: asset, Status: status, Version: version}
	return nil
}

func requireApproved(registry AssetRegistry, asset domain.AssetSymbol, version uint64) error {
	value, exists := registry.Current(asset)
	if !exists || value.Asset != asset || value.Status != domain.AssetApproved || value.Version != version {
		return portfolioError("asset_not_approved")
	}
	return nil
}

func knownStatus(status domain.AssetStatus) bool {
	return status == domain.AssetApproved || status == domain.AssetScanOnly || status == domain.AssetBlocked ||
		status == domain.AssetPendingReview
}
