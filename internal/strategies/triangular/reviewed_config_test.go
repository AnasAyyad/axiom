package triangular

import (
	"testing"

	platformconfig "axiom/internal/config"
)

func TestReviewedConfigurationMapsExactlyToRuntimeContract(t *testing.T) {
	platform := platformconfig.DefaultV1BConfiguration()
	runtime, err := ConfigurationFromReviewed(platform.Triangular)
	if err != nil {
		t.Fatal(err)
	}
	expected := DefaultConfiguration()
	if runtime.StrategyVersion != expected.StrategyVersion ||
		runtime.ModelVersion != expected.ModelVersion ||
		runtime.MaximumCycleNotional != expected.MaximumCycleNotional ||
		runtime.MaximumBookAge != expected.MaximumBookAge ||
		runtime.CandidateLifetime != expected.CandidateLifetime ||
		runtime.AdditionalSafetyMargin != expected.AdditionalSafetyMargin ||
		runtime.LatencyDeterioration != expected.LatencyDeterioration ||
		runtime.MaximumRecoveryAttempts != 1 ||
		runtime.OpportunityMetricWindow != 1000 ||
		!runtime.DynamicSizing || len(runtime.SizeLadder) != 4 {
		t.Fatalf("runtime graph drifted: %#v", runtime)
	}
}

func TestReviewedConfigurationFailsClosedOnContractMutation(t *testing.T) {
	platform := platformconfig.DefaultV1BConfiguration()
	platform.Triangular.Parameters[0].Value = "2"
	if _, err := ConfigurationFromReviewed(platform.Triangular); err == nil {
		t.Fatal("mutated reviewed contract was accepted")
	}
}
