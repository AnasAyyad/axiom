package triangular

import (
	"time"

	"axiom/internal/domain"
)

// Configuration is the immutable exact B4 strategy/model contract.
type Configuration struct {
	StrategyVersion         string
	ModelVersion            string
	SizeLadder              []domain.Quantity
	MaximumCycleNotional    domain.Quantity
	DynamicSizing           bool
	MaximumBookAge          time.Duration
	CandidateLifetime       time.Duration
	AdditionalSafetyMargin  domain.Percent
	LatencyDeterioration    domain.Rate
	MaximumRecoveryAttempts uint32
	OpportunityMetricWindow uint32
	ClaimModel              string
}

// DefaultConfiguration returns the reviewed B4 baseline.
func DefaultConfiguration() Configuration {
	return Configuration{
		StrategyVersion:         "triangular.v1b.1",
		ModelVersion:            "triangular-exact-depth.v1",
		SizeLadder:              []domain.Quantity{quantity("10"), quantity("25"), quantity("50"), quantity("100")},
		MaximumCycleNotional:    quantity("100"),
		DynamicSizing:           true,
		MaximumBookAge:          250 * time.Millisecond,
		CandidateLifetime:       250 * time.Millisecond,
		AdditionalSafetyMargin:  percent("0.0015"),
		LatencyDeterioration:    rate("0.0005"),
		MaximumRecoveryAttempts: 1,
		OpportunityMetricWindow: 1000,
		ClaimModel:              "atomic-multi-resource.v1",
	}
}

func quantity(value string) domain.Quantity {
	result, err := domain.ParseQuantity(value)
	if err != nil {
		panic(err)
	}
	return result
}

func percent(value string) domain.Percent {
	result, err := domain.ParsePercent(value)
	if err != nil {
		panic(err)
	}
	return result
}

func rate(value string) domain.Rate {
	result, err := domain.ParseRate(value)
	if err != nil {
		panic(err)
	}
	return result
}
