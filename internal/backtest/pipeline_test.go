package backtest

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/replay"
)

func TestOperationalPipelineFailsClosedWithoutA9A10Dependencies(t *testing.T) {
	if _, err := NewPipelineProcessor(PipelineDependencies{}); err == nil {
		t.Fatal("incomplete operational pipeline was enabled")
	}
}

func TestOperationalPipelineReleasesAllocationAfterRiskFailure(t *testing.T) {
	allocator := &pipelineAllocationProbe{}
	processor, err := NewPipelineProcessor(PipelineDependencies{
		Strategy: pipelineStrategyProbe{}, Allocator: allocator, Risk: pipelineRiskProbe{},
		Planner: pipelinePlannerProbe{}, Broker: pipelineBrokerProbe{},
		Reduce: func(context.Context, AllocatedIntent, execution.SimulatedPlan, []execution.OrderEvent) (json.RawMessage, json.RawMessage, error) {
			return nil, nil, nil
		}, Metrics: func() Metrics { return Metrics{} },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = processor.Process(context.Background(), replay.Event{Ordinal: 1}); err == nil {
		t.Fatal("risk failure was accepted")
	}
	if allocator.closed != AllocationReleased {
		t.Fatalf("allocation disposition = %s", allocator.closed)
	}
}

type pipelineStrategyProbe struct{}

func (pipelineStrategyProbe) Evaluate(context.Context, replay.Event) (Candidate, error) {
	return Candidate{Ordinal: 1, Payload: json.RawMessage(`{}`)}, nil
}

type pipelineAllocationProbe struct{ closed AllocationDisposition }

func (*pipelineAllocationProbe) Allocate(context.Context, Candidate) (AllocatedIntent, error) {
	return AllocatedIntent{Ordinal: 1, Payload: json.RawMessage(`{}`)}, nil
}

func (probe *pipelineAllocationProbe) CloseAllocation(
	_ context.Context,
	_ AllocatedIntent,
	disposition AllocationDisposition,
) error {
	probe.closed = disposition
	return nil
}

type pipelineRiskProbe struct{}

func (pipelineRiskProbe) Approve(context.Context, AllocatedIntent) (execution.ApprovedIntent, error) {
	return execution.ApprovedIntent{}, errors.New("rejected")
}

type pipelinePlannerProbe struct{}

func (pipelinePlannerProbe) Plan(context.Context, execution.ApprovedIntent) (execution.SimulatedPlan, error) {
	return execution.SimulatedPlan{}, nil
}

type pipelineBrokerProbe struct{}

func (pipelineBrokerProbe) Submit(context.Context, execution.SimulatedPlan) ([]execution.OrderEvent, error) {
	return nil, nil
}

func (pipelineBrokerProbe) Cancel(context.Context, domain.VirtualOrderID, string) ([]execution.OrderEvent, error) {
	return nil, nil
}
