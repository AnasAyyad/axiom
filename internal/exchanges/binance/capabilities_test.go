package binance

import (
	"errors"
	"testing"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
)

func TestV1ACapabilitiesArePublicOnly(t *testing.T) {
	t.Parallel()
	descriptor, err := Capabilities(time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	for _, feature := range []exchangecontracts.Feature{
		exchangecontracts.FeaturePublicMarketData,
		exchangecontracts.FeatureInstrumentMetadata,
		exchangecontracts.FeatureHistoricalTrades,
		exchangecontracts.FeatureHistoricalCandles,
		exchangecontracts.FeatureBookSnapshots,
		exchangecontracts.FeatureIncrementalDepth,
	} {
		if err := descriptor.Require(feature); err != nil {
			t.Fatalf("expected %s support: %v", feature, err)
		}
	}
	for _, feature := range []exchangecontracts.Feature{
		exchangecontracts.FeatureChecksums,
		exchangecontracts.FeaturePrivateData,
		exchangecontracts.FeatureOrders,
		exchangecontracts.FeatureImmediateOrCancel,
		exchangecontracts.FeatureFillOrKill,
		exchangecontracts.FeaturePostOnly,
		exchangecontracts.FeatureCancellation,
		exchangecontracts.FeatureClientGeneratedIDs,
		exchangecontracts.FeatureReconciliation,
	} {
		var failure *exchangecontracts.Error
		if err := descriptor.Require(feature); !errors.As(err, &failure) || failure.Kind != exchangecontracts.ErrorCapability {
			t.Fatalf("expected typed unsupported result for %s, got %v", feature, err)
		}
	}
}
