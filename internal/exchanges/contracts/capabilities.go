package exchangecontracts

import (
	"sort"
	"time"
)

// ExchangeID identifies an exchange without leaking a native DTO.
type ExchangeID string

// Environment identifies the exchange environment represented by a descriptor.
type Environment string

// AccountMode identifies whether a capability set has any account scope.
type AccountMode string

// V1A environment and account-mode values.
const (
	EnvironmentProductionPublic Environment = "production_public"
	AccountModePublicOnly       AccountMode = "public_only"
)

// Feature is one versioned exchange capability.
type Feature string

// Capability features are descriptive. Unsupported private features never
// create callable methods on the public client.
const (
	FeaturePublicMarketData   Feature = "public_market_data"
	FeatureInstrumentMetadata Feature = "instrument_metadata"
	FeatureHistoricalTrades   Feature = "historical_trades"
	FeatureHistoricalCandles  Feature = "historical_candles"
	FeatureBookSnapshots      Feature = "book_snapshots"
	FeatureIncrementalDepth   Feature = "incremental_depth"
	FeatureChecksums          Feature = "checksums"
	FeaturePrivateData        Feature = "private_data"
	FeatureOrders             Feature = "orders"
	FeatureImmediateOrCancel  Feature = "immediate_or_cancel"
	FeatureFillOrKill         Feature = "fill_or_kill"
	FeaturePostOnly           Feature = "post_only"
	FeatureCancellation       Feature = "cancellation"
	FeatureClientGeneratedIDs Feature = "client_generated_ids"
	FeatureReconciliation     Feature = "reconciliation"
)

// Support is an explicit capability disposition.
type Support string

// Capability dispositions are closed and fail closed on unknown values.
const (
	Supported   Support = "supported"
	Unsupported Support = "unsupported"
)

// Constraint is one stable capability restriction.
type Constraint struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
}

// Capability describes support and any constrained values for one feature.
type Capability struct {
	Feature     Feature      `json:"feature"`
	Support     Support      `json:"support"`
	Constraints []Constraint `json:"constraints"`
}

// Descriptor is an immutable environment/version-aware capability record.
type Descriptor struct {
	Exchange     ExchangeID   `json:"exchange"`
	Environment  Environment  `json:"environment"`
	AccountMode  AccountMode  `json:"account_mode"`
	Version      string       `json:"version"`
	ObservedAt   time.Time    `json:"observed_at"`
	Capabilities []Capability `json:"capabilities"`
}

// Validate rejects incomplete, duplicate, unordered, or unsafe descriptors.
func (descriptor Descriptor) Validate() error {
	if descriptor.Exchange == "" || descriptor.Environment != EnvironmentProductionPublic ||
		descriptor.AccountMode != AccountModePublicOnly || descriptor.Version == "" ||
		descriptor.ObservedAt.IsZero() || descriptor.ObservedAt.Location() != time.UTC {
		return NewError(ErrorValidation, OperationCapability, 0)
	}
	previous := Feature("")
	for _, capability := range descriptor.Capabilities {
		if !validFeature(capability.Feature) || capability.Feature <= previous ||
			(capability.Support != Supported && capability.Support != Unsupported) ||
			(capability.Support == Unsupported && len(capability.Constraints) != 0) ||
			!validConstraints(capability.Constraints) {
			return NewError(ErrorValidation, OperationCapability, 0)
		}
		previous = capability.Feature
	}
	return nil
}

// Require returns a typed capability error when feature is not supported.
func (descriptor Descriptor) Require(feature Feature) error {
	if err := descriptor.Validate(); err != nil || !validFeature(feature) {
		return NewError(ErrorValidation, OperationCapability, 0)
	}
	index := sort.Search(len(descriptor.Capabilities), func(index int) bool {
		return descriptor.Capabilities[index].Feature >= feature
	})
	if index == len(descriptor.Capabilities) || descriptor.Capabilities[index].Feature != feature ||
		descriptor.Capabilities[index].Support != Supported {
		return NewError(ErrorCapability, OperationCapability, 0)
	}
	return nil
}

func validConstraints(constraints []Constraint) bool {
	previous := ""
	for _, constraint := range constraints {
		if constraint.Name == "" || constraint.Name <= previous || len(constraint.Values) == 0 ||
			!sort.StringsAreSorted(constraint.Values) {
			return false
		}
		for index, value := range constraint.Values {
			if value == "" || (index > 0 && constraint.Values[index-1] == value) {
				return false
			}
		}
		previous = constraint.Name
	}
	return true
}

func validFeature(feature Feature) bool {
	switch feature {
	case FeaturePublicMarketData, FeatureInstrumentMetadata, FeatureHistoricalTrades,
		FeatureHistoricalCandles, FeatureBookSnapshots, FeatureIncrementalDepth,
		FeatureChecksums, FeaturePrivateData, FeatureOrders, FeatureImmediateOrCancel,
		FeatureFillOrKill, FeaturePostOnly, FeatureCancellation, FeatureClientGeneratedIDs,
		FeatureReconciliation:
		return true
	default:
		return false
	}
}
