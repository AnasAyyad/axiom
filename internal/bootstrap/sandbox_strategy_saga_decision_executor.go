package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"axiom/internal/config"
	"axiom/internal/replay"
	"axiom/internal/sandbox"
	"axiom/internal/strategies/crossarb"
	"axiom/internal/strategies/triangular"
)

// SandboxStrategySagaFactsSource loads durable facts for one multileg evaluation.
type SandboxStrategySagaFactsSource interface {
	SandboxStrategySagaPlanFacts(
		context.Context,
		sandbox.StrategySessionWork,
		sandbox.StrategySessionExecutionLease,
		time.Time,
	) (SandboxSagaPlanFacts, error)
}

// SandboxStrategySagaMarketReader supplies coherent public market generations.
type SandboxStrategySagaMarketReader interface {
	ReadTriangular(
		context.Context,
		sandbox.StrategySessionWork,
		time.Time,
	) (SandboxTriangularMarketInput, error)
	ReadCrossExchange(
		context.Context,
		sandbox.StrategySessionWork,
		time.Time,
	) (SandboxCrossExchangeMarketInput, error)
}

// SandboxStrategySagaPipelineSource builds shared strategy saga pipelines.
type SandboxStrategySagaPipelineSource interface {
	Triangular(
		context.Context,
		sandbox.StrategySessionExecutionLease,
		triangular.Input,
		SandboxSagaPlanFacts,
	) (*sandbox.SagaPlanPipeline, error)
	CrossExchange(
		context.Context,
		sandbox.StrategySessionExecutionLease,
		crossarb.Input,
		SandboxSagaPlanFacts,
	) (*sandbox.SagaPlanPipeline, error)
}

// SandboxStrategySagaDecisionExecutor evaluates credential-free multi-leg
// inputs and persists only through the shared saga pipeline. It never receives
// a private adapter, peer fence, or direct order-submission capability.
type SandboxStrategySagaDecisionExecutor struct {
	facts     SandboxStrategySagaFactsSource
	market    SandboxStrategySagaMarketReader
	risk      sandbox.StrategySagaRiskObservationProjector
	pipelines SandboxStrategySagaPipelineSource
	decisions sandbox.StrategyDecisionRecorder
}

// NewSandboxStrategySagaDecisionExecutor binds facts, markets, and saga pipelines.
func NewSandboxStrategySagaDecisionExecutor(
	facts SandboxStrategySagaFactsSource,
	market SandboxStrategySagaMarketReader,
	riskProjector sandbox.StrategySagaRiskObservationProjector,
	pipelines SandboxStrategySagaPipelineSource,
	decisions sandbox.StrategyDecisionRecorder,
) (*SandboxStrategySagaDecisionExecutor, error) {
	if facts == nil || market == nil || riskProjector == nil || pipelines == nil || decisions == nil {
		return nil, fmt.Errorf("sandbox_strategy_saga_decision_executor_invalid")
	}
	return &SandboxStrategySagaDecisionExecutor{facts: facts, market: market,
		risk: riskProjector, pipelines: pipelines, decisions: decisions}, nil
}

// EvaluateStrategySession evaluates one multileg sandbox strategy session.
func (executor *SandboxStrategySagaDecisionExecutor) EvaluateStrategySession(
	ctx context.Context,
	work sandbox.StrategySessionWork,
	record sandbox.StrategySessionConfiguration,
	lease sandbox.StrategySessionExecutionLease,
	now time.Time,
) (sandbox.StrategySessionEvaluation, error) {
	if executor == nil || executor.facts == nil || executor.market == nil || executor.risk == nil ||
		executor.pipelines == nil || executor.decisions == nil || ctx == nil ||
		work.ValidAt(now) != nil || lease.ValidFor(work) != nil || now.IsZero() || now.Location() != time.UTC {
		return sandbox.StrategySessionEvaluation{}, fmt.Errorf("sandbox_strategy_saga_decision_executor_invalid")
	}
	product, err := decodeSandboxStrategyConfiguration(work, record, now)
	if err != nil {
		return sandbox.StrategySessionEvaluation{}, err
	}
	if work.Strategy == sandbox.StrategyCrossExchangeArbitrage &&
		work.Account.Exchange == sandbox.ExchangeBybit {
		return sandbox.NewStrategySessionEvaluation(work,
			sandbox.StrategySessionEvaluationWaiting, "waiting_for_binance_coordinator", now)
	}
	if work.Strategy == sandbox.StrategyCrossExchangeArbitrage {
		return executor.evaluateCrossExchange(ctx, work, lease, product, now)
	}
	if work.Strategy != sandbox.StrategyTriangular {
		return sandbox.StrategySessionEvaluation{}, fmt.Errorf("sandbox_strategy_saga_decision_executor_invalid")
	}
	return executor.evaluateTriangular(ctx, work, lease, product, now)
}

func (executor *SandboxStrategySagaDecisionExecutor) evaluateTriangular(ctx context.Context,
	work sandbox.StrategySessionWork, lease sandbox.StrategySessionExecutionLease,
	product config.Configuration, now time.Time,
) (sandbox.StrategySessionEvaluation, error) {
	facts, err := executor.facts.SandboxStrategySagaPlanFacts(ctx, work, lease, now)
	if err != nil {
		return sandbox.NewStrategySessionEvaluation(work,
			sandbox.StrategySessionEvaluationWaiting, "waiting_for_multileg_facts", now)
	}
	market, err := executor.market.ReadTriangular(ctx, work, now)
	if err != nil {
		return sandbox.NewStrategySessionEvaluation(work,
			sandbox.StrategySessionEvaluationWaiting, "waiting_for_synchronized_books", now)
	}
	riskMarket, err := market.RiskMarket(work)
	if err != nil {
		return sandbox.NewStrategySessionEvaluation(work,
			sandbox.StrategySessionEvaluationWaiting, "waiting_for_synchronized_books", now)
	}
	projection := sandbox.StrategySagaRiskProjectionMember{Admission: facts.Coordinator,
		Snapshot: facts.Snapshots[work.Account.ID], Market: riskMarket,
		Facts: facts.RiskFacts[work.Account.ID]}
	riskInputs, err := executor.risk.ProjectStrategySagaRiskInputs(ctx, lease, work,
		[]sandbox.StrategySagaRiskProjectionMember{projection}, now)
	if err != nil || riskInputs == nil {
		return sandbox.NewStrategySessionEvaluation(work,
			sandbox.StrategySessionEvaluationWaiting, "waiting_for_multileg_risk", now)
	}
	input, err := BuildTriangularSandboxInput(work, product, market, facts, riskInputs)
	if err != nil {
		return sandbox.NewStrategySessionEvaluation(work,
			sandbox.StrategySessionEvaluationWaiting, "waiting_for_multileg_capital", now)
	}
	pipeline, err := executor.pipelines.Triangular(ctx, lease, input, facts)
	if err != nil || pipeline == nil {
		return sandbox.NewStrategySessionEvaluation(work,
			sandbox.StrategySessionEvaluationWaiting, "waiting_for_multileg_pipeline", now)
	}
	return executor.processPipeline(ctx, work, lease, facts.Coordinator,
		input.Ordinal, input.LogicalTime, input, pipeline, now)
}

func (executor *SandboxStrategySagaDecisionExecutor) evaluateCrossExchange(
	ctx context.Context,
	work sandbox.StrategySessionWork,
	lease sandbox.StrategySessionExecutionLease,
	product config.Configuration,
	now time.Time,
) (sandbox.StrategySessionEvaluation, error) {
	facts, err := executor.facts.SandboxStrategySagaPlanFacts(ctx, work, lease, now)
	if err != nil {
		return sandbox.NewStrategySessionEvaluation(work,
			sandbox.StrategySessionEvaluationWaiting, "waiting_for_multileg_facts", now)
	}
	market, err := executor.market.ReadCrossExchange(ctx, work, now)
	if err != nil {
		return sandbox.NewStrategySessionEvaluation(work,
			sandbox.StrategySessionEvaluationWaiting, "waiting_for_synchronized_books", now)
	}
	projections, waitingReason := crossExchangeRiskProjections(market, facts)
	if waitingReason != "" {
		return sandbox.NewStrategySessionEvaluation(work,
			sandbox.StrategySessionEvaluationWaiting, waitingReason, now)
	}
	riskInputs, err := executor.risk.ProjectStrategySagaRiskInputs(ctx, lease, work, projections, now)
	if err != nil || riskInputs == nil {
		return sandbox.NewStrategySessionEvaluation(work,
			sandbox.StrategySessionEvaluationWaiting, "waiting_for_multileg_risk", now)
	}
	input, err := BuildCrossExchangeSandboxInput(work, product, market, facts, riskInputs)
	if err != nil {
		return sandbox.NewStrategySessionEvaluation(work,
			sandbox.StrategySessionEvaluationWaiting, "waiting_for_multileg_capital", now)
	}
	pipeline, err := executor.pipelines.CrossExchange(ctx, lease, input, facts)
	if err != nil || pipeline == nil {
		return sandbox.NewStrategySessionEvaluation(work,
			sandbox.StrategySessionEvaluationWaiting, "waiting_for_multileg_pipeline", now)
	}
	return executor.processPipeline(ctx, work, lease, facts.Coordinator,
		input.Ordinal, input.LogicalTime, input, pipeline, now)
}

func crossExchangeRiskProjections(market SandboxCrossExchangeMarketInput,
	facts SandboxSagaPlanFacts,
) ([]sandbox.StrategySagaRiskProjectionMember, string) {
	projections := make([]sandbox.StrategySagaRiskProjectionMember, 0, 2)
	for _, exchange := range []sandbox.Exchange{sandbox.ExchangeBinance, sandbox.ExchangeBybit} {
		admission, exists := facts.Admissions[exchange]
		if !exists {
			return nil, "waiting_for_multileg_facts"
		}
		riskMarket, err := market.RiskMarket(admission.Work)
		if err != nil {
			return nil, "waiting_for_synchronized_books"
		}
		projections = append(projections, sandbox.StrategySagaRiskProjectionMember{Admission: admission,
			Snapshot: facts.Snapshots[admission.Work.Account.ID], Market: riskMarket,
			Facts: facts.RiskFacts[admission.Work.Account.ID]})
	}
	return projections, ""
}

func (executor *SandboxStrategySagaDecisionExecutor) processPipeline(
	ctx context.Context,
	work sandbox.StrategySessionWork,
	lease sandbox.StrategySessionExecutionLease,
	admission sandbox.StrategySessionAdmission,
	ordinal uint64,
	logicalTime uint64,
	input any,
	pipeline *sandbox.SagaPlanPipeline,
	now time.Time,
) (sandbox.StrategySessionEvaluation, error) {
	canonical, err := json.Marshal(input)
	if err != nil {
		return sandbox.NewStrategySessionEvaluation(work,
			sandbox.StrategySessionEvaluationBlocked, "multileg_input_invalid", now)
	}
	event := replay.Event{Ordinal: ordinal, LogicalTime: logicalTime, Canonical: canonical}
	result, processErr := pipeline.Process(ctx, event)
	if processErr == nil && result.SandboxPlan.ID != "" {
		return sandbox.NewStrategySessionEvaluation(work,
			sandbox.StrategySessionEvaluationEvaluated, "strategy_plan_approved", now)
	}
	state, reason, recordDecision := sandboxSagaPipelineOutcome(processErr)
	if recordDecision {
		if err = executor.recordNoPlan(ctx, lease, admission, event, reason, now); err != nil {
			return sandbox.NewStrategySessionEvaluation(work,
				sandbox.StrategySessionEvaluationBlocked, "strategy_decision_record_failed", now)
		}
	}
	return sandbox.NewStrategySessionEvaluation(work, state, reason, now)
}

func sandboxSagaPipelineOutcome(
	err error,
) (sandbox.StrategySessionEvaluationState, string, bool) {
	if err == nil {
		return sandbox.StrategySessionEvaluationBlocked, "multileg_pipeline_invalid", false
	}
	value := err.Error()
	switch {
	case strings.HasSuffix(value, "strategy_saga_pipeline_strategy_failed"):
		return sandbox.StrategySessionEvaluationEvaluated, "strategy_candidate_rejected", true
	case strings.HasSuffix(value, "strategy_saga_pipeline_allocation_failed"):
		return sandbox.StrategySessionEvaluationEvaluated, "strategy_allocation_rejected", true
	case strings.HasSuffix(value, "strategy_saga_pipeline_risk_failed"):
		return sandbox.StrategySessionEvaluationEvaluated, "central_risk_rejected", true
	case strings.HasSuffix(value, "strategy_saga_pipeline_planning_failed"):
		return sandbox.StrategySessionEvaluationBlocked, "strategy_planning_failed", false
	case strings.HasSuffix(value, "strategy_saga_pipeline_projection_failed"):
		return sandbox.StrategySessionEvaluationBlocked, "strategy_plan_projection_failed", false
	case strings.HasSuffix(value, "strategy_saga_pipeline_durable_plan_failed"):
		return sandbox.StrategySessionEvaluationBlocked, "strategy_plan_persistence_uncertain", false
	default:
		return sandbox.StrategySessionEvaluationBlocked, "multileg_pipeline_failed", false
	}
}

func (executor *SandboxStrategySagaDecisionExecutor) recordNoPlan(
	ctx context.Context,
	lease sandbox.StrategySessionExecutionLease,
	admission sandbox.StrategySessionAdmission,
	event replay.Event,
	reason string,
	now time.Time,
) error {
	digest := sha256.Sum256(append(append([]byte(nil), event.Canonical...), []byte("\x00"+reason)...))
	decision, err := json.Marshal(struct {
		ID         string `json:"id"`
		Ordinal    uint64 `json:"ordinal"`
		Action     string `json:"action"`
		ReasonCode string `json:"reason_code"`
	}{ID: "sandbox-saga-decision-" + hex.EncodeToString(digest[:16]),
		Ordinal: event.Ordinal, Action: "none", ReasonCode: reason})
	if err != nil {
		return fmt.Errorf("sandbox_strategy_saga_decision_invalid")
	}
	evidence, err := sandbox.NewStrategyDecisionEvidence(admission, event, decision)
	if err != nil {
		return fmt.Errorf("sandbox_strategy_saga_decision_invalid")
	}
	return executor.decisions.RecordSandboxStrategyDecision(ctx, lease.Owner, lease.Fence,
		admission.Work, evidence, now)
}

var _ sandbox.StrategySessionExecutor = (*SandboxStrategySagaDecisionExecutor)(nil)
