package bootstrap

import (
	"testing"
	"time"
)

func TestOwnerConsoleNextPendingFinalizedCandleIncludesDelayAndEvaluationState(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 1, 0, time.UTC)
	lastEvaluated := time.Date(2026, 8, 9, 8, 0, 0, 0, time.UTC)
	if got := ownerConsoleNextPendingFinalizedCandle(now, 4*time.Hour, 2*time.Second, lastEvaluated); !got.Equal(
		time.Date(2026, 8, 9, 12, 0, 2, 0, time.UTC),
	) {
		t.Fatalf("current close eligibility=%s", got)
	}
	now = now.Add(2 * time.Second)
	if got := ownerConsoleNextPendingFinalizedCandle(now, 4*time.Hour, 2*time.Second, lastEvaluated); !got.Equal(
		time.Date(2026, 8, 9, 12, 0, 2, 0, time.UTC),
	) {
		t.Fatalf("overdue close eligibility=%s", got)
	}
	lastEvaluated = time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	if got := ownerConsoleNextPendingFinalizedCandle(now, 4*time.Hour, 2*time.Second, lastEvaluated); !got.Equal(
		time.Date(2026, 8, 9, 16, 0, 2, 0, time.UTC),
	) {
		t.Fatalf("next close eligibility=%s", got)
	}
}

func TestPublicShadowInputStateKeepsClockAndBookFailureFailClosed(t *testing.T) {
	if state, _, fresh := publicShadowInputState("HEALTHY", false, nil); state != "PAUSED" || fresh {
		t.Fatalf("clock-ineligible state=%s fresh=%t", state, fresh)
	}
	if state, _, fresh := publicShadowInputState("HEALTHY", true, nil); state != "HEALTHY" || !fresh {
		t.Fatalf("eligible state=%s fresh=%t", state, fresh)
	}
}
