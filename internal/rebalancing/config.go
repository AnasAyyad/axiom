package rebalancing

import (
	"time"

	"axiom/internal/domain"
)

// Configuration is the immutable inventory rebalancing advisory optimizer contract.
type Configuration struct {
	OptimizerVersion      string
	FactSchemaVersion     string
	CostModelVersion      string
	Mode                  string
	NaturalReversalPolicy string
	ApprovedAssets        []string
	Exchanges             []string
	MaximumHops           uint32
	MaximumCandidates     uint32
	MinimumConfidence     domain.Percent
	MaximumTotalCost      domain.Money
	MaximumDuration       time.Duration
	MaximumRiskScore      domain.Percent
	MinimumChecklistSteps uint32
}

// DefaultConfiguration returns the reviewed inventory rebalancing baseline.
func DefaultConfiguration() Configuration {
	return Configuration{
		OptimizerVersion:      "inventory-rebalancing@1.0.0",
		FactSchemaVersion:     "rebalancing-fact.v1",
		CostModelVersion:      "rebalancing-cost.v1",
		Mode:                  "advisory_only",
		NaturalReversalPolicy: "prefer_eligible_before_transfer",
		ApprovedAssets:        []string{"BTC", "ETH", "USDT"},
		Exchanges:             []string{"binance", "bybit"},
		MaximumHops:           6,
		MaximumCandidates:     1024,
		MinimumConfidence:     mustPercent("0.80"),
		MaximumTotalCost:      mustMoney("25"),
		MaximumDuration:       7 * 24 * time.Hour,
		MaximumRiskScore:      mustPercent("1"),
		MinimumChecklistSteps: 4,
	}
}

func validConfiguration(configuration Configuration) bool {
	wanted := DefaultConfiguration()
	maximumConfidence := mustPercent("1")
	return configuration.OptimizerVersion == wanted.OptimizerVersion &&
		configuration.FactSchemaVersion == wanted.FactSchemaVersion &&
		configuration.CostModelVersion == wanted.CostModelVersion &&
		configuration.Mode == wanted.Mode &&
		configuration.NaturalReversalPolicy == wanted.NaturalReversalPolicy &&
		equalStrings(configuration.ApprovedAssets, wanted.ApprovedAssets) &&
		equalStrings(configuration.Exchanges, wanted.Exchanges) &&
		configuration.MaximumHops == wanted.MaximumHops &&
		configuration.MaximumCandidates == wanted.MaximumCandidates &&
		configuration.MinimumConfidence.Compare(wanted.MinimumConfidence) >= 0 &&
		configuration.MinimumConfidence.Compare(maximumConfidence) <= 0 &&
		configuration.MaximumTotalCost.Compare(wanted.MaximumTotalCost) == 0 &&
		configuration.MaximumDuration == wanted.MaximumDuration &&
		configuration.MaximumRiskScore.Compare(wanted.MaximumRiskScore) == 0 &&
		configuration.MinimumChecklistSteps == wanted.MinimumChecklistSteps
}

func mustMoney(value string) domain.Money {
	result, err := domain.ParseMoney(value)
	if err != nil {
		panic(err)
	}
	return result
}

func mustPercent(value string) domain.Percent {
	result, err := domain.ParsePercent(value)
	if err != nil {
		panic(err)
	}
	return result
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
