package backtest

import (
	"context"
	"encoding/json"

	"axiom/internal/replay"
)

// JobClaim is one exclusively claimed credential-free offline run.
type JobClaim struct {
	ID       string
	Manifest RunManifest
	Source   replay.Source
}

// JobStore owns durable claim, completion, and failure state.
type JobStore interface {
	Claim(context.Context) (JobClaim, bool, error)
	Complete(context.Context, string, CanonicalResult, []byte) error
	Fail(context.Context, string, string) error
}

// ProcessorFactory supplies the shared operational pipeline for one job.
type ProcessorFactory func(JobClaim) (Processor, error)

// Worker runs only offline replay sources and has no exchange transport.
type Worker struct {
	store   JobStore
	factory ProcessorFactory
	pacer   replay.Pacer
}

// NewWorker constructs a credential-free backtest/replay job runner.
func NewWorker(store JobStore, factory ProcessorFactory, pacer replay.Pacer) (*Worker, error) {
	if store == nil || factory == nil || pacer == nil {
		return nil, backtestError("worker_configuration_invalid")
	}
	return &Worker{store: store, factory: factory, pacer: pacer}, nil
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
	processor, err := worker.factory(claim)
	if err != nil || processor == nil {
		return true, worker.fail(ctx, claim.ID, "operational_pipeline_incomplete")
	}
	controller, err := replay.NewController(claim.Source, worker.pacer, replay.MaximumTiming, 1)
	if err != nil {
		return true, worker.fail(ctx, claim.ID, "replay_controller_invalid")
	}
	engine, err := NewEngine(controller, processor, claim.Manifest)
	if err != nil {
		return true, worker.fail(ctx, claim.ID, "run_manifest_invalid")
	}
	result, err := engine.Run(ctx)
	if err != nil {
		return true, worker.fail(ctx, claim.ID, "offline_run_failed")
	}
	canonical, err := json.Marshal(result)
	if err != nil || worker.store.Complete(ctx, claim.ID, result, canonical) != nil {
		return true, backtestError("offline_result_persist_failed")
	}
	return true, nil
}

func (worker *Worker) fail(ctx context.Context, id, reason string) error {
	if id == "" || worker.store.Fail(ctx, id, reason) != nil {
		return backtestError("offline_job_failure_persist_failed")
	}
	return backtestError(reason)
}
