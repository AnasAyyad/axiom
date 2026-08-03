package c6

import (
	"context"
	"fmt"
	"slices"
	"time"
)

// RequiredChaosScenarios is the exact closed C6 deterministic fault set.
var RequiredChaosScenarios = []string{
	"websocket_disconnect", "rest_timeout", "database_slowdown",
	"database_failure", "process_kill", "fencing_loss", "duplicate_event",
	"out_of_order_event", "partial_fill", "late_fill", "cancel_fill_race",
	"ambiguous_timeout", "account_reset", "stream_snapshot_recovery",
}

// ChaosHarness executes one named scenario using the supplied deterministic
// seed and returns a bounded evidence fact.
type ChaosHarness interface {
	Exercise(context.Context, string, string) (bool, string, error)
}

// RunDeterministicChaos executes every closed scenario with a caller-supplied
// deterministic seed. The harness must prove fail-closed recovery; this
// function never connects to an exchange.
func RunDeterministicChaos(
	ctx context.Context,
	harness ChaosHarness,
	seed string,
	now time.Time,
) ([]ChaosEvent, error) {
	if harness == nil || seed == "" || now.IsZero() ||
		now.Location() != time.UTC {
		return nil, fmt.Errorf("c6_chaos_rejected")
	}
	seedHash := hashValues(seed)
	events := make([]ChaosEvent, 0, len(RequiredChaosScenarios))
	for index, scenario := range RequiredChaosScenarios {
		passed, fact, err := harness.Exercise(ctx, scenario, seed)
		if err != nil || fact == "" {
			return nil, fmt.Errorf("c6_chaos_harness_failed:%s", scenario)
		}
		outcome := "FAILED"
		if passed {
			outcome = "PASSED"
		}
		events = append(events, ChaosEvent{
			Scenario: scenario, Outcome: outcome,
			DeterministicSeedHash: seedHash,
			EvidenceHash: hashValues(
				scenario, outcome, seedHash, fact,
			),
			OccurredAt: now.Add(time.Duration(index) * time.Nanosecond),
		})
	}
	return events, nil
}

func validateChaos(events []ChaosEvent) bool {
	if ValidateChaosEvidence(events) != nil {
		return false
	}
	for _, event := range events {
		if event.Outcome != "PASSED" {
			return false
		}
	}
	return true
}

// ValidateChaosEvidence verifies the complete deterministic scenario set while
// preserving failed outcomes as auditable evidence.
func ValidateChaosEvidence(events []ChaosEvent) error {
	if len(events) != len(RequiredChaosScenarios) {
		return fmt.Errorf("c6_chaos_set_incomplete")
	}
	seen := make(map[string]bool, len(events))
	for _, event := range events {
		if !slices.Contains(RequiredChaosScenarios, event.Scenario) ||
			!slices.Contains([]string{"PASSED", "FAILED"}, event.Outcome) ||
			!sha256Pattern.MatchString(event.DeterministicSeedHash) ||
			!sha256Pattern.MatchString(event.EvidenceHash) ||
			event.OccurredAt.IsZero() || seen[event.Scenario] {
			return fmt.Errorf("c6_chaos_evidence_invalid")
		}
		seen[event.Scenario] = true
	}
	return nil
}
