package postgres

import (
	"strings"
	"testing"
)

func TestSandboxStrategyAssetsIsClosedToSupportedSpotPairs(t *testing.T) {
	for _, test := range []struct {
		instrument, base, quote string
		ok                      bool
	}{
		{"BTCUSDT", "BTC", "USDT", true},
		{"ETHUSDT", "ETH", "USDT", true},
		{"BTCUSD", "", "", false},
	} {
		base, quote, ok := sandboxStrategyAssets(test.instrument)
		if base != test.base || quote != test.quote || ok != test.ok {
			t.Fatalf("instrument=%s base=%s quote=%s ok=%t", test.instrument, base, quote, ok)
		}
	}
}

func TestStrategyAssetEligibilityQueryRequiresCurrentEqualApprovals(t *testing.T) {
	for _, fragment := range []string{
		"effective_at<=$3", "status='approved'", "count(*) FILTER", "min(version),max(version)",
	} {
		if !strings.Contains(strategyAssetEligibilitySQL, fragment) {
			t.Fatalf("asset eligibility query missing %q", fragment)
		}
	}
}
