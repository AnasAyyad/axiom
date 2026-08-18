package postgres

import (
	"testing"
	"time"
)

func TestEvaluationStageRetryDelayIsBoundedWithoutTerminalAttemptLimit(t *testing.T) {
	t.Parallel()
	want := []time.Duration{15 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute,
		4 * time.Minute, 8 * time.Minute, 10 * time.Minute}
	for index, expected := range want {
		if actual := evaluationStageRetryDelay(index + 1); actual != expected {
			t.Fatalf("attempt=%d delay=%s want=%s", index+1, actual, expected)
		}
	}
	if actual := evaluationStageRetryDelay(10_000); actual != 10*time.Minute {
		t.Fatalf("large retry delay=%s", actual)
	}
	if actual := historicalRetryDelay(10_000); actual != 10*time.Minute {
		t.Fatalf("large historical retry delay=%s", actual)
	}
}

func TestEvaluationCampaignSuggestedActionDistinguishesRecoveryFromTerminalFailure(t *testing.T) {
	t.Parallel()
	paused := evaluationCampaignSuggestedAction("PAUSED_RECOVERABLE", "PERSISTENCE_FAILED")
	terminal := evaluationCampaignSuggestedAction("PARTIAL", "PERSISTENCE_FAILED")
	if paused == terminal || paused != "The same stage will retry automatically. Restore PostgreSQL and storage health; completed stages and the current checkpoint are preserved." {
		t.Fatalf("paused action=%q terminal action=%q", paused, terminal)
	}
}
