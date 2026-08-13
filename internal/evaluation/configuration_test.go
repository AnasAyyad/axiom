package evaluation

import (
	"encoding/json"
	"testing"

	"axiom/internal/config"
)

func TestBalancedRunConfigurationsAreDistinctValidAndDoNotMutateDefault(t *testing.T) {
	base := config.DefaultMultiStrategyConfiguration()
	original := base.Portfolio.StartingCapital.Value
	seen := map[string]struct{}{}
	for _, candidate := range BalancedFullDefinition() {
		value, err := BalancedRunConfiguration(base, candidate.ConfigurationKey, 2_000_000_000)
		if err != nil || value.Portfolio.StartingCapital.Value != "2000" {
			t.Fatalf("configuration %s: %#v %v", candidate.ConfigurationKey, value.Portfolio, err)
		}
		encoded, _ := json.Marshal(value)
		key := string(encoded)
		if _, duplicate := seen[key]; duplicate {
			t.Fatalf("duplicate configuration %s", candidate.ConfigurationKey)
		}
		seen[key] = struct{}{}
	}
	if base.Portfolio.StartingCapital.Value != original || config.DefaultConfiguration().Portfolio.StartingCapital.Value != "500" {
		t.Fatal("normative default was mutated")
	}
}

func TestBalancedCombinedConfigurationIsSeparateTenThousandProfile(t *testing.T) {
	value, err := BalancedCombinedConfiguration(config.DefaultMultiStrategyConfiguration())
	if err != nil || value.Portfolio.StartingCapital.Value != "10000" ||
		config.DefaultConfiguration().Portfolio.StartingCapital.Value != "500" {
		t.Fatalf("combined profile = %#v, %v", value.Portfolio, err)
	}
}
