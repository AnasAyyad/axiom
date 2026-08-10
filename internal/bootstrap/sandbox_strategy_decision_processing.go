package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/domain"
	"axiom/internal/replay"
	"axiom/internal/sandbox"
)

type sandboxStrategyDecisionEvidenceSource interface {
	DecisionEvidence(replay.Event) (json.RawMessage, error)
}

// sandboxStrategySubmissionLimits repeats the compiled sandbox ceiling used
// by the existing connection-check flow. Automatic strategies cannot supply
// an alternate policy or loosen any of these limits.
func sandboxStrategySubmissionLimits() sandbox.SubmissionLimits {
	return sandbox.SubmissionLimits{MaximumOrderNotional: "10", MaximumDailyNotional: "50",
		MaximumOpenPerAccount: 1, MaximumOpenGlobal: 2}
}

func (executor *SandboxStrategyDecisionExecutor) processDecision(
	ctx context.Context,
	work sandbox.StrategySessionWork,
	admission sandbox.StrategySessionAdmission,
	asset domain.AssetSymbol,
	market sandbox.StrategyMarketInput,
	facts SandboxStrategySizingFacts,
	strategy backtest.Strategy,
	evidenceSource sandboxStrategyDecisionEvidenceSource,
	dependencies sandbox.StrategyPipelineDependencies,
	inventory sandbox.StrategyOwnedInventory,
	ordinal uint64,
	logicalTime uint64,
	input any,
	lease sandbox.StrategySessionExecutionLease,
	now time.Time,
) (sandbox.StrategySessionEvaluation, error) {
	canonical, err := json.Marshal(input)
	if err != nil {
		return sandbox.StrategySessionEvaluation{}, fmt.Errorf("sandbox_strategy_decision_executor_invalid")
	}
	event := replay.Event{Ordinal: ordinal, LogicalTime: logicalTime, Canonical: canonical}
	if inventory.ValidFor(admission, asset) != nil {
		return sandbox.NewStrategySessionEvaluation(work, sandbox.StrategySessionEvaluationWaiting, "waiting_for_owned_inventory", now)
	}
	pipeline, err := sandbox.NewAdmittedSingleVenueStrategyPipeline(
		admission, facts.AccountSnapshot, inventory, strategy, dependencies, sandboxStrategySubmissionLimits(),
	)
	if err != nil {
		return sandbox.NewStrategySessionEvaluation(work, sandbox.StrategySessionEvaluationWaiting, "waiting_for_strategy_pipeline", now)
	}
	if _, err = pipeline.Process(ctx, event); err == nil {
		return sandbox.NewStrategySessionEvaluation(work, sandbox.StrategySessionEvaluationEvaluated, "strategy_plan_approved", now)
	}
	state, reason := sandboxSinglePipelineOutcome(err)
	return executor.recordRejectedDecision(
		ctx, work, admission, event, evidenceSource, lease, state, reason, now,
	)
}

func (executor *SandboxStrategyDecisionExecutor) recordRejectedDecision(
	ctx context.Context,
	work sandbox.StrategySessionWork,
	admission sandbox.StrategySessionAdmission,
	event replay.Event,
	evidenceSource sandboxStrategyDecisionEvidenceSource,
	lease sandbox.StrategySessionExecutionLease,
	state sandbox.StrategySessionEvaluationState,
	reason string,
	now time.Time,
) (sandbox.StrategySessionEvaluation, error) {
	decision, evidenceErr := evidenceSource.DecisionEvidence(event)
	evidence, bindErr := sandbox.NewStrategyDecisionEvidence(admission, event, decision)
	if evidenceErr != nil || bindErr != nil {
		return sandbox.NewStrategySessionEvaluation(work, sandbox.StrategySessionEvaluationBlocked, "strategy_decision_unavailable", now)
	}
	if recordErr := executor.decisions.RecordSandboxStrategyDecision(ctx, lease.Owner, lease.Fence, work, evidence, now); recordErr != nil {
		return sandbox.NewStrategySessionEvaluation(work, sandbox.StrategySessionEvaluationBlocked, "strategy_decision_record_failed", now)
	}
	if evidence.ValidForPlan(sandbox.ApprovedSandboxPlan{SessionID: work.SessionID}) == nil {
		return sandbox.NewStrategySessionEvaluation(work, sandbox.StrategySessionEvaluationBlocked, "strategy_pipeline_blocked", now)
	}
	return sandbox.NewStrategySessionEvaluation(work, state, reason, now)
}

func sandboxSinglePipelineOutcome(err error) (sandbox.StrategySessionEvaluationState, string) {
	if err == nil {
		return sandbox.StrategySessionEvaluationBlocked, "strategy_pipeline_blocked"
	}
	value := err.Error()
	switch {
	case strings.HasSuffix(value, "strategy_stage_failed"):
		return sandbox.StrategySessionEvaluationEvaluated, "strategy_candidate_rejected"
	case strings.HasSuffix(value, "allocation_stage_failed"):
		return sandbox.StrategySessionEvaluationEvaluated, "strategy_allocation_rejected"
	case strings.HasSuffix(value, "risk_stage_failed"):
		return sandbox.StrategySessionEvaluationEvaluated, "central_risk_rejected"
	case strings.HasSuffix(value, "planning_stage_failed"):
		return sandbox.StrategySessionEvaluationBlocked, "strategy_planning_failed"
	case strings.HasSuffix(value, "simulation_stage_failed"), strings.HasSuffix(value, "durable_stage_failed"):
		return sandbox.StrategySessionEvaluationBlocked, "strategy_plan_persistence_uncertain"
	default:
		return sandbox.StrategySessionEvaluationBlocked, "strategy_pipeline_blocked"
	}
}
