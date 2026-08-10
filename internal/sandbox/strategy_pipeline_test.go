package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/replay"
)

func TestStrategyPipelineUsesEverySharedStageBeforeDurablePlan(t *testing.T) {
	event := strategyPipelineEvent()
	strategy := &strategyPipelineStrategy{candidate: backtest.Candidate{Ordinal: event.Ordinal, Payload: []byte(`{"candidate":true}`)}, decision: []byte(`{"decision":true}`)}
	allocator := &strategyPipelineAllocator{allocated: backtest.AllocatedIntent{Ordinal: event.Ordinal, Payload: []byte(`{"allocation":true}`)}}
	riskEngine := &strategyPipelineRisk{approved: strategyPipelineApproved(t)}
	planner := &strategyPipelinePlanner{plan: strategyPipelineExecutionPlan(t)}
	builder := &strategyPipelineBuilder{plan: strategyPipelineApprovedPlan(t)}
	store := &strategyPipelineStore{}
	pipeline, err := NewStrategyPipeline(strategy, allocator, riskEngine, planner, builder, store, strategyPipelineLimits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := pipeline.Process(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if result.Material.Candidate.Ordinal != event.Ordinal || result.Material.Allocated.Ordinal != event.Ordinal ||
		result.Material.Approved != riskEngine.approved || result.Material.Plan.ID != planner.plan.ID ||
		result.Plan.ID != builder.plan.ID || store.calls != 1 || allocator.closed != "" {
		t.Fatalf("result=%#v store_calls=%d allocation_close=%q", result, store.calls, allocator.closed)
	}
}

func TestStrategyPipelineReleasesOrQuarantinesAllocationOnEveryDownstreamFailure(t *testing.T) {
	tests := []struct {
		name       string
		riskErr    error
		plannerErr error
		builderErr error
		storeErr   error
		wantClosed backtest.AllocationDisposition
	}{
		{name: "risk", riskErr: errors.New("risk"), wantClosed: backtest.AllocationReleased},
		{name: "planner", plannerErr: errors.New("planner"), wantClosed: backtest.AllocationReleased},
		{name: "builder", builderErr: errors.New("builder"), wantClosed: backtest.AllocationReleased},
		{name: "durable store", storeErr: errors.New("store"), wantClosed: backtest.AllocationQuarantined},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event := strategyPipelineEvent()
			allocator := &strategyPipelineAllocator{allocated: backtest.AllocatedIntent{Ordinal: event.Ordinal, Payload: []byte(`{}`)}}
			pipeline, err := NewStrategyPipeline(
				&strategyPipelineStrategy{candidate: backtest.Candidate{Ordinal: event.Ordinal, Payload: []byte(`{}`)}, decision: []byte(`{}`)}, allocator,
				&strategyPipelineRisk{approved: strategyPipelineApproved(t), err: test.riskErr},
				&strategyPipelinePlanner{plan: strategyPipelineExecutionPlan(t), err: test.plannerErr},
				&strategyPipelineBuilder{plan: strategyPipelineApprovedPlan(t), err: test.builderErr},
				&strategyPipelineStore{err: test.storeErr}, strategyPipelineLimits(), NoKillPoint{},
			)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = pipeline.Process(context.Background(), event); err == nil || allocator.closed != test.wantClosed {
				t.Fatalf("error=%v allocation_close=%q", err, allocator.closed)
			}
		})
	}
}

func TestStrategyPipelineRejectsIncompleteDependenciesAndInvalidEvent(t *testing.T) {
	if _, err := NewStrategyPipeline(nil, nil, nil, nil, nil, nil, SubmissionLimits{}, nil); err == nil {
		t.Fatal("incomplete pipeline accepted")
	}
	pipeline, err := NewStrategyPipeline(
		&strategyPipelineStrategy{}, &strategyPipelineAllocator{}, &strategyPipelineRisk{},
		&strategyPipelinePlanner{}, &strategyPipelineBuilder{}, &strategyPipelineStore{}, strategyPipelineLimits(), nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pipeline.Process(context.Background(), replay.Event{}); err == nil {
		t.Fatal("invalid event accepted")
	}
}

func TestAdmittedSingleVenueStrategyPipelineBindsExactAdmissionFacts(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	pipeline, err := NewAdmittedSingleVenueStrategyPipeline(
		strategyPlanAdmission(now), strategyPlanSnapshot(t, now, "1"), strategyPlanInventory(t, now, "1"),
		&strategyPipelineStrategy{}, StrategyPipelineDependencies{
			Allocator: &strategyPipelineAllocator{}, Risk: &strategyPipelineRisk{},
			Planner: &strategyPipelinePlanner{}, Store: &strategyPipelineStore{},
		}, strategyPipelineLimits(),
	)
	if err != nil || pipeline == nil {
		t.Fatalf("admitted pipeline = %#v %v", pipeline, err)
	}
	staleSnapshot := strategyPlanSnapshot(t, now, "1")
	staleSnapshot.ObservedAt = now.Add(time.Second)
	if _, err = NewAdmittedSingleVenueStrategyPipeline(
		strategyPlanAdmission(now), staleSnapshot, strategyPlanInventory(t, now, "1"),
		&strategyPipelineStrategy{}, StrategyPipelineDependencies{}, strategyPipelineLimits(),
	); err == nil {
		t.Fatal("non-admission snapshot was accepted for pipeline construction")
	}
}

type strategyPipelineStrategy struct {
	candidate backtest.Candidate
	decision  json.RawMessage
	err       error
}

func (item *strategyPipelineStrategy) Evaluate(context.Context, replay.Event) (backtest.Candidate, error) {
	return item.candidate, item.err
}

func (item *strategyPipelineStrategy) DecisionEvidence(replay.Event) (json.RawMessage, error) {
	if item.decision == nil {
		return json.RawMessage(`{}`), nil
	}
	return append(json.RawMessage(nil), item.decision...), nil
}

type strategyPipelineAllocator struct {
	allocated backtest.AllocatedIntent
	err       error
	closed    backtest.AllocationDisposition
}

func (item *strategyPipelineAllocator) Allocate(context.Context, backtest.Candidate) (backtest.AllocatedIntent, error) {
	return item.allocated, item.err
}
func (item *strategyPipelineAllocator) CloseAllocation(_ context.Context, _ backtest.AllocatedIntent, disposition backtest.AllocationDisposition) error {
	item.closed = disposition
	return nil
}

type strategyPipelineRisk struct {
	approved execution.ApprovedIntent
	err      error
}

func (item *strategyPipelineRisk) Approve(context.Context, backtest.AllocatedIntent) (execution.ApprovedIntent, error) {
	return item.approved, item.err
}

type strategyPipelinePlanner struct {
	plan execution.SimulatedPlan
	err  error
}

func (item *strategyPipelinePlanner) Plan(context.Context, execution.ApprovedIntent) (execution.SimulatedPlan, error) {
	return item.plan, item.err
}

type strategyPipelineBuilder struct {
	plan ApprovedSandboxPlan
	err  error
}

func (item *strategyPipelineBuilder) BuildStrategyPlan(context.Context, StrategyPipelineMaterial) (ApprovedSandboxPlan, error) {
	return item.plan, item.err
}

type strategyPipelineStore struct {
	calls int
	err   error
}

func (item *strategyPipelineStore) ApprovePlan(context.Context, ApprovedSandboxPlan, SubmissionLimits, KillPoint) error {
	item.calls++
	return item.err
}
func (*strategyPipelineStore) ClaimOutbox(context.Context, AccountID, uint64, string, uint64, time.Time, time.Duration, int, KillPoint) ([]SubmissionOutbox, error) {
	return nil, nil
}
func (*strategyPipelineStore) MarkSubmitting(context.Context, string, uint64, time.Time, KillPoint) error {
	return nil
}
func (*strategyPipelineStore) MarkUnknown(context.Context, string, uint64, time.Time, KillPoint) error {
	return nil
}
func (*strategyPipelineStore) MarkCancelPending(context.Context, AccountID, uint64, string, string, uint64, time.Time, KillPoint) (string, error) {
	return "", nil
}
func (*strategyPipelineStore) MarkCancelUnknown(context.Context, string, uint64, time.Time, KillPoint) error {
	return nil
}
func (*strategyPipelineStore) AppendPrivateEvent(context.Context, string, uint64, PrivateEvent, KillPoint) error {
	return nil
}

func strategyPipelineEvent() replay.Event {
	return replay.Event{Ordinal: 1, LogicalTime: 1, Canonical: []byte(`{"event":true}`)}
}
func strategyPipelineLimits() SubmissionLimits {
	return SubmissionLimits{MaximumOrderNotional: "10", MaximumDailyNotional: "50", MaximumOpenPerAccount: 1, MaximumOpenGlobal: 2}
}
func strategyPipelineApproved(t *testing.T) execution.ApprovedIntent {
	t.Helper()
	id, err := domain.NewDecisionID("sandbox-strategy-decision")
	if err != nil {
		t.Fatal(err)
	}
	return execution.ApprovedIntent{DecisionID: id, ApprovalHash: strategyPipelineHash, PolicyHash: strategyPipelineHash}
}
func strategyPipelineExecutionPlan(t *testing.T) execution.SimulatedPlan {
	t.Helper()
	id, err := domain.NewExecutionPlanID("sandbox-strategy-plan")
	if err != nil {
		t.Fatal(err)
	}
	return execution.SimulatedPlan{ID: id, Intent: strategyPipelineApproved(t), Namespace: "sandbox"}
}
func strategyPipelineApprovedPlan(t *testing.T) ApprovedSandboxPlan {
	t.Helper()
	return ApprovedSandboxPlan{ID: "sandbox-strategy-plan", SessionID: "session", ConfigurationID: "configuration"}
}

const strategyPipelineHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
