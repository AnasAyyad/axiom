package evaluation

import (
	"errors"
	"testing"
	"time"
)

func TestCampaignLifecyclePreservesCheckpointAndEndsOnlyAfterReport(t *testing.T) {
	campaign := Campaign{ID: "campaign-1", Preset: BalancedFullV1, State: StatePending}
	if err := Start(&campaign); err != nil {
		t.Fatal(err)
	}
	if campaign.CurrentStage != StageHistoricalImport {
		t.Fatalf("stage = %s", campaign.CurrentStage)
	}
	if err := PauseRecoverable(&campaign, ReasonDataUnavailable); err != nil {
		t.Fatal(err)
	}
	if err := Resume(&campaign); err != nil {
		t.Fatal(err)
	}
	for campaign.State == StateRunning {
		if err := CompleteStage(&campaign); err != nil {
			t.Fatal(err)
		}
	}
	if campaign.State != StateCompleted || len(campaign.CompletedStages) != len(Stages()) {
		t.Fatalf("campaign = %#v", campaign)
	}
}

func TestDeferredRecoveryPreservesStageAndCompletedEvidence(t *testing.T) {
	campaign := Campaign{ID: "campaign-1", Preset: BalancedFullV1, State: StatePausedRecoverable,
		CurrentStage: StageRecorderQualify, CompletedStages: []Stage{StageHistoricalImport, StageExistingDataAudit},
		ValidRecording: time.Hour, Revision: 4}
	if err := DeferRecovery(&campaign, ReasonPersistenceFailed); err != nil {
		t.Fatal(err)
	}
	if campaign.State != StatePausedRecoverable || campaign.CurrentStage != StageRecorderQualify ||
		len(campaign.CompletedStages) != 2 || campaign.ValidRecording != time.Hour || campaign.Revision != 5 {
		t.Fatalf("campaign=%#v", campaign)
	}
}

func TestCampaignBlockAndCancelNeedPartialReport(t *testing.T) {
	blocked := Campaign{ID: "campaign-1", Preset: BalancedFullV1, State: StatePending}
	if err := Start(&blocked); err != nil {
		t.Fatal(err)
	}
	if err := Block(&blocked, ReasonStorageInsufficient); err != nil {
		t.Fatal(err)
	}
	if err := MarkPartial(&blocked); err != nil {
		t.Fatal(err)
	}
	if blocked.State != StatePartial {
		t.Fatalf("state = %s", blocked.State)
	}
	canceled := Campaign{ID: "campaign-2", Preset: BalancedFullV1, State: StatePending}
	if err := Cancel(&canceled); err != nil {
		t.Fatal(err)
	}
	if canceled.Reason != ReasonCanceled {
		t.Fatalf("reason = %s", canceled.Reason)
	}
	if err := Start(&canceled); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("start error = %v", err)
	}
}

func TestStorageForecastFailsClosed(t *testing.T) {
	forecast := StorageForecast{RecordedBytes: 80 * 1024 * 1024 * 1024, MeasuredBytesPerHour: 100 * 1024 * 1024, ShadowHours: 168, SafetyBufferBytes: 2 * 1024 * 1024 * 1024}
	if !forecast.ShadowAdmissible() {
		t.Fatal("expected admissible storage")
	}
	forecast.RecordedBytes = 190 * 1024 * 1024 * 1024
	if forecast.ShadowAdmissible() {
		t.Fatal("must reject cap overrun")
	}
}
