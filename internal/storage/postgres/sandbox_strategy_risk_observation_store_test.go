package postgres

import (
	"strings"
	"testing"
)

func TestStrategyRiskObservationQueriesBindLeaseAndEveryImmutableInput(t *testing.T) {
	for _, required := range []string{
		"strategy_session.state='running'", "parent.state='ARMED'",
		"strategy_session.revision=$5", "arm.revision=$46",
		"configuration.configuration_hash=$43", "arm.expires_at>$38",
	} {
		if !strings.Contains(insertSandboxStrategyRiskObservationSQL, required) {
			t.Fatalf("risk observation insert omits %q", required)
		}
	}
	for _, required := range []string{
		"observation.strategy_revision=$4", "observation.snapshot_hash=$14",
		"observation.market_hash=$15", "observation.policy_id=$16",
		"observation.policy_version=$17", "observation.policy_hash=$18",
		"arm.revoked_at IS NULL", "observation.observed_at>$13-interval '250 milliseconds'",
	} {
		if !strings.Contains(sandboxStrategyRiskObservationSQL, required) {
			t.Fatalf("risk observation read omits %q", required)
		}
	}
}

func TestParseSandboxStrategyRiskPercentagesRejectsMalformedValue(t *testing.T) {
	values := [11]string{"0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "not-a-number"}
	if _, err := parseSandboxStrategyRiskPercentages(values); err == nil {
		t.Fatal("malformed durable risk percentage accepted")
	}
}
