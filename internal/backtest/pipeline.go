package backtest

import (
	"context"
	"encoding/json"

	"axiom/internal/execution"
	"axiom/internal/replay"
)

// Candidate is opaque strategy output tied to one canonical input event.
type Candidate struct {
	Ordinal uint64
	Payload json.RawMessage
}

// AllocatedIntent is opaque exclusive-ownership output for central risk.
type AllocatedIntent struct {
	Ordinal uint64
	Payload json.RawMessage
}

// Strategy is the consumer-owned decision interface shared by every mode.
type Strategy interface {
	Evaluate(context.Context, replay.Event) (Candidate, error)
}

// Allocator is the consumer-owned exclusive-reservation interface.
type Allocator interface {
	Allocate(context.Context, Candidate) (AllocatedIntent, error)
}

// RiskEngine is the consumer-owned central approval interface.
type RiskEngine interface {
	Approve(context.Context, AllocatedIntent) (execution.ApprovedIntent, error)
}

// PipelineDependencies prevent historical mode from substituting simplified logic.
type PipelineDependencies struct {
	Strategy  Strategy
	Allocator Allocator
	Risk      RiskEngine
	Planner   execution.ExecutionPlanner
	Broker    execution.Broker
	Reduce    func(context.Context, []execution.OrderEvent) (json.RawMessage, json.RawMessage, error)
	Metrics   func() Metrics
}

// PipelineProcessor composes the same allocation, risk, execution, and accounting path.
type PipelineProcessor struct{ dependencies PipelineDependencies }

// NewPipelineProcessor fails closed until every operational dependency exists.
func NewPipelineProcessor(dependencies PipelineDependencies) (*PipelineProcessor, error) {
	if dependencies.Strategy == nil || dependencies.Allocator == nil || dependencies.Risk == nil ||
		dependencies.Planner == nil || dependencies.Broker == nil || dependencies.Reduce == nil || dependencies.Metrics == nil {
		return nil, backtestError("operational_pipeline_incomplete")
	}
	return &PipelineProcessor{dependencies: dependencies}, nil
}

// Process executes the shared mode-independent pipeline once.
func (processor *PipelineProcessor) Process(ctx context.Context, event replay.Event) (EventResult, error) {
	candidate, err := processor.dependencies.Strategy.Evaluate(ctx, event)
	if err != nil || candidate.Ordinal != event.Ordinal {
		return EventResult{}, backtestError("strategy_stage_failed")
	}
	allocated, err := processor.dependencies.Allocator.Allocate(ctx, candidate)
	if err != nil || allocated.Ordinal != event.Ordinal {
		return EventResult{}, backtestError("allocation_stage_failed")
	}
	approved, err := processor.dependencies.Risk.Approve(ctx, allocated)
	if err != nil {
		return EventResult{}, backtestError("risk_stage_failed")
	}
	plan, err := processor.dependencies.Planner.Plan(ctx, approved)
	if err != nil {
		return EventResult{}, backtestError("planning_stage_failed")
	}
	events, err := processor.dependencies.Broker.Submit(ctx, plan)
	if err != nil {
		return EventResult{}, backtestError("simulation_stage_failed")
	}
	orders, balances, err := processor.dependencies.Reduce(ctx, events)
	if err != nil {
		return EventResult{}, backtestError("durable_stage_failed")
	}
	decision, _ := json.Marshal(approved)
	return EventResult{Ordinal: event.Ordinal, Decision: decision, Orders: orders, Balances: balances}, nil
}

// Metrics returns the shared processor's canonical Section 21 result metrics.
func (processor *PipelineProcessor) Metrics() Metrics { return processor.dependencies.Metrics() }

var _ Processor = (*PipelineProcessor)(nil)
