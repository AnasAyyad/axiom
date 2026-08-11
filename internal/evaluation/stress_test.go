package evaluation

import "testing"

func TestFocusedStressSuiteExercisesEveryRequiredScenario(t *testing.T) {
	first := RunFocusedStressSuite()
	second := RunFocusedStressSuite()
	if len(first) != len(focusedStressScenarios) || len(second) != len(first) {
		t.Fatalf("stress results=%d/%d want=%d", len(first), len(second), len(focusedStressScenarios))
	}
	for index, result := range first {
		if result.Scenario != focusedStressScenarios[index] || !result.Passed ||
			len(result.EvidenceHash) != 64 || result.EvidenceHash != second[index].EvidenceHash {
			t.Fatalf("stress result %d = %#v / %#v", index, result, second[index])
		}
	}
}
