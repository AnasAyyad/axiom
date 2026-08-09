package bootstrap

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/sandbox"
	"axiom/internal/strategies/meanreversion"
	"axiom/internal/strategies/trend"
)

// SandboxStrategySizingFactsSource supplies the complete non-secret account
// and policy projection for one immutable configuration snapshot.
type SandboxStrategySizingFactsSource interface {
	SandboxStrategySizingFacts(
		context.Context,
		sandbox.StrategySessionWork,
		config.Configuration,
		sandbox.StrategySessionAdmission,
		sandbox.StrategySessionExecutionLease,
		time.Time,
	) (SandboxStrategySizingFacts, error)
}

// SandboxStrategyAdmissionReader rechecks the complete live arm, eligibility,
// and safety admission at one decision instant. It deliberately does not
// expose engine switches or adapter credentials to a strategy executor.
type SandboxStrategyAdmissionReader interface {
	SandboxStrategySessionAdmission(
		context.Context,
		sandbox.StrategySessionWork,
		time.Time,
	) (sandbox.StrategySessionAdmission, error)
}

// SandboxStrategyPipelineDependenciesSource supplies the real shared
// allocator/risk/planning/durable-store dependencies before a one-leg strategy
// evaluation starts. The source receives the exact public market input and
// bounded sizing facts that will produce the candidate; it must not fetch a
// later book, infer account capacity, or replace session-owned inventory.
//
// This boundary deliberately runs before Strategy.Evaluate. A dependency
// source may read durable projections while constructing one immutable view,
// but it may never insert a database hop between the strategy, allocator,
// central risk, and execution-planning stages of the hot path.
type SandboxStrategyPipelineDependenciesSource interface {
	SandboxStrategyPipelineDependencies(
		context.Context,
		sandbox.StrategySessionAdmission,
		SandboxStrategySizingFacts,
		sandbox.StrategyMarketInput,
		sandbox.StrategyOwnedInventory,
		backtest.Strategy,
	) (sandbox.StrategyPipelineDependencies, error)
}

// SandboxStrategyDecisionExecutor evaluates the two single-venue pure
// strategies from public data plus projections, then invokes the shared
// allocator/risk/plan pipeline. It has neither an exchange adapter nor a
// dispatcher and cannot submit an order itself.
type SandboxStrategyDecisionExecutor struct {
	market    *sandbox.StrategyMarketInputReader
	projector *SandboxStrategyPositionProjector
	facts     SandboxStrategySizingFactsSource
	risk      sandbox.StrategyRiskObservationProjector
	admission SandboxStrategyAdmissionReader
	inventory sandbox.StrategyOwnedInventorySource
	pipelines SandboxStrategyPipelineDependenciesSource
	decisions sandbox.StrategyDecisionRecorder
}

// NewSandboxStrategyDecisionExecutor constructs an uninstalled, fail-closed
// single-venue evaluator. Every dynamic source is explicit so tests and later
// runtime wiring cannot silently substitute account, policy, or fence facts.
func NewSandboxStrategyDecisionExecutor(
	market sandbox.StrategyMarketData,
	clock domain.Clock,
	projector *SandboxStrategyPositionProjector,
	facts SandboxStrategySizingFactsSource,
	riskProjector sandbox.StrategyRiskObservationProjector,
	admission SandboxStrategyAdmissionReader,
	inventory sandbox.StrategyOwnedInventorySource,
	pipelines SandboxStrategyPipelineDependenciesSource,
	decisions sandbox.StrategyDecisionRecorder,
) (*SandboxStrategyDecisionExecutor, error) {
	reader, err := sandbox.NewStrategyMarketInputReader(market, clock)
	if err != nil || projector == nil || facts == nil || riskProjector == nil || admission == nil || inventory == nil ||
		pipelines == nil || decisions == nil {
		return nil, fmt.Errorf("sandbox_strategy_decision_executor_invalid")
	}
	return &SandboxStrategyDecisionExecutor{market: reader, projector: projector, facts: facts, risk: riskProjector,
		admission: admission, inventory: inventory, pipelines: pipelines, decisions: decisions}, nil
}

// EvaluateStrategySession evaluates one exact, fenced, single-venue session.
// A plan is durable only through StrategyPipeline; rejected/no-plan decisions
// are durably journaled under the same lease for future position projection.
func (executor *SandboxStrategyDecisionExecutor) EvaluateStrategySession(
	ctx context.Context,
	work sandbox.StrategySessionWork,
	record sandbox.StrategySessionConfiguration,
	lease sandbox.StrategySessionExecutionLease,
	now time.Time,
) (sandbox.StrategySessionEvaluation, error) {
	if executor == nil || executor.market == nil || executor.projector == nil || ctx == nil ||
		work.ValidAt(now) != nil || lease.ValidFor(work) != nil || now.IsZero() || now.Location() != time.UTC {
		return sandbox.StrategySessionEvaluation{}, fmt.Errorf("sandbox_strategy_decision_executor_invalid")
	}
	product, err := decodeSandboxStrategyConfiguration(work, record, now)
	if err != nil {
		return sandbox.StrategySessionEvaluation{}, err
	}
	if work.Strategy == sandbox.StrategyTriangular || work.Strategy == sandbox.StrategyCrossExchangeArbitrage {
		return sandbox.NewStrategySessionEvaluation(
			work, sandbox.StrategySessionEvaluationWaiting, "waiting_for_multileg_facts", now,
		)
	}
	inputs, waitingReason, err := executor.singleVenueEvaluationInputs(ctx, work, product, lease, now)
	if err != nil {
		return sandbox.StrategySessionEvaluation{}, err
	}
	if waitingReason != "" {
		return sandbox.NewStrategySessionEvaluation(work,
			sandbox.StrategySessionEvaluationWaiting, waitingReason, now)
	}
	if !executor.strategyRiskProjectionReady(ctx, work, lease, inputs, now) {
		return sandbox.NewStrategySessionEvaluation(work,
			sandbox.StrategySessionEvaluationWaiting, "waiting_for_risk_projection", now)
	}
	switch work.Strategy {
	case sandbox.StrategyTrend:
		return executor.evaluateTrend(ctx, work, inputs.market, product, inputs.admission, inputs.facts, lease, now)
	case sandbox.StrategyMeanReversion:
		return executor.evaluateMeanReversion(ctx, work, inputs.market, product, inputs.admission, inputs.facts, lease, now)
	default:
		return sandbox.StrategySessionEvaluation{}, fmt.Errorf("sandbox_strategy_decision_executor_invalid")
	}
}

type sandboxSingleEvaluationInputs struct {
	market    sandbox.StrategyMarketInput
	admission sandbox.StrategySessionAdmission
	facts     SandboxStrategySizingFacts
}

func (executor *SandboxStrategyDecisionExecutor) singleVenueEvaluationInputs(ctx context.Context,
	work sandbox.StrategySessionWork, product config.Configuration,
	lease sandbox.StrategySessionExecutionLease, now time.Time,
) (sandboxSingleEvaluationInputs, string, error) {
	requirements, err := sandboxStrategyReadinessRequirements(work.Strategy, product)
	if err != nil {
		return sandboxSingleEvaluationInputs{}, "", err
	}
	instrument, err := sandboxStrategyReadinessInstrument(work.Instrument)
	if err != nil {
		return sandboxSingleEvaluationInputs{}, "", err
	}
	market, err := executor.market.ReadAt(
		ctx, instrument, requirements, domain.EventTime{UTC: now, Sequence: 1},
	)
	if err != nil {
		return sandboxSingleEvaluationInputs{}, "waiting_for_public_market_data", nil
	}
	admission, err := executor.admission.SandboxStrategySessionAdmission(ctx, work, now)
	if err != nil || admission.Valid() != nil || admission.Work != work || !admission.ApprovedAt.Equal(now) {
		return sandboxSingleEvaluationInputs{}, "waiting_for_strategy_admission", nil
	}
	facts, err := executor.facts.SandboxStrategySizingFacts(ctx, work, product, admission, lease, now)
	if err != nil || facts.ValidFor(work, now) != nil {
		return sandboxSingleEvaluationInputs{}, "waiting_for_sizing_facts", nil
	}
	triggerHash, err := sandboxStrategyEvaluationTrigger(work, market)
	if err != nil {
		return sandboxSingleEvaluationInputs{}, "waiting_for_public_market_data", nil
	}
	if facts.PriorEvaluationTriggerHash != "" && facts.PriorEvaluationTriggerHash == triggerHash {
		return sandboxSingleEvaluationInputs{}, "waiting_for_next_strategy_trigger", nil
	}
	return sandboxSingleEvaluationInputs{market: market, admission: admission, facts: facts}, "", nil
}

func (executor *SandboxStrategyDecisionExecutor) strategyRiskProjectionReady(ctx context.Context,
	work sandbox.StrategySessionWork, lease sandbox.StrategySessionExecutionLease,
	inputs sandboxSingleEvaluationInputs, now time.Time,
) bool {
	observation, err := executor.risk.ProjectStrategyRiskObservation(
		ctx, lease, inputs.admission, inputs.facts.AccountSnapshot, inputs.market,
		inputs.facts.CentralRiskFacts, now,
	)
	return err == nil && observation.ValidFor(work, inputs.facts.AccountSnapshot, inputs.market,
		inputs.facts.CentralRiskFacts, now) == nil
}

func (executor *SandboxStrategyDecisionExecutor) evaluateTrend(
	ctx context.Context,
	work sandbox.StrategySessionWork,
	market sandbox.StrategyMarketInput,
	product config.Configuration,
	admission sandbox.StrategySessionAdmission,
	facts SandboxStrategySizingFacts,
	lease sandbox.StrategySessionExecutionLease,
	now time.Time,
) (sandbox.StrategySessionEvaluation, error) {
	configuration, err := trend.NewConfiguration(product.Trend)
	if err != nil {
		return sandbox.StrategySessionEvaluation{}, fmt.Errorf("sandbox_strategy_decision_executor_invalid")
	}
	position, err := executor.projector.TrendPosition(ctx, work, configuration, now)
	if err != nil {
		return sandbox.NewStrategySessionEvaluation(work, sandbox.StrategySessionEvaluationWaiting, "waiting_for_position_projection", now)
	}
	input, err := BuildTrendInput(work, configuration, market, position, facts, now)
	if err != nil {
		return sandbox.NewStrategySessionEvaluation(work, sandbox.StrategySessionEvaluationBlocked, "strategy_input_invalid", now)
	}
	evaluator, err := trend.NewEvaluator(configuration)
	if err != nil {
		return sandbox.StrategySessionEvaluation{}, fmt.Errorf("sandbox_strategy_decision_executor_invalid")
	}
	adapter, err := trend.NewAdapter(evaluator)
	if err != nil {
		return sandbox.StrategySessionEvaluation{}, fmt.Errorf("sandbox_strategy_decision_executor_invalid")
	}
	dependencies, inventory, evaluation, err := executor.pipelineDependencies(
		ctx, work, admission, market.Instrument.Base, market, facts, adapter, now,
	)
	if err != nil {
		return evaluation, nil
	}
	return executor.processDecision(ctx, work, admission, market.Instrument.Base, market, facts,
		adapter, adapter, dependencies, inventory, input.Ordinal, input.LogicalTime, input, lease, now)
}

func (executor *SandboxStrategyDecisionExecutor) evaluateMeanReversion(
	ctx context.Context,
	work sandbox.StrategySessionWork,
	market sandbox.StrategyMarketInput,
	product config.Configuration,
	admission sandbox.StrategySessionAdmission,
	facts SandboxStrategySizingFacts,
	lease sandbox.StrategySessionExecutionLease,
	now time.Time,
) (sandbox.StrategySessionEvaluation, error) {
	configuration, err := meanreversion.NewConfiguration(product.MeanReversion)
	if err != nil {
		return sandbox.StrategySessionEvaluation{}, fmt.Errorf("sandbox_strategy_decision_executor_invalid")
	}
	position, err := executor.projector.MeanReversionPosition(ctx, work, configuration, now)
	if err != nil {
		return sandbox.NewStrategySessionEvaluation(work, sandbox.StrategySessionEvaluationWaiting, "waiting_for_position_projection", now)
	}
	input, err := BuildMeanReversionInput(work, configuration, market, position, facts, now)
	if err != nil {
		return sandbox.NewStrategySessionEvaluation(work, sandbox.StrategySessionEvaluationBlocked, "strategy_input_invalid", now)
	}
	evaluator, err := meanreversion.NewEvaluator(configuration)
	if err != nil {
		return sandbox.StrategySessionEvaluation{}, fmt.Errorf("sandbox_strategy_decision_executor_invalid")
	}
	adapter, err := meanreversion.NewAdapter(evaluator)
	if err != nil {
		return sandbox.StrategySessionEvaluation{}, fmt.Errorf("sandbox_strategy_decision_executor_invalid")
	}
	dependencies, inventory, evaluation, err := executor.pipelineDependencies(
		ctx, work, admission, market.Instrument.Base, market, facts, adapter, now,
	)
	if err != nil {
		return evaluation, nil
	}
	return executor.processDecision(ctx, work, admission, market.Instrument.Base, market, facts,
		adapter, adapter, dependencies, inventory, input.Ordinal, input.LogicalTime, input, lease, now)
}

// pipelineDependencies snapshots strategy-owned inventory and the shared
// pipeline stages before the evaluator sees a market event. This preserves one
// in-process, immutable handoff from evaluation through durable plan approval.
func (executor *SandboxStrategyDecisionExecutor) pipelineDependencies(
	ctx context.Context,
	work sandbox.StrategySessionWork,
	admission sandbox.StrategySessionAdmission,
	asset domain.AssetSymbol,
	market sandbox.StrategyMarketInput,
	facts SandboxStrategySizingFacts,
	strategy backtest.Strategy,
	now time.Time,
) (sandbox.StrategyPipelineDependencies, sandbox.StrategyOwnedInventory, sandbox.StrategySessionEvaluation, error) {
	inventory, err := executor.inventory.StrategyOwnedInventory(ctx, work, asset, now)
	if err != nil || inventory.ValidFor(admission, asset) != nil {
		evaluation, _ := sandbox.NewStrategySessionEvaluation(
			work, sandbox.StrategySessionEvaluationWaiting, "waiting_for_owned_inventory", now,
		)
		return sandbox.StrategyPipelineDependencies{}, sandbox.StrategyOwnedInventory{}, evaluation,
			fmt.Errorf("sandbox_strategy_inventory_unavailable")
	}
	dependencies, err := executor.pipelines.SandboxStrategyPipelineDependencies(
		ctx, admission, facts, market, inventory, strategy,
	)
	if err != nil {
		evaluation, _ := sandbox.NewStrategySessionEvaluation(
			work, sandbox.StrategySessionEvaluationWaiting, "waiting_for_strategy_pipeline", now,
		)
		return sandbox.StrategyPipelineDependencies{}, sandbox.StrategyOwnedInventory{}, evaluation,
			fmt.Errorf("sandbox_strategy_pipeline_unavailable")
	}
	return dependencies, inventory, sandbox.StrategySessionEvaluation{}, nil
}

var _ sandbox.StrategySessionExecutor = (*SandboxStrategyDecisionExecutor)(nil)
