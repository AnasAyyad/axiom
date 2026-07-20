package backtest

import (
	"context"
	"encoding/json"
	"time"

	"axiom/internal/config"
	"axiom/internal/replay"
)

// JobClaim is one exclusively claimed credential-free offline run.
type JobClaim struct {
	ID            string
	Manifest      RunManifest
	Configuration config.Configuration
	Source        replay.Source
	TimingMode    replay.TimingMode
	Acceleration  uint64
	ResumeOrdinal uint64
	SingleStep    bool
}

// JobControl is the current authoritative replay control posture.
type JobControl struct {
	PauseRequested bool
	SingleStep     bool
	ResumeOrdinal  uint64
}

// JobStore owns durable claim, completion, and failure state.
type JobStore interface {
	Claim(context.Context) (JobClaim, bool, error)
	Renew(context.Context, string) error
	Complete(context.Context, string, CanonicalResult, []byte) error
	Fail(context.Context, string, string) error
}

// ControlledJobStore adds durable replay pause/checkpoint semantics without
// broadening the backtest-only store contract.
type ControlledJobStore interface {
	JobStore
	Control(context.Context, string) (JobControl, error)
	Pause(context.Context, string, uint64, []byte) error
}

// ProcessorFactory supplies the shared operational pipeline for one job.
type ProcessorFactory func(JobClaim) (Processor, error)

// Worker runs only offline replay sources and has no exchange transport.
type Worker struct {
	store     JobStore
	factory   ProcessorFactory
	pacer     replay.Pacer
	heartbeat time.Duration
}

// NewWorker constructs a credential-free backtest/replay job runner.
func NewWorker(store JobStore, factory ProcessorFactory, pacer replay.Pacer) (*Worker, error) {
	if store == nil || factory == nil || pacer == nil {
		return nil, backtestError("worker_configuration_invalid")
	}
	return &Worker{store: store, factory: factory, pacer: pacer, heartbeat: 10 * time.Second}, nil
}

// RunOne claims and completes at most one offline job.
func (worker *Worker) RunOne(ctx context.Context) (bool, error) {
	claim, ok, err := worker.store.Claim(ctx)
	if err != nil || !ok {
		return ok, err
	}
	if claim.ID == "" || claim.Source == nil || (claim.Manifest.Mode != "backtest" && claim.Manifest.Mode != "replay") {
		return true, worker.fail(ctx, claim.ID, "offline_job_invalid")
	}
	return true, worker.runClaim(ctx, claim)
}

func (worker *Worker) runClaim(ctx context.Context, claim JobClaim) error {
	processor, err := worker.factory(claim)
	if err != nil || processor == nil {
		return worker.fail(ctx, claim.ID, "operational_pipeline_incomplete")
	}
	controller, err := replay.NewController(claim.Source, worker.pacer, claim.TimingMode, claim.Acceleration)
	if err != nil {
		return worker.fail(ctx, claim.ID, "replay_controller_invalid")
	}
	engine, err := NewEngine(controller, processor, claim.Manifest)
	if err != nil {
		return worker.fail(ctx, claim.ID, "run_manifest_invalid")
	}
	result, paused, cursor, err := worker.runEngineWithLease(ctx, claim, engine)
	if err != nil {
		return worker.fail(ctx, claim.ID, "offline_run_failed")
	}
	if paused {
		checkpoint, encodeErr := json.Marshal(result)
		controlled, ok := worker.store.(ControlledJobStore)
		if encodeErr != nil || !ok || controlled.Pause(ctx, claim.ID, cursor, checkpoint) != nil {
			return worker.fail(ctx, claim.ID, "offline_pause_failed")
		}
		return nil
	}
	canonical, err := json.Marshal(result)
	if err != nil || worker.store.Complete(ctx, claim.ID, result, canonical) != nil {
		return backtestError("offline_result_persist_failed")
	}
	return nil
}

func (worker *Worker) runEngineWithLease(ctx context.Context, claim JobClaim,
	engine *Engine) (CanonicalResult, bool, uint64, error) {
	workContext, cancel := context.WithCancel(ctx)
	defer cancel()
	type outcome struct {
		result CanonicalResult
		paused bool
		cursor uint64
		err    error
	}
	completed := make(chan outcome, 1)
	go func() {
		cursor := uint64(0)
		pauseAfter := worker.pauseCallback(workContext, claim, &cursor)
		result, paused, err := engine.RunControlled(workContext, pauseAfter)
		completed <- outcome{result: result, paused: paused, cursor: cursor, err: err}
	}()
	ticker := time.NewTicker(worker.heartbeat)
	defer ticker.Stop()
	for {
		select {
		case result := <-completed:
			return result.result, result.paused, result.cursor, result.err
		case <-ticker.C:
			if err := worker.store.Renew(workContext, claim.ID); err != nil {
				cancel()
				<-completed
				return CanonicalResult{}, false, 0, backtestError("offline_job_lease_lost")
			}
		case <-ctx.Done():
			cancel()
			<-completed
			return CanonicalResult{}, false, 0, ctx.Err()
		}
	}
}

func (worker *Worker) pauseCallback(ctx context.Context, claim JobClaim,
	cursor *uint64) func(uint64) (bool, error) {
	controlled, ok := worker.store.(ControlledJobStore)
	if claim.Manifest.Mode != "replay" || !ok {
		return nil
	}
	return func(ordinal uint64) (bool, error) {
		*cursor = ordinal
		if ordinal <= claim.ResumeOrdinal {
			return false, nil
		}
		control, err := controlled.Control(ctx, claim.ID)
		if err != nil || control.ResumeOrdinal != claim.ResumeOrdinal || control.SingleStep != claim.SingleStep {
			return false, backtestError("replay_control_unavailable")
		}
		return control.PauseRequested || control.SingleStep, nil
	}
}

func (worker *Worker) fail(ctx context.Context, id, reason string) error {
	if id == "" || worker.store.Fail(ctx, id, reason) != nil {
		return backtestError("offline_job_failure_persist_failed")
	}
	return backtestError(reason)
}
