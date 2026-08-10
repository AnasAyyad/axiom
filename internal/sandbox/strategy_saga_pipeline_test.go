package sandbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/replay"
)

func TestSagaPlanPipelineUsesEverySharedStageBeforeDurableAcceptance(t *testing.T) {
	stages := make([]string, 0, 6)
	allocator := &sagaPipelineAllocator{stages: &stages}
	store := &sagaPipelineStore{stages: &stages}
	pipeline, err := NewSagaPlanPipeline(
		sagaPipelineStrategy{stages: &stages}, allocator,
		sagaPipelineRisk{stages: &stages}, sagaPipelinePlanner{stages: &stages},
		sagaPipelineBuilder{stages: &stages}, store, defaultSagaPipelineLimits(), nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := pipeline.Process(context.Background(), replay.Event{
		Ordinal: 1, LogicalTime: 1, Canonical: []byte(`{"event":"multileg"}`),
	})
	if err != nil || result.SandboxPlan.ID != "execution_plan:multileg" || store.approved != 1 {
		t.Fatalf("result=%#v approved=%d error=%v", result, store.approved, err)
	}
	want := []string{"strategy", "allocator", "risk", "planner", "builder", "store"}
	if len(stages) != len(want) {
		t.Fatalf("stages=%v", stages)
	}
	for index := range want {
		if stages[index] != want[index] {
			t.Fatalf("stages=%v want=%v", stages, want)
		}
	}
	if len(allocator.closed) != 0 {
		t.Fatalf("durably accepted allocation was closed early: %v", allocator.closed)
	}
}

func TestSagaPlanPipelineQuarantinesUncertainDurableFailure(t *testing.T) {
	stages := make([]string, 0, 6)
	allocator := &sagaPipelineAllocator{stages: &stages}
	store := &sagaPipelineStore{stages: &stages, err: errors.New("commit_unknown")}
	pipeline, err := NewSagaPlanPipeline(
		sagaPipelineStrategy{stages: &stages}, allocator,
		sagaPipelineRisk{stages: &stages}, sagaPipelinePlanner{stages: &stages},
		sagaPipelineBuilder{stages: &stages}, store, defaultSagaPipelineLimits(), NoKillPoint{},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pipeline.Process(context.Background(), replay.Event{
		Ordinal: 1, LogicalTime: 1, Canonical: []byte(`{"event":"multileg"}`),
	})
	if err == nil || len(allocator.closed) != 1 ||
		allocator.closed[0] != backtest.AllocationQuarantined {
		t.Fatalf("error=%v dispositions=%v", err, allocator.closed)
	}
}

type sagaPipelineStrategy struct{ stages *[]string }

func (item sagaPipelineStrategy) EvaluateSaga(
	context.Context,
	replay.Event,
) (backtest.SagaCandidate, error) {
	*item.stages = append(*item.stages, "strategy")
	return backtest.SagaCandidate{Ordinal: 1, Payload: []byte(`{"candidate":true}`)}, nil
}

type sagaPipelineAllocator struct {
	stages *[]string
	closed []backtest.AllocationDisposition
}

func (item *sagaPipelineAllocator) AllocateSaga(
	context.Context,
	backtest.SagaCandidate,
) (backtest.SagaAllocation, error) {
	*item.stages = append(*item.stages, "allocator")
	return backtest.SagaAllocation{Ordinal: 1, Payload: []byte(`{"allocation":true}`)}, nil
}

func (item *sagaPipelineAllocator) CloseSagaAllocation(
	_ context.Context,
	_ backtest.SagaAllocation,
	disposition backtest.AllocationDisposition,
) error {
	item.closed = append(item.closed, disposition)
	return nil
}

type sagaPipelineRisk struct{ stages *[]string }

func (item sagaPipelineRisk) ApproveSaga(
	context.Context,
	backtest.SagaAllocation,
) (backtest.SagaApproval, error) {
	*item.stages = append(*item.stages, "risk")
	return backtest.SagaApproval{Ordinal: 1, Payload: []byte(`{"approval":true}`)}, nil
}

type sagaPipelinePlanner struct{ stages *[]string }

func (item sagaPipelinePlanner) PlanSaga(
	context.Context,
	backtest.SagaApproval,
) (backtest.SagaPlan, error) {
	*item.stages = append(*item.stages, "planner")
	return backtest.SagaPlan{Ordinal: 1, Payload: []byte(`{"plan":true}`)}, nil
}

type sagaPipelineBuilder struct{ stages *[]string }

func (item sagaPipelineBuilder) BuildSandboxSagaPlan(
	context.Context,
	backtest.SagaPlan,
) (ApprovedSandboxPlan, error) {
	*item.stages = append(*item.stages, "builder")
	return ApprovedSandboxPlan{ID: "execution_plan:multileg"}, nil
}

type sagaPipelineStore struct {
	stages   *[]string
	approved int
	err      error
}

func (item *sagaPipelineStore) ApprovePlan(
	context.Context,
	ApprovedSandboxPlan,
	SubmissionLimits,
	KillPoint,
) error {
	*item.stages = append(*item.stages, "store")
	item.approved++
	return item.err
}

func (*sagaPipelineStore) ClaimOutbox(context.Context, AccountID, uint64, string, uint64,
	time.Time, time.Duration, int, KillPoint) ([]SubmissionOutbox, error) {
	return nil, nil
}
func (*sagaPipelineStore) MarkSubmitting(context.Context, string, uint64, time.Time, KillPoint) error {
	return nil
}
func (*sagaPipelineStore) MarkUnknown(context.Context, string, uint64, time.Time, KillPoint) error {
	return nil
}
func (*sagaPipelineStore) MarkCancelPending(context.Context, AccountID, uint64, string, string, uint64,
	time.Time, KillPoint) (string, error) {
	return "", nil
}
func (*sagaPipelineStore) MarkCancelUnknown(context.Context, string, uint64, time.Time, KillPoint) error {
	return nil
}
func (*sagaPipelineStore) AppendPrivateEvent(context.Context, string, uint64, PrivateEvent, KillPoint) error {
	return nil
}

func defaultSagaPipelineLimits() SubmissionLimits {
	return SubmissionLimits{MaximumOrderNotional: "10", MaximumDailyNotional: "50",
		MaximumOpenPerAccount: 1, MaximumOpenGlobal: 2}
}
