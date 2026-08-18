package evaluation

import (
	"context"
	"testing"
	"time"
)

type driverStub struct {
	progress StageProgress
	report   Report
	mode     string
}

func (driver *driverStub) HistoricalImport(context.Context, Campaign) (StageProgress, error) {
	return driver.progress, nil
}
func (driver *driverStub) ExistingDataAudit(context.Context, Campaign) (StageProgress, error) {
	return driver.progress, nil
}
func (driver *driverStub) RotateRecorder(context.Context, Campaign) (StageProgress, error) {
	return driver.progress, nil
}
func (driver *driverStub) QualifyRecorder(context.Context, Campaign) (StageProgress, error) {
	return driver.progress, nil
}
func (driver *driverStub) OfflineMatrix(_ context.Context, _ Campaign, mode string) (StageProgress, error) {
	driver.mode = mode
	return driver.progress, nil
}
func (driver *driverStub) SelectCandidates(context.Context, Campaign) (StageProgress, error) {
	return driver.progress, nil
}
func (driver *driverStub) CombinedShadow(context.Context, Campaign) (StageProgress, error) {
	return driver.progress, nil
}
func (driver *driverStub) BuildReport(context.Context, Campaign, bool) (Report, error) {
	return driver.report, nil
}

func TestOrchestratorStartsAndAdvancesOneCheckpoint(t *testing.T) {
	driver := &driverStub{progress: StageProgress{State: ProgressComplete, Summary: "done"}}
	orchestrator, err := NewOrchestrator(driver)
	if err != nil {
		t.Fatal(err)
	}
	pending := Claim{Campaign: Campaign{ID: "campaign", Preset: BalancedFullV1, State: StatePending}, ClaimEpoch: 1}
	outcome, err := orchestrator.Execute(context.Background(), pending)
	if err != nil || outcome.Kind != OutcomeStarted {
		t.Fatalf("outcome=%#v err=%v", outcome, err)
	}
	running := pending
	running.Campaign.State, running.Campaign.CurrentStage = StateRunning, StageReplayMatrix
	outcome, err = orchestrator.Execute(context.Background(), running)
	if err != nil || outcome.Kind != OutcomeCompleted || driver.mode != "replay" {
		t.Fatalf("outcome=%#v mode=%s err=%v", outcome, driver.mode, err)
	}
}

func TestOrchestratorCreatesPartialAndFinalReports(t *testing.T) {
	partial, _ := NewReport("partial", VerdictBlocked, ReasonCanceled, "Canceled.", map[string]string{"state": "partial"}, time.Now())
	driver := &driverStub{report: partial}
	orchestrator, _ := NewOrchestrator(driver)
	claim := Claim{Campaign: Campaign{ID: "campaign", Preset: BalancedFullV1, State: StateCanceled}, ClaimEpoch: 1}
	outcome, err := orchestrator.Execute(context.Background(), claim)
	if err != nil || outcome.Kind != OutcomePartialReported || outcome.Report == nil {
		t.Fatalf("outcome=%#v err=%v", outcome, err)
	}
	final, _ := NewReport("final", VerdictContinue, "", "Complete.", map[string]string{"state": "final"}, time.Now())
	driver.report, driver.progress = final, StageProgress{State: ProgressComplete}
	claim.Campaign.State, claim.Campaign.CurrentStage = StateRunning, StageFinalReport
	outcome, err = orchestrator.Execute(context.Background(), claim)
	if err != nil || outcome.Kind != OutcomeCompleted || outcome.Report == nil {
		t.Fatalf("outcome=%#v err=%v", outcome, err)
	}
}

func TestOrchestratorKeepsRecoverablePauseUntilDependencyRetry(t *testing.T) {
	driver := &driverStub{progress: StageProgress{State: ProgressPause,
		Reason: ReasonDataUnavailable, Summary: "retry pending"}}
	orchestrator, _ := NewOrchestrator(driver)
	claim := Claim{Campaign: Campaign{ID: "campaign", Preset: BalancedFullV1,
		State: StatePausedRecoverable, CurrentStage: StageHistoricalImport, Revision: 2}, ClaimEpoch: 1}
	outcome, err := orchestrator.Execute(context.Background(), claim)
	if err != nil || outcome.Kind != OutcomeRetryDeferred || outcome.Reason != ReasonDataUnavailable {
		t.Fatalf("outcome=%#v err=%v", outcome, err)
	}
}
