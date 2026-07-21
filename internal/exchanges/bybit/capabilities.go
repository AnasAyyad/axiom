package bybit

import (
	"sort"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
)

const capabilityVersion = "bybit-public-v1b-1"

// Capabilities returns the explicit credential-free Bybit B1 descriptor.
func Capabilities(observedAt time.Time) (exchangecontracts.Descriptor, error) {
	capabilities := []exchangecontracts.Capability{
		bybitSupported(exchangecontracts.FeaturePublicMarketData),
		bybitSupported(exchangecontracts.FeatureInstrumentMetadata),
		bybitSupported(exchangecontracts.FeatureHistoricalTrades),
		bybitConstrained(exchangecontracts.FeatureHistoricalCandles, "interval", "15m", "1h", "4h"),
		bybitSupported(exchangecontracts.FeatureTickers),
		bybitConstrained(exchangecontracts.FeatureBookSnapshots, "maximum_depth", "1", "50", "200", "1000"),
		bybitConstrained(exchangecontracts.FeatureIncrementalDepth, "cadence", "10ms", "20ms", "100ms"),
		bybitUnsupported(exchangecontracts.FeatureChecksums),
		bybitUnsupported(exchangecontracts.FeaturePrivateData),
		bybitUnsupported(exchangecontracts.FeatureOrders),
		bybitUnsupported(exchangecontracts.FeatureImmediateOrCancel),
		bybitUnsupported(exchangecontracts.FeatureFillOrKill),
		bybitUnsupported(exchangecontracts.FeaturePostOnly),
		bybitUnsupported(exchangecontracts.FeatureCancellation),
		bybitUnsupported(exchangecontracts.FeatureClientGeneratedIDs),
		bybitUnsupported(exchangecontracts.FeatureReconciliation),
	}
	sort.Slice(capabilities, func(left, right int) bool {
		return capabilities[left].Feature < capabilities[right].Feature
	})
	descriptor := exchangecontracts.Descriptor{Exchange: "bybit",
		Environment: exchangecontracts.EnvironmentProductionPublic,
		AccountMode: exchangecontracts.AccountModePublicOnly, Version: capabilityVersion,
		ObservedAt: observedAt, Capabilities: capabilities}
	if err := descriptor.Validate(); err != nil {
		return exchangecontracts.Descriptor{}, err
	}
	return descriptor, nil
}

func bybitSupported(feature exchangecontracts.Feature) exchangecontracts.Capability {
	return exchangecontracts.Capability{Feature: feature, Support: exchangecontracts.Supported}
}

func bybitConstrained(
	feature exchangecontracts.Feature,
	name string,
	values ...string,
) exchangecontracts.Capability {
	sort.Strings(values)
	return exchangecontracts.Capability{Feature: feature, Support: exchangecontracts.Supported,
		Constraints: []exchangecontracts.Constraint{{Name: name, Values: values}}}
}

func bybitUnsupported(feature exchangecontracts.Feature) exchangecontracts.Capability {
	return exchangecontracts.Capability{Feature: feature, Support: exchangecontracts.Unsupported}
}
