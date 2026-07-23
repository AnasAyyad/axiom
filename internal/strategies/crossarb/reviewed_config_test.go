package crossarb

import (
	"testing"

	"axiom/internal/config"
)

func TestReviewedConfigurationMapsExactlyAndFailsClosed(t *testing.T) {
	reviewed := config.DefaultV1BConfiguration().CrossExchange
	configuration, err := ConfigurationFromReviewed(reviewed)
	if err != nil || !validConfiguration(configuration) {
		t.Fatalf("configuration = %#v, %v", configuration, err)
	}
	reviewed.Parameters[9].Value = "0.29"
	if _, err = ConfigurationFromReviewed(reviewed); err == nil {
		t.Fatal("out-of-contract reviewed parameter accepted")
	}
}
