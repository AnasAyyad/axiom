package meanreversion

import (
	"testing"

	"axiom/internal/domain"
)

type plannerCandidateSource struct{}

func (plannerCandidateSource) Candidate(_ domain.DecisionID) (Candidate, bool) {
	return Candidate{}, false
}

func TestPlannerAcceptsClosedSandboxModes(t *testing.T) {
	for _, mode := range []string{"testnet", "demo"} {
		if _, err := NewPlanner(mode, "BTCUSDT", plannerCandidateSource{}); err != nil {
			t.Fatalf("mode %q was rejected: %v", mode, err)
		}
	}
	if _, err := NewPlanner("live", "BTCUSDT", plannerCandidateSource{}); err == nil {
		t.Fatal("live mode was accepted")
	}
}
