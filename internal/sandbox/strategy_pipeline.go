package sandbox

import (
	"context"
	"encoding/json"
	"fmt"

	"axiom/internal/backtest"
	"axiom/internal/execution"
	"axiom/internal/replay"
)

// StrategyPlanBuilder converts a risk-approved shared execution plan into the
// exact armed, fenced sandbox plan that the durable dispatcher understands.
// It is deliberately a pure planning boundary: it has no exchange transport
// and cannot acknowledge, fill, or reconcile an order.
type StrategyPlanBuilder interface {
	BuildStrategyPlan(context.Context, StrategyPipelineMaterial) (ApprovedSandboxPlan, error)
}

// StrategyPipelineMaterial is the complete non-secret hand-off between the
// common strategy pipeline and the sandbox-specific durable-plan builder.
// Candidate and allocation payloads are retained only so the builder can bind
// evidence hashes to the real preceding stages; neither is an order request.
type StrategyPipelineMaterial struct {
	Event            replay.Event
	DecisionEvidence []byte
	Candidate        backtest.Candidate
	Allocated        backtest.AllocatedIntent
	Approved         execution.ApprovedIntent
	Plan             execution.SimulatedPlan
}

// StrategyPipelineResult records the exact staged outputs after a durable
// sandbox plan has been accepted. External submission, private events,
// accounting, and reconciliation occur later through the existing dispatcher
// and reducer; this result never represents an exchange acknowledgement.
type StrategyPipelineResult struct {
	Material StrategyPipelineMaterial
	Plan     ApprovedSandboxPlan
}

// StrategyPipeline composes the same strategy, allocator, central-risk, and
// execution-planning stages used by research modes with the sandbox runtime durable
// sandbox outbox. It prevents an automatic strategy from calling an exchange
// adapter directly or bypassing durable reservations and admission checks.
type StrategyPipeline struct {
	strategy       backtest.Strategy
	decisionSource backtest.DecisionEvidenceSource
	allocator      backtest.Allocator
	risk           backtest.RiskEngine
	planner        execution.ExecutionPlanner
	builder        StrategyPlanBuilder
	store          DispatcherRepository
	limits         SubmissionLimits
	kill           KillPoint
}

// NewStrategyPipeline constructs a fail-closed automatic sandbox bridge.
// An allocation closer is mandatory so every failed downstream stage releases
// known-safe ownership or quarantines it when durable acceptance is uncertain.
func NewStrategyPipeline(
	strategy backtest.Strategy,
	allocator backtest.Allocator,
	riskEngine backtest.RiskEngine,
	planner execution.ExecutionPlanner,
	builder StrategyPlanBuilder,
	store DispatcherRepository,
	limits SubmissionLimits,
	kill KillPoint,
) (*StrategyPipeline, error) {
	if strategy == nil || allocator == nil || riskEngine == nil || planner == nil ||
		builder == nil || store == nil || !validStrategyPipelineLimits(limits) {
		return nil, contractError("strategy_pipeline_invalid")
	}
	decisionSource, suppliesEvidence := strategy.(backtest.DecisionEvidenceSource)
	if !suppliesEvidence || decisionSource == nil {
		return nil, contractError("strategy_pipeline_invalid")
	}
	if _, ok := allocator.(backtest.AllocationCloser); !ok {
		return nil, contractError("strategy_pipeline_invalid")
	}
	if kill == nil {
		kill = NoKillPoint{}
	}
	return &StrategyPipeline{strategy: strategy, decisionSource: decisionSource, allocator: allocator,
		risk: riskEngine, planner: planner, builder: builder, store: store,
		limits: limits, kill: kill}, nil
}

func validStrategyPipelineLimits(limits SubmissionLimits) bool {
	return limits.MaximumOrderNotional == "10" &&
		limits.MaximumDailyNotional == "50" &&
		limits.MaximumOpenPerAccount == 1 && limits.MaximumOpenGlobal == 2
}

// Process evaluates one canonical market event and durably accepts at most
// one resulting entry plan. It intentionally returns no order events because
// a plan approval is not an exchange acknowledgement.
func (pipeline *StrategyPipeline) Process(
	ctx context.Context,
	event replay.Event,
) (StrategyPipelineResult, error) {
	if pipeline == nil || ctx == nil || event.Ordinal == 0 ||
		event.LogicalTime == 0 || len(event.Canonical) == 0 {
		return StrategyPipelineResult{}, contractError("strategy_pipeline_event_invalid")
	}
	candidate, err := pipeline.strategy.Evaluate(ctx, event)
	if err != nil || candidate.Ordinal != event.Ordinal {
		return StrategyPipelineResult{}, contractError("strategy_pipeline_strategy_failed")
	}
	decision, err := pipeline.decisionSource.DecisionEvidence(event)
	if err != nil || !json.Valid(decision) {
		return StrategyPipelineResult{}, contractError("strategy_pipeline_decision_evidence_failed")
	}
	allocated, err := pipeline.allocator.Allocate(ctx, candidate)
	if err != nil || allocated.Ordinal != event.Ordinal {
		return StrategyPipelineResult{}, contractError("strategy_pipeline_allocation_failed")
	}
	approved, err := pipeline.risk.Approve(ctx, allocated)
	if err != nil {
		return StrategyPipelineResult{}, pipeline.close(allocated, backtest.AllocationReleased,
			"strategy_pipeline_risk_failed")
	}
	plan, err := pipeline.planner.Plan(ctx, approved)
	if err != nil {
		return StrategyPipelineResult{}, pipeline.close(allocated, backtest.AllocationReleased,
			"strategy_pipeline_planning_failed")
	}
	material := StrategyPipelineMaterial{Event: event, DecisionEvidence: append([]byte(nil), decision...), Candidate: candidate,
		Allocated: allocated, Approved: approved, Plan: plan}
	sandboxPlan, err := pipeline.builder.BuildStrategyPlan(ctx, material)
	if err != nil {
		return StrategyPipelineResult{}, pipeline.close(allocated, backtest.AllocationReleased,
			"strategy_pipeline_sandbox_plan_failed")
	}
	if err = pipeline.store.ApprovePlan(ctx, sandboxPlan, pipeline.limits, pipeline.kill); err != nil {
		return StrategyPipelineResult{}, pipeline.close(allocated, backtest.AllocationQuarantined,
			"strategy_pipeline_durable_plan_failed")
	}
	return StrategyPipelineResult{Material: material, Plan: sandboxPlan}, nil
}

func (pipeline *StrategyPipeline) close(
	allocated backtest.AllocatedIntent,
	disposition backtest.AllocationDisposition,
	reason string,
) error {
	closer := pipeline.allocator.(backtest.AllocationCloser)
	if err := closer.CloseAllocation(context.Background(), allocated, disposition); err != nil {
		if disposition == backtest.AllocationReleased &&
			closer.CloseAllocation(context.Background(), allocated, backtest.AllocationQuarantined) == nil {
			return contractError(reason)
		}
		return fmt.Errorf("%s: allocation_cleanup_failed", reason)
	}
	return contractError(reason)
}
