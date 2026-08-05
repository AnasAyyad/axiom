package backtest

import (
	"context"
	"encoding/json"

	"axiom/internal/replay"
)

// SagaCandidate is opaque multi-leg strategy output tied to one immutable
// replay event. Only the strategy-specific allocator may interpret Payload.
type SagaCandidate struct {
	Ordinal uint64
	Payload json.RawMessage
}

// SagaAllocation is an all-or-nothing multi-resource reservation. It is kept
// opaque so a planner cannot bypass the allocation boundary.
type SagaAllocation struct {
	Ordinal uint64
	Payload json.RawMessage
}

// SagaApproval is a central-risk approval token for an allocated multi-leg
// candidate. It intentionally reveals no mutable order values.
type SagaApproval struct {
	Ordinal uint64
	Payload json.RawMessage
}

// SagaPlan is an approved multi-leg execution plan. It is created only after
// central risk has accepted the atomic allocation.
type SagaPlan struct {
	Ordinal uint64
	Payload json.RawMessage
}

// SagaExecution is the normalized result of a deterministic multi-leg
// simulation or a sandbox-bound execution adapter.
type SagaExecution struct {
	Ordinal uint64
	Payload json.RawMessage
}

// SagaStrategy evaluates canonical event evidence into a strategy-owned
// candidate. It performs no allocation, risk, or exchange operation.
type SagaStrategy interface {
	EvaluateSaga(context.Context, replay.Event) (SagaCandidate, error)
}

// SagaAllocator exclusively claims the complete resource set required by a
// multi-leg candidate, and cleans it up after downstream failure.
type SagaAllocator interface {
	AllocateSaga(context.Context, SagaCandidate) (SagaAllocation, error)
	CloseSagaAllocation(context.Context, SagaAllocation, AllocationDisposition) error
}

// SagaRiskEngine is the central policy boundary for a complete allocation.
type SagaRiskEngine interface {
	ApproveSaga(context.Context, SagaAllocation) (SagaApproval, error)
}

// SagaPlanner turns only a central-risk approval into an execution saga plan.
type SagaPlanner interface {
	PlanSaga(context.Context, SagaApproval) (SagaPlan, error)
}

// SagaBroker executes only a strategy plan through a deterministic simulator
// or separately credential-owning sandbox adapter.
type SagaBroker interface {
	SubmitSaga(context.Context, SagaPlan) (SagaExecution, error)
}

// SagaReducer emits canonical decision, order, execution, balance, journal,
// and reconciliation evidence for one completed strategy event.
type SagaReducer interface {
	ReduceSaga(context.Context, SagaAllocation, SagaPlan, SagaExecution) (EventResult, error)
}

// SagaPipelineDependencies are the shared multi-leg equivalent of
// PipelineDependencies. Each stage is mandatory so history, replay, shadow,
// and sandbox modes cannot substitute a simplified strategy path.
type SagaPipelineDependencies struct {
	Strategy  SagaStrategy
	Allocator SagaAllocator
	Risk      SagaRiskEngine
	Planner   SagaPlanner
	Broker    SagaBroker
	Reducer   SagaReducer
	Metrics   func() Metrics
}

// SagaPipelineProcessor composes evaluator, atomic allocation, central risk,
// plan, execution, accounting, and reconciliation reduction for every
// multi-leg strategy mode.
type SagaPipelineProcessor struct{ dependencies SagaPipelineDependencies }

// NewSagaPipelineProcessor fails closed until the whole durable pipeline is
// available. It deliberately accepts no direct exchange client.
func NewSagaPipelineProcessor(dependencies SagaPipelineDependencies) (*SagaPipelineProcessor, error) {
	if dependencies.Strategy == nil || dependencies.Allocator == nil || dependencies.Risk == nil ||
		dependencies.Planner == nil || dependencies.Broker == nil || dependencies.Reducer == nil ||
		dependencies.Metrics == nil {
		return nil, backtestError("saga_pipeline_incomplete")
	}
	return &SagaPipelineProcessor{dependencies: dependencies}, nil
}

// Process executes one canonical multi-leg decision through the same failure
// disposition rules as the existing single-leg shared pipeline.
func (processor *SagaPipelineProcessor) Process(ctx context.Context, event replay.Event) (EventResult, error) {
	candidate, err := processor.dependencies.Strategy.EvaluateSaga(ctx, event)
	if err != nil || candidate.Ordinal != event.Ordinal {
		return EventResult{}, backtestError("saga_strategy_stage_failed")
	}
	allocated, err := processor.dependencies.Allocator.AllocateSaga(ctx, candidate)
	if err != nil || allocated.Ordinal != event.Ordinal {
		return EventResult{}, backtestError("saga_allocation_stage_failed")
	}
	approved, err := processor.dependencies.Risk.ApproveSaga(ctx, allocated)
	if err != nil || approved.Ordinal != event.Ordinal {
		return EventResult{}, processor.closeAllocation(ctx, allocated, AllocationReleased, "saga_risk_stage_failed")
	}
	plan, err := processor.dependencies.Planner.PlanSaga(ctx, approved)
	if err != nil || plan.Ordinal != event.Ordinal {
		return EventResult{}, processor.closeAllocation(ctx, allocated, AllocationReleased, "saga_planning_stage_failed")
	}
	executed, err := processor.dependencies.Broker.SubmitSaga(ctx, plan)
	if err != nil || executed.Ordinal != event.Ordinal {
		return EventResult{}, processor.closeAllocation(ctx, allocated, AllocationQuarantined, "saga_execution_stage_failed")
	}
	result, err := processor.dependencies.Reducer.ReduceSaga(ctx, allocated, plan, executed)
	if err != nil || result.Ordinal != event.Ordinal {
		return EventResult{}, processor.closeAllocation(ctx, allocated, AllocationQuarantined, "saga_reduction_stage_failed")
	}
	return result, nil
}

func (processor *SagaPipelineProcessor) closeAllocation(
	ctx context.Context,
	allocation SagaAllocation,
	disposition AllocationDisposition,
	failure string,
) error {
	if err := processor.dependencies.Allocator.CloseSagaAllocation(ctx, allocation, disposition); err != nil {
		if disposition == AllocationReleased &&
			processor.dependencies.Allocator.CloseSagaAllocation(ctx, allocation, AllocationQuarantined) == nil {
			return backtestError(failure)
		}
		return backtestError("saga_allocation_cleanup_failed")
	}
	return backtestError(failure)
}

// Metrics returns the canonical metrics owned by the installed multi-leg
// pipeline rather than synthesizing a mode-specific result.
func (processor *SagaPipelineProcessor) Metrics() Metrics { return processor.dependencies.Metrics() }

var _ Processor = (*SagaPipelineProcessor)(nil)
