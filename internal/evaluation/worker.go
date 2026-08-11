package evaluation

import (
	"context"
	"errors"
	"time"
)

// Claim is one exclusively leased campaign checkpoint.
type Claim struct {
	Campaign   Campaign
	ClaimEpoch int64
}

// OutcomeKind describes one atomic worker result.
type OutcomeKind string

// Worker outcome kinds map one fenced execution to one durable transition.
const (
	OutcomeStarted         OutcomeKind = "STARTED"
	OutcomeWaiting         OutcomeKind = "WAITING"
	OutcomeCompleted       OutcomeKind = "COMPLETED"
	OutcomePaused          OutcomeKind = "PAUSED_RECOVERABLE"
	OutcomeBlocked         OutcomeKind = "BLOCKED"
	OutcomeResumed         OutcomeKind = "RESUMED"
	OutcomePartialReported OutcomeKind = "PARTIAL_REPORTED"
)

// Outcome is applied only while the claim epoch remains fenced.
type Outcome struct {
	Kind                OutcomeKind
	Reason              ReasonCode
	Summary             string
	ValidRecordingDelta time.Duration
	ValidShadowDelta    time.Duration
	Checkpoint          []byte
	LinkedResourceType  string
	LinkedResourceID    string
	Report              *Report
}

// Store owns durable exclusive claims and atomic transitions.
type Store interface {
	Claim(context.Context) (Claim, bool, error)
	Renew(context.Context, Claim) error
	Apply(context.Context, Claim, Outcome) error
}

// StageExecutor resolves only server-owned inputs and drives existing job,
// recorder, shadow, and report infrastructure.
type StageExecutor interface {
	Execute(context.Context, Claim) (Outcome, error)
}

// Worker advances at most one durable campaign checkpoint per iteration.
type Worker struct {
	store     Store
	executor  StageExecutor
	heartbeat time.Duration
}

// NewWorker constructs a credential-free campaign worker.
func NewWorker(store Store, executor StageExecutor) (*Worker, error) {
	if store == nil || executor == nil {
		return nil, errors.New("evaluation_worker_dependencies_missing")
	}
	return &Worker{store: store, executor: executor, heartbeat: 10 * time.Second}, nil
}

// RunOne claims and atomically advances at most one campaign.
func (worker *Worker) RunOne(ctx context.Context) (bool, error) {
	claim, found, err := worker.store.Claim(ctx)
	if err != nil || !found {
		return found, err
	}
	if !claim.Campaign.Valid() || claim.ClaimEpoch <= 0 {
		return true, worker.store.Apply(ctx, claim, Outcome{Kind: OutcomeBlocked,
			Reason: ReasonSafetyFailed, Summary: "Campaign checkpoint failed validation."})
	}
	outcome, err := worker.executeWithLease(ctx, claim)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
			err.Error() == "evaluation_campaign_claim_lost" {
			return true, err
		}
		outcome = Outcome{Kind: OutcomeBlocked, Reason: ReasonPersistenceFailed,
			Summary: "Campaign stage failed before a safe checkpoint."}
	}
	if !validOutcome(claim.Campaign, outcome) {
		outcome = Outcome{Kind: OutcomeBlocked, Reason: ReasonSafetyFailed,
			Summary: "Campaign stage returned an invalid transition."}
	}
	return true, worker.store.Apply(ctx, claim, outcome)
}

func (worker *Worker) executeWithLease(ctx context.Context, claim Claim) (Outcome, error) {
	workContext, cancel := context.WithCancel(ctx)
	defer cancel()
	type executionResult struct {
		outcome Outcome
		err     error
	}
	completed := make(chan executionResult, 1)
	go func() {
		outcome, err := worker.executor.Execute(workContext, claim)
		completed <- executionResult{outcome: outcome, err: err}
	}()
	ticker := time.NewTicker(worker.heartbeat)
	defer ticker.Stop()
	for {
		select {
		case result := <-completed:
			return result.outcome, result.err
		case <-ticker.C:
			if err := worker.store.Renew(workContext, claim); err != nil {
				cancel()
				<-completed
				return Outcome{}, errors.New("evaluation_campaign_claim_lost")
			}
		case <-ctx.Done():
			cancel()
			<-completed
			return Outcome{}, ctx.Err()
		}
	}
}

func validOutcome(campaign Campaign, outcome Outcome) bool {
	if outcome.ValidRecordingDelta < 0 || outcome.ValidShadowDelta < 0 {
		return false
	}
	if len(outcome.Checkpoint) > 1<<20 || (outcome.LinkedResourceType == "") != (outcome.LinkedResourceID == "") {
		return false
	}
	if campaign.State == StateCanceled || campaign.State == StateBlocked {
		return outcome.Kind == OutcomePartialReported && outcome.Report != nil && outcome.Report.Valid() && outcome.Report.State == "partial"
	}
	switch outcome.Kind {
	case OutcomeStarted:
		return campaign.State == StatePending && outcome.Reason == "" && outcome.Report == nil
	case OutcomeWaiting, OutcomeCompleted, OutcomeResumed:
		if outcome.Kind == OutcomeCompleted && campaign.CurrentStage == StageFinalReport {
			return outcome.Reason == "" && outcome.Report != nil && outcome.Report.Valid() && outcome.Report.State == "final"
		}
		return outcome.Reason == "" && outcome.Report == nil
	case OutcomePaused, OutcomeBlocked:
		return outcome.Reason != ""
	default:
		return false
	}
}
