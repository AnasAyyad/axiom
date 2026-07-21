package binance

import (
	"sort"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
)

const capabilityVersion = "binance-public-v1a-1"

// Capabilities returns the explicit credential-free Binance V1A descriptor.
func Capabilities(observedAt time.Time) (exchangecontracts.Descriptor, error) {
	capabilities := []exchangecontracts.Capability{
		supported(exchangecontracts.FeaturePublicMarketData),
		supported(exchangecontracts.FeatureInstrumentMetadata),
		supported(exchangecontracts.FeatureHistoricalTrades),
		constrained(exchangecontracts.FeatureHistoricalCandles, "interval", "15m", "1h", "4h"),
		supported(exchangecontracts.FeatureTickers),
		constrained(exchangecontracts.FeatureBookSnapshots, "maximum_depth", "100", "1000", "500", "5000"),
		constrained(exchangecontracts.FeatureIncrementalDepth, "cadence", "100ms", "realtime"),
		unsupported(exchangecontracts.FeatureChecksums),
		unsupported(exchangecontracts.FeaturePrivateData),
		unsupported(exchangecontracts.FeatureOrders),
		unsupported(exchangecontracts.FeatureImmediateOrCancel),
		unsupported(exchangecontracts.FeatureFillOrKill),
		unsupported(exchangecontracts.FeaturePostOnly),
		unsupported(exchangecontracts.FeatureCancellation),
		unsupported(exchangecontracts.FeatureClientGeneratedIDs),
		unsupported(exchangecontracts.FeatureReconciliation),
	}
	sort.Slice(capabilities, func(left, right int) bool {
		return capabilities[left].Feature < capabilities[right].Feature
	})
	descriptor := exchangecontracts.Descriptor{
		Exchange:     "binance",
		Environment:  exchangecontracts.EnvironmentProductionPublic,
		AccountMode:  exchangecontracts.AccountModePublicOnly,
		Version:      capabilityVersion,
		ObservedAt:   observedAt,
		Capabilities: capabilities,
	}
	if err := descriptor.Validate(); err != nil {
		return exchangecontracts.Descriptor{}, err
	}
	return descriptor, nil
}

func supported(feature exchangecontracts.Feature) exchangecontracts.Capability {
	return exchangecontracts.Capability{Feature: feature, Support: exchangecontracts.Supported}
}

func constrained(feature exchangecontracts.Feature, name string, values ...string) exchangecontracts.Capability {
	sort.Strings(values)
	return exchangecontracts.Capability{
		Feature:     feature,
		Support:     exchangecontracts.Supported,
		Constraints: []exchangecontracts.Constraint{{Name: name, Values: values}},
	}
}

func unsupported(feature exchangecontracts.Feature) exchangecontracts.Capability {
	return exchangecontracts.Capability{Feature: feature, Support: exchangecontracts.Unsupported}
}
