package backtest

import (
	"context"
	"errors"
	"testing"

	"axiom/internal/replay"
)

func TestSagaPipelineRunsEveryRequiredStageInOrder(t *testing.T) {
	stages := []string{}
	pipeline, err := NewSagaPipelineProcessor(SagaPipelineDependencies{
		Strategy: sagaStageFunc{evaluate: func(context.Context, replay.Event) (SagaCandidate, error) {
			stages = append(stages, "strategy")
			return SagaCandidate{Ordinal: 7}, nil
		}},
		Allocator: sagaStageFunc{allocate: func(context.Context, SagaCandidate) (SagaAllocation, error) {
			stages = append(stages, "allocator")
			return SagaAllocation{Ordinal: 7}, nil
		}},
		Risk: sagaStageFunc{approve: func(context.Context, SagaAllocation) (SagaApproval, error) {
			stages = append(stages, "risk")
			return SagaApproval{Ordinal: 7}, nil
		}},
		Planner: sagaStageFunc{plan: func(context.Context, SagaApproval) (SagaPlan, error) {
			stages = append(stages, "planner")
			return SagaPlan{Ordinal: 7}, nil
		}},
		Broker: sagaStageFunc{submit: func(context.Context, SagaPlan) (SagaExecution, error) {
			stages = append(stages, "broker")
			return SagaExecution{Ordinal: 7}, nil
		}},
		Reducer: sagaStageFunc{reduce: func(context.Context, SagaAllocation, SagaPlan, SagaExecution) (EventResult, error) {
			stages = append(stages, "reducer")
			return EventResult{Ordinal: 7}, nil
		}},
		Metrics: func() Metrics { return Metrics{Trades: 2} },
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := pipeline.Process(context.Background(), replay.Event{Ordinal: 7})
	if err != nil || result.Ordinal != 7 || pipeline.Metrics().Trades != 2 {
		t.Fatalf("result=%#v error=%v", result, err)
	}
	want := []string{"strategy", "allocator", "risk", "planner", "broker", "reducer"}
	if len(stages) != len(want) {
		t.Fatalf("stages=%v", stages)
	}
	for index := range want {
		if stages[index] != want[index] {
			t.Fatalf("stage %d=%q want %q", index, stages[index], want[index])
		}
	}
}

func TestSagaPipelineReleasesOrQuarantinesEveryDownstreamFailure(t *testing.T) {
	tests := []struct {
		name        string
		fail        string
		disposition AllocationDisposition
	}{
		{name: "risk", fail: "risk", disposition: AllocationReleased},
		{name: "planner", fail: "planner", disposition: AllocationReleased},
		{name: "broker", fail: "broker", disposition: AllocationQuarantined},
		{name: "reducer", fail: "reducer", disposition: AllocationQuarantined},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			closed := []AllocationDisposition{}
			pipeline := sagaPipelineForTest(test.fail, &closed)
			if _, err := pipeline.Process(context.Background(), replay.Event{Ordinal: 1}); err == nil {
				t.Fatal("downstream failure was accepted")
			}
			if len(closed) != 1 || closed[0] != test.disposition {
				t.Fatalf("cleanup=%v want %s", closed, test.disposition)
			}
		})
	}
}

func TestSagaPipelineFailsClosedForIncompleteDependenciesAndStageOrdinalMismatch(t *testing.T) {
	if _, err := NewSagaPipelineProcessor(SagaPipelineDependencies{}); err == nil {
		t.Fatal("incomplete saga pipeline accepted")
	}
	closed := []AllocationDisposition{}
	pipeline := sagaPipelineForTest("", &closed)
	pipeline.dependencies.Broker = sagaStageFunc{submit: func(context.Context, SagaPlan) (SagaExecution, error) {
		return SagaExecution{Ordinal: 2}, nil
	}}
	if _, err := pipeline.Process(context.Background(), replay.Event{Ordinal: 1}); err == nil ||
		len(closed) != 1 || closed[0] != AllocationQuarantined {
		t.Fatalf("ordinal mismatch error=%v cleanup=%v", err, closed)
	}
}

func sagaPipelineForTest(fail string, closed *[]AllocationDisposition) *SagaPipelineProcessor {
	pipeline, err := NewSagaPipelineProcessor(SagaPipelineDependencies{
		Strategy: sagaStageFunc{evaluate: func(context.Context, replay.Event) (SagaCandidate, error) {
			return SagaCandidate{Ordinal: 1}, nil
		}},
		Allocator: sagaStageFunc{allocate: func(context.Context, SagaCandidate) (SagaAllocation, error) {
			return SagaAllocation{Ordinal: 1}, nil
		}, close: func(_ context.Context, _ SagaAllocation, disposition AllocationDisposition) error {
			*closed = append(*closed, disposition)
			return nil
		}},
		Risk: sagaStageFunc{approve: func(context.Context, SagaAllocation) (SagaApproval, error) {
			if fail == "risk" {
				return SagaApproval{}, errors.New("risk")
			}
			return SagaApproval{Ordinal: 1}, nil
		}},
		Planner: sagaStageFunc{plan: func(context.Context, SagaApproval) (SagaPlan, error) {
			if fail == "planner" {
				return SagaPlan{}, errors.New("planner")
			}
			return SagaPlan{Ordinal: 1}, nil
		}},
		Broker: sagaStageFunc{submit: func(context.Context, SagaPlan) (SagaExecution, error) {
			if fail == "broker" {
				return SagaExecution{}, errors.New("broker")
			}
			return SagaExecution{Ordinal: 1}, nil
		}},
		Reducer: sagaStageFunc{reduce: func(context.Context, SagaAllocation, SagaPlan, SagaExecution) (EventResult, error) {
			if fail == "reducer" {
				return EventResult{}, errors.New("reducer")
			}
			return EventResult{Ordinal: 1}, nil
		}},
		Metrics: func() Metrics { return Metrics{} },
	})
	if err != nil {
		panic(err)
	}
	return pipeline
}

type sagaStageFunc struct {
	evaluate func(context.Context, replay.Event) (SagaCandidate, error)
	allocate func(context.Context, SagaCandidate) (SagaAllocation, error)
	close    func(context.Context, SagaAllocation, AllocationDisposition) error
	approve  func(context.Context, SagaAllocation) (SagaApproval, error)
	plan     func(context.Context, SagaApproval) (SagaPlan, error)
	submit   func(context.Context, SagaPlan) (SagaExecution, error)
	reduce   func(context.Context, SagaAllocation, SagaPlan, SagaExecution) (EventResult, error)
}

func (value sagaStageFunc) EvaluateSaga(ctx context.Context, event replay.Event) (SagaCandidate, error) {
	return value.evaluate(ctx, event)
}
func (value sagaStageFunc) AllocateSaga(ctx context.Context, candidate SagaCandidate) (SagaAllocation, error) {
	return value.allocate(ctx, candidate)
}
func (value sagaStageFunc) CloseSagaAllocation(ctx context.Context, allocation SagaAllocation, disposition AllocationDisposition) error {
	return value.close(ctx, allocation, disposition)
}
func (value sagaStageFunc) ApproveSaga(ctx context.Context, allocation SagaAllocation) (SagaApproval, error) {
	return value.approve(ctx, allocation)
}
func (value sagaStageFunc) PlanSaga(ctx context.Context, approval SagaApproval) (SagaPlan, error) {
	return value.plan(ctx, approval)
}
func (value sagaStageFunc) SubmitSaga(ctx context.Context, plan SagaPlan) (SagaExecution, error) {
	return value.submit(ctx, plan)
}
func (value sagaStageFunc) ReduceSaga(ctx context.Context, allocation SagaAllocation, plan SagaPlan, execution SagaExecution) (EventResult, error) {
	return value.reduce(ctx, allocation, plan, execution)
}
