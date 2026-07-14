package domain

import "regexp"

var assetSymbolPattern = regexp.MustCompile(`^[A-Z0-9]{2,12}$`)

// AssetSymbol is a canonical upper-case asset symbol.
type AssetSymbol string

// ParseAssetSymbol validates a canonical asset symbol.
func ParseAssetSymbol(value string) (AssetSymbol, error) {
	if !assetSymbolPattern.MatchString(value) {
		return "", domainError(CodeInvalidInstrument, "asset_symbol")
	}
	return AssetSymbol(value), nil
}

// AssetStatus is the explicit registry disposition for an asset.
type AssetStatus string

// Supported asset registry statuses.
const (
	AssetApproved      AssetStatus = "approved"
	AssetScanOnly      AssetStatus = "scan_only"
	AssetBlocked       AssetStatus = "blocked"
	AssetPendingReview AssetStatus = "pending_review"
)

// Asset describes one versioned canonical registry entry.
type Asset struct {
	Symbol AssetSymbol `json:"symbol"`
	Status AssetStatus `json:"status"`
}

// DefaultAssets returns the only approved assets in the initial V1A registry.
func DefaultAssets() []Asset {
	return []Asset{
		{Symbol: "USDT", Status: AssetApproved},
		{Symbol: "BTC", Status: AssetApproved},
		{Symbol: "ETH", Status: AssetApproved},
	}
}

// ValidateAssetRegistry rejects duplicates, unknown states, and unsafe defaults.
func ValidateAssetRegistry(assets []Asset) error {
	seen := make(map[AssetSymbol]struct{}, len(assets))
	for _, asset := range assets {
		if _, err := ParseAssetSymbol(string(asset.Symbol)); err != nil || !validAssetStatus(asset.Status) {
			return domainError(CodeInvalidInstrument, "asset_registry")
		}
		if _, duplicate := seen[asset.Symbol]; duplicate {
			return domainError(CodeInvalidInstrument, "asset_registry")
		}
		seen[asset.Symbol] = struct{}{}
	}
	return nil
}

func validAssetStatus(status AssetStatus) bool {
	switch status {
	case AssetApproved, AssetScanOnly, AssetBlocked, AssetPendingReview:
		return true
	default:
		return false
	}
}
