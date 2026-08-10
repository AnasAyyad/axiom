package sandbox

import (
	"context"

	"axiom/internal/backtest"
	"axiom/internal/replay"
)

// SagaSandboxPlanBuilder is the only strategy-specific bridge allowed to
// interpret an opaque centrally approved multi-leg plan. It may materialize
// durable submissions and reservations, but it has no exchange adapter.
type SagaSandboxPlanBuilder interface {
	BuildSandboxSagaPlan(context.Context, backtest.SagaPlan) (ApprovedSandboxPlan, error)
}

// SagaPlanPipelineResult preserves every immutable shared-stage result and
// the exact durable sandbox plan. External acknowledgements and fills arrive
// later through the fenced dispatcher/private-event reducer.
type SagaPlanPipelineResult struct {
	Candidate   backtest.SagaCandidate
	Allocation  backtest.SagaAllocation
	Approval    backtest.SagaApproval
	Plan        backtest.SagaPlan
	SandboxPlan ApprovedSandboxPlan
}

// SagaPlanPipeline composes strategy, atomic allocation, central risk,
// planning, strategy-owned projection, and durable acceptance for automatic
// multi-leg sandbox work. It intentionally has no synchronous broker: the
// separately credential-owning exchange engines claim their own outbox legs.
type SagaPlanPipeline struct {
	strategy  backtest.SagaStrategy
	allocator backtest.SagaAllocator
	risk      backtest.SagaRiskEngine
	planner   backtest.SagaPlanner
	builder   SagaSandboxPlanBuilder
	store     DispatcherRepository
	limits    SubmissionLimits
	kill      KillPoint
}

// NewSagaPlanPipeline fails closed unless every shared stage and the fixed
// 10/50/1/2 sandbox capacity policy are installed.
func NewSagaPlanPipeline(
	strategy backtest.SagaStrategy,
	allocator backtest.SagaAllocator,
	riskEngine backtest.SagaRiskEngine,
	planner backtest.SagaPlanner,
	builder SagaSandboxPlanBuilder,
	store DispatcherRepository,
	limits SubmissionLimits,
	kill KillPoint,
) (*SagaPlanPipeline, error) {
	if strategy == nil || allocator == nil || riskEngine == nil || planner == nil ||
		builder == nil || store == nil || !validStrategyPipelineLimits(limits) {
		return nil, contractError("strategy_saga_pipeline_invalid")
	}
	if kill == nil {
		kill = NoKillPoint{}
	}
	return &SagaPlanPipeline{strategy: strategy, allocator: allocator, risk: riskEngine,
		planner: planner, builder: builder, store: store, limits: limits, kill: kill}, nil
}

// Process evaluates and durably accepts one exact multi-leg decision. Known
// pre-persistence failures release the atomic allocation; an uncertain durable
// acceptance failure quarantines it for recovery.
func (pipeline *SagaPlanPipeline) Process(
	ctx context.Context,
	event replay.Event,
) (SagaPlanPipelineResult, error) {
	if pipeline == nil || ctx == nil || event.Ordinal == 0 || event.LogicalTime == 0 ||
		len(event.Canonical) == 0 {
		return SagaPlanPipelineResult{}, contractError("strategy_saga_pipeline_event_invalid")
	}
	candidate, err := pipeline.strategy.EvaluateSaga(ctx, event)
	if err != nil || candidate.Ordinal != event.Ordinal {
		return SagaPlanPipelineResult{}, contractError("strategy_saga_pipeline_strategy_failed")
	}
	allocation, err := pipeline.allocator.AllocateSaga(ctx, candidate)
	if err != nil || allocation.Ordinal != event.Ordinal {
		return SagaPlanPipelineResult{}, contractError("strategy_saga_pipeline_allocation_failed")
	}
	approval, err := pipeline.risk.ApproveSaga(ctx, allocation)
	if err != nil || approval.Ordinal != event.Ordinal {
		return SagaPlanPipelineResult{}, pipeline.close(
			ctx, allocation, backtest.AllocationReleased, "strategy_saga_pipeline_risk_failed",
		)
	}
	plan, err := pipeline.planner.PlanSaga(ctx, approval)
	if err != nil || plan.Ordinal != event.Ordinal {
		return SagaPlanPipelineResult{}, pipeline.close(
			ctx, allocation, backtest.AllocationReleased, "strategy_saga_pipeline_planning_failed",
		)
	}
	sandboxPlan, err := pipeline.builder.BuildSandboxSagaPlan(ctx, plan)
	if err != nil {
		return SagaPlanPipelineResult{}, pipeline.close(
			ctx, allocation, backtest.AllocationReleased, "strategy_saga_pipeline_projection_failed",
		)
	}
	if err = pipeline.store.ApprovePlan(ctx, sandboxPlan, pipeline.limits, pipeline.kill); err != nil {
		return SagaPlanPipelineResult{}, pipeline.close(
			ctx, allocation, backtest.AllocationQuarantined, "strategy_saga_pipeline_durable_plan_failed",
		)
	}
	return SagaPlanPipelineResult{Candidate: candidate, Allocation: allocation,
		Approval: approval, Plan: plan, SandboxPlan: sandboxPlan}, nil
}

func (pipeline *SagaPlanPipeline) close(
	ctx context.Context,
	allocation backtest.SagaAllocation,
	disposition backtest.AllocationDisposition,
	reason string,
) error {
	if err := pipeline.allocator.CloseSagaAllocation(ctx, allocation, disposition); err != nil {
		if disposition == backtest.AllocationReleased &&
			pipeline.allocator.CloseSagaAllocation(ctx, allocation, backtest.AllocationQuarantined) == nil {
			return contractError(reason)
		}
		return contractError("strategy_saga_pipeline_allocation_cleanup_failed")
	}
	return contractError(reason)
}
