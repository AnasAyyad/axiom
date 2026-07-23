package crossarb

import (
	"time"

	"axiom/internal/domain"
)

// Configuration is the immutable exact B5 strategy contract.
type Configuration struct {
	StrategyVersion                 string
	ModelVersion                    string
	ApprovedInstruments             []string
	MaximumBookAge                  time.Duration
	MaximumInterBookSkew            time.Duration
	MaximumClockUncertainty         time.Duration
	CandidateLifetime               time.Duration
	MaximumNotional                 domain.Balance
	ReducedDirectionMaximumNotional domain.Balance
	MinimumClosedCycleEdge          domain.Percent
	LowerBand                       domain.Percent
	TargetBand                      domain.Percent
	UpperBand                       domain.Percent
	MaximumRecoveryAttempts         uint32
	DispatchMode                    string
	ClaimModel                      string
}

// DefaultConfiguration returns the reviewed B5 baseline.
func DefaultConfiguration() Configuration {
	return Configuration{
		StrategyVersion:                 "cross-exchange.v1b.1",
		ModelVersion:                    "cross-exchange-closed-cycle.v1",
		ApprovedInstruments:             []string{"BTCUSDT", "ETHUSDT"},
		MaximumBookAge:                  250 * time.Millisecond,
		MaximumInterBookSkew:            250 * time.Millisecond,
		MaximumClockUncertainty:         100 * time.Millisecond,
		CandidateLifetime:               250 * time.Millisecond,
		MaximumNotional:                 balance("100"),
		ReducedDirectionMaximumNotional: balance("50"),
		MinimumClosedCycleEdge:          percent("0"),
		LowerBand:                       percent("0.30"), TargetBand: percent("0.50"), UpperBand: percent("0.70"),
		MaximumRecoveryAttempts: 1,
		DispatchMode:            "concurrent",
		ClaimModel:              "atomic-multi-resource.v1",
	}
}

func validConfiguration(configuration Configuration) bool {
	return configuration.StrategyVersion == "cross-exchange.v1b.1" &&
		configuration.ModelVersion == "cross-exchange-closed-cycle.v1" &&
		len(configuration.ApprovedInstruments) == 2 &&
		configuration.ApprovedInstruments[0] == "BTCUSDT" &&
		configuration.ApprovedInstruments[1] == "ETHUSDT" &&
		configuration.MaximumBookAge == 250*time.Millisecond &&
		configuration.MaximumInterBookSkew == 250*time.Millisecond &&
		configuration.MaximumClockUncertainty == 100*time.Millisecond &&
		configuration.CandidateLifetime == 250*time.Millisecond &&
		configuration.MaximumNotional.String() == "100" &&
		configuration.ReducedDirectionMaximumNotional.String() == "50" &&
		configuration.MinimumClosedCycleEdge.String() == "0" &&
		configuration.LowerBand.String() == "0.3" &&
		configuration.TargetBand.String() == "0.5" &&
		configuration.UpperBand.String() == "0.7" &&
		configuration.MaximumRecoveryAttempts == 1 &&
		configuration.DispatchMode == "concurrent" &&
		configuration.ClaimModel == "atomic-multi-resource.v1"
}

func balance(value string) domain.Balance {
	result, err := domain.ParseBalance(value)
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
