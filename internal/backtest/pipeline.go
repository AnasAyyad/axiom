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

// AllocationDisposition defines conservative ownership cleanup after a downstream failure.
type AllocationDisposition string

// Supported downstream-failure dispositions.
const (
	AllocationReleased    AllocationDisposition = "released"
	AllocationQuarantined AllocationDisposition = "quarantined"
)

// AllocationCloser closes exclusive claims when a later pipeline stage fails.
type AllocationCloser interface {
	CloseAllocation(context.Context, AllocatedIntent, AllocationDisposition) error
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
	Reduce    func(context.Context, AllocatedIntent, execution.SimulatedPlan, []execution.OrderEvent) (json.RawMessage, json.RawMessage, error)
	Metrics   func() Metrics
}

// PipelineProcessor composes the same allocation, risk, execution, and accounting path.
type PipelineProcessor struct{ dependencies PipelineDependencies }

// NewPipelineProcessor fails closed until every operational dependency exists.
func NewPipelineProcessor(dependencies PipelineDependencies) (*PipelineProcessor, error) {
	_, closesAllocations := dependencies.Allocator.(AllocationCloser)
	if dependencies.Strategy == nil || dependencies.Allocator == nil || !closesAllocations || dependencies.Risk == nil ||
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
		if processor.closeAllocation(ctx, allocated, AllocationReleased) != nil {
			return EventResult{}, backtestError("allocation_cleanup_failed")
		}
		return EventResult{}, backtestError("risk_stage_failed")
	}
	plan, err := processor.dependencies.Planner.Plan(ctx, approved)
	if err != nil {
		if processor.closeAllocation(ctx, allocated, AllocationReleased) != nil {
			return EventResult{}, backtestError("allocation_cleanup_failed")
		}
		return EventResult{}, backtestError("planning_stage_failed")
	}
	events, err := processor.dependencies.Broker.Submit(ctx, plan)
	if err != nil {
		if processor.closeAllocation(ctx, allocated, AllocationQuarantined) != nil {
			return EventResult{}, backtestError("allocation_cleanup_failed")
		}
		return EventResult{}, backtestError("simulation_stage_failed")
	}
	orders, balances, err := processor.dependencies.Reduce(ctx, allocated, plan, events)
	if err != nil {
		if processor.closeAllocation(ctx, allocated, AllocationQuarantined) != nil {
			return EventResult{}, backtestError("allocation_cleanup_failed")
		}
		return EventResult{}, backtestError("durable_stage_failed")
	}
	decision, _ := json.Marshal(approved)
	canonicalEvents, err := canonicalExecutionEvents(plan, events)
	if err != nil {
		return EventResult{}, backtestError("durable_stage_failed")
	}
	executionEvents, _ := json.Marshal(canonicalEvents)
	return EventResult{Ordinal: event.Ordinal, Decision: decision, Orders: orders,
		ExecutionEvents: executionEvents, Balances: balances}, nil
}

func canonicalExecutionEvents(plan execution.SimulatedPlan,
	events []execution.OrderEvent) ([]execution.OrderEvent, error) {
	canonical := make([]execution.OrderEvent, 0, len(events))
	if len(events) == 0 {
		return canonical, nil
	}
	for _, leg := range plan.Legs {
		identity := execution.OrderIdentity{ID: leg.OrderID, PlanID: plan.ID,
			ClientOrderID: leg.ClientOrderID, Instrument: leg.Instrument, Side: leg.Side, Quantity: leg.Quantity}
		legEvents := make([]execution.OrderEvent, 0, len(events))
		for _, event := range events {
			if event.OrderID == leg.OrderID {
				legEvents = append(legEvents, event)
			}
		}
		_, applied, err := execution.ReduceOrderEvents(identity, legEvents)
		if err != nil || len(applied) == 0 {
			return nil, backtestError("canonical_execution_events_invalid")
		}
		canonical = append(canonical, applied...)
	}
	return canonical, nil
}

func (processor *PipelineProcessor) closeAllocation(
	ctx context.Context,
	allocated AllocatedIntent,
	disposition AllocationDisposition,
) error {
	closer := processor.dependencies.Allocator.(AllocationCloser)
	if err := closer.CloseAllocation(ctx, allocated, disposition); err != nil {
		if disposition == AllocationReleased {
			return closer.CloseAllocation(ctx, allocated, AllocationQuarantined)
		}
		return err
	}
	return nil
}

// Metrics returns the shared processor's canonical Section 21 result metrics.
func (processor *PipelineProcessor) Metrics() Metrics { return processor.dependencies.Metrics() }

var _ Processor = (*PipelineProcessor)(nil)
