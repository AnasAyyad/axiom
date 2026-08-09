package rebalancing

import (
	"testing"
	"time"

	platformconfig "axiom/internal/config"
)

func TestConfigurationFromReviewedMapsExactInventoryRebalancingContract(t *testing.T) {
	reviewed := platformconfig.DefaultMultiStrategyConfiguration().Rebalancing
	configuration, err := ConfigurationFromReviewed(reviewed)
	if err != nil {
		t.Fatal(err)
	}
	wanted := DefaultConfiguration()
	if !validConfiguration(configuration) ||
		configuration.MaximumHops != 6 || configuration.MaximumCandidates != 1024 ||
		configuration.MinimumConfidence.String() != "0.8" ||
		configuration.MaximumTotalCost.String() != "25" ||
		configuration.MaximumDuration != 7*24*time.Hour ||
		configuration.MaximumRiskScore.String() != "1" ||
		configuration.MinimumChecklistSteps != 4 {
		t.Fatalf("mapped configuration = %#v, want %#v", configuration, wanted)
	}
	configuration.ApprovedAssets[0] = "MUTATED"
	if reviewed.ApprovedAssets[0] == "MUTATED" {
		t.Fatal("reviewed configuration shares asset slice")
	}
}

func TestConfigurationFromReviewedRejectsAlteredContract(t *testing.T) {
	reviewed := platformconfig.DefaultMultiStrategyConfiguration().Rebalancing
	reviewed.Parameters[0].Value = "5"
	if _, err := ConfigurationFromReviewed(reviewed); errorCode(err) != "reviewed_configuration_invalid" {
		t.Fatalf("altered reviewed contract accepted: %v", err)
	}
}
