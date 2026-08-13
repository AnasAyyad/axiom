package bootstrap

import (
	"context"
	"fmt"

	"axiom/internal/risk"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/sandbox"
	"axiom/internal/strategies/crossarb"
	"axiom/internal/strategies/triangular"
)

// SandboxStrategySagaPipelineFactory composes the real shared multi-leg hot
// path from one immutable input. It restores central risk for every decision
// and owns no exchange adapter or credential-bearing object.
type SandboxStrategySagaPipelineFactory struct {
	riskEngines SandboxStrategyRiskEngineSource
	store       sandbox.DispatcherRepository
}

// CrossExchange constructs the complete paired concurrent pipeline. The
// coordinator fence scopes only the input-local atomic claim allocator; peer
// engine ownership remains independently verified by the persistence store.
func (factory *SandboxStrategySagaPipelineFactory) CrossExchange(
	ctx context.Context,
	lease sandbox.StrategySessionExecutionLease,
	input crossarb.Input,
	facts SandboxSagaPlanFacts,
) (*sandbox.SagaPlanPipeline, error) {
	work := facts.Coordinator.Work
	if factory == nil || factory.riskEngines == nil || factory.store == nil || ctx == nil ||
		work.Strategy != sandbox.StrategyCrossExchangeArbitrage ||
		work.Account.Exchange != sandbox.ExchangeBinance || lease.ValidFor(work) != nil ||
		validateSandboxSagaFacts(facts, sandbox.StrategyCrossExchangeArbitrage, 2) != nil ||
		input.Ordinal == 0 || input.LogicalTime == 0 || !input.Now.Equal(facts.Coordinator.ApprovedAt) ||
		input.ConfigurationHash != work.ConfigurationHash ||
		input.ValidateEventBinding(input.Ordinal, input.LogicalTime) != nil {
		return nil, fmt.Errorf("sandbox_cross_exchange_pipeline_unavailable")
	}
	if _, err := input.RecordedRiskInput(); err != nil {
		return nil, fmt.Errorf("sandbox_cross_exchange_pipeline_unavailable")
	}
	engine, err := factory.riskEngines.SandboxStrategyRiskEngine(ctx, input.Now)
	if err != nil || engine == nil || engine.State() != risk.StateNormal {
		return nil, fmt.Errorf("sandbox_cross_exchange_pipeline_unavailable")
	}
	allocator, err := crossarb.NewRecordedSagaAllocator(runtimecore.FencingToken(lease.Fence))
	if err != nil {
		return nil, fmt.Errorf("sandbox_cross_exchange_pipeline_invalid")
	}
	riskAdapter, err := crossarb.NewSagaRiskAdapter(engine, crossarb.RecordedSagaRiskInputs{})
	if err != nil {
		return nil, fmt.Errorf("sandbox_cross_exchange_pipeline_invalid")
	}
	builder, err := NewCrossExchangeSandboxPlanBuilder(facts)
	if err != nil {
		return nil, fmt.Errorf("sandbox_cross_exchange_pipeline_invalid")
	}
	pipeline, err := sandbox.NewSagaPlanPipeline(crossarb.NewSagaStrategyAdapter(), allocator,
		riskAdapter, crossarb.NewSagaPlanner(), builder, factory.store,
		sandboxStrategySubmissionLimits(), sandbox.NoKillPoint{})
	if err != nil {
		return nil, fmt.Errorf("sandbox_cross_exchange_pipeline_invalid")
	}
	return pipeline, nil
}

// NewSandboxStrategySagaPipelineFactory binds shared multileg strategy stages.
func NewSandboxStrategySagaPipelineFactory(
	riskEngines SandboxStrategyRiskEngineSource,
	store sandbox.DispatcherRepository,
) (*SandboxStrategySagaPipelineFactory, error) {
	if riskEngines == nil || store == nil {
		return nil, fmt.Errorf("sandbox_strategy_saga_pipeline_factory_invalid")
	}
	return &SandboxStrategySagaPipelineFactory{riskEngines: riskEngines, store: store}, nil
}

// Triangular constructs the complete automatic sequential pipeline. The
// input-scoped allocator derives its resources from the canonical account and
// market evidence; the durable builder independently rechecks plan facts.
func (factory *SandboxStrategySagaPipelineFactory) Triangular(
	ctx context.Context,
	lease sandbox.StrategySessionExecutionLease,
	input triangular.Input,
	facts SandboxSagaPlanFacts,
) (*sandbox.SagaPlanPipeline, error) {
	work := facts.Coordinator.Work
	if factory == nil || factory.riskEngines == nil || factory.store == nil || ctx == nil ||
		work.Strategy != sandbox.StrategyTriangular || lease.ValidFor(work) != nil ||
		validateSandboxSagaFacts(facts, sandbox.StrategyTriangular, 1) != nil ||
		input.Ordinal == 0 || input.LogicalTime == 0 || !input.Now.Equal(facts.Coordinator.ApprovedAt) ||
		input.Exchange != string(work.Account.Exchange) || input.ConfigurationHash != work.ConfigurationHash ||
		input.ValidateEventBinding(input.Ordinal, input.LogicalTime) != nil {
		return nil, fmt.Errorf("sandbox_triangular_pipeline_unavailable")
	}
	if _, err := input.RecordedRiskInput(); err != nil {
		return nil, fmt.Errorf("sandbox_triangular_pipeline_unavailable")
	}
	engine, err := factory.riskEngines.SandboxStrategyRiskEngine(ctx, input.Now)
	if err != nil || engine == nil || engine.State() != risk.StateNormal {
		return nil, fmt.Errorf("sandbox_triangular_pipeline_unavailable")
	}
	allocator, err := triangular.NewRecordedSagaAllocator(
		"strategy-session-"+string(work.SessionID), runtimecore.FencingToken(lease.Fence),
	)
	if err != nil {
		return nil, fmt.Errorf("sandbox_triangular_pipeline_invalid")
	}
	riskAdapter, err := triangular.NewSagaRiskAdapter(engine, triangular.RecordedSagaRiskInputs{})
	if err != nil {
		return nil, fmt.Errorf("sandbox_triangular_pipeline_invalid")
	}
	builder, err := NewTriangularSandboxPlanBuilder(facts)
	if err != nil {
		return nil, fmt.Errorf("sandbox_triangular_pipeline_invalid")
	}
	pipeline, err := sandbox.NewSagaPlanPipeline(triangular.NewSagaStrategyAdapter(), allocator,
		riskAdapter, triangular.NewSagaPlanner(), builder, factory.store,
		sandboxStrategySubmissionLimits(), sandbox.NoKillPoint{})
	if err != nil {
		return nil, fmt.Errorf("sandbox_triangular_pipeline_invalid")
	}
	return pipeline, nil
}
