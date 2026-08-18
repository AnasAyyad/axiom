package evaluation

import (
	"context"
	"fmt"
	"time"
)

// ProgressState is a stage driver's safe checkpoint result.
type ProgressState string

// Stage progress states describe one safe orchestration checkpoint outcome.
const (
	ProgressWaiting  ProgressState = "waiting"
	ProgressComplete ProgressState = "complete"
	ProgressPause    ProgressState = "pause"
	ProgressBlock    ProgressState = "block"
)

// StageProgress contains only durable deltas and stable owner-facing context.
type StageProgress struct {
	State               ProgressState
	Reason              ReasonCode
	Summary             string
	RetryAfter          time.Duration
	ValidRecordingDelta time.Duration
	ValidShadowDelta    time.Duration
	Checkpoint          []byte
	LinkedResourceType  string
	LinkedResourceID    string
}

// Driver connects the campaign policy to existing durable jobs, recorder,
// public shadow engine, data catalogue, and report storage. It owns no
// production-private exchange transport.
type Driver interface {
	HistoricalImport(context.Context, Campaign) (StageProgress, error)
	ExistingDataAudit(context.Context, Campaign) (StageProgress, error)
	RotateRecorder(context.Context, Campaign) (StageProgress, error)
	QualifyRecorder(context.Context, Campaign) (StageProgress, error)
	OfflineMatrix(context.Context, Campaign, string) (StageProgress, error)
	SelectCandidates(context.Context, Campaign) (StageProgress, error)
	CombinedShadow(context.Context, Campaign) (StageProgress, error)
	BuildReport(context.Context, Campaign, bool) (Report, error)
}

// Orchestrator maps stage-specific progress to the one durable campaign state
// machine. A driver error is returned to Worker, which pauses the same stage
// for checkpoint-preserving recovery.
type Orchestrator struct{ driver Driver }

// NewOrchestrator constructs a complete automatic stage executor.
func NewOrchestrator(driver Driver) (*Orchestrator, error) {
	if driver == nil {
		return nil, fmt.Errorf("evaluation_driver_missing")
	}
	return &Orchestrator{driver: driver}, nil
}

// Execute advances one checkpoint. It never combines two durable transitions
// in one claim, which makes restart behavior deterministic.
func (orchestrator *Orchestrator) Execute(ctx context.Context, claim Claim) (Outcome, error) {
	campaign := claim.Campaign
	if campaign.State == StatePending {
		return Outcome{Kind: OutcomeStarted, Summary: "Historical import stage started."}, nil
	}
	if campaign.State == StateCanceled || campaign.State == StateBlocked {
		report, err := orchestrator.driver.BuildReport(ctx, campaign, true)
		if err != nil {
			return Outcome{}, err
		}
		return Outcome{Kind: OutcomePartialReported, Summary: "Partial report preserved.", Report: &report}, nil
	}
	progress, err := orchestrator.stage(ctx, campaign)
	if err != nil {
		return Outcome{}, err
	}
	if campaign.State == StatePausedRecoverable && (progress.State == ProgressWaiting || progress.State == ProgressComplete) {
		return Outcome{Kind: OutcomeResumed, Summary: "Stage prerequisites recovered."}, nil
	}
	if campaign.State == StatePausedRecoverable && progress.State == ProgressPause {
		return Outcome{Kind: OutcomeRetryDeferred, Reason: progress.Reason, Summary: progress.Summary,
			RetryAfter: progress.RetryAfter, Checkpoint: progress.Checkpoint,
			LinkedResourceType: progress.LinkedResourceType, LinkedResourceID: progress.LinkedResourceID}, nil
	}
	outcome := Outcome{Reason: progress.Reason, Summary: progress.Summary,
		RetryAfter:          progress.RetryAfter,
		ValidRecordingDelta: progress.ValidRecordingDelta, ValidShadowDelta: progress.ValidShadowDelta,
		Checkpoint: progress.Checkpoint, LinkedResourceType: progress.LinkedResourceType,
		LinkedResourceID: progress.LinkedResourceID}
	switch progress.State {
	case ProgressWaiting:
		outcome.Kind = OutcomeWaiting
	case ProgressComplete:
		outcome.Kind = OutcomeCompleted
		if campaign.CurrentStage == StageFinalReport {
			report, reportErr := orchestrator.driver.BuildReport(ctx, campaign, false)
			if reportErr != nil {
				return Outcome{}, reportErr
			}
			outcome.Report = &report
		}
	case ProgressPause:
		outcome.Kind = OutcomePaused
	case ProgressBlock:
		outcome.Kind = OutcomeBlocked
	default:
		return Outcome{}, fmt.Errorf("evaluation_stage_progress_invalid")
	}
	return outcome, nil
}

func (orchestrator *Orchestrator) stage(ctx context.Context, campaign Campaign) (StageProgress, error) {
	switch campaign.CurrentStage {
	case StageHistoricalImport:
		return orchestrator.driver.HistoricalImport(ctx, campaign)
	case StageExistingDataAudit:
		return orchestrator.driver.ExistingDataAudit(ctx, campaign)
	case StageRecorderRotation:
		return orchestrator.driver.RotateRecorder(ctx, campaign)
	case StageRecorderQualify:
		return orchestrator.driver.QualifyRecorder(ctx, campaign)
	case StageBacktestMatrix:
		return orchestrator.driver.OfflineMatrix(ctx, campaign, "backtest")
	case StageReplayMatrix:
		return orchestrator.driver.OfflineMatrix(ctx, campaign, "replay")
	case StageCandidateSelection:
		return orchestrator.driver.SelectCandidates(ctx, campaign)
	case StageCombinedShadow:
		return orchestrator.driver.CombinedShadow(ctx, campaign)
	case StageFinalReport:
		return StageProgress{State: ProgressComplete, Summary: "Final report inputs are complete."}, nil
	default:
		return StageProgress{}, fmt.Errorf("evaluation_stage_unknown")
	}
}
